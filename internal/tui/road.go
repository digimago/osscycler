package tui

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The road view: a pseudo-3D picture of the road ahead in half-block
// pixels, drawn like the sprite-scaling racing games of the 1980s. Bends
// come from the course's GPX track, hills from its elevation profile, and
// stripes and trees scroll past with the rider's speed.

const (
	roadEyeM     = 1.6   // eye height above the road: a rider on a bike, as the 3D renderer
	roadHalfM    = 2.75  // half the road's width
	roadLineM    = 0.15  // edge and centre line width
	roadDashM    = 3.0   // centre-line dashes, with gaps as long
	roadStripeM  = 5.0   // grass and asphalt shading alternate this often
	roadDrawM    = 400.0 // how far ahead the road is drawn
	roadFogFromM = 60.0  // fog starts here and is full at roadDrawM
	roadSegM     = 2.0   // the road is projected in steps this long
	roadFOVDeg   = 70.0  // horizontal field of view
	roadSmoothM  = 40.0  // the track is smoothed over this span (GPS jitter)
	roadTreeM    = 6.0   // a tree may stand every this many metres
	roadGhostM   = 1.0   // the ghost rides this far right of the centre line
	// roadPitchFollow is how much of the grade the view tilts with. A
	// rider keeps their gaze nearer level than the bike: tilting with all
	// of it made a steady 10 % climb look flat (Mountain Mash's first
	// climb); at 0.15 the road visibly rises ahead.
	roadPitchFollow = 0.15
	roadNearM       = 15.0 // land use is given this far out from the centre line
	roadFarM        = 60.0 // and this far
	// roadFrame paces the animation between state updates.
	roadFrame = 50 * time.Millisecond
)

type rgb struct{ r, g, b uint8 }

var (
	skyTop     = rgb{0x3a, 0x6e, 0xb5}
	skyHorizon = rgb{0xb9, 0xd3, 0xe8}
	fogColor   = rgb{0xb4, 0xc8, 0xd4}
	hillColor  = rgb{0x6f, 0x8f, 0x7e}
	grassA     = rgb{0x5a, 0x9e, 0x3a}
	grassB     = rgb{0x4e, 0x8c, 0x31}
	asphaltA   = rgb{0x6c, 0x6c, 0x70}
	asphaltB   = rgb{0x66, 0x66, 0x6a}
	lineColor  = rgb{0xe8, 0xe8, 0xe8}
	finishDark = rgb{0x20, 0x20, 0x20}
	trunkColor = rgb{0x5b, 0x40, 0x2a}
	leafColor  = rgb{0x2f, 0x6b, 0x2a}
	pineColor  = rgb{0x1f, 0x4f, 0x2f}
	ghostBody  = rgb{0xff, 0x5f, 0xd7}
	ghostBike  = rgb{0x40, 0x30, 0x48}
)

func mix(a, b rgb, t float64) rgb {
	t = math.Max(0, math.Min(t, 1))
	f := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return rgb{f(a.r, b.r), f(a.g, b.g), f(a.b, b.b)}
}

// roadScene is a course prepared for drawing: the track smoothed and in
// metres, with elevation, per profile sample.
type roadScene struct {
	step, finish     float64
	loop             bool // the finish is the start: the road goes round
	east, north, ele []float64
	// From map data, when the core has it: four land uses per sample (left
	// far, left near, right near, right far) and the buildings by distance.
	land        []byte
	buildings   []*pb.Building
	attribution string
	// Roads meeting the route, car parks and place-name signs.
	branches []worldBranch
	parking  []*pb.ParkingArea
	signs    []*pb.PlaceSign
}

// newRoadScene returns nil for a course without a track (an older core).
func newRoadScene(c *pb.Course) *roadScene {
	e, n, ele := c.GetProfileEastM(), c.GetProfileNorthM(), c.GetProfileElevationM()
	step := c.GetProfileStepM()
	if len(e) < 2 || len(n) != len(e) || len(ele) != len(e) || step <= 0 {
		return nil
	}
	k := int(math.Round(roadSmoothM / step / 2))
	sc := &roadScene{step: step, finish: c.GetDistanceM(), loop: c.GetLoop(),
		east: movingAverage(e, k), north: movingAverage(n, k), ele: movingAverage(ele, 0),
		buildings: c.GetBuildings(), attribution: c.GetAttribution()}
	if sc.loop {
		sc.east, sc.north = loopAverage(e, k), loopAverage(n, k)
	}
	if l := c.GetLandUse(); len(l) == 4*len(e) {
		sc.land = l
	}
	sc.branches = sc.branchesOf(c.GetJunctions())
	sc.parking, sc.signs = c.GetParking(), c.GetPlaceSigns()
	return sc
}

// landAt is the land use at distance s and lateral metres from the centre
// line (right positive).
func (sc *roadScene) landAt(s, lateral float64) pb.LandUse {
	if sc.land == nil {
		return pb.LandUse_LAND_USE_UNSPECIFIED
	}
	i := max(0, min(int(math.Round(s/sc.step)), len(sc.land)/4-1))
	near := math.Abs(lateral) < (roadNearM+roadFarM)/2
	var k int
	switch {
	case lateral < 0 && near:
		k = 1
	case lateral < 0:
		k = 0
	case near:
		k = 2
	default:
		k = 3
	}
	return pb.LandUse(sc.land[4*i+k])
}

// movingAverage is centred over 2k+1 samples, narrowing at the ends.
func movingAverage(v []float32, k int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		w := min(k, i, len(v)-1-i)
		var sum float64
		for j := i - w; j <= i+w; j++ {
			sum += float64(v[j])
		}
		out[i] = sum / float64(2*w+1)
	}
	return out
}

// loopAverage is movingAverage round a loop: the last sample is the
// first again, and the average reaches across the line.
func loopAverage(v []float32, k int) []float64 {
	n := len(v) - 1 // distinct samples
	out := make([]float64, len(v))
	for i := range n {
		var sum float64
		for j := i - k; j <= i+k; j++ {
			sum += float64(v[((j%n)+n)%n])
		}
		out[i] = sum / float64(2*k+1)
	}
	out[n] = out[0]
	return out
}

// lapPos is s within the lap on a loop; s itself otherwise.
func (sc *roadScene) lapPos(s float64) float64 {
	if !sc.loop {
		return s
	}
	return math.Mod(math.Mod(s, sc.finish)+sc.finish, sc.finish)
}

// at returns the road's centre at distance s: east, north and elevation.
// Past either end the road runs on straight and level; a loop goes round.
func (sc *roadScene) at(s float64) (x, y, z float64) {
	s = sc.lapPos(s)
	last := len(sc.east) - 1
	pos := s / sc.step
	i := max(0, min(int(math.Floor(pos)), last-1))
	f := pos - float64(i) // beyond [0, 1] past the ends: extrapolates
	fz := math.Max(0, math.Min(f, 1))
	return sc.east[i] + (sc.east[i+1]-sc.east[i])*f,
		sc.north[i] + (sc.north[i+1]-sc.north[i])*f,
		sc.ele[i] + (sc.ele[i+1]-sc.ele[i])*fz
}

// roadCamera sits on the road at the rider's eye, looking along the road
// and pitched with its grade.
type roadCamera struct {
	x, y, z    float64 // east, north, elevation of the eye
	sinH, cosH float64 // heading, clockwise from north
	heading    float64
	pitch      float64
	f          float64 // focal length in pixels
	cx, axisY  float64 // screen position of the view axis
}

func (sc *roadScene) camera(pos float64, w, h int) roadCamera {
	x, y, z := sc.at(pos)
	// Look along the road just ahead, so GPS wobble doesn't shake the view.
	ax, ay, _ := sc.at(pos - 5)
	bx, by, _ := sc.at(pos + 15)
	_, _, za := sc.at(pos - 10)
	_, _, zb := sc.at(pos + 20)
	hd := math.Atan2(bx-ax, by-ay)
	return roadCamera{
		x: x, y: y, z: z + roadEyeM,
		sinH: math.Sin(hd), cosH: math.Cos(hd), heading: hd,
		pitch: math.Atan((zb-za)/30) * roadPitchFollow,
		f:     float64(w) / 2 / math.Tan(roadFOVDeg/2*math.Pi/180),
		cx:    float64(w) / 2, axisY: float64(h) * 0.4,
	}
}

// project maps a point to the screen; ok is false behind the camera.
// depth is the distance along the view direction.
func (c roadCamera) project(x, y, z float64) (sx, sy, depth float64, ok bool) {
	dx, dy := x-c.x, y-c.y
	depth = dx*c.sinH + dy*c.cosH
	if depth < 0.5 {
		return 0, 0, depth, false
	}
	side := dx*c.cosH - dy*c.sinH
	sx = c.cx + c.f*side/depth
	sy = c.axisY - c.f*math.Tan(math.Atan2(z-c.z, depth)-c.pitch)
	return sx, sy, depth, true
}

// horizon is the screen row of the horizon.
func (c roadCamera) horizon() float64 { return c.axisY + c.f*math.Tan(c.pitch) }

// roadSeg is a projected point on the road's centre line.
type roadSeg struct {
	s, sx, sy, depth float64
	// clip is the highest row nearer road covered when this point was
	// reached: anything standing here is hidden below it.
	clip float64
}

// render draws the view from distance pos as rows lines of w columns.
// ghost is the ghost's distance, negative without one.
func (sc *roadScene) render(pos, ghost float64, w, rows int) string {
	h := rows * 2
	px := sc.pixels(pos, ghost, w, h)
	var b strings.Builder
	b.Grow(w * rows * 8)
	for r := range rows {
		var fg, bg rgb
		for x := range w {
			top, bot := px[2*r*w+x], px[(2*r+1)*w+x]
			if x == 0 || top != fg {
				writeSGR(&b, 38, top)
				fg = top
			}
			if x == 0 || bot != bg {
				writeSGR(&b, 48, bot)
				bg = bot
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeSGR(b *strings.Builder, kind int, c rgb) {
	b.WriteString("\x1b[")
	b.WriteString(strconv.Itoa(kind))
	b.WriteString(";2;")
	b.WriteString(strconv.Itoa(int(c.r)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(int(c.g)))
	b.WriteByte(';')
	b.WriteString(strconv.Itoa(int(c.b)))
	b.WriteByte('m')
}

// pixels draws the view into a w×h buffer, row by row.
func (sc *roadScene) pixels(pos, ghost float64, w, h int) []rgb {
	cam := sc.camera(pos, w, h)
	px := make([]rgb, w*h)
	cv := canvas{px: px, w: w, h: h}

	// Sky, distant hills that pan as the road turns, and far-off land.
	hz := cam.horizon()
	for y := range h {
		c := mix(fogColor, grassB, 0.25)
		if fy := float64(y) + 0.5; fy < hz {
			c = mix(skyTop, skyHorizon, fy/math.Max(hz, 1))
		}
		for x := range w {
			px[y*w+x] = c
		}
	}
	for x := range w {
		bearing := cam.heading + math.Atan((float64(x)+0.5-cam.cx)/cam.f)
		top := hz - cam.f*hillAngle(bearing)
		ya, yb := span(top, hz, cv.h)
		for y := ya; y < yb; y++ {
			cv.set(x, y, hillColor)
		}
	}

	// The road, near to far. Each stretch fills the rows between its ends
	// that nearer road hasn't covered, so crests hide what lies behind.
	ex := sc.extras(pos)
	onRoad := make([]bool, w*h)
	clip := float64(h)
	var segs []roadSeg
	// The first point just ahead of the eye; the rest on a fixed grid along
	// the course, so the road's shape doesn't shimmer as the rider moves.
	for s := pos + 1; s <= pos+roadDrawM; s = nextGrid(s, pos) {
		sx, sy, depth, ok := cam.project(sc.at(s))
		if !ok {
			if len(segs) > 0 {
				break // the road turns back past the camera
			}
			continue
		}
		cur := roadSeg{s: s, sx: sx, sy: sy, depth: depth, clip: clip}
		if n := len(segs); n > 0 && cur.sy < clip {
			sc.band(px, onRoad, w, cam, segs[n-1], cur, clip)
			clip = math.Max(0, cur.sy)
		}
		segs = append(segs, cur)
	}

	// Trees, buildings and the ghost, far to near, each hidden below the
	// road nearer than where it stands.
	if len(segs) == 0 {
		return px
	}
	// segAt projects distance s itself, hidden below the road nearer than s.
	segAt := func(s float64) (roadSeg, bool) {
		i := sort.Search(len(segs), func(i int) bool { return segs[i].s > s }) // first point beyond s
		if i == 0 || i == len(segs) {
			return roadSeg{}, false
		}
		sx, sy, depth, ok := cam.project(sc.at(s))
		if !ok {
			return roadSeg{}, false
		}
		return roadSeg{s: s, sx: sx, sy: sy, depth: depth, clip: segs[i].clip}, true
	}
	type sprite struct {
		s    float64
		draw func()
	}
	var sprites []sprite
	// From just ahead of the rider: a roadside tree is still in view a few
	// metres ahead (dropping them at 8 m made trees vanish while passing).
	for _, t := range sc.treeSpots(pos+1, pos+roadDrawM) {
		s, k := t.s, t.k
		g, ok := segAt(s)
		if !ok {
			continue
		}
		for side := range 2 {
			for n := range 3 {
				hash := splitmix(uint64(k)*8 + uint64(side*4+n))
				off := roadHalfM + 1.5 + float64(hash>>8%400)/10
				land := sc.landAt(s, off*float64(2*side-1))
				rule := treeRules[land]
				if hash%64 >= rule.per64 {
					continue
				}
				if rule.rowM > 0 { // orchards plant in rows
					off = roadHalfM + 3 + math.Round(off/rule.rowM)*rule.rowM
				}
				lateral := off * float64(2*side-1)
				if ex.lotAt(s, lateral, 2) != nil {
					continue
				}
				if cx, cy, rx, ry := sc.frameAt(s); ex.onBranch(cx+rx*lateral, cy+ry*lateral, 2) {
					continue
				}
				height := rule.minH + float64(hash>>20%100)/100*rule.spanH
				pine := hash>>28%4 < rule.pine4
				sprites = append(sprites, sprite{s, func() { drawTree(cv, cam, g, lateral, height, pine) }})
			}
		}
	}
	for _, b := range sc.buildings {
		s0, s1 := b.GetDistanceM()-b.GetLengthM()/2, b.GetDistanceM()+b.GetLengthM()/2
		if s1 < pos+3 || s0 > pos+roadDrawM {
			continue
		}
		first, last := segs[0].s, segs[len(segs)-1].s-0.01
		near, ok := segAt(math.Max(s0, first))
		if !ok {
			continue
		}
		far, ok := segAt(math.Min(s1, last))
		if !ok {
			far = segs[len(segs)-1]
		}
		front := s0 >= first
		sprites = append(sprites, sprite{math.Max(s0, pos), func() { drawBuilding(cv, cam, b, near, far, front) }})
	}
	for _, b := range ex.branches {
		if g, ok := segAt(b.d); ok {
			sprites = append(sprites, sprite{b.d, func() { sc.drawBranch(cv, onRoad, cam, b, g.clip) }})
		}
	}
	for _, p := range ex.lots {
		near := math.Max(p.GetDistanceM()-p.GetLengthM()/2, pos+2)
		if g, ok := segAt(near); ok {
			// Drawn before the cars in it: placed at its far end.
			sprites = append(sprites, sprite{p.GetDistanceM() + p.GetLengthM()/2, func() { sc.drawLot(cv, onRoad, cam, p, pos+2, g.clip) }})
		}
	}
	for _, p := range ex.lots {
		spots, colors := carSpots(p)
		for k, sp := range spots {
			if sp[0] < pos+3 {
				continue
			}
			if g, ok := segAt(sp[0]); ok {
				lateral, body := sp[1], colors[k]
				sprites = append(sprites, sprite{sp[0], func() { drawCar(cv, cam, g, lateral, body) }})
			}
		}
	}
	for _, s := range sc.signs {
		if d := s.GetDistanceM(); d > pos+3 {
			if g, ok := segAt(d); ok {
				sprites = append(sprites, sprite{d, func() { drawSign(cv, cam, g, roadHalfM+1.2) }})
			}
		}
	}
	if g, ok := segAt(ghost); ok && ghost > pos+2 {
		sprites = append(sprites, sprite{ghost, func() { drawGhost(cv, cam, g) }})
	}
	sort.SliceStable(sprites, func(i, j int) bool { return sprites[i].s > sprites[j].s })
	for _, sp := range sprites {
		sp.draw()
	}
	return px
}

// treeSpot is a place for roadside trees: s along the road, k seeding
// what grows there.
type treeSpot struct {
	s float64
	k int
}

// treeSpots lists the tree places in (from, to], far to near: every
// roadTreeM metres, and on a loop the same ones every lap.
func (sc *roadScene) treeSpots(from, to float64) []treeSpot {
	var out []treeSpot
	if !sc.loop {
		for k := int(math.Floor(to / roadTreeM)); float64(k)*roadTreeM > from; k-- {
			out = append(out, treeSpot{float64(k) * roadTreeM, k})
		}
		return out
	}
	perLap := int(sc.finish / roadTreeM)
	for lap := math.Floor(to / sc.finish); lap >= math.Floor(from/sc.finish); lap-- {
		for k := perLap - 1; k >= 0; k-- {
			if s := lap*sc.finish + float64(k)*roadTreeM; s > from && s <= to {
				out = append(out, treeSpot{s, k})
			}
		}
	}
	return out
}

// nextGrid is the next road point after s: on the roadSegM grid, at least
// half a step on so no band gets too thin.
func nextGrid(s, pos float64) float64 {
	n := (math.Floor(s/roadSegM) + 1) * roadSegM
	if n-s < roadSegM/2 && s == pos+1 {
		n += roadSegM
	}
	return n
}

// band fills the rows between a nearer and a farther road point.
// Road pixels are marked in onRoad, so flat things beside it (roads
// meeting it, car parks) stay under it.
func (sc *roadScene) band(px []rgb, onRoad []bool, w int, cam roadCamera, near, far roadSeg, clip float64) {
	top, bottom := far.sy, math.Min(near.sy, clip)
	if bottom-top < 1e-9 {
		return
	}
	for y := max(0, int(math.Ceil(top-0.5))); float64(y)+0.5 < bottom && y < len(px)/w; y++ {
		t := (float64(y) + 0.5 - top) / (near.sy - top) // 0 far .. 1 near
		// Perspective-correct depth and distance along the road.
		depth := 1 / (1/far.depth + (1/near.depth-1/far.depth)*t)
		u := 0.0
		if far.depth != near.depth {
			u = (far.depth - depth) / (far.depth - near.depth)
		}
		s := far.s + (near.s-far.s)*u
		mid := far.sx + (near.sx-far.sx)*t
		mPerPx := depth / cam.f
		fog := (depth - roadFogFromM) / (roadDrawM - roadFogFromM)

		odd := int(math.Floor(s/roadStripeM))%2 != 0
		grass, asphalt := grassA, asphaltA
		if odd {
			grass, asphalt = grassB, asphaltB
		}
		dash := math.Mod(s, 2*roadDashM) < roadDashM
		line := s - sc.finish // into the chequered line, 2 m deep
		if sc.loop {
			line = sc.lapPos(s) // a line every lap
		}
		finish := line >= 0 && line < 2
		row := px[y*w : (y+1)*w]
		for x := range row {
			lateral := (float64(x) + 0.5 - mid) * mPerPx
			ax := math.Abs(lateral)
			var c rgb
			switch {
			case ax > roadHalfM:
				c = sc.ground(s, lateral, odd, grass)
			case finish:
				c = lineColor
				if (int(math.Floor(lateral/0.5))+int(math.Floor(line/0.5)))%2 != 0 {
					c = finishDark
				}
			case ax > roadHalfM-roadLineM, dash && ax < roadLineM/2:
				c = lineColor
			default:
				c = asphalt
			}
			if ax <= roadHalfM {
				onRoad[y*w+x] = true
			}
			row[x] = mix(c, fogColor, fog)
		}
	}
}

// hillAngle is the height of the distant hills, in radians above the
// horizon, by compass bearing; whole frequencies keep it seamless.
func hillAngle(b float64) float64 {
	return 0.03 + 0.012*math.Sin(3*b+1) + 0.008*math.Sin(7*b+2) + 0.004*math.Sin(13*b+0.5)
}

// drawTree stands a tree off metres beside the road (right is positive):
// a pine or a leafy one.
func drawTree(cv canvas, cam roadCamera, g roadSeg, off, height float64, pine bool) {
	scale := cam.f / g.depth // pixels per metre
	hPx := height * scale
	if hPx < 1 {
		return
	}
	cx := g.sx + off*scale
	fog := (g.depth - roadFogFromM) / (roadDrawM - roadFogFromM)
	trunk, leaves := mix(trunkColor, fogColor, fog), mix(leafColor, fogColor, fog)
	if pine {
		leaves = mix(pineColor, fogColor, fog)
	}
	crown := 0.45 * height * scale // half width
	ya, yb := span(g.sy-hPx, math.Min(g.sy, g.clip), cv.h)
	for y := ya; y < yb; y++ {
		q := (g.sy - float64(y) - 0.5) / hPx // 0 at the foot, 1 at the top
		var half float64
		c := leaves
		switch {
		case pine && q > 0.15:
			half = crown * (1 - (q-0.15)/0.85)
		case !pine && q > 0.3:
			d := (q - 0.65) / 0.35
			half = crown * math.Sqrt(math.Max(0, 1-d*d))
		default:
			half, c = math.Max(0.5, 0.15*scale), trunk
		}
		xa, xb := span(cx-half, cx+half, cv.w)
		for x := xa; x < xb; x++ {
			cv.set(x, y, c)
		}
	}
}

// drawGhost draws the ghost rider seen from behind, half see-through.
func drawGhost(cv canvas, cam roadCamera, g roadSeg) {
	scale := cam.f / g.depth
	hPx := 1.75 * scale
	if hPx < 1 {
		return
	}
	cx := g.sx + roadGhostM*scale
	fog := (g.depth - roadFogFromM) / (roadDrawM - roadFogFromM)
	ya, yb := span(g.sy-hPx, math.Min(g.sy, g.clip), cv.h)
	for y := ya; y < yb; y++ {
		q := (g.sy - float64(y) - 0.5) / hPx
		half, c := 0.06*scale, ghostBike // a wheel, edge on
		switch {
		case q > 0.86:
			half, c = 0.11*scale, ghostBody // head
		case q > 0.55:
			half, c = 0.24*scale, ghostBody // back and shoulders
		case q > 0.35:
			half, c = 0.15*scale, ghostBody // legs
		}
		half = math.Max(half, 0.5)
		c = mix(c, fogColor, fog)
		xa, xb := span(cx-half, cx+half, cv.w)
		for x := xa; x < xb; x++ {
			cv.set(x, y, c)
		}
	}
}

// splitmix scatters trees: the same slot always gets the same tree.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ x>>30) * 0xbf58476d1ce4e5b9
	x = (x ^ x>>27) * 0x94d049bb133111eb
	return x ^ x>>31
}

// treeRule is how trees grow on a kind of land: the chance of each of
// three candidate spots per slot and side, out of 64; heights; the share
// of pines, out of 4; and the row spacing of planted trees.
type treeRule struct {
	per64       uint64
	minH, spanH float64
	pine4       uint64
	rowM        float64
}

var treeRules = map[pb.LandUse]treeRule{
	pb.LandUse_LAND_USE_UNSPECIFIED: {per64: 3, minH: 6, spanH: 7, pine4: 2},
	pb.LandUse_LAND_USE_MEADOW:      {per64: 3, minH: 6, spanH: 7, pine4: 1},
	pb.LandUse_LAND_USE_FARMLAND:    {per64: 1, minH: 6, spanH: 6, pine4: 0},
	pb.LandUse_LAND_USE_FOREST:      {per64: 56, minH: 11, spanH: 9, pine4: 2},
	pb.LandUse_LAND_USE_BUILT:       {per64: 5, minH: 5, spanH: 5, pine4: 1},
	pb.LandUse_LAND_USE_WATER:       {},
	pb.LandUse_LAND_USE_ORCHARD:     {per64: 40, minH: 3, spanH: 1.5, rowM: 4},
	pb.LandUse_LAND_USE_HEATH:       {per64: 8, minH: 2, spanH: 4, pine4: 3},
}

// groundColors per land use: two shades that alternate (in stripes across
// the road, or rows along it for crops).
var groundColors = map[pb.LandUse][2]rgb{
	pb.LandUse_LAND_USE_FARMLAND: {{0x8f, 0xa0, 0x3c}, {0x6e, 0x80, 0x2c}},
	pb.LandUse_LAND_USE_FOREST:   {{0x2e, 0x52, 0x27}, {0x29, 0x4a, 0x23}},
	pb.LandUse_LAND_USE_BUILT:    {{0x8a, 0x93, 0x7c}, {0x80, 0x88, 0x73}},
	pb.LandUse_LAND_USE_WATER:    {{0x3d, 0x70, 0xa0}, {0x45, 0x7a, 0xab}},
	pb.LandUse_LAND_USE_ORCHARD:  {{0x66, 0xa6, 0x44}, {0x5c, 0x98, 0x3d}},
	pb.LandUse_LAND_USE_HEATH:    {{0x86, 0x6a, 0x78}, {0x7a, 0x60, 0x6c}},
}

// ground is the colour beside the road; grass is the plain verge.
func (sc *roadScene) ground(s, lateral float64, odd bool, grass rgb) rgb {
	if math.Abs(lateral) < roadHalfM+1 {
		return grass
	}
	land := sc.landAt(s, lateral)
	c, ok := groundColors[land]
	if !ok {
		return grass
	}
	if land == pb.LandUse_LAND_USE_FARMLAND {
		odd = int(math.Floor(lateral/1.2))%2 != 0 // crop rows
	}
	if odd {
		return c[1]
	}
	return c[0]
}

var (
	wallColors = map[pb.BuildingKind][]rgb{
		pb.BuildingKind_BUILDING_KIND_HOUSE: {{0x9c, 0x4a, 0x32}, {0xb5, 0x6a, 0x45}, {0xd8, 0xd2, 0xc4}, {0xc9, 0xb2, 0x8a}},
		pb.BuildingKind_BUILDING_KIND_FLAT:  {{0x9a, 0x9e, 0xa3}, {0xb8, 0xb4, 0xa8}, {0x8c, 0x5a, 0x48}},
		pb.BuildingKind_BUILDING_KIND_BARN:  {{0x5a, 0x4a, 0x3a}, {0x3c, 0x46, 0x40}, {0x8a, 0x84, 0x78}},
	}
	roofColors = []rgb{{0x4a, 0x3a, 0x38}, {0xa0, 0x4b, 0x2e}, {0x3a, 0x3e, 0x44}}
	glassColor = rgb{0x34, 0x44, 0x55}
)

// drawBuilding draws a building as a box beside the road: the wall facing
// the road from its near to its far end, then (when it is still ahead)
// the end facing the rider, with a gable for pitched roofs and windows by
// floor.
func drawBuilding(cv canvas, cam roadCamera, b *pb.Building, near, far roadSeg, front bool) {
	hash := splitmix(math.Float64bits(b.GetDistanceM()) ^ math.Float64bits(b.GetOffsetM()))
	kind := b.GetKind()
	walls, ok := wallColors[kind]
	if !ok {
		kind, walls = pb.BuildingKind_BUILDING_KIND_HOUSE, wallColors[pb.BuildingKind_BUILDING_KIND_HOUSE]
	}
	wall, roof := walls[hash%uint64(len(walls))], roofColors[hash>>8%uint64(len(roofColors))]
	h, depth := b.GetHeightM(), b.GetDepthM()
	var roofH float64
	switch kind {
	case pb.BuildingKind_BUILDING_KIND_HOUSE:
		roofH = math.Min(h*0.45, depth*0.5)
	case pb.BuildingKind_BUILDING_KIND_BARN:
		roofH = math.Min(h*0.5, depth*0.45)
	}
	wallH := h - roofH
	windows := kind != pb.BuildingKind_BUILDING_KIND_BARN
	side := math.Copysign(1, b.GetOffsetM())
	inner, outer := b.GetOffsetM()-side*depth/2, b.GetOffsetM()+side*depth/2
	clip := near.clip
	fogAt := func(d float64) float64 { return (d - roadFogFromM) / (roadDrawM - roadFogFromM) }
	isWindow := func(along, up, floorH float64) bool {
		return windows && math.Mod(along, 2.6) > 1.1 && math.Mod(up, floorH) > 0.9 && math.Mod(up, floorH) < 2.2 && up < wallH-0.4
	}
	floorH := 3.0

	// The wall facing the road, with a band of roof above it.
	fN, fF := cam.f/near.depth, cam.f/far.depth
	xN, xF := near.sx+inner*fN, far.sx+inner*fF
	if math.Abs(xF-xN) >= 0.5 {
		lo, hi := math.Min(xN, xF), math.Max(xN, xF)
		xa, xb := span(lo, hi, cv.w)
		for x := xa; x < xb; x++ {
			t := (float64(x) + 0.5 - xN) / (xF - xN)
			if t < 0 || t > 1 {
				continue
			}
			d := 1 / (1/near.depth + (1/far.depth-1/near.depth)*t)
			f := cam.f / d
			ground := near.sy + (far.sy-near.sy)*t
			top := ground - wallH*f
			along := (d - near.depth) / math.Max(far.depth-near.depth, 1e-9) * b.GetLengthM()
			fog := fogAt(d)
			ya, yb := span(top-roofH*f*0.6, math.Min(ground, clip), cv.h)
			for y := ya; y < yb; y++ {
				c := mix(wall, rgb{0, 0, 0}, 0.25) // in shade
				if float64(y)+0.5 < top {
					c = roof
				} else if isWindow(along, (ground-float64(y)-0.5)/f, floorH) {
					c = glassColor
				}
				cv.set(x, y, mix(c, fogColor, fog))
			}
		}
	}
	if !front {
		return
	}
	// The end facing the rider, with its gable.
	left, right := near.sx+math.Min(inner, outer)*fN, near.sx+math.Max(inner, outer)*fN
	mid := near.sx + b.GetOffsetM()*fN
	top := near.sy - wallH*fN
	fog := fogAt(near.depth)
	xa, xb := span(left, right, cv.w)
	for x := xa; x < xb; x++ {
		across := (float64(x) + 0.5 - left) / fN
		gable := roofH * fN * (1 - math.Abs(float64(x)+0.5-mid)/((right-left)/2))
		ya, yb := span(top-gable, math.Min(near.sy, clip), cv.h)
		for y := ya; y < yb; y++ {
			c := wall
			switch {
			case float64(y)+0.5 < top && kind == pb.BuildingKind_BUILDING_KIND_BARN:
				c = roof
			case float64(y)+0.5 < top:
				c = mix(wall, rgb{0, 0, 0}, 0.1)
			case isWindow(across, (near.sy-float64(y)-0.5)/fN, floorH):
				c = glassColor
			}
			cv.set(x, y, mix(c, fogColor, fog))
		}
	}
}

// canvas is the pixel buffer the scenery is drawn into.
type canvas struct {
	px   []rgb
	w, h int
}

func (cv canvas) set(x, y int, c rgb) {
	if x >= 0 && x < cv.w && y >= 0 && y < cv.h {
		cv.px[y*cv.w+x] = c
	}
}

// span clips the pixels from lo up to hi to 0..n-1. A sprite right beside
// the rider projects to thousands of pixels across; only those on screen
// are visited.
func span(lo, hi float64, n int) (int, int) {
	a, b := math.Max(0, math.Floor(lo)), math.Min(float64(n), math.Ceil(hi))
	if !(a < b) { // also NaN
		return 0, 0
	}
	return int(a), int(b)
}
