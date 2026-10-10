package control

import (
	"context"
	"testing"
	"time"
)

func TestPauseGoesFlatAndBack(t *testing.T) {
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
	s.Set(Power, 220)
	wait("erg 220")
	hub.SetPaused(true)
	wait("grade 0.0")
	n := len(tr.all())
	time.Sleep(3 * tickInterval)
	if len(tr.all()) != n {
		t.Errorf("paused: kept sending %v", tr.all()[n:])
	}
	if st, _ := hub.Latest(); st.Control.Mode != Power || st.Control.Target != 220 {
		t.Errorf("paused: the control itself changed: %+v", st.Control)
	}
	hub.SetPaused(false)
	wait("erg 220")
}
