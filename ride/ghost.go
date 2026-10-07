package ride

import (
	"errors"
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

// GhostSource finds ghosts in the rider's history.
type GhostSource interface {
	// Ghost is the personal best on c from startM; nil, nil when there is
	// none (a first ride on that stretch).
	Ghost(c *course.Course, startM float64) (*Ghost, error)
	// Race is the ride on c that finished at finished, with where it
	// started. ErrUnknownRide if there is none, ErrCourseChanged if the
	// course no longer matches it.
	Race(c *course.Course, finished time.Time) (g *Ghost, startM float64, err error)
}

var (
	ErrUnknownRide   = errors.New("ride: no such ride in the history")
	ErrCourseChanged = errors.New("ride: the course has changed since that ride")
)

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
