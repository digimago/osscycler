package tui

import (
	"testing"
	"time"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// A ghost speed from the core is taken as it is, however unevenly the
// updates arrive; without one (an older core) it is estimated from them.
func TestGhostSpeedFromTheCore(t *testing.T) {
	clock := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	m := New("x:1", nil)
	m.now = func() time.Time { return clock }
	at := func(d, v float64) *pb.State {
		return &pb.State{Ride: &pb.Ride{Phase: pb.RidePhase_RIDE_PHASE_RIDING, CourseId: "c", CourseDistanceM: 5000,
			Ghost: &pb.RideGhost{Label: "PB", DistanceM: d, SpeedMps: v}}}
	}
	step := func(st *pb.State, dt time.Duration) {
		old := m.ride()
		clock = clock.Add(dt)
		m.st = st
		m = m.followGhost(old)
	}
	step(at(100, 7.5), 0)
	step(at(101, 7.5), 400*time.Millisecond) // 2.5 m/s by the reports: uneven
	if m.ghostV != 7.5 {
		t.Errorf("ghost speed %.2f, want the core's 7.5", m.ghostV)
	}
	step(at(102, 0), 400*time.Millisecond) // an older core: estimated
	if m.ghostV == 7.5 || m.ghostV <= 0 {
		t.Errorf("estimated ghost speed %.2f", m.ghostV)
	}
}
