package world

import (
	"math"
	"sort"
)

// The riding line: where on the road a rider rides, as on a closed road
// (owner, 2026-10-08): in the middle of their own half, drifting to near
// the centre line through bends towards that side and out towards the
// edge through the others, out-in-out, so the bike steers no more than it
// needs to and keeps its speed. Never across the centre line: riders
// may come the other way. The ride itself follows the centre line; the
// line only puts the renderers' rider (the eyes) sideways.
const (
	centreGapM = 0.4  // the line stays this far from the centre line
	edgeGapM   = 0.6  // and from the carriageway's edge
	fullBendM  = 25.0 // a bend this tight (radius) or tighter uses all the room
	bendSpanM  = 10.0 // curvature is taken over this far either side
	lineSpanM  = 30.0 // the line moves over this far before and after a bend
	// The rider anticipates the road (owner, 2026-10-09: a narrowing made
	// hard sideways turns): the room is the narrowest within aheadM either
	// way, a bend takes bendShare of the room either side of the lane's
	// middle, and the line moves at most driftPerM sideways per metre.
	aheadM    = 15.0
	bendShare = 0.5
	driftPerM = 0.03
)

// ridingLine is the line at distances d (equal steps) along the smooth
// line, on a carriageway widths wide there: metres right of the centre
// line (negative left). keepRight is false where riders keep left.
func ridingLine(line *smoothLine, d, widths []float64, keepRight bool) []float64 {
	n := len(d)
	if n == 0 {
		return nil
	}
	side := 1.0
	if !keepRight {
		side = -1 // mirrored: the bends towards the centre are the right-hand ones
	}
	step := 1.0
	if n > 1 {
		step = (d[n-1] - d[0]) / float64(n-1)
	}
	narrow := make([]float64, n)
	reach := max(0, int(math.Round(aheadM/step)))
	for i := range d {
		narrow[i] = widths[i]
		for k := max(0, i-reach); k <= min(n-1, i+reach); k++ {
			narrow[i] = math.Min(narrow[i], widths[k])
		}
	}
	widths = narrow
	target := make([]float64, n)
	for i := range d {
		lo, hi, mid := room(widths[i])
		// Curvature over the bend: the turn from bendSpanM before to after,
		// over the distance between; left positive.
		ax, ay, _ := line.heading(d[i] - bendSpanM)
		bx, by, _ := line.heading(d[i] + bendSpanM)
		k := math.Atan2(ax*by-ay*bx, ax*bx+ay*by) / (2 * bendSpanM) * side
		s := bendShare * math.Max(-1, math.Min(1, k*fullBendM))
		if s > 0 { // a bend towards the centre line
			target[i] = mid + s*(lo-mid)
		} else {
			target[i] = mid - s*(hi-mid)
		}
	}
	// Smoothed along the road (a triangle over lineSpanM each way), so the
	// rider moves over before a bend and back after it.
	span := max(1, int(math.Round(lineSpanM/step)))
	out := make([]float64, n)
	for i := range d {
		var sum, wsum float64
		for k := max(0, i-span); k <= min(n-1, i+span); k++ {
			w := float64(span + 1 - abs(k-i))
			sum += w * target[k]
			wsum += w
		}
		lo, hi, _ := room(widths[i])
		out[i] = side * math.Max(lo, math.Min(hi, sum/wsum))
	}
	limitDrift(out, step)
	return out
}

// limitDrift keeps a riding line (metres off the centre line every step
// metres, all on one side) from moving sideways faster than driftPerM: it
// only ever brings the line nearer the centre line, so it stays within the
// room it had.
func limitDrift(line []float64, step float64) {
	r := driftPerM * step
	for range 2 {
		for i := 1; i < len(line); i++ {
			line[i] = towards(line[i], line[i-1], r)
		}
		for i := len(line) - 2; i >= 0; i-- {
			line[i] = towards(line[i], line[i+1], r)
		}
	}
}

// towards is v, but no further from the centre line than next plus r.
func towards(v, next, r float64) float64 {
	if math.Abs(v) > math.Abs(next)+r {
		return math.Copysign(math.Abs(next)+r, v)
	}
	return v
}

// room is how far right of the centre line a rider may ride on a
// carriageway width wide, and the middle of their half.
func room(width float64) (lo, hi, mid float64) {
	half := width / 2
	lo = math.Min(centreGapM, half/2)
	hi = math.Max(lo, half-edgeGapM)
	return lo, hi, (lo + hi) / 2
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Sharp turns on the rider's path (owner, 2026-10-09: a right turn at a
// junction was taken on a few metres' radius): where the path turns more
// than turnDeg within turnSpanM, it is replaced from turnLeadM before the
// turn to as far after by a smooth curve (a cubic Hermite with the path's
// headings there), as a rider sweeps through a junction's rounded corner.
// keepOnRoad afterwards keeps it on the asphalt.
const (
	turnDeg   = 45.0
	turnSpanM = 20.0
	turnLeadM = 15.0
)

// smoothTurns smooths the sharp turns of a path (points every step m).
func smoothTurns(x, y []float64, step float64, loop bool) {
	n := len(x)
	span := max(1, int(math.Round(turnSpanM/2/step)))
	lead := max(2, int(math.Round(turnLeadM/step)))
	if n < 2*lead+2 {
		return
	}
	heading := func(i, j int) float64 { return math.Atan2(y[j]-y[i], x[j]-x[i]) }
	turnAt := func(i int) float64 {
		a, b, c := i-span, i, i+span
		if a < 0 || c >= n {
			return 0
		}
		return math.Abs(math.Remainder(heading(b, c)-heading(a, b), 2*math.Pi))
	}
	for i := lead; i < n-lead; i++ {
		t := turnAt(i)
		if t < turnDeg*math.Pi/180 || t < turnAt(i-1) || t < turnAt(i+1) {
			continue // not the sharpest point of a sharp turn
		}
		a, b := i-lead, i+lead
		if a-1 < 0 || b+1 >= n {
			continue // no heading to meet before or after
		}
		ha, hb := heading(a-1, a), heading(b, b+1)
		l := math.Hypot(x[b]-x[a], y[b]-y[a])
		ta := [2]float64{math.Cos(ha) * l, math.Sin(ha) * l}
		tb := [2]float64{math.Cos(hb) * l, math.Sin(hb) * l}
		// Sample the curve finely, then put the path's points along it at
		// equal distances.
		const fine = 64
		var cx, cy, cd []float64
		for k := 0; k <= fine; k++ {
			u := float64(k) / fine
			h00, h10, h01, h11 := 2*u*u*u-3*u*u+1, u*u*u-2*u*u+u, -2*u*u*u+3*u*u, u*u*u-u*u
			px := h00*x[a] + h10*ta[0] + h01*x[b] + h11*tb[0]
			py := h00*y[a] + h10*ta[1] + h01*y[b] + h11*tb[1]
			d := 0.0
			if k > 0 {
				d = cd[k-1] + math.Hypot(px-cx[k-1], py-cy[k-1])
			}
			cx, cy, cd = append(cx, px), append(cy, py), append(cd, d)
		}
		for j := a + 1; j < b; j++ {
			want := cd[fine] * float64(j-a) / float64(b-a)
			k := sort.SearchFloat64s(cd, want)
			k = max(1, min(fine, k))
			f := (want - cd[k-1]) / math.Max(cd[k]-cd[k-1], 1e-9)
			x[j], y[j] = cx[k-1]+f*(cx[k]-cx[k-1]), cy[k-1]+f*(cy[k]-cy[k-1])
		}
		i = b
	}
}

// Spikes in the rider's path (owner, 2026-10-10: the rider jerked aside on
// the Posbank Loop at 5.425 km, where the matching passes between the
// Beekhuizenseweg's two carriageways: one point 0.74 m off the line through
// its neighbours, they on it): a point further than spikeM off the line
// through its neighbours, while they lie off theirs to the other side or
// by under a third of that (a spike pulls its neighbours' lines its way, so
// they read about half of it the other way; a bend bends them alike, the
// same way), is moved onto that line. A few
// rounds, as one spike may hide behind another.
const spikeM = 0.25

func despike(x, y []float64, loop bool) {
	n := len(x)
	if n < 5 {
		return
	}
	idx := func(i int) (int, bool) {
		if loop {
			return (i%n + n) % n, true
		}
		return i, i >= 0 && i < n
	}
	// off is how far point i is from the line through i-1 and i+1 (signed:
	// left positive), and the foot of that on the line.
	off := func(i int) (float64, float64, float64, bool) {
		a, okA := idx(i - 1)
		b, okB := idx(i + 1)
		if !okA || !okB {
			return 0, 0, 0, false
		}
		de, dn := x[b]-x[a], y[b]-y[a]
		l2 := de*de + dn*dn
		if l2 == 0 {
			return 0, 0, 0, false
		}
		t := ((x[i]-x[a])*de + (y[i]-y[a])*dn) / l2
		fe, fn := x[a]+t*de, y[a]+t*dn
		d := math.Hypot(x[i]-fe, y[i]-fn)
		if de*(y[i]-y[a])-dn*(x[i]-x[a]) < 0 {
			d = -d
		}
		return d, fe, fn, true
	}
	// spike tells whether i is one, and where it belongs.
	spike := func(i int) (float64, float64, float64, bool) {
		d, fe, fn, ok := off(i)
		if !ok || math.Abs(d) <= spikeM {
			return 0, 0, 0, false
		}
		for _, j := range []int{i - 1, i + 1} {
			k, ok := idx(j)
			if !ok {
				continue
			}
			if dj, _, _, ok := off(k); ok && dj*d > 0 && math.Abs(dj) > math.Abs(d)/3 {
				return 0, 0, 0, false // bent the same way: a bend
			}
		}
		return d, fe, fn, true
	}
	for range 3 {
		// The biggest first: a spike's neighbours read as spikes too
		// (half its size, the other way) until it is gone.
		type cand struct {
			i int
			d float64
		}
		var cs []cand
		for i := range n {
			if d, _, _, ok := spike(i); ok {
				cs = append(cs, cand{i, math.Abs(d)})
			}
		}
		if len(cs) == 0 {
			return
		}
		sort.Slice(cs, func(a, b int) bool { return cs[a].d > cs[b].d || cs[a].d == cs[b].d && cs[a].i < cs[b].i })
		done := map[int]bool{}
		for _, c := range cs {
			if done[c.i] {
				continue
			}
			if _, fe, fn, ok := spike(c.i); ok {
				x[c.i], y[c.i] = fe, fn
				for _, j := range []int{c.i - 1, c.i, c.i + 1} {
					if k, ok := idx(j); ok {
						done[k] = true
					}
				}
			}
		}
	}
}
