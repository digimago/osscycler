package control

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/digimago/osscycler/telemetry"
)

type trainer struct {
	mu   sync.Mutex
	sent []string
	fail bool
}

func (t *trainer) add(s string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fail {
		return fmt.Errorf("trainer away")
	}
	t.sent = append(t.sent, s)
	return nil
}
func (t *trainer) SetTargetPower(_ context.Context, w float64) error {
	return t.add(fmt.Sprintf("erg %.0f", w))
}
func (t *trainer) SetGrade(_ context.Context, g float64) error {
	return t.add(fmt.Sprintf("grade %.1f", g))
}
func (t *trainer) SetResistance(_ context.Context, p float64) error {
	return t.add(fmt.Sprintf("level %.1f", p))
}
func (t *trainer) all() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.sent...)
}

func newSession() (*Session, *telemetry.Hub, *trainer) {
	hub, tr := telemetry.NewHub(), &trainer{}
	return New(hub, tr, 16, slog.New(slog.NewTextHandler(io.Discard, nil))), hub, tr
}

func TestClamp(t *testing.T) {
	s, _, _ := newSession()
	for _, c := range []struct {
		m       Mode
		in, out float64
	}{
		{Power, 5, 25}, {Power, 5000, 1000}, {Power, 201.6, 202},
		{Grade, 25, 16}, {Grade, -30, -10}, {Grade, 4.26, 4.3},
		{Level, 120, 100}, {Level, -5, 0}, {Level, 33.3, 33.5},
	} {
		if got, err := s.Clamp(c.m, c.in); err != nil || got != c.out {
			t.Errorf("Clamp(%v, %v) = %v, %v; want %v", c.m, c.in, got, err, c.out)
		}
	}
	if _, err := s.Set(Off, 1); err == nil {
		t.Error("Set(Off) accepted; release with Release")
	}
}

func TestSetSendRelease(t *testing.T) {
	s, hub, tr := newSession()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	wait := func(want string) {
		t.Helper()
		for range 100 {
			if sent := tr.all(); len(sent) > 0 && sent[len(sent)-1] == want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("never sent %q: %v", want, tr.all())
	}
	if v, err := s.Set(Power, 200); err != nil || v != 200 {
		t.Fatal(v, err)
	}
	wait("erg 200")
	if st, _ := hub.Latest(); st.Control.Mode != Power || st.Control.Target != 200 {
		t.Errorf("state %+v", st.Control)
	}
	s.Set(Grade, 6)
	wait("grade 6.0")
	s.Set(Level, 40)
	wait("level 40.0")
	n := len(tr.all())
	time.Sleep(3 * tickInterval)
	if len(tr.all()) != n {
		t.Errorf("resent before %v: %v", resend, tr.all()[n:])
	}

	// Release: flat once, then the trainer is left alone.
	s.Release()
	wait("grade 0.0")
	n = len(tr.all())
	time.Sleep(3 * tickInterval)
	if len(tr.all()) != n {
		t.Errorf("kept sending after release: %v", tr.all()[n:])
	}
	if st, _ := hub.Latest(); st.Control.Mode != Off {
		t.Errorf("state after release %+v", st.Control)
	}
}

func TestRetriesWhenTrainerAway(t *testing.T) {
	s, _, tr := newSession()
	tr.fail = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	s.Set(Power, 150)
	time.Sleep(2 * tickInterval)
	tr.mu.Lock()
	tr.fail = false
	tr.mu.Unlock()
	for range 100 {
		if len(tr.all()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if sent := tr.all(); len(sent) != 1 || sent[0] != "erg 150" {
		t.Errorf("after the trainer came back: %v", sent)
	}
}

func TestOneDriverAtATime(t *testing.T) {
	s, hub, _ := newSession()
	hub.Update(func(st *telemetry.State) bool { st.Ride.Phase = telemetry.RideRiding; return true })
	if _, err := s.Set(Grade, 5); err != ErrBusy {
		t.Errorf("during a course ride: %v", err)
	}
	hub.Update(func(st *telemetry.State) bool {
		st.Ride.Phase, st.Workout.Phase = telemetry.RideNone, telemetry.WorkoutPaused
		return true
	})
	if _, err := s.Set(Power, 200); err != ErrBusy {
		t.Errorf("during a workout: %v", err)
	}
}

func TestOnALoop(t *testing.T) {
	s, hub, tr := newSession()
	hub.Update(func(st *telemetry.State) bool {
		st.Ride.Phase, st.Ride.Loop = telemetry.RideRiding, true
		return true
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	// A loop ride goes on under manual control...
	if _, err := s.Set(Power, 180); err != nil {
		t.Fatalf("set on a loop: %v", err)
	}
	for range 100 {
		if len(tr.all()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// ...and takes the trainer straight back on release: no flat.
	s.Release()
	time.Sleep(4 * tickInterval)
	if sent := tr.all(); len(sent) != 1 || sent[0] != "erg 180" {
		t.Errorf("sent %v", sent)
	}
	// A course ride still keeps manual control off.
	hub.Update(func(st *telemetry.State) bool { st.Ride.Loop = false; return true })
	if _, err := s.Set(Power, 180); err != ErrBusy {
		t.Errorf("set during a course ride: %v", err)
	}
}
