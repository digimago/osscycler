package ride

import (
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

// BenchmarkTick is the session's work 4 times a second during a ride:
// read the power, step the simulation, publish the ride state.
func BenchmarkTick(b *testing.B) {
	s, hub, _ := newSession(b)
	setPower(hub, 200)
	now := time.Now()
	if err := s.Start("test"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(tickInterval)
		s.tick(now)
		if ride(hub).Phase == telemetry.RideFinished {
			b.StopTimer()
			s.Stop()
			s.Start("test")
			b.StartTimer()
		}
	}
}

// BenchmarkStart arms a ride (where the drag area is worked out) and
// stops it.
func BenchmarkStart(b *testing.B) {
	s, _, _ := newSession(b)
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Start("test"); err != nil {
			b.Fatal(err)
		}
		s.Stop()
	}
}
