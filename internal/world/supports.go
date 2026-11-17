package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Under a deck (owner, 2026-11-17, the Erasmusbrug: the piers in the Maas,
// in the Dutch register as buildings, stood through its deck as blocks of
// flats, and a building under its north ramp walled the road off): a
// building stands no higher than the deck over it, and a bridge's supports
// are drawn as the map draws them, up to the deck they carry.
const (
	deckGapM     = 0.5 // a building under a deck keeps this far below its underside
	underDeckMin = 2.5 // lower than this under a deck, a building is left out
	coverStepM   = 2.0 // a footprint is looked at this often across its outline
	// A rider's room over the riding line: a building there blocks the road.
	riderClearM = 2.5
)

// deckCover tells how low the underside of the decks over a point lies
// (+Inf: no deck over it).
type deckCover struct {
	lines []*roadLine
	grid  map[[2]int][][2]int // line, sample of the decks' samples by 10 m cell
}

func newDeckCover(lines []*roadLine) *deckCover {
	c := &deckCover{lines: lines, grid: map[[2]int][][2]int{}}
	for li, l := range lines {
		for i, s := range l.samples {
			if s.aloft {
				k := [2]int{int(math.Floor(s.e / 10)), int(math.Floor(s.n / 10))}
				c.grid[k] = append(c.grid[k], [2]int{li, i})
			}
		}
	}
	return c
}

// under is the lowest underside of a deck over e, n: within the deck's
// edge and its edge beam of one of its samples.
func (c *deckCover) under(e, n float64) float64 {
	low := math.Inf(1)
	k := [2]int{int(math.Floor(e / 10)), int(math.Floor(n / 10))}
	for gx := k[0] - 2; gx <= k[0]+2; gx++ {
		for gy := k[1] - 2; gy <= k[1]+2; gy++ {
			for _, r := range c.grid[[2]int{gx, gy}] {
				s := c.lines[r[0]].samples[r[1]]
				if math.Hypot(s.e-e, s.n-n) <= s.edge+edgeBeamW {
					low = math.Min(low, s.ele-deckDepthM)
				}
			}
		}
	}
	return low
}

// coverPoints are the points a footprint is judged by: its corners and
// every coverStepM inside it.
func coverPoints(poly [][2]float64) [][2]float64 {
	pts := append([][2]float64{}, poly...)
	lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, q := range poly {
		lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[1])}
		hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[1])}
	}
	for x := lo[0] + coverStepM/2; x < hi[0]; x += coverStepM {
		for y := lo[1] + coverStepM/2; y < hi[1]; y += coverStepM {
			if inPoly(poly, x, y) {
				pts = append(pts, [2]float64{x, y})
			}
		}
	}
	return pts
}

// underDecks cuts the buildings under a deck down to deckGapM below its
// underside, flat-roofed, and leaves out those that would be lower than
// underDeckMin.
func underDecks(s *surface, cover *deckCover, fps []scenery.Footprint) []scenery.Footprint {
	out := fps[:0:0]
	for _, f := range fps {
		pts := coverPoints(f.Outline)
		low, gLo, gHi := math.Inf(1), math.Inf(1), math.Inf(-1)
		for _, q := range pts {
			low = math.Min(low, cover.under(q[0], q[1]))
		}
		if math.IsInf(low, 1) {
			out = append(out, f)
			continue
		}
		for _, q := range pts {
			g := s.at(q[0], q[1])
			gLo, gHi = math.Min(gLo, g), math.Max(gHi, g)
		}
		base := gHi // buildingMesh measures a guessed height from the highest ground
		if f.FromLowest {
			base = gLo
		}
		room := low - deckGapM - base
		if room < underDeckMin {
			continue
		}
		if f.WallM+f.RoofM > room {
			f.Kind, f.WallM, f.RoofM = scenery.KindFlat, room, 0
		}
		out = append(out, f)
	}
	return out
}

// supported tells whether a deck (line l's samples i to j) passes over a
// support the map draws: it stands on those, not on piers of its own.
func supported(sups []scenery.Support, l *roadLine, i, j int) bool {
	for _, sp := range sups {
		for k := i; k <= j; k++ {
			if inPoly(sp.Outline, l.samples[k].e, l.samples[k].n) {
				return true
			}
			if nearPoly(sp.Outline, l.samples[k].e, l.samples[k].n, l.samples[k].edge) {
				return true
			}
		}
	}
	return false
}

// nearPoly tells whether e, n lies within r of poly's outline.
func nearPoly(poly [][2]float64, e, n, r float64) bool {
	for k := range poly {
		a, b := poly[k], poly[(k+1)%len(poly)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		t := 0.0
		if l2 := dx*dx + dy*dy; l2 > 0 {
			t = math.Max(0, math.Min(1, ((e-a[0])*dx+(n-a[1])*dy)/l2))
		}
		if math.Hypot(a[0]+t*dx-e, a[1]+t*dy-n) <= r {
			return true
		}
	}
	return false
}

// supportMesh draws a support as the map outlines it: a block from footM
// into the ground (the river's bed, under water) up to the underside of the
// deck over it. One no deck passes over (a bridge not drawn) is left out.
func supportMesh(prim func(e, n float64) *gltf.Primitive, s *surface, cover *deckCover, sp scenery.Support, concrete [3]float32) {
	pts := coverPoints(sp.Outline)
	top, low := math.Inf(1), math.Inf(1)
	for _, q := range pts {
		top = math.Min(top, cover.under(q[0], q[1]))
		low = math.Min(low, s.at(q[0], q[1]))
	}
	if math.IsInf(top, 1) || top <= low {
		return
	}
	var cx, cy float64
	for _, q := range sp.Outline {
		cx, cy = cx+q[0]/float64(len(sp.Outline)), cy+q[1]/float64(len(sp.Outline))
	}
	prism(prim(cx, cy), sp.Outline, low-footM, top, concrete)
}

// prism is a block over a counter-clockwise outline (east, north), from
// low up to top, closed on every side.
func prism(p *gltf.Primitive, poly [][2]float64, low, top float64, c [3]float32) {
	at := func(q [2]float64, y float64) [3]float64 { return [3]float64{q[0], y, -q[1]} }
	for k := range poly {
		a, b := poly[k], poly[(k+1)%len(poly)]
		// Counter-clockwise seen from above: outward is to the right of a→b.
		out := [3]float64{b[1] - a[1], 0, b[0] - a[0]}
		if l := math.Hypot(out[0], out[2]); l > 0 {
			out = scale3(out, 1/l)
		}
		quad(p, at(a, low), at(b, low), at(b, top), at(a, top), out, c)
	}
	for _, t := range earClip(poly) {
		triangle(p, at(poly[t[0]], top), at(poly[t[1]], top), at(poly[t[2]], top), [3]float64{0, 1, 0}, c)
		triangle(p, at(poly[t[0]], low), at(poly[t[2]], low), at(poly[t[1]], low), [3]float64{0, -1, 0}, c)
	}
}

// triangle adds the triangle a b c facing n, in colour col.
func triangle(p *gltf.Primitive, a, b, c, n [3]float64, col [3]float32) {
	base := uint32(len(p.Positions) / 3)
	for _, q := range [][3]float64{a, b, c} {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		p.Colors = append(p.Colors, col[0], col[1], col[2])
	}
	tri(p, base, base+1, base+2, n)
}

// blockedRide is how many metres of the riding line (every checkRideM) a
// building stands on, within a rider's room above the road (riderClearM),
// by stretch of checkSpotM.
func blockedRide(m *Manifest, s *surface, fps []scenery.Footprint) map[int]float64 {
	type ref struct {
		f      *scenery.Footprint
		lo, hi [2]float64
	}
	grid := map[[2]int][]ref{}
	for k := range fps {
		f := &fps[k]
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, q := range f.Outline {
			lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[1])}
			hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[1])}
		}
		for gx := int(math.Floor(lo[0] / 20)); gx <= int(math.Floor(hi[0]/20)); gx++ {
			for gy := int(math.Floor(lo[1] / 20)); gy <= int(math.Floor(hi[1]/20)); gy++ {
				grid[[2]int{gx, gy}] = append(grid[[2]int{gx, gy}], ref{f, lo, hi})
			}
		}
	}
	out := map[int]float64{}
	path, lane, step := m.Path.XYZ, m.Path.LaneM, m.Path.StepM
	n := len(path) / 3
	for i := 0; i+1 < n; i++ {
		ax, ay, az, bx, by, bz := path[3*i], path[3*i+1], path[3*i+2], path[3*i+3], path[3*i+4], path[3*i+5]
		dx, dz := bx-ax, bz-az
		l := math.Hypot(dx, dz)
		if l < 1e-9 {
			continue
		}
		dx, dz = dx/l, dz/l
		for k := checkRideM / 2; k < step; k += checkRideM {
			f := k / step
			off := 0.0
			if len(lane) == n {
				off = lane[i] + f*(lane[i+1]-lane[i])
			}
			e, nn := ax+f*(bx-ax)-dz*off, -(az + f*(bz-az) + dx*off)
			y := ay + f*(by-ay)
			for _, r := range grid[[2]int{int(math.Floor(e / 20)), int(math.Floor(nn / 20))}] {
				if e < r.lo[0] || e > r.hi[0] || nn < r.lo[1] || nn > r.hi[1] || !inPoly(r.f.Outline, e, nn) {
					continue
				}
				g := s.at(e, nn)
				if g < y+riderClearM && g+r.f.WallM+r.f.RoofM > y {
					out[int((float64(i)*step+k)/checkSpotM)] += checkRideM
					break
				}
			}
		}
	}
	return out
}
