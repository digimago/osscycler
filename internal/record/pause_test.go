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

// A simulated rider is never recorded: no activity opens for it, and one
// already open waits, its timer stopped, as when parked.
func TestSimulatedRiderIsNotRecorded(t *testing.T) {
	r := newRecorder(t)
	sim := func(int) telemetry.State {
		st := state(200, 90, 8)
		st.Simulated = true
		return st
	}
	play(r, []step{{60, sim}})
	if r.a != nil {
		t.Fatal("an activity opened for the simulated rider")
	}
	r2 := newRecorder(t)
	play(r2, []step{{30, func(int) telemetry.State { return state(200, 90, 8) }}, {400, sim}})
	if r2.a == nil || r2.a.running {
		t.Fatalf("the activity under way: %v (want it open, its timer stopped)", r2.a)
	}
	if got := r2.a.session.timer; got > 31*time.Second {
		t.Errorf("timer %v: the simulated minutes counted", got)
	}
}
