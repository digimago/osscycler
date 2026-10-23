package tui

import (
	"math"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The human touches in the road view, kept sparse: the roads that meet the
// route where it changes roads or crosses a major road, the way into a car
// park, the car parks themselves with a few cars, and place-name signs.

var (
	lotA      = rgb{0x9a, 0x94, 0x84} // gravel and paving
	signBlue  = rgb{0x1f, 0x4e, 0x9c}
	signWhite = rgb{0xf0, 0xf0, 0xf0}
	postGrey  = rgb{0x8a, 0x8e, 0x92}
	carColors = []rgb{{0xb0, 0x2a, 0x2a}, {0xe0, 0xe0, 0xe0}, {0x30, 0x34, 0x3a}, {0x2c, 0x50, 0x8c}, {0x9a, 0x9e, 0xa4}, {0x5a, 0x6a, 0x3a}}
)

// placeShowM: the info line names the place this far past its sign.
const placeShowM = 500.0

// branchDrawM is as far as roads meeting the route are drawn: further out
// the view's bands across the road no longer keep them straight.
const branchDrawM = 40.0

// worldBranch is a road leaving a junction, in the scene's metres: its
// centre line from the route's road, at most branchDrawM long.
type worldBranch struct {
	d    float64 // the junction's distance along the course
	pts  [][2]float64
	half float64
}

// branchesOf places a course's junction branches along their own lines
// (owner, 2026-10-22: in town, drawn straight along their bearing from the
// junction, side roads ran through houses where they bend, and started
// out of nowhere where the junction had been merged from nodes metres
// apart), joined to the scene's smoothed road nearest their start; along
// the bearing from the junction where the core sends no line (an older
// core).
func (sc *roadScene) branchesOf(js []*pb.Junction) []worldBranch {
	var out []worldBranch
	for _, j := range js {
		x, y, _ := sc.at(j.GetDistanceM())
		for _, b := range j.GetBranches() {
			var pts [][2]float64
			if e, n := b.GetLineEastM(), b.GetLineNorthM(); len(e) >= 2 && len(n) == len(e) {
				first := [2]float64{float64(e[0]), float64(n[0])}
				if r := sc.nearestRoad(first, j.GetDistanceM()); math.Hypot(r[0]-first[0], r[1]-first[1]) > 0.3 {
					pts = append(pts, r)
				}
				for i := range e {
					pts = append(pts, [2]float64{float64(e[i]), float64(n[i])})
				}
			} else {
				rad := b.GetBearingDeg() * math.Pi / 180
				l := b.GetLengthM()
				pts = [][2]float64{{x, y}, {x + math.Sin(rad)*l, y + math.Cos(rad)*l}}
			}
			out = append(out, worldBranch{d: j.GetDistanceM(), pts: clipLine(pts, branchDrawM), half: math.Max(b.GetWidthM(), 3) / 2})
		}
	}
	return out
}

// nearestRoad is the scene's road point nearest p within branchJoinM of
// distance d along it.
func (sc *roadScene) nearestRoad(p [2]float64, d float64) [2]float64 {
	best, at := math.Inf(1), [2]float64{}
	for s := d - branchJoinM; s <= d+branchJoinM; s++ {
		x, y, _ := sc.at(s)
		if dd := math.Hypot(x-p[0], y-p[1]); dd < best {
			best, at = dd, [2]float64{x, y}
		}
	}
	return at
}

// branchJoinM: a branch's line is joined to the road this far either way
// of its junction (a merged junction's branches start up to mergeM, 25 m,
// from it).
const branchJoinM = 30.0

// clipLine is line pts cut off length metres along it.
func clipLine(pts [][2]float64, length float64) [][2]float64 {
	out := [][2]float64{pts[0]}
	run := 0.0
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		if run+l >= length {
			f := (length - run) / math.Max(l, 1e-9)
			return append(out, [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])})
		}
		run += l
		out = append(out, b)
	}
	return out
}

// sceneExtras are the junctions and car parks near the rider, gathered
// once a frame.
type sceneExtras struct {
	branches []worldBranch
	lots     []*pb.ParkingArea
}

func (sc *roadScene) extras(pos float64) sceneExtras {
	var ex sceneExtras
	lo, hi := pos-120, pos+roadDrawM+120
	for _, b := range sc.branches {
		if b.d > lo && b.d < hi {
			ex.branches = append(ex.branches, b)
		}
	}
	for _, p := range sc.parking {
		if p.GetDistanceM()+p.GetLengthM()/2 > lo && p.GetDistanceM()-p.GetLengthM()/2 < hi {
			ex.lots = append(ex.lots, p)
		}
	}
	return ex
}

// onBranch tells whether the point x, y lies on a branch, widened by
// margin.
func (ex sceneExtras) onBranch(x, y, margin float64) bool {
	for _, b := range ex.branches {
		for i := 1; i < len(b.pts); i++ {
			a, c := b.pts[i-1], b.pts[i]
			dx, dy := c[0]-a[0], c[1]-a[1]
			l := math.Hypot(dx, dy)
			if l < 1e-9 {
				continue
			}
			dx, dy = dx/l, dy/l
			px, py := x-a[0], y-a[1]
			if t := px*dx + py*dy; t < 0 || t > l {
				continue
			}
			if math.Abs(px*dy-py*dx) < b.half+margin {
				return true
			}
		}
	}
	return false
}

// lotAt is the car park at distance s and lateral metres, if any.
func (ex sceneExtras) lotAt(s, lateral, margin float64) *pb.ParkingArea {
	for _, p := range ex.lots {
		if math.Abs(s-p.GetDistanceM()) < p.GetLengthM()/2+margin && math.Abs(lateral-p.GetOffsetM()) < p.GetDepthM()/2+margin {
			return p
		}
	}
	return nil
}

// frameAt is the road's centre at s and the unit vector to its right.
func (sc *roadScene) frameAt(s float64) (cx, cy, rx, ry float64) {
	cx, cy, _ = sc.at(s)
	ax, ay, _ := sc.at(s - 1)
	bx, by, _ := sc.at(s + 1)
	dx, dy := bx-ax, by-ay
	l := math.Hypot(dx, dy)
	if l == 0 {
		return cx, cy, 1, 0
	}
	return cx, cy, dy / l, -dx / l
}

// placeAt is the place the rider entered within the last placeShowM, if
// any.
func (sc *roadScene) placeAt(pos float64) string {
	name := ""
	for _, s := range sc.signs {
		if s.GetDistanceM() <= pos && pos-s.GetDistanceM() < placeShowM {
			name = s.GetName()
		}
	}
	return name
}

// carSpots places a few parked cars in a car park: one per 350 m², two to
// nine, the same ones every frame.
func carSpots(p *pb.ParkingArea) (spots [][2]float64, colors []rgb) {
	l, w := p.GetLengthM(), p.GetDepthM()
	n := max(2, min(9, int(l*w/350)))
	seed := math.Float64bits(p.GetDistanceM()) ^ math.Float64bits(p.GetOffsetM())
	for k := range n {
		h := splitmix(seed + uint64(k)*0x9e37)
		along := p.GetDistanceM() - l/2 + 3 + float64(h%1000)/1000*math.Max(l-6, 0)
		across := p.GetOffsetM() - w/2 + 2.5 + float64(h>>10%1000)/1000*math.Max(w-5, 0)
		spots = append(spots, [2]float64{along, across})
		colors = append(colors, carColors[h>>20%uint64(len(carColors))])
	}
	return spots, colors
}

// drawCar draws a parked car seen from the road: a body and a cabin with
// glass.
func drawCar(cv canvas, cam roadCamera, g roadSeg, off float64, body rgb) {
	scale := cam.f / g.depth
	if 1.5*scale < 1 {
		return
	}
	cx := g.sx + off*scale
	fog := (g.depth - roadFogFromM) / (roadDrawM - roadFogFromM)
	ya, yb := span(g.sy-1.45*scale, math.Min(g.sy, g.clip), cv.h)
	for y := ya; y < yb; y++ {
		up := (g.sy - float64(y) - 0.5) / scale // metres above the ground
		half, c := 2.1, body
		switch {
		case up < 0.25:
			half, c = 1.9, rgb{0x22, 0x22, 0x24} // wheels and shade
		case up > 0.85:
			half, c = 1.3, glassColor
		}
		xa, xb := span(cx-half*scale, cx+half*scale, cv.w)
		for x := xa; x < xb; x++ {
			cv.set(x, y, mix(c, fogColor, fog))
		}
	}
}

// drawSign draws a place-name sign on its post, off metres right of the
// centre line: the Dutch one, a wide blue panel with a line of white
// lettering. (A rim to scale is under a pixel; drawn, it looked absurd.)
func drawSign(cv canvas, cam roadCamera, g roadSeg, off float64) {
	scale := cam.f / g.depth
	if 2.6*scale < 2 {
		return
	}
	cx := g.sx + off*scale
	fog := (g.depth - roadFogFromM) / (roadDrawM - roadFogFromM)
	const panelW, panelH, top = 1.8, 0.6, 2.5
	lettered := panelH*scale >= 5
	ya, yb := span(g.sy-top*scale, math.Min(g.sy, g.clip), cv.h)
	for y := ya; y < yb; y++ {
		up := (g.sy - float64(y) - 0.5) / scale
		if up < top-panelH { // the post
			xa, xb := span(cx-math.Max(0.04*scale, 0.5), cx+math.Max(0.04*scale, 0.5), cv.w)
			for x := xa; x < xb; x++ {
				cv.set(x, y, mix(postGrey, fogColor, fog))
			}
			continue
		}
		v := (up - (top - panelH)) / panelH // 0 at the panel's foot, 1 at its top
		xa, xb := span(cx-panelW/2*scale, cx+panelW/2*scale, cv.w)
		for x := xa; x < xb; x++ {
			u := (float64(x) + 0.5 - (cx - panelW/2*scale)) / (panelW * scale)
			c := signBlue
			// The name, as letters too small to read, once the panel is
			// tall enough to keep blue above and below them.
			if lettered && v > 0.36 && v < 0.64 && u > 0.12 && u < 0.88 && int(u*16)%3 != 2 {
				c = signWhite
			}
			cv.set(x, y, mix(c, fogColor, fog))
		}
	}
}

// drawBranch lays a road meeting the route flat at the junction's
// elevation, from the route's centre out, in perspective; the route's own
// road stays on top, and nearer road (a crest) hides it below clip.
func (sc *roadScene) drawBranch(cv canvas, onRoad []bool, cam roadCamera, b worldBranch, clip float64) {
	_, _, z := sc.at(b.d)
	for i := 1; i < len(b.pts); i++ {
		a, c := b.pts[i-1], b.pts[i]
		l := math.Hypot(c[0]-a[0], c[1]-a[1])
		if l < 1e-9 {
			continue
		}
		dx, dy := (c[0]-a[0])/l, (c[1]-a[1])/l
		// Only the part well in front of the eye: depth is linear along it.
		depth := func(t float64) float64 {
			return (a[0]+dx*t-cam.x)*cam.sinH + (a[1]+dy*t-cam.y)*cam.cosH
		}
		t0, t1 := 0.0, l
		d0, d1 := depth(t0), depth(t1)
		const minDepth = 2.0
		switch {
		case d0 < minDepth && d1 < minDepth:
			continue
		case d0 < minDepth:
			t0 += (minDepth - d0) / (d1 - d0) * (t1 - t0)
		case d1 < minDepth:
			t1 = t0 + (minDepth-d0)/(d1-d0)*(t1-t0)
		}
		px, py := dy*b.half, -dx*b.half // across the branch
		quad := [4][3]float64{
			{a[0] + dx*t0 - px, a[1] + dy*t0 - py, z}, {a[0] + dx*t0 + px, a[1] + dy*t0 + py, z},
			{a[0] + dx*t1 + px, a[1] + dy*t1 + py, z}, {a[0] + dx*t1 - px, a[1] + dy*t1 - py, z},
		}
		fillFlat(cv, onRoad, cam, quad, asphaltA, clip)
	}
}

// drawLot lays a car park flat beside the road, from where it is past
// from on.
func (sc *roadScene) drawLot(cv canvas, onRoad []bool, cam roadCamera, p *pb.ParkingArea, from, clip float64) {
	s0, s1 := math.Max(p.GetDistanceM()-p.GetLengthM()/2, from), p.GetDistanceM()+p.GetLengthM()/2
	if s1 <= s0 {
		return
	}
	cx, cy, rx, ry := sc.frameAt(p.GetDistanceM())
	_, _, z := sc.at(p.GetDistanceM())
	dx, dy := -ry, rx // along the road
	a0, a1 := s0-p.GetDistanceM(), s1-p.GetDistanceM()
	o0, o1 := p.GetOffsetM()-p.GetDepthM()/2, p.GetOffsetM()+p.GetDepthM()/2
	at := func(a, o float64) [3]float64 { return [3]float64{cx + dx*a + rx*o, cy + dy*a + ry*o, z} }
	fillFlat(cv, onRoad, cam, [4][3]float64{at(a0, o0), at(a0, o1), at(a1, o1), at(a1, o0)}, lotA, clip)
}

// fillFlat fills a flat four-sided shape on the ground, in perspective,
// fogged by its distance, leaving the road and anything below clip alone.
func fillFlat(cv canvas, onRoad []bool, cam roadCamera, quad [4][3]float64, c rgb, clip float64) {
	var xs, ys [4]float64
	var depth float64
	for i, q := range quad {
		sx, sy, d, ok := cam.project(q[0], q[1], q[2])
		if !ok {
			return
		}
		xs[i], ys[i], depth = sx, sy, depth+d/4
	}
	c = mix(c, fogColor, (depth-roadFogFromM)/(roadDrawM-roadFogFromM))
	top, bottom := math.Min(math.Min(ys[0], ys[1]), math.Min(ys[2], ys[3])), math.Max(math.Max(ys[0], ys[1]), math.Max(ys[2], ys[3]))
	ya, yb := span(top, math.Min(bottom, clip), cv.h)
	for y := ya; y < yb; y++ {
		fy := float64(y) + 0.5
		lo, hi := math.Inf(1), math.Inf(-1)
		for i := range 4 {
			j := (i + 1) % 4
			if (ys[i] <= fy) == (ys[j] <= fy) {
				continue
			}
			x := xs[i] + (fy-ys[i])/(ys[j]-ys[i])*(xs[j]-xs[i])
			lo, hi = math.Min(lo, x), math.Max(hi, x)
		}
		xa, xb := span(lo, hi, cv.w)
		for x := xa; x < xb; x++ {
			if !onRoad[y*cv.w+x] {
				cv.set(x, y, c)
			}
		}
	}
}
