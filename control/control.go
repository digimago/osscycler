// Package control is manual trainer control outside course rides and
// workouts: a fixed target power (ERG, FE page 0x31), a fixed grade
// (simulation, 0x33) or a fixed brake level (basic resistance, 0x30).
// One thing drives the trainer at a time: manual control is refused
// while a course ride or workout runs, and they are refused while it is
// on. Releasing it hands the trainer back flat.
package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/telemetry"
)

// Trainer is the control the session needs.
type Trainer interface {
	SetTargetPower(ctx context.Context, watts float64) error
	SetGrade(ctx context.Context, gradePct float64) error
	SetResistance(ctx context.Context, pct float64) error
}

// Mode is what the trainer holds. It is telemetry's, so the state carries it.
type Mode = telemetry.ControlMode

const (
	Off   = telemetry.ControlOff
	Power = telemetry.ControlPower
	Grade = telemetry.ControlGrade
	Level = telemetry.ControlLevel
)

// Limits. ERG is capped well below the Flux's 2000 W; grades follow the
// trainer's maximum (MaxGradePct) and Zwift's steepest descents.
const (
	MinPowerW   = 25
	MaxPowerW   = 1000
	MinGradePct = -10
	MinLevelPct = 0
	MaxLevelPct = 100
)

var (
	// ErrBusy: a course ride or workout drives the trainer.
	ErrBusy = errors.New("control: a course ride or workout is under way")
	// ErrMode: not a mode that can be set.
	ErrMode = errors.New("control: unknown mode")
)

const (
	tickInterval = 250 * time.Millisecond
	// resend repeats the command in case an earlier send was lost.
	resend = 5 * time.Second
)

// Session holds the manual target, if any.
type Session struct {
	hub      *telemetry.Hub
	trainer  Trainer
	maxGrade float64
	log      *slog.Logger

	mu     sync.Mutex
	mode   Mode
	target float64
	flat   bool // released: send flat once, then leave the trainer alone
}

// New returns a session; maxGradePct is the steepest the trainer renders.
func New(hub *telemetry.Hub, trainer Trainer, maxGradePct float64, log *slog.Logger) *Session {
	return &Session{hub: hub, trainer: trainer, maxGrade: maxGradePct, log: log}
}

// Clamp keeps a target within the mode's limits.
func (s *Session) Clamp(m Mode, v float64) (float64, error) {
	switch m {
	case Power:
		return math.Max(MinPowerW, math.Min(math.Round(v), MaxPowerW)), nil
	case Grade:
		return math.Max(MinGradePct, math.Min(math.Round(v*10)/10, s.maxGrade)), nil
	case Level:
		return math.Max(MinLevelPct, math.Min(math.Round(v*2)/2, MaxLevelPct)), nil // 0x30 has 0.5 % steps
	}
	return 0, fmt.Errorf("%w %d", ErrMode, m)
}

// Set takes manual control (or changes its target) and returns the target
// in effect, clamped to the mode's limits.
func (s *Session) Set(m Mode, v float64) (float64, error) {
	v, err := s.Clamp(m, v)
	if err != nil {
		return 0, err
	}
	// A loop ride goes on meanwhile, as the road to ride on.
	if st, _ := s.hub.Latest(); (st.Ride.Phase.Active() && !st.Ride.OnLoop()) || st.Workout.Phase.Active() {
		return 0, ErrBusy
	}
	s.mu.Lock()
	changed := s.mode != m || s.target != v
	s.mode, s.target, s.flat = m, v, false
	s.mu.Unlock()
	if changed {
		s.publish(m, v)
		s.log.Info("manual control", "mode", m, "target", v)
	}
	return v, nil
}

// Release ends manual control; the trainer is set flat.
func (s *Session) Release() {
	s.mu.Lock()
	was := s.mode
	s.mode, s.target, s.flat = Off, 0, was != Off
	s.mu.Unlock()
	if was != Off {
		s.publish(Off, 0)
		s.log.Info("manual control released")
	}
}

func (s *Session) publish(m Mode, v float64) {
	s.hub.Update(func(st *telemetry.State) bool {
		st.Control = telemetry.Control{Mode: m, Target: v, Changed: st.Time}
		return true
	})
}

// Run sends the target to the trainer until ctx is done: on every change,
// and again every few seconds in case a send was lost.
func (s *Session) Run(ctx context.Context) {
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	var (
		sentMode Mode
		sentVal  float64
		sentAt   time.Time
		lastErr  string
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		m, v, flat := s.mode, s.target, s.flat
		s.mu.Unlock()
		if m == Off && !flat {
			sentMode = Off
			continue
		}
		if m == sentMode && v == sentVal && time.Since(sentAt) < resend {
			continue
		}
		var err error
		switch m {
		case Power:
			err = s.trainer.SetTargetPower(ctx, v)
		case Grade:
			err = s.trainer.SetGrade(ctx, v)
		case Level:
			err = s.trainer.SetResistance(ctx, v)
		case Off: // released: hand the trainer back flat, once
			if st, _ := s.hub.Latest(); st.Ride.OnLoop() {
				// A loop takes it straight back, at its own grade.
				s.mu.Lock()
				if s.mode == Off {
					s.flat = false
				}
				s.mu.Unlock()
				continue
			}
			err = s.trainer.SetGrade(ctx, 0)
		}
		if err != nil {
			if err.Error() != lastErr && ctx.Err() == nil {
				s.log.Warn("manual trainer command", "mode", m, "target", v, "err", err)
				lastErr = err.Error()
			}
			continue // retried next tick
		}
		lastErr = ""
		sentMode, sentVal, sentAt = m, v, time.Now()
		if m == Off {
			s.mu.Lock()
			if s.mode == Off {
				s.flat = false
			}
			s.mu.Unlock()
		}
	}
}
