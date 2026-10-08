package ride

import (
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

func loopSession(t *testing.T, ghosts GhostSource) (*Session, *telemetry.Hub, *course.Course) {
	t.Helper()
	hub := telemetry.NewHub()
	cfg := DefaultConfig(sim.DefaultParams(87, 9))
	cfg.Ghosts = ghosts
	cfg.StartDistanceM = 150 // ignored on a loop: laps count from the line
	oval := course.Tracks()[0]
	s := NewSession(hub, &recordingTrainer{}, []*course.Course{oval, testCourse(t)}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s, hub, oval
}

// pedal ticks the session for d at w watts from t0 on; it returns the time
// reached.
func pedal(s *Session, hub *telemetry.Hub, t0 time.Time, d time.Duration, w uint16, each func(telemetry.Ride)) time.Time {
	setPower(hub, w)
	at := t0
	for end := t0.Add(d); at.Before(end); at = at.Add(tickInterval) {
		s.tick(at)
		if each != nil {
			each(ride(hub))
		}
	}
	return at
}

func TestLoopLaps(t *testing.T) {
	s, hub, oval := loopSession(t, nil)
	if err := s.Start(oval.ID); err != nil {
		t.Fatal(err)
	}
	if r := ride(hub); !r.Loop || r.Lap != 1 || r.StartDistanceM != 0 || r.DistanceM != 0 {
		t.Fatalf("armed: %+v", r)
	}
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var laps []telemetry.Lap
	pedal(s, hub, t0, 5*time.Minute, 250, func(r telemetry.Ride) {
		if r.Phase != telemetry.RideRiding || r.DistanceM < 0 || r.DistanceM >= oval.Distance {
			t.Fatalf("lap %d: phase %v at %.1f m", r.Lap, r.Phase, r.DistanceM)
		}
		if l := r.LastLap; l.N > 0 && (len(laps) == 0 || laps[len(laps)-1].N != l.N) {
			laps = append(laps, l)
			if r.Lap != l.N+1 {
				t.Errorf("lap %d done, lap %d under way", l.N, r.Lap)
			}
		}
	})
	if len(laps) < 5 {
		t.Fatalf("%d laps in 5 minutes at 250 W", len(laps))
	}
	// The first lap starts from a standstill; the flying laps after it are
	// faster, and all alike at a steady power.
	first, second := laps[0].Elapsed, laps[1].Elapsed
	if second >= first || laps[1].StartSpeedMPS < 8 || laps[0].StartSpeedMPS != 0 {
		t.Errorf("lap 1 %v, lap 2 %v entered at %.1f m/s", first, second, laps[1].StartSpeedMPS)
	}
	// Up to speed by lap 3 (a rider and bike of 96 kg take a while).
	if d := (laps[3].Elapsed - laps[2].Elapsed).Abs(); d > 100*time.Millisecond {
		t.Errorf("flying laps %v and %v", laps[2].Elapsed, laps[3].Elapsed)
	}
	if math.Abs(laps[1].AvgPowerW-250) > 1 {
		t.Errorf("lap 2 average %.1f W", laps[1].AvgPowerW)
	}
	// Lap times add up to the ride clock (up to the lap under way).
	r := ride(hub)
	var sum time.Duration
	for _, l := range laps {
		sum += l.Elapsed
	}
	if d := (r.Elapsed - r.LapElapsed - sum).Abs(); d > time.Millisecond {
		t.Errorf("laps add up to %v, clock %v minus lap %v", sum, r.Elapsed, r.LapElapsed)
	}
	// The fastest lap so far is the ghost, raced on the lap's own clock.
	if r.BestLap.N < 2 || !strings.HasPrefix(r.Ghost.Label, "PB lap ") || r.Ghost.Elapsed != r.BestLap.Elapsed {
		t.Errorf("best lap %d (%v), ghost %q %v", r.BestLap.N, r.BestLap.Elapsed, r.Ghost.Label, r.Ghost.Elapsed)
	}
	if r.Ghost.Gap.Abs() > 200*time.Millisecond {
		t.Errorf("riding the same power as the ghost, %v off it", r.Ghost.Gap)
	}

	// Stopping ends the loop ride; the trainer goes flat.
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if r := ride(hub); r.Phase != telemetry.RideAborted || s.target == nil || *s.target != 0 {
		t.Errorf("after stop: %v, target %v", r.Phase, s.target)
	}
}

func TestLoopYieldsTheTrainer(t *testing.T) {
	s, hub, oval := loopSession(t, nil)
	working := func(on bool) {
		hub.Update(func(st *telemetry.State) bool {
			st.Workout.Phase = map[bool]telemetry.WorkoutPhase{true: telemetry.WorkoutRunning}[on]
			return true
		})
	}
	working(true)
	// A course ride waits for the workout; a loop rides alongside it.
	if err := s.Start("test"); err != ErrRideActive {
		t.Errorf("course ride during a workout: %v", err)
	}
	if err := s.Start(oval.ID); err != nil {
		t.Fatalf("loop during a workout: %v", err)
	}
	if r := ride(hub); !r.Yielded || s.target != nil {
		t.Fatalf("yielded %v, target %v", r.Yielded, s.target)
	}
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	at := pedal(s, hub, t0, 30*time.Second, 200, nil)
	if r := ride(hub); r.DistanceM < 100 || s.target != nil {
		t.Errorf("during the workout: %.0f m, target %v", r.DistanceM, s.target)
	}
	// The workout ends: the loop's grade drives the trainer again.
	working(false)
	pedal(s, hub, at, time.Second, 200, nil)
	if r := ride(hub); r.Yielded || s.target == nil {
		t.Errorf("after the workout: yielded %v, target %v", r.Yielded, s.target)
	}
	// Stopping during a workout leaves the trainer to it.
	working(true)
	pedal(s, hub, at.Add(time.Second), time.Second, 200, nil)
	_ = s.Stop()
	if s.target != nil {
		t.Errorf("stopping during a workout set the trainer to %v", *s.target)
	}
}

func TestLoopKeepsAChosenGhost(t *testing.T) {
	slow := &Ghost{Label: "7 Oct 18:30", Elapsed: 5 * time.Minute,
		Trace: []TracePoint{{0, 0}, {5 * time.Minute, 400}}}
	s, hub, oval := loopSession(t, oneGhost{g: slow})
	if err := s.StartAgainst(oval.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	pedal(s, hub, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), 2*time.Minute, 250, nil)
	if r := ride(hub); r.LastLap.N < 2 || r.Ghost.Label != slow.Label {
		t.Errorf("after %d laps the ghost is %q", r.LastLap.N, r.Ghost.Label)
	}
}

func TestReplayLap(t *testing.T) {
	s, hub, oval := loopSession(t, nil)
	_ = s.Start(oval.ID)
	pedal(s, hub, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), 2*time.Minute, 250, nil)
	lap2 := ride(hub).BestLap
	if lap2.N != 2 {
		t.Fatalf("best lap %d", lap2.N)
	}
	// Re-ridden at the speed it began with, the lap takes as long.
	res := Replay(oval, sim.DefaultParams(87, 9), 0, lap2.StartSpeedMPS, steady(250, 2*time.Minute))
	if !res.Finished || (res.Elapsed-lap2.Elapsed).Abs() > 300*time.Millisecond || res.DistanceM != oval.Distance {
		t.Errorf("replayed lap: finished %v in %v at %.0f m, live %v", res.Finished, res.Elapsed, res.DistanceM, lap2.Elapsed)
	}
	if last := res.Trace[len(res.Trace)-1]; last.At != res.Elapsed || last.DistanceM != oval.Distance {
		t.Errorf("trace ends at %+v", last)
	}
}
