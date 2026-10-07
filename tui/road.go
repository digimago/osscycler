package tui

import (
	"math"
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
	roadEyeM     = 2.0   // eye height above the road
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
	east, north, ele []float64
}

// newRoadScene returns nil for a course without a track (an older core).
func newRoadScene(c *pb.Course) *roadScene {
	e, n, ele := c.GetProfileEastM(), c.GetProfileNorthM(), c.GetProfileElevationM()
	step := c.GetProfileStepM()
	if len(e) < 2 || len(n) != len(e) || len(ele) != len(e) || step <= 0 {
		return nil
	}
	k := int(math.Round(roadSmoothM / step / 2))
	return &roadScene{step: step, finish: c.GetDistanceM(),
		east: movingAverage(e, k), north: movingAverage(n, k), ele: movingAverage(ele, 0)}
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

// at returns the road's centre at distance s: east, north and elevation.
// Past either end the road runs on straight and level.
func (sc *roadScene) at(s float64) (x, y, z float64) {
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
		pitch: math.Atan((zb - za) / 30),
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
	set := func(x, y int, c rgb) {
		if x >= 0 && x < w && y >= 0 && y < h {
			px[y*w+x] = c
		}
	}

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
		for y := int(math.Floor(top)); float64(y) < hz; y++ {
			set(x, y, hillColor)
		}
	}

	// The road, near to far. Each stretch fills the rows between its ends
	// that nearer road hasn't covered, so crests hide what lies behind.
	clip := float64(h)
	var segs []roadSeg
	for s := pos + 1; s <= pos+roadDrawM; s += roadSegM {
		sx, sy, depth, ok := cam.project(sc.at(s))
		if !ok {
			if len(segs) > 0 {
				break // the road turns back past the camera
			}
			continue
		}
		cur := roadSeg{s: s, sx: sx, sy: sy, depth: depth, clip: clip}
		if n := len(segs); n > 0 && cur.sy < clip {
			sc.band(px, w, cam, segs[n-1], cur, clip)
			clip = math.Max(0, cur.sy)
		}
		segs = append(segs, cur)
	}

	// Trees and the ghost, far to near, each hidden below the road nearer
	// than where it stands.
	if len(segs) == 0 {
		return px
	}
	segAt := func(s float64) (roadSeg, bool) {
		i := int(math.Round((s - segs[0].s) / roadSegM))
		if i < 0 || i >= len(segs) {
			return roadSeg{}, false
		}
		return segs[i], true
	}
	ghostDrawn := ghost < 0
	for k := int(math.Floor((pos + roadDrawM) / roadTreeM)); float64(k)*roadTreeM > pos+8; k-- {
		s := float64(k) * roadTreeM
		if !ghostDrawn && ghost >= s {
			ghostDrawn = true
			if g, ok := segAt(ghost); ok && ghost > pos+2 {
				drawGhost(set, cam, g)
			}
		}
		hash := splitmix(uint64(k))
		if hash%4 != 0 { // one in four slots has a tree
			continue
		}
		g, ok := segAt(s)
		if !ok {
			continue
		}
		side := 1.0
		if hash&(1<<8) != 0 {
			side = -1
		}
		off := side * (roadHalfM + 4 + float64(hash>>16%80)/10)
		height := 6 + float64(hash>>24%70)/10
		drawTree(set, cam, g, off, height, hash&(1<<9) != 0)
	}
	if !ghostDrawn {
		if g, ok := segAt(ghost); ok && ghost > pos+2 {
			drawGhost(set, cam, g)
		}
	}
	return px
}

// band fills the rows between a nearer and a farther road point.
func (sc *roadScene) band(px []rgb, w int, cam roadCamera, near, far roadSeg, clip float64) {
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

		grass, asphalt := grassA, asphaltA
		if int(math.Floor(s/roadStripeM))%2 != 0 {
			grass, asphalt = grassB, asphaltB
		}
		dash := math.Mod(s, 2*roadDashM) < roadDashM
		finish := s >= sc.finish && s < sc.finish+2
		row := px[y*w : (y+1)*w]
		for x := range row {
			lateral := (float64(x) + 0.5 - mid) * mPerPx
			ax := math.Abs(lateral)
			var c rgb
			switch {
			case ax > roadHalfM:
				c = grass
			case finish:
				c = lineColor
				if (int(math.Floor(lateral/0.5))+int(math.Floor((s-sc.finish)/0.5)))%2 != 0 {
					c = finishDark
				}
			case ax > roadHalfM-roadLineM, dash && ax < roadLineM/2:
				c = lineColor
			default:
				c = asphalt
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
func drawTree(set func(x, y int, c rgb), cam roadCamera, g roadSeg, off, height float64, pine bool) {
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
	for y := int(math.Floor(g.sy - hPx)); float64(y) < math.Min(g.sy, g.clip); y++ {
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
		for x := int(math.Floor(cx - half)); float64(x) < cx+half; x++ {
			set(x, y, c)
		}
	}
}

// drawGhost draws the ghost rider seen from behind, half see-through.
func drawGhost(set func(x, y int, c rgb), cam roadCamera, g roadSeg) {
	scale := cam.f / g.depth
	hPx := 1.75 * scale
	if hPx < 1 {
		return
	}
	cx := g.sx + roadGhostM*scale
	fog := (g.depth - roadFogFromM) / (roadDrawM - roadFogFromM)
	for y := int(math.Floor(g.sy - hPx)); float64(y) < math.Min(g.sy, g.clip); y++ {
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
		for x := int(math.Floor(cx - half)); float64(x) < cx+half; x++ {
			set(x, y, c)
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
