package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/ant"
)

// Without a stick the rider can ride a simulated rider, and stop it: the
// stick is looked for again, the made-up readings gone.
func TestSwitch(t *testing.T) {
	hub := NewHub()
	stick := NewStick(func() (*ant.Node, func(), error) { return nil, nil, errors.New("no stick") }, hub, Config{}, slog.New(slog.DiscardHandler))
	stick.Poll = 50 * time.Millisecond
	sw := NewSwitch(stick, hub)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sw.Run(ctx) }()

	waitFor(t, hub, "the stick looked for", func(st State) bool { return st.Radio.Known && !st.Radio.Present && !st.Simulated })
	if err := sw.SetGrade(ctx, 3); !errors.Is(err, ErrTrainerUnavailable) {
		t.Errorf("a grade without a trainer: %v", err)
	}

	sw.SetSimulated(true)
	waitFor(t, hub, "the simulated rider", func(st State) bool {
		return st.Simulated && st.Trainer.Sensor.Status == StatusConnected && st.Trainer.PowerW.OK && st.HeartRate.BPM.OK
	})
	if err := sw.SetGrade(ctx, 3); err != nil {
		t.Errorf("a grade for the simulated rider: %v", err)
	}

	sw.SetSimulated(false)
	waitFor(t, hub, "back to the stick", func(st State) bool {
		return !st.Simulated && st.Trainer.Sensor.Status != StatusConnected && !st.Trainer.PowerW.OK && !st.HeartRate.BPM.OK
	})
	cancel()
	if err := <-done; err != nil {
		t.Errorf("run: %v", err)
	}
}
