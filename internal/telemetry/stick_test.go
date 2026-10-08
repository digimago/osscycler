package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/fec"
)

func TestStickWaitsAndComesBack(t *testing.T) {
	hub := NewHub()
	var (
		mu      sync.Mutex
		calls   int
		plugged *fakeStick
	)
	open := func() (*ant.Node, func(), error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls <= 2 || calls == 4 { // not there at first, nor right after the unplug
			return nil, nil, errors.New("open /dev/ttyANT: no such file or directory")
		}
		node, f := newFakeStick(t)
		plugged = f
		return node, func() { node.Close() }, nil
	}
	cfg := Config{DisableHRM: true, User: fec.UserConfig{UserWeightKg: 87}}
	s := NewStick(open, hub, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- s.Run(ctx) }()

	st := waitFor(t, hub, "the stick looked for", func(st State) bool { return st.Radio.Known && !st.Radio.Present })
	if st.Radio.Error == "" {
		t.Error("no reason given for the missing stick")
	}
	if err := s.SetGrade(ctx, 3); !errors.Is(err, ErrTrainerUnavailable) {
		t.Errorf("command without a stick: %v", err)
	}
	waitFor(t, hub, "the stick found", func(st State) bool {
		return st.Radio.Present && st.Trainer.Sensor.Status == StatusSearching
	})

	// The trainer pairs; then the stick is pulled out.
	mu.Lock()
	f := plugged
	mu.Unlock()
	f.broadcast(trainerChannel, [8]byte{0x10, 25, 0, 0, 0, 0, 0, 0x30})
	waitFor(t, hub, "the trainer", func(st State) bool { return st.Trainer.Sensor.Status == StatusConnected })
	f.conn.Close()
	st = waitFor(t, hub, "the stick gone", func(st State) bool { return st.Radio.Known && !st.Radio.Present })
	if st.Trainer.Sensor.Status != StatusLost || st.Trainer.PowerW.OK {
		t.Errorf("after the unplug the trainer is %v, power %v: readings must go", st.Trainer.Sensor.Status, st.Trainer.PowerW)
	}

	// Plugged back in: running again, and the core never stopped.
	waitFor(t, hub, "the stick back", func(st State) bool { return st.Radio.Present })
	select {
	case err := <-done:
		t.Fatalf("Run returned while the stick came and went: %v", err)
	default:
	}
	if got := s.Stats().Rx; got == 0 {
		t.Error("counters lost across reopening")
	}
	// A weight set while running carries to the next opening.
	s.SetUser(ctx, fec.UserConfig{UserWeightKg: 90})
	s.mu.Lock()
	w := s.cfg.User.UserWeightKg
	s.mu.Unlock()
	if w != 90 {
		t.Errorf("user config %.0f kg", w)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}
