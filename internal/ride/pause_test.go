package ride

import (
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

func TestPauseHoldsTheRideAndItsClock(t *testing.T) {
	s, hub, _ := newSession(t)
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	setPower(hub, 250)
	now := time.Now()
	for range 1200 { // 5 minutes into the climb
		now = now.Add(tickInterval)
		s.tick(now)
	}
	before := ride(hub)
	if before.DistanceM < 1100 || before.TrainerGradePct < 5 {
		t.Fatalf("not on the climb yet: %.0f m, trainer %.1f %%", before.DistanceM, before.TrainerGradePct)
	}

	hub.SetPaused(true)
	for range 4 * 600 { // ten minutes for a coffee, still pedalling a little
		now = now.Add(tickInterval)
		s.tick(now)
	}
	held := ride(hub)
	if math.Abs(held.DistanceM-before.DistanceM) > 1e-9 || held.SpeedMPS != 0 {
		t.Errorf("paused: moved from %.1f to %.1f m at %.1f m/s", before.DistanceM, held.DistanceM, held.SpeedMPS)
	}
	if held.Elapsed > before.Elapsed+tickInterval {
		t.Errorf("paused: the clock ran on, %s → %s", before.Elapsed, held.Elapsed)
	}
	if held.TrainerGradePct != 0 || !held.Paused {
		t.Errorf("paused: trainer %.1f %% (want flat), paused %v", held.TrainerGradePct, held.Paused)
	}

	hub.SetPaused(false)
	for range 40 { // 10 s on
		now = now.Add(tickInterval)
		s.tick(now)
	}
	after := ride(hub)
	if after.DistanceM <= held.DistanceM || after.TrainerGradePct < 5 {
		t.Errorf("resumed: %.1f m (was %.1f), trainer %.1f %%", after.DistanceM, held.DistanceM, after.TrainerGradePct)
	}
	if d := after.Elapsed - held.Elapsed; d < 9*time.Second || d > 11*time.Second {
		t.Errorf("resumed: the clock moved %s in 10 s of riding (the pause must not count)", d)
	}
	if !after.Paused {
		t.Error("a ride that was paused must stay marked")
	}
}

func TestPauseWhileArmedWaits(t *testing.T) {
	s, hub, _ := newSession(t)
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	hub.SetPaused(true)
	setPower(hub, 200)
	now := time.Now()
	for range 20 {
		now = now.Add(tickInterval)
		s.tick(now)
	}
	if r := ride(hub); r.Phase != telemetry.RideArmed || r.Paused {
		t.Errorf("paused before the start: phase %v, paused %v (want armed, not a paused ride)", r.Phase, r.Paused)
	}
	hub.SetPaused(false)
	now = now.Add(tickInterval)
	s.tick(now)
	if r := ride(hub); r.Phase != telemetry.RideRiding || r.Paused {
		t.Errorf("resumed: phase %v, paused %v", r.Phase, r.Paused)
	}
}

func TestAPausedLapIsNeitherBestNorGhost(t *testing.T) {
	hub := telemetry.NewHub()
	oval := course.Tracks()[0]
	if oval.ID != "oval-400" {
		t.Fatalf("track 0 is %s", oval.ID)
	}
	cfg := DefaultConfig(sim.DefaultParams(80, 9))
	s := NewSession(hub, &recordingTrainer{}, []*course.Course{oval}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(oval.ID); err != nil {
		t.Fatal(err)
	}
	setPower(hub, 300)
	now := time.Now()
	lap := func() telemetry.Ride {
		n := ride(hub).Lap
		for ride(hub).Lap == n {
			now = now.Add(tickInterval)
			s.tick(now)
		}
		return ride(hub)
	}
	// Lap 1 with a pause in it; lap 2 without.
	for range 40 {
		now = now.Add(tickInterval)
		s.tick(now)
	}
	hub.SetPaused(true)
	for range 400 {
		now = now.Add(tickInterval)
		s.tick(now)
	}
	hub.SetPaused(false)
	r := lap()
	if !r.LastLap.Paused || r.BestLap.N != 0 {
		t.Errorf("after the paused lap: last %+v, best %+v (want paused, no best)", r.LastLap, r.BestLap)
	}
	if g := r.Ghost; g.Label != "" {
		t.Errorf("a paused lap became the ghost: %+v", g)
	}
	r = lap()
	if r.LastLap.Paused || r.BestLap.N != 2 || r.Ghost.Label != "PB lap 2" {
		t.Errorf("after a clean lap: last %+v, best %+v, ghost %q", r.LastLap, r.BestLap, r.Ghost.Label)
	}
}
