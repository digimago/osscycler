package course

import "strings"

// ReverseSuffix ends the ID of a course ridden the other way: the Posbank
// Loop is "posbank", its reverse "posbank-reverse".
const ReverseSuffix = "-reverse"

// reverseName is a reversed course's name after its forward one's.
const reverseName = " (reverse)"

// Reversed is the course ridden the other way (owner, 2026-10-13: a course
// of its own in the menu, with its own PBs, ghosts, history and world
// path): from c's finish to its start, the same road, the climbs now
// descents. The profile is c's own run backwards, sample for sample (the
// samples lie on the same equal grid either way, and despiking and
// smoothing are symmetric, so it is the profile the reversed GPX would
// give). Use withReverse for the pair a menu offers.
//
// c must be a course as loaded, before Lapped: a lapped course has left
// out the run-in back to its start, which the reverse rides first. Lap the
// result for the reverse's own lap. The test tracks aren't reversed:
// their made-up landscapes are laid out along their own way.
func (c *Course) Reversed() *Course {
	if c.Lap != nil || c.Loop {
		panic("course: Reversed of a lapped course or a loop: " + c.ID)
	}
	r := &Course{
		ID: c.ID + ReverseSuffix, Name: c.Name + reverseName,
		Distance: c.Distance, Spacing: c.Spacing,
		Gain: c.Loss, Loss: c.Gain, MaxGrade: -c.MinGrade, MinGrade: -c.MaxGrade,
		Builtin: c.Builtin, Included: c.Included, Credit: c.Credit, raw: c.rawLength(),
	}
	n := len(c.dist)
	r.dist = append([]float64(nil), c.dist...) // the same grid
	r.ele = make([]float64, n)
	for i := range n {
		r.ele[i] = c.ele[n-1-i]
	}
	r.grade = make([]float64, n-1)
	for i := range n - 1 {
		r.grade[i] = (r.ele[i+1] - r.ele[i]) / (r.dist[i+1] - r.dist[i]) * 100
	}
	m := len(c.trackDist)
	r.trackDist, r.lat, r.lon = make([]float64, m), make([]float64, m), make([]float64, m)
	for k := range m {
		r.trackDist[k] = c.Distance - c.trackDist[m-1-k]
		r.lat[k], r.lon[k] = c.lat[m-1-k], c.lon[m-1-k]
	}
	r.trackDist[0], r.trackDist[m-1] = 0, c.Distance
	return r
}

// withReverse is the course as loaded made ready to ride, both ways: c
// lapped, and its reverse lapped, Forward the first.
func withReverse(c *Course) (fwd, rev *Course) {
	fwd = c.Lapped()
	rev = c.Reversed().Lapped()
	rev.Forward = fwd
	return fwd, rev
}

// reversible tells whether a course with this ID gets a reverse: not one
// that is a reverse itself by its name.
func reversible(id string) bool { return !strings.HasSuffix(id, ReverseSuffix) }

// rawLength is the course's length as loaded, before Lapped left out the
// run-in back.
func (c *Course) rawLength() float64 {
	if c.raw > 0 {
		return c.raw
	}
	return c.Distance
}

// toRaw is where d along c lies along the course as loaded: the same, but
// past a lap's end (its tail) the lap's start again.
func (c *Course) toRaw(d float64) float64 {
	if l := c.Lap; l != nil && !c.Loop && d > l.EndM {
		return l.StartM + d - l.EndM
	}
	return d
}

// ForwardAt is where the point d along a reversed course lies along its
// Forward course: what the map data's stretches, kept for the forward
// course, go by. Where the forward course has left that point out (its
// run-in back to the start, which a lapped course doesn't ride) it is
// taken on the run-in out, which follows the same road, so it is only
// about right where the two run apart. On a course that isn't reversed, d.
func (c *Course) ForwardAt(d float64) float64 {
	f := c.Forward
	if f == nil {
		return d
	}
	p := c.rawLength() - c.toRaw(d)
	if l := f.Lap; l != nil && p > l.EndM {
		p = min(c.rawLength()-p, l.StartM)
	}
	return max(0, min(p, f.Distance))
}
