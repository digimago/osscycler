package world

import (
	"math"
	"sort"
)

// A route from a router or a recording is a line of points, often tens of
// metres apart, with a corner at each: a road built straight along it is
// blocky, its kerbs notched and folded at every corner. smoothLine rounds
// each corner off with an arc, as roads are laid out (straights joined by
// arcs): as large as the straights on either side allow (so neighbouring
// arcs meet without a kink), but never further than maxDevM from the
// point, so the road stays where it is and clear of the houses beside it.
//
// It keeps the line's own distances: a point at distance d on the line is
// at distance d on the smooth line too, the arc taking the place of the
// stretch of line it replaces. A rounded corner is centimetres shorter,
// so a renderer's rider is that far off for a moment; the ride itself
// follows the line.
type smoothLine struct {
	d, x, y []float64 // the points: distance along the line, east, north
	arcs    []arc     // by point; zero where the line runs straight on
	loop    bool
}

// arc replaces the line from d0 to d1 around a point.
type arc struct {
	d0, d1       float64
	cx, cy, r    float64 // centre and radius
	start, sweep float64 // angle at d0 (radians, from east), and signed sweep
	present      bool
}

const (
	maxDevM   = 1.0                 // a rounded corner stays this close to its point
	minTurn   = 0.5 * math.Pi / 180 // straighter corners stay as they are
	arcStepM  = 0.5                 // a rounded corner is sampled this finely
	maxArcRad = 4 * math.Pi / 180   // or every few degrees, whichever is finer
)

// newSmoothLine rounds the corners of the line through points at
// distances d (increasing). A loop's first point is its last. dev, when
// not nil, is how far from its point a corner at distance d may be rounded
// (maxDevM otherwise): further where the route turns at a junction, whose
// paved apron is wider than the road.
func newSmoothLine(d, x, y []float64, loop bool, dev func(d float64) float64) *smoothLine {
	s := &smoothLine{d: d, x: x, y: y, arcs: make([]arc, len(d)), loop: loop}
	n := len(d)
	for i := range n {
		pi, ni := i-1, i+1
		if i == 0 || i == n-1 {
			if !loop || n < 4 {
				continue
			}
			if i == n-1 {
				continue // the loop's last point is its first, rounded there
			}
			pi = n - 2
		}
		ax, ay := x[i]-x[pi], y[i]-y[pi]
		bx, by := x[ni]-x[i], y[ni]-y[i]
		la, lb := math.Hypot(ax, ay), math.Hypot(bx, by)
		if la == 0 || lb == 0 {
			continue
		}
		ax, ay, bx, by = ax/la, ay/la, bx/lb, by/lb
		turn := math.Atan2(ax*by-ay*bx, ax*bx+ay*by) // signed, left positive
		th := math.Abs(turn)
		if th < minTurn || th > math.Pi-0.01 {
			continue
		}
		// The arc meets the line t before and after the point: at most half
		// of each straight (the neighbouring arc has the other half), and
		// no further from the point than maxDevM.
		t := math.Min(la, lb) / 2
		md := maxDevM
		if dev != nil {
			md = dev(d[i])
		}
		rMax := md / (1/math.Cos(th/2) - 1)
		t = math.Min(t, rMax*math.Tan(th/2))
		r := t / math.Tan(th/2)
		t0x, t0y := x[i]-ax*t, y[i]-ay*t
		// The centre lies to the side the line turns to.
		side := math.Copysign(1, turn)
		cx, cy := t0x-ay*r*side, t0y+ax*r*side
		s.arcs[i] = arc{
			d0: d[i] - t, d1: d[i] + t, cx: cx, cy: cy, r: r,
			start: math.Atan2(t0y-cy, t0x-cx), sweep: turn, present: true,
		}
		if i == 0 { // the loop's start: its arc begins before the line ends
			s.arcs[i].d0 = d[n-1] - t
		}
	}
	return s
}

func (s *smoothLine) length() float64 { return s.d[len(s.d)-1] }

// at is the smooth line at distance d.
func (s *smoothLine) at(d float64) (x, y float64) {
	L := s.length()
	if s.loop {
		if d = math.Mod(d, L); d < 0 {
			d += L
		}
	} else {
		d = math.Max(0, math.Min(d, L))
	}
	i := sort.SearchFloat64s(s.d, d) // the first point at or past d
	cands := []int{i - 1, i, i + 1}
	if s.loop {
		cands = append(cands, 0) // the start's arc, also near the end
	}
	for _, k := range cands {
		if k < 0 || k >= len(s.d) {
			continue
		}
		a := s.arcs[k]
		if !a.present {
			continue
		}
		dd := d
		if s.loop && k == 0 && d > L/2 {
			dd -= L // the start's arc, before the line ends
		}
		lo, hi := a.d0, a.d1
		if s.loop && k == 0 {
			lo -= L
		}
		if dd >= lo && dd <= hi {
			f := (dd - lo) / (hi - lo)
			ang := a.start + f*a.sweep
			return a.cx + a.r*math.Cos(ang), a.cy + a.r*math.Sin(ang)
		}
	}
	if i == 0 {
		return s.x[0], s.y[0]
	}
	if i >= len(s.d) {
		return s.x[len(s.x)-1], s.y[len(s.y)-1]
	}
	f := (d - s.d[i-1]) / (s.d[i] - s.d[i-1])
	return s.x[i-1] + f*(s.x[i]-s.x[i-1]), s.y[i-1] + f*(s.y[i]-s.y[i-1])
}

// heading is the smooth line's direction at d, as a unit vector east and
// north, and its curvature there (1/m, left positive).
func (s *smoothLine) heading(d float64) (de, dn, k float64) {
	const h = 0.5
	x0, y0 := s.at(d - h)
	x1, y1 := s.at(d)
	x2, y2 := s.at(d + h)
	ax, ay := x1-x0, y1-y0
	bx, by := x2-x1, y2-y1
	l := math.Hypot(x2-x0, y2-y0)
	if l == 0 {
		return 0, 1, 0
	}
	la, lb := math.Hypot(ax, ay), math.Hypot(bx, by)
	if la > 0 && lb > 0 {
		k = math.Atan2(ax*by-ay*bx, ax*bx+ay*by) / ((la + lb) / 2)
	}
	return (x2 - x0) / l, (y2 - y0) / l, k
}

// stations are distances along the line every step at most, and finer
// within the rounded corners (every arcStepM, or every few degrees).
func (s *smoothLine) stations(step float64) []float64 {
	L := s.length()
	var out []float64
	for d := 0.0; d < L; {
		out = append(out, d)
		next := d + step
		for k, a := range s.arcs {
			if !a.present {
				continue
			}
			spans := [][2]float64{{a.d0, a.d1}}
			if s.loop && k == 0 { // the start's arc spans the line's end and start
				spans = [][2]float64{{a.d0, L}, {0, a.d1}}
			}
			for _, sp := range spans {
				if sp[1] <= d || sp[0] >= next {
					continue
				}
				if sp[0] > d {
					next = math.Min(next, sp[0]) // the arc's own start
				} else {
					next = math.Min(next, d+math.Min(arcStepM, maxArcRad*a.r))
				}
			}
		}
		d = math.Max(next, d+1e-3)
	}
	return append(out, L)
}
