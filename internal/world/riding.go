package world

import (
	"math"
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
