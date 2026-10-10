package unattended

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

func TestAloneMeansPausedThenEnded(t *testing.T) {
	hub := telemetry.NewHub()
	ends := 0
	w := &Watcher{Hub: hub, Cfg: Config{PauseAfter: 30 * time.Second, EndAfter: 30 * time.Minute}, Log: slog.New(slog.DiscardHandler),
		End: func(context.Context) {
			ends++
			hub.Update(func(st *telemetry.State) bool { st.Ride.Phase = telemetry.RideAborted; return true })
		}}
	riding := func(heads int) {
		hub.Update(func(st *telemetry.State) bool { st.Ride.Phase, st.Heads = telemetry.RideRiding, heads; return true })
	}
	paused := func() bool { st, _ := hub.Latest(); return st.Paused }
	t0 := time.Now()
	at := func(d time.Duration) { w.step(context.Background(), t0.Add(d)) }

	riding(1)
	at(0)
	at(time.Hour)
	if paused() || ends != 0 {
		t.Fatal("paused or ended with a screen connected")
	}
	riding(0) // the screen quit (or crashed)
	at(time.Hour + time.Second)
	at(time.Hour + 20*time.Second)
	if paused() {
		t.Fatal("paused before PauseAfter: a screen restarting has 30 s")
	}
	at(time.Hour + 32*time.Second)
	if !paused() || ends != 0 {
		t.Fatalf("after 31 s alone: paused %v, ends %d", paused(), ends)
	}
	at(time.Hour + 31*time.Minute + 2*time.Second)
	if ends != 1 || paused() {
		t.Fatalf("after 30 min alone: ends %d, paused %v (want ended and the pause lifted)", ends, paused())
	}
	at(time.Hour + 40*time.Minute)
	if ends != 1 {
		t.Errorf("ended %d times", ends)
	}
}

func TestAScreenComingBackKeepsItPaused(t *testing.T) {
	hub := telemetry.NewHub()
	w := &Watcher{Hub: hub, Cfg: DefaultConfig(), Log: slog.New(slog.DiscardHandler), End: func(context.Context) { t.Error("ended") }}
	hub.Update(func(st *telemetry.State) bool { st.Workout.Phase, st.Heads = telemetry.WorkoutRunning, 0; return true })
	t0 := time.Now()
	w.step(context.Background(), t0)
	w.step(context.Background(), t0.Add(31*time.Second))
	hub.Update(func(st *telemetry.State) bool { st.Heads = 1; return true })
	w.step(context.Background(), t0.Add(2*time.Hour))
	if st, _ := hub.Latest(); !st.Paused {
		t.Error("a screen coming back carried on by itself: the rider carries on with P")
	}
}

func TestNothingUnderWayIsLeftAlone(t *testing.T) {
	hub := telemetry.NewHub()
	w := &Watcher{Hub: hub, Cfg: DefaultConfig(), Log: slog.New(slog.DiscardHandler), End: func(context.Context) { t.Error("ended") }}
	t0 := time.Now()
	for d := time.Duration(0); d < time.Hour; d += time.Minute {
		w.step(context.Background(), t0.Add(d))
	}
	if st, _ := hub.Latest(); st.Paused {
		t.Error("paused a core with nothing under way")
	}
}
