package telemetry

import (
	"context"
	"sync"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/fec"
)

// Switch is the trainer the core drives: the ANT+ stick, or a simulated
// rider (Fake) in its place while the rider asks for one (owner,
// 2026-10-10: carry on without the hardware, on made-up numbers, when it
// isn't at hand). Only one runs at a time: switching stops the one and
// clears its readings before the other starts. State.Simulated says
// which.
type Switch struct {
	stick *Stick
	fake  *Fake
	hub   *Hub

	mu     sync.Mutex
	sim    bool
	change chan struct{} // closed by SetSimulated: Run switches
}

func NewSwitch(stick *Stick, hub *Hub) *Switch {
	return &Switch{stick: stick, fake: NewFake(hub), hub: hub, change: make(chan struct{})}
}

// SetSimulated turns the simulated rider on or off.
func (s *Switch) SetSimulated(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sim == on {
		return
	}
	s.sim = on
	close(s.change)
	s.change = make(chan struct{})
}

func (s *Switch) state() (bool, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sim, s.change
}

// Run runs the stick or the simulated rider, whichever is on, until ctx
// is done.
func (s *Switch) Run(ctx context.Context) error {
	for {
		sim, change := s.state()
		s.hub.Update(func(st *State) bool {
			if st.Simulated == sim {
				return false
			}
			st.Simulated = sim
			return true
		})
		run, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		if sim {
			go func() { done <- s.fake.Run(run) }()
		} else {
			go func() { done <- s.stick.Run(run) }()
		}
		select {
		case <-ctx.Done():
			cancel()
			<-done
			return nil
		case <-change:
			cancel()
			<-done
		case err := <-done:
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		// Neither the one stopped nor the other yet: no readings, no
		// calibration, the sensors searching.
		s.hub.Update(func(st *State) bool {
			st.Trainer = Trainer{Sensor: Sensor{Status: StatusSearching}}
			st.HeartRate = HeartRate{Sensor: Sensor{Status: StatusSearching}}
			return true
		})
	}
}

func (s *Switch) current() interface {
	SetGrade(context.Context, float64) error
	SetResistance(context.Context, float64) error
	SetTargetPower(context.Context, float64) error
	SetUser(context.Context, fec.UserConfig) error
	StartSpinDown(context.Context) error
	CancelCalibration(context.Context) error
} {
	if sim, _ := s.state(); sim {
		return s.fake
	}
	return s.stick
}

func (s *Switch) SetGrade(ctx context.Context, pct float64) error {
	return s.current().SetGrade(ctx, pct)
}
func (s *Switch) SetResistance(ctx context.Context, pct float64) error {
	return s.current().SetResistance(ctx, pct)
}
func (s *Switch) SetTargetPower(ctx context.Context, w float64) error {
	return s.current().SetTargetPower(ctx, w)
}

// SetUser goes to the stick always, so the trainer has the rider's weight
// when it is back; the simulated rider has no weight to feel.
func (s *Switch) SetUser(ctx context.Context, u fec.UserConfig) error {
	return s.stick.SetUser(ctx, u)
}
func (s *Switch) StartSpinDown(ctx context.Context) error { return s.current().StartSpinDown(ctx) }
func (s *Switch) CancelCalibration(ctx context.Context) error {
	return s.current().CancelCalibration(ctx)
}

// Stats are the stick's counters.
func (s *Switch) Stats() ant.Stats { return s.stick.Stats() }
