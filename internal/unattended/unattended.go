// Package unattended looks after a core that no screen is connected to
// (owner, 2026-10-10: quitting a screen mid-ride, or its crashing, must not
// leave a ride hanging): with something under way and no head (a TUI or
// 3D view, State.Heads) for PauseAfter, the core pauses; still alone after
// EndAfter, the ride, workout or manual control is ended and the activity
// saved. A head that comes back finds it paused, and carries on with P.
package unattended

import (
	"context"
	"log/slog"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

// Config: how long alone before pausing, and before ending.
type Config struct {
	PauseAfter, EndAfter time.Duration
}

// DefaultConfig gives a screen 30 s to come back (a restart, a crash) and
// a rider half an hour before the ride is ended.
func DefaultConfig() Config { return Config{PauseAfter: 30 * time.Second, EndAfter: 30 * time.Minute} }

// Watcher watches the hub; End ends whatever is under way and saves the
// activity.
type Watcher struct {
	Hub *telemetry.Hub
	Cfg Config
	End func(ctx context.Context)
	Log *slog.Logger

	alone time.Time // since when no head and something under way (zero: not so)
	ended bool      // ended for this time alone
}

// UnderWay is whether something drives the trainer or awaits the rider: a
// course ride armed or riding, a workout, manual control.
func UnderWay(st telemetry.State) bool {
	return st.Ride.Phase.Active() || st.Workout.Phase.Active() || st.Control.Mode != telemetry.ControlOff
}

// Run checks every second until ctx is done.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			w.step(ctx, now)
		}
	}
}

func (w *Watcher) step(ctx context.Context, now time.Time) {
	st, _ := w.Hub.Latest()
	if st.Heads > 0 || !UnderWay(st) {
		w.alone, w.ended = time.Time{}, false
		return
	}
	if w.alone.IsZero() {
		w.alone = now
		return
	}
	alone := now.Sub(w.alone)
	if alone >= w.Cfg.PauseAfter && !st.Paused {
		w.Hub.SetPaused(true)
		w.Log.Info("no screen connected: paused", "after", alone.Round(time.Second))
	}
	if alone >= w.Cfg.EndAfter && !w.ended {
		w.ended = true
		w.Log.Info("no screen connected: ending the ride and saving it", "after", alone.Round(time.Minute))
		w.End(ctx)
		w.Hub.SetPaused(false) // nothing left to hold
	}
}
