package history

import (
	"errors"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/ride"
)

func TestAPausedRideIsNoPB(t *testing.T) {
	fast := res(1, "mash", 0, 10000, 1200) // a rest halfway up, the clock stopped
	fast.Paused = true
	s := &Store{Dir: write(t,
		res(0, "mash", 0, 10000, 1500),
		fast,
		res(2, "mash", 0, 10000, 1450), // the PB: the paused ride doesn't count
	)}
	es, err := s.Results()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		day := int(e.Finished.Sub(t0).Hours() / 24)
		if e.PB != (day == 2) {
			t.Errorf("day %d (paused %v): PB %v", day, e.Paused, e.PB)
		}
	}
	if b, ok := s.Best("mash", 0, 10000); !ok || b.ElapsedS != 1450 {
		t.Errorf("best %+v, %v: want the unpaused 1450 s", b, ok)
	}
	c := &course.Course{ID: "mash", Distance: 10000}
	if _, _, err := s.Race(c, fast.Finished); !errors.Is(err, ride.ErrPausedRide) {
		t.Errorf("racing the paused ride: %v, want ErrPausedRide", err)
	}
}
