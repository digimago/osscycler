package record

import (
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

func TestPauseKeepsTheActivity(t *testing.T) {
	r := newRecorder(t)
	parked := func(int) telemetry.State {
		st := state(120, 40, 3) // turning the pedals over now and then
		st.Paused = true
		return st
	}
	play(r, []step{
		{60, func(int) telemetry.State { return state(200, 90, 8) }},
		{20 * 60, parked}, // twenty minutes: past EndAfter (5 min)
		{30, func(int) telemetry.State { return state(200, 90, 8) }},
	})
	if r.a == nil {
		t.Fatal("the activity ended during the pause")
	}
	r.publish()
	st, _ := r.hub.Latest()
	// The timer stops as the pause begins (not PauseAfter later, as when
	// the rider just stands still) and runs on after it: the seconds
	// between the 60 steps before (59) and the 30 after (29).
	if d := st.Recording.Timer; d != 88*time.Second {
		t.Errorf("timer %s, want 88 s of riding (91 is the 3 s of auto-pause: the pause didn't stop it at once)", d)
	}
}

func TestAPausedRideIsMarkedInTheResults(t *testing.T) {
	r := newRecorder(t)
	paused := func(phase telemetry.RidePhase) func(int) telemetry.State {
		return func(i int) telemetry.State {
			st := onCourse(phase)(i)
			st.Ride.Paused = true
			return st
		}
	}
	play(r, []step{
		{60, paused(telemetry.RideRiding)},
		{5, paused(telemetry.RideFinished)},
	})
	rs, err := ReadResults(r.cfg.Dir)
	if err != nil || len(rs) != 1 {
		t.Fatalf("results %v, %v", rs, err)
	}
	if !rs[0].Paused {
		t.Errorf("the result of a paused ride isn't marked: %+v", rs[0])
	}
}
