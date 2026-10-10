// Package history is the rider's record of finished course rides: every
// result, and the personal best on each stretch (a course from a given
// start point). It reads the recorder's results file on every call, so
// a ride finished a moment ago is in it.
package history

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/replay"
	"github.com/digimago/osscycler/internal/ride"
	"github.com/digimago/osscycler/internal/sim"
)

// Entry is a finished course ride.
type Entry struct {
	record.Result
	// PB marks the fastest ride on its stretch.
	PB bool
}

// Store reads the history from a recording directory.
type Store struct {
	Dir string
}

// SameStretch reports whether two rides are comparable: the same course
// from the same start, over the same distance (a course file edited
// since would make an old time meaningless).
func SameStretch(a, b record.Result) bool {
	return a.CourseID == b.CourseID && math.Abs(a.StartM-b.StartM) < 1 && math.Abs(a.DistanceM-b.DistanceM) < 5
}

// Results returns every finished course ride, newest first, with the
// fastest on each stretch marked.
func (s *Store) Results() ([]Entry, error) {
	rs, err := record.ReadResults(s.Dir)
	if err != nil {
		return nil, err
	}
	es := make([]Entry, len(rs))
	for i, r := range rs {
		// A paused ride (its time is riding time, with a rest in it) is
		// never the PB, and no ride has to beat it to be one.
		es[i] = Entry{Result: r, PB: !r.Paused}
		for _, o := range rs {
			if r.Paused || o.Paused {
				continue
			}
			// Strictly faster, or as fast and earlier: one PB per stretch.
			if SameStretch(r, o) && (o.ElapsedS < r.ElapsedS || (o.ElapsedS == r.ElapsedS && o.Finished.Before(r.Finished))) {
				es[i].PB = false
				break
			}
		}
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].Finished.After(es[j].Finished) })
	return es, nil
}

// Best returns the personal best on a stretch: courseID from startM over
// distanceM.
func (s *Store) Best(courseID string, startM, distanceM float64) (Entry, bool) {
	es, err := s.Results()
	if err != nil {
		return Entry{}, false
	}
	want := record.Result{CourseID: courseID, StartM: startM, DistanceM: distanceM}
	for _, e := range es {
		if e.PB && SameStretch(e.Result, want) {
			return e, true
		}
	}
	return Entry{}, false
}

// Ghost is the personal best on course c from startM, ready to race.
// nil, nil when the stretch has no PB yet.
func (s *Store) Ghost(c *course.Course, startM float64) (*ride.Ghost, error) {
	best, ok := s.Best(c.ID, startM, c.Distance-startM)
	if !ok {
		return nil, nil
	}
	return s.ghost(c, best)
}

// Race is the ride on course c that finished at finished (to the second,
// as results are kept), ready to race from where it started.
func (s *Store) Race(c *course.Course, finished time.Time) (*ride.Ghost, float64, error) {
	es, err := s.Results()
	if err != nil {
		return nil, 0, err
	}
	for _, e := range es {
		if e.CourseID != c.ID || !e.Finished.Equal(finished.Truncate(time.Second)) {
			continue
		}
		if !SameStretch(e.Result, record.Result{CourseID: c.ID, StartM: e.StartM, DistanceM: c.Distance - e.StartM}) {
			return nil, 0, ride.ErrCourseChanged
		}
		if e.Paused {
			// Its recording holds the stop, its time doesn't: no ghost.
			return nil, 0, ride.ErrPausedRide
		}
		g, err := s.ghost(c, e)
		return g, e.StartM, err
	}
	return nil, 0, ride.ErrUnknownRide
}

// ghost replays a finished ride's recorded lap into a trace, timed to
// finish on the recorded time so the gap at the line agrees with the
// history.
func (s *Store) ghost(c *course.Course, e Entry) (*ride.Ghost, error) {
	power, err := replay.LapPower(filepath.Join(s.Dir, e.File), e.Lap)
	if err != nil {
		return nil, err
	}
	params, ok := e.Sim.Params()
	if !ok {
		params = sim.DefaultParams(75, 9)
	}
	day := e.Finished.Local().Format("2 Jan")
	label := "PB " + day
	if !e.PB {
		label = day + " " + e.Finished.Local().Format("15:04")
	}
	res := ride.Replay(c, params, e.StartM, e.StartSpeedMPS, power)
	if !res.Finished || res.Elapsed <= 0 {
		return nil, fmt.Errorf("the ride of %s doesn't replay to the finish", label)
	}
	recorded := time.Duration(e.ElapsedS * float64(time.Second))
	scale := float64(recorded) / float64(res.Elapsed)
	trace := make([]ride.TracePoint, len(res.Trace))
	for i, p := range res.Trace {
		trace[i] = ride.TracePoint{At: time.Duration(float64(p.At) * scale), DistanceM: p.DistanceM}
	}
	return &ride.Ghost{Label: label, Elapsed: recorded, Trace: trace}, nil
}
