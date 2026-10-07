package ride

import (
	"sort"
	"time"

	"github.com/digimago/osscycler/course"
)

// TracePoint is where a ride was on the course at a reading of its clock.
type TracePoint struct {
	At        time.Duration // ride clock
	DistanceM float64       // along the course
}

// Ghost is an earlier ride to race: its position over its ride clock,
// from (0, its start) to (Elapsed, the finish).
type Ghost struct {
	Label   string        // e.g. "PB 7 Oct"
	Elapsed time.Duration // its time to the line
	Trace   []TracePoint  // distance never decreases
}

// GhostSource finds a ghost for a ride on c from startM; nil, nil when
// there is none (a first ride on that stretch).
type GhostSource interface {
	Ghost(c *course.Course, startM float64) (*Ghost, error)
}

// DistanceAt is where the ghost was t into its ride; at the line once it
// has finished.
func (g *Ghost) DistanceAt(t time.Duration) float64 {
	tr := g.Trace
	i := sort.Search(len(tr), func(i int) bool { return tr[i].At >= t })
	switch {
	case i == 0:
		return tr[0].DistanceM
	case i == len(tr):
		return tr[len(tr)-1].DistanceM
	}
	a, b := tr[i-1], tr[i]
	return a.DistanceM + (b.DistanceM-a.DistanceM)*float64(t-a.At)/float64(b.At-a.At)
}

// TimeAt is when the ghost first got to distance d: its ride clock there.
// A stop doesn't count twice; past its finish it is Elapsed.
func (g *Ghost) TimeAt(d float64) time.Duration {
	tr := g.Trace
	i := sort.Search(len(tr), func(i int) bool { return tr[i].DistanceM >= d })
	switch {
	case i == 0:
		return tr[0].At
	case i == len(tr):
		return g.Elapsed
	}
	a, b := tr[i-1], tr[i]
	return a.At + time.Duration(float64(b.At-a.At)*(d-a.DistanceM)/(b.DistanceM-a.DistanceM))
}

// Gap is how far the rider, elapsed into the ride at distance d, is
// behind the ghost in time: positive behind, negative ahead. Measured at
// the rider's position, like a time check in a race.
func (g *Ghost) Gap(elapsed time.Duration, d float64) time.Duration {
	return elapsed - g.TimeAt(d)
}
