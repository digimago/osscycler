// Package erg runs structured workouts in ERG mode: it keeps the workout
// clock, turns FTP-relative targets into watts and holds the trainer there.
package erg

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/telemetry"
	"github.com/digimago/osscycler/workout"
)

var (
	ErrActive = errors.New("erg: a workout, ride or manual control is already under way")
	ErrIdle   = errors.New("erg: no workout under way")
)

// Trainer is the control the session needs: ERG for targets, grade for
// free-ride segments and for handing the trainer back flat.
type Trainer interface {
	SetTargetPower(ctx context.Context, watts float64) error
	SetGrade(ctx context.Context, gradePct float64) error
}

type Config struct {
	FTPW         float64
	IntensityPct float64 // 100 = as written
	// PauseAfter is how long the clock keeps running once the rider stops
	// pedalling, before the workout pauses.
	PauseAfter time.Duration
}

func DefaultConfig(ftp float64) Config {
	return Config{FTPW: ftp, IntensityPct: 100, PauseAfter: 15 * time.Second}
}

const (
	tickInterval = 250 * time.Millisecond
	resend       = 5 * time.Second // repeat an unchanged command in case one was lost
	messageShown = 10 * time.Second

	MinIntensity, MaxIntensity = 50.0, 150.0
	MinFTP, MaxFTP             = 50.0, 600.0
)

// command is what the trainer should be doing.
type command struct {
	erg   bool
	watts float64 // when erg
	grade float64 // otherwise
}

type Session struct {
	hub     *telemetry.Hub
	trainer Trainer
	lib     *workout.Library
	log     *slog.Logger

	mu     sync.Mutex
	cfg    Config
	run    *run
	target *command // nil: leave the trainer alone
}

type run struct {
	id        string
	w         *workout.Workout
	segs      []workout.Segment
	blockAt   []time.Duration // start of each block in the timeline
	phase     telemetry.WorkoutPhase
	elapsed   time.Duration
	last      time.Time
	lastPedal time.Time
	energyJ   float64
	riddenS   float64
}

func NewSession(hub *telemetry.Hub, trainer Trainer, lib *workout.Library, cfg Config, log *slog.Logger) *Session {
	cfg.FTPW = clamp(cfg.FTPW, MinFTP, MaxFTP)
	cfg.IntensityPct = clamp(cfg.IntensityPct, MinIntensity, MaxIntensity)
	s := &Session{hub: hub, trainer: trainer, lib: lib, cfg: cfg, log: log}
	s.publishSettings()
	return s
}

func (s *Session) Library() *workout.Library { return s.lib }

// Start arms a workout from the library; the clock starts at the first
// pedal stroke.
func (s *Session) Start(id string) error {
	w, err := s.lib.Get(id)
	if err != nil {
		return err
	}
	// A loop ride goes on during the workout, as the road to ride on.
	if st, _ := s.hub.Latest(); (st.Ride.Phase.Active() && !st.Ride.OnLoop()) || st.Control.Mode != telemetry.ControlOff {
		return ErrActive
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil && s.run.phase.Active() {
		return ErrActive
	}
	segs := w.Timeline()
	r := &run{id: id, w: w, segs: segs, phase: telemetry.WorkoutArmed, blockAt: make([]time.Duration, len(w.Blocks))}
	for i := len(segs) - 1; i >= 0; i-- {
		r.blockAt[segs[i].Block] = segs[i].Start
	}
	s.run = r
	s.target = s.commandAt(r)
	s.publish(r)
	s.log.Info("workout armed", "workout", w.Name, "duration", w.Duration().Round(time.Second), "ftp_w", s.cfg.FTPW)
	return nil
}

// Stop aborts a workout under way, or dismisses a finished one. The
// trainer is handed back flat.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run
	if r == nil {
		return nil
	}
	s.target = &command{grade: 0}
	if r.phase.Active() {
		r.phase = telemetry.WorkoutAborted
		s.publish(r)
		s.log.Info("workout aborted", "workout", r.w.Name, "elapsed", r.elapsed.Round(time.Second))
	} else {
		s.hub.Update(func(st *telemetry.State) bool {
			st.Workout = telemetry.Workout{Changed: st.Time, FTPW: s.cfg.FTPW, IntensityPct: s.cfg.IntensityPct}
			return true
		})
	}
	s.run = nil
	return nil
}

// Skip jumps to the start of the next segment.
func (s *Session) Skip() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run
	if r == nil || !r.phase.Active() {
		return ErrIdle
	}
	i, _, _ := workout.At(r.segs, r.elapsed)
	if i+1 < len(r.segs) {
		r.elapsed = r.segs[i+1].Start
	} else {
		r.elapsed = r.w.Duration()
	}
	s.log.Info("segment skipped", "workout", r.w.Name, "at", r.elapsed.Round(time.Second))
	s.advance(r)
	return nil
}

// SetIntensity scales every target (100 = as written) and returns the value
// in effect after clamping.
func (s *Session) SetIntensity(pct float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.IntensityPct = clamp(math.Round(pct), MinIntensity, MaxIntensity)
	s.retarget()
	s.log.Info("intensity", "percent", s.cfg.IntensityPct)
	return s.cfg.IntensityPct
}

// SetFTP changes the FTP targets are scaled by and returns the value in
// effect after clamping.
func (s *Session) SetFTP(watts float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.FTPW = clamp(math.Round(watts), MinFTP, MaxFTP)
	s.retarget()
	s.log.Info("ftp", "watts", s.cfg.FTPW)
	return s.cfg.FTPW
}

func (s *Session) retarget() {
	if r := s.run; r != nil && r.phase.Active() {
		s.target = s.commandAt(r)
		s.publish(r)
	} else {
		s.publishSettings()
	}
}

// Run advances the workout and drives the trainer until ctx is done.
func (s *Session) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { s.send(ctx) })
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case now := <-tick.C:
			s.tick(now)
		}
	}
}

func (s *Session) tick(now time.Time) {
	st, _ := s.hub.Latest()
	power := 0.0
	if st.Trainer.PowerW.OK {
		power = float64(st.Trainer.PowerW.V)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run
	if r == nil || !r.phase.Active() {
		return
	}
	pedalling := power > 0
	switch r.phase {
	case telemetry.WorkoutArmed:
		if !pedalling {
			return
		}
		r.phase, r.last, r.lastPedal = telemetry.WorkoutRunning, now, now
		s.log.Info("workout started", "workout", r.w.Name)
	case telemetry.WorkoutPaused:
		r.last = now
		if !pedalling {
			return
		}
		r.phase, r.lastPedal = telemetry.WorkoutRunning, now
		s.log.Info("workout resumed", "workout", r.w.Name, "at", r.elapsed.Round(time.Second))
	}
	dt := min(now.Sub(r.last), time.Second) // a stalled loop doesn't skip workout time
	r.last = now
	if pedalling {
		r.lastPedal = now
	} else if now.Sub(r.lastPedal) > s.cfg.PauseAfter {
		r.phase = telemetry.WorkoutPaused
		s.log.Info("workout paused", "workout", r.w.Name, "at", r.elapsed.Round(time.Second))
		s.publish(r)
		return
	}
	r.elapsed += dt
	r.energyJ += power * dt.Seconds()
	r.riddenS += dt.Seconds()
	s.advance(r)
}

// advance updates the trainer command and the published state for the
// current elapsed time, finishing the workout at its end.
func (s *Session) advance(r *run) {
	if r.elapsed >= r.w.Duration() {
		r.elapsed = r.w.Duration()
		r.phase = telemetry.WorkoutFinished
		s.target = &command{grade: 0}
		s.log.Info("workout finished", "workout", r.w.Name, "avg_power_w", math.Round(r.avgPower()))
	} else {
		s.target = s.commandAt(r)
	}
	s.publish(r)
}

func (s *Session) commandAt(r *run) *command {
	i, frac, ok := workout.At(r.segs, r.elapsed)
	if !ok || r.segs[i].Kind == workout.KindFree {
		return &command{grade: 0}
	}
	return &command{erg: true, watts: s.watts(frac)}
}

func (s *Session) watts(frac float64) float64 {
	return workout.Watts(frac, s.cfg.FTPW, s.cfg.IntensityPct/100)
}

func (r *run) avgPower() float64 {
	if r.riddenS == 0 {
		return 0
	}
	return r.energyJ / r.riddenS
}

func (s *Session) publish(r *run) {
	w := telemetry.Workout{
		Phase:        r.phase,
		ID:           r.id,
		Name:         r.w.Name,
		Duration:     r.w.Duration(),
		Elapsed:      r.elapsed,
		FTPW:         s.cfg.FTPW,
		IntensityPct: s.cfg.IntensityPct,
		AvgPowerW:    r.avgPower(),
	}
	if i, frac, ok := workout.At(r.segs, r.elapsed); ok {
		seg := r.segs[i]
		w.Segment, w.SegmentLabel = i, seg.Label
		w.SegmentRemaining = seg.Start + seg.Duration - r.elapsed
		w.Free = seg.Kind == workout.KindFree
		if !w.Free {
			w.TargetW = s.watts(frac)
		}
		w.TargetCadence = seg.Cadence
		if i+1 < len(r.segs) {
			next := r.segs[i+1]
			w.NextLabel, w.NextFree = next.Label, next.Kind == workout.KindFree
			if !w.NextFree {
				w.NextTargetW = s.watts(next.From)
			}
		}
		w.Message = r.message(seg.Block)
	}
	s.hub.Update(func(st *telemetry.State) bool {
		w.Changed = st.Workout.Changed
		if st.Workout.Phase != r.phase || st.Workout.ID != r.id {
			w.Changed = st.Time
		}
		st.Workout = w
		return true
	})
}

// message returns the text event of the block that is currently showing.
func (r *run) message(block int) string {
	b := r.w.Blocks[block]
	into := r.elapsed - r.blockAt[block]
	for i := len(b.Texts) - 1; i >= 0; i-- {
		t := b.Texts[i]
		if into >= t.Offset && into < t.Offset+messageShown {
			return t.Message
		}
	}
	return ""
}

func (s *Session) publishSettings() {
	ftp, pct := s.cfg.FTPW, s.cfg.IntensityPct
	s.hub.Update(func(st *telemetry.State) bool {
		st.Workout.FTPW, st.Workout.IntensityPct = ftp, pct
		return true
	})
}

// send applies the latest command, outside the tick loop so a slow
// acknowledgement never stalls the clock.
func (s *Session) send(ctx context.Context) {
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	var (
		sent    *command
		sentAt  time.Time
		lastErr string
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		var want *command
		if s.target != nil {
			c := *s.target
			want = &c
		}
		active := s.run != nil && s.run.phase.Active()
		s.mu.Unlock()
		if want == nil {
			sent = nil
			continue
		}
		same := sent != nil && sent.erg == want.erg &&
			math.Abs(sent.watts-want.watts) < 1 && math.Abs(sent.grade-want.grade) < 0.1
		if same && !(active && time.Since(sentAt) > resend) {
			continue
		}
		if st, _ := s.hub.Latest(); !want.erg && st.Ride.OnLoop() {
			// On a loop its grade applies in free segments, and after the
			// workout the loop takes the trainer straight back.
			sent = nil
			if !active {
				s.mu.Lock()
				if s.run == nil || !s.run.phase.Active() {
					s.target = nil
				}
				s.mu.Unlock()
			}
			continue
		}
		var err error
		if want.erg {
			err = s.trainer.SetTargetPower(ctx, want.watts)
		} else {
			err = s.trainer.SetGrade(ctx, want.grade)
		}
		if err != nil {
			if err.Error() != lastErr && ctx.Err() == nil {
				s.log.Warn("workout trainer command", "erg", want.erg, "watts", want.watts, "err", err)
				lastErr = err.Error()
			}
			continue
		}
		lastErr, sent, sentAt = "", want, time.Now()
		if !active && !want.erg {
			// Workout over and trainer flat: hand the trainer back.
			s.mu.Lock()
			if s.run == nil || !s.run.phase.Active() {
				s.target = nil
			}
			s.mu.Unlock()
		}
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(v, hi)) }
