package ride

import (
	"math"
	"testing"
	"time"

	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
)

// steady is w watts every second for d.
func steady(w float64, d time.Duration) []PowerSample {
	var ps []PowerSample
	for at := time.Duration(0); at < d; at += time.Second {
		ps = append(ps, PowerSample{At: at, PowerW: w})
	}
	return ps
}

func TestReplayMatchesLive(t *testing.T) {
	// The live loop, as a session ticks it.
	s, hub, _ := newSession(t)
	s.Start("test")
	setPower(hub, 250)
	now := time.Now()
	for ride(hub).Phase != telemetry.RideFinished {
		s.tick(now)
		now = now.Add(tickInterval)
	}
	live := ride(hub)

	got := Replay(testCourse(t), sim.DefaultParams(87, 9), 0, steady(250, 20*time.Minute))
	if !got.Finished || got.Elapsed != live.Elapsed || got.DistanceM != live.DistanceM {
		t.Errorf("replay %+v, live elapsed %v at %.1f m", got, live.Elapsed, live.DistanceM)
	}
	if math.Abs(got.AvgPowerW-250) > 0.5 || math.Abs(got.ClimbedM-live.ClimbedM) > 1e-9 {
		t.Errorf("power %.1f, climbed %.1f (live %.1f)", got.AvgPowerW, got.ClimbedM, live.ClimbedM)
	}
}

func TestReplayPauseAndRunOut(t *testing.T) {
	c, p := testCourse(t), sim.DefaultParams(87, 9)
	// Two minutes riding, a minute without records (a stop), two more.
	power := append(steady(250, 2*time.Minute), steady(250, 2*time.Minute)...)
	for i := 120; i < len(power); i++ {
		power[i].At += 3 * time.Minute
	}
	paused := Replay(c, p, 0, power)
	straight := Replay(c, p, 0, steady(250, 4*time.Minute))
	pedalled := Replay(c, p, 0, steady(250, 5*time.Minute))
	if paused.Finished || straight.Finished || pedalled.Finished {
		t.Fatal("five minutes at 250 W finished 3 km with an 8 % km")
	}
	// The clock runs through the stop, and the power runs out at the end.
	if d := paused.Elapsed - straight.Elapsed; d < time.Minute-time.Second || d > time.Minute+time.Second {
		t.Errorf("paused ride clock %v, straight %v: want a minute more", paused.Elapsed, straight.Elapsed)
	}
	// Coasting through the stop isn't riding: less far than pedalling on.
	if paused.DistanceM >= pedalled.DistanceM-100 {
		t.Errorf("paused ride reached %.0f m, pedalling on %.0f m", paused.DistanceM, pedalled.DistanceM)
	}
}

func TestReplayRollingStart(t *testing.T) {
	c, p := testCourse(t), sim.DefaultParams(87, 9)
	full := Replay(c, p, 0, steady(250, 20*time.Minute))
	last := Replay(c, p, 2000, steady(250, 20*time.Minute)) // the descent only
	if !last.Finished || last.Elapsed >= full.Elapsed/2 {
		t.Errorf("from 2 km: %+v (full ride %v)", last, full.Elapsed)
	}
	if none := Replay(c, p, 0, nil); none.Finished || none.Elapsed != 0 {
		t.Errorf("no power: %+v", none)
	}
}
