package ride

import (
	"math"
	"testing"
	"time"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
)

type oneGhost struct{ g *Ghost }

func (o oneGhost) Ghost(*course.Course, float64) (*Ghost, error) { return o.g, nil }

// ghostOf replays power on the test course into a ghost.
func ghostOf(t *testing.T, power []PowerSample) *Ghost {
	t.Helper()
	res := Replay(testCourse(t), sim.DefaultParams(87, 9), 0, power)
	if !res.Finished {
		t.Fatal("ghost ride didn't finish")
	}
	return &Ghost{Label: "PB", Elapsed: res.Elapsed, Trace: res.Trace}
}

func TestGhostLookups(t *testing.T) {
	g := ghostOf(t, steady(250, 20*time.Minute))
	if g.Trace[0].At != 0 || g.Trace[0].DistanceM != 0 || g.DistanceAt(g.Elapsed) < 2999.9 {
		t.Fatalf("trace from %+v to %.1f m", g.Trace[0], g.DistanceAt(g.Elapsed))
	}
	for _, d := range []float64{0, 500, 1500, 2999} {
		if back := g.DistanceAt(g.TimeAt(d)); math.Abs(back-d) > 0.01 {
			t.Errorf("DistanceAt(TimeAt(%.0f)) = %.3f", d, back)
		}
	}
	if g.TimeAt(5000) != g.Elapsed || g.DistanceAt(time.Hour) != g.Trace[len(g.Trace)-1].DistanceM {
		t.Error("past the finish")
	}
}

// race rides the test course live at power against ghost g, calling
// check each tick; it returns the final ride state.
func race(t *testing.T, g *Ghost, power func(at time.Duration) uint16, check func(telemetry.Ride)) telemetry.Ride {
	t.Helper()
	s, hub, _ := newSession(t)
	s.cfg.Ghosts = oneGhost{g}
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	if r := ride(hub); r.Ghost.Label != "PB" || r.Ghost.Gap != 0 || r.Ghost.DistanceM != 0 {
		t.Fatalf("armed ghost %+v", r.Ghost)
	}
	t0 := time.Now()
	for at := time.Duration(0); ride(hub).Phase != telemetry.RideFinished; at += tickInterval {
		setPower(hub, power(at))
		s.tick(t0.Add(at))
		if check != nil {
			check(ride(hub))
		}
		if at > time.Hour {
			t.Fatal("never finished")
		}
	}
	return ride(hub)
}

func TestRaceTheGhost(t *testing.T) {
	g := ghostOf(t, steady(250, 20*time.Minute))

	// The same effort stays level with the ghost all the way.
	same := race(t, g, func(time.Duration) uint16 { return 250 }, func(r telemetry.Ride) {
		if d := r.Ghost.Gap.Abs(); d > 50*time.Millisecond {
			t.Fatalf("same power, gap %v at %.0f m", r.Ghost.Gap, r.DistanceM)
		}
	})
	if same.Ghost.Gap.Abs() > 50*time.Millisecond {
		t.Errorf("same power finished %v off the ghost", same.Ghost.Gap)
	}

	// Harder: ahead, and the finishing gap is the difference in times.
	fast := race(t, g, func(time.Duration) uint16 { return 300 }, nil)
	if fast.Ghost.Gap >= -5*time.Second {
		t.Errorf("300 W against a 250 W ghost: gap %v", fast.Ghost.Gap)
	}
	if want := fast.Elapsed - g.Elapsed; (fast.Ghost.Gap - want).Abs() > time.Millisecond {
		t.Errorf("finish gap %v, times differ by %v", fast.Ghost.Gap, want)
	}
	if fast.Ghost.DistanceM >= fast.DistanceM {
		t.Errorf("rider ahead, yet the ghost is at %.0f m and the rider at %.0f m", fast.Ghost.DistanceM, fast.DistanceM)
	}

	// 30 s without pedalling on the flat: the bike coasts on, so it costs
	// less than 30 s, but well over nothing.
	stop := race(t, g, func(at time.Duration) uint16 {
		if at >= time.Minute && at < 90*time.Second {
			return 0
		}
		return 250
	}, nil)
	if stop.Ghost.Gap < 10*time.Second || stop.Ghost.Gap > 30*time.Second {
		t.Errorf("30 s coasting: finished %v behind the ghost", stop.Ghost.Gap)
	}
}

func TestNoGhost(t *testing.T) {
	s, hub, _ := newSession(t)
	s.cfg.Ghosts = oneGhost{nil}
	s.Start("test")
	setPower(hub, 250)
	s.tick(time.Now())
	if g := ride(hub).Ghost; g != (telemetry.RideGhost{}) {
		t.Errorf("ghost without one: %+v", g)
	}
}
