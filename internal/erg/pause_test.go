package erg

import (
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

func TestPauseHoldsTheWorkout(t *testing.T) {
	s, hub, _ := setup(t)
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	power(hub, 100)
	ride(s, &now, 20*time.Second)
	before := state(hub)
	if before.Phase != telemetry.WorkoutRunning || before.TargetW != 100 {
		t.Fatalf("not running: %+v", before)
	}

	hub.SetPaused(true)
	ride(s, &now, 10*time.Minute) // still pedalling: the pause holds anyway
	held := state(hub)
	if held.Phase != telemetry.WorkoutPaused || held.Elapsed != before.Elapsed {
		t.Errorf("paused: phase %v, elapsed %s → %s", held.Phase, before.Elapsed, held.Elapsed)
	}
	// The trainer's command (what the sender applies), not the segment's
	// target, which the heads still show.
	trainer := func() command {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.target == nil {
			return command{grade: -99}
		}
		return *s.target
	}
	if c := trainer(); c.erg || c.grade != 0 {
		t.Errorf("paused: the trainer gets %+v, want flat", c)
	}

	hub.SetPaused(false)
	ride(s, &now, 10*time.Second)
	after := state(hub)
	if c := trainer(); after.Phase != telemetry.WorkoutRunning || !c.erg || c.watts != 100 {
		t.Errorf("resumed: %+v, trainer %+v", after, c)
	}
	if d := after.Elapsed - held.Elapsed; d < 9*time.Second || d > 11*time.Second {
		t.Errorf("resumed: %s of workout in 10 s", d)
	}
}
