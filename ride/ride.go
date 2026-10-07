// Package ride runs course rides: it moves a simulated rider along a
// course using the trainer's power, keeps the clock for a timed effort, and
// sends the trainer the grade ahead.
package ride

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
)

var (
	ErrUnknownCourse = errors.New("ride: unknown course")
	ErrRideActive    = errors.New("ride: a ride, workout or manual control is already under way")
)

// Trainer is the control the session needs from the trainer.
type Trainer interface {
	SetGrade(ctx context.Context, gradePct float64) error
}

type Config struct {
	Params sim.Params
	// Difficulty scales climbs sent to the trainer (0..1). Riding time
	// always uses the real grade.
	Difficulty float64
	// MaxGradePct is the steepest grade the trainer can apply.
	MaxGradePct float64
	// Lookahead sends the grade this far ahead (at current speed), so the
	// trainer's response lag lines up with the course.
	Lookahead time.Duration
	// StartDistanceM starts rides this far into the course, with a rolling
	// start, to practise a section (and for the demo). Clamped to leave
	// some course to ride.
	StartDistanceM float64
	// Ghosts, if set, gives each ride an earlier one to race (the PB).
	Ghosts GhostSource
}

// rollingStartW is the power whose steady speed a rolling start begins at.
const rollingStartW = 200

// minRideM is the least course left after a start offset.
const minRideM = 100

// DefaultConfig suits a Tacx Flux 2 (16 % maximum) with Zwift's default
// trainer difficulty of 50 %.
func DefaultConfig(params sim.Params) Config {
	return Config{Params: params, Difficulty: 0.5, MaxGradePct: 16, Lookahead: time.Second}
}

const (
	tickInterval = 250 * time.Millisecond
	// gradeResend repeats the trainer grade even when it hasn't changed, in
	// case an earlier send was lost.
	gradeResend = 5 * time.Second
	// gradeEpsilon is the smallest change worth sending (FE-C resolution is
	// 0.01 %; the feel threshold is far coarser).
	gradeEpsilon = 0.1
)

// Session holds the course library and at most one ride.
type Session struct {
	hub     *telemetry.Hub
	trainer Trainer
	cfg     Config
	log     *slog.Logger
	courses []*course.Course

	mu  sync.Mutex
	run *run // nil when no ride is armed or under way
	// target is the trainer grade the sender should apply; nil leaves the
	// trainer alone (no ride, so other controls aren't fought over).
	target *float64
}

type run struct {
	c       *course.Course
	rider   sim.Rider
	phase   telemetry.RidePhase
	start   time.Time
	last    time.Time
	energyJ float64
	climbed float64
	lastEle float64
	startM  float64 // where on the course the ride began
	ghost   *Ghost  // nil: nothing to race
}

func NewSession(hub *telemetry.Hub, trainer Trainer, courses []*course.Course, cfg Config, log *slog.Logger) *Session {
	cfg.Difficulty = clampDifficulty(cfg.Difficulty)
	s := &Session{hub: hub, trainer: trainer, cfg: cfg, log: log, courses: courses}
	hub.Update(func(st *telemetry.State) bool {
		st.Ride.DifficultyPct = cfg.Difficulty * 100
		return true
	})
	return s
}

func clampDifficulty(d float64) float64 { return math.Max(0, math.Min(d, 1)) }

// SetDifficulty changes the share of climbs the trainer applies (0..100 %)
// and returns the value in effect. A ride under way feels it at the next
// tick; the riding time keeps using the real grade.
func (s *Session) SetDifficulty(pct float64) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Difficulty = clampDifficulty(pct / 100)
	pct = s.cfg.Difficulty * 100
	if r := s.run; r != nil && r.phase.Active() {
		g := s.trainerGrade(r.c, r.rider.DistanceM, r.rider.SpeedMPS)
		s.target = &g
	}
	s.hub.Update(func(st *telemetry.State) bool {
		st.Ride.DifficultyPct = pct
		if s.target != nil && st.Ride.Phase.Active() {
			st.Ride.TrainerGradePct = *s.target
		}
		return true
	})
	s.log.Info("difficulty", "percent", round(pct, 0))
	return pct
}

func (s *Session) Courses() []*course.Course { return s.courses }

// SetMass sets rider plus bike for rides started from now on; a ride
// under way keeps the mass it started with, so its time stays fair.
func (s *Session) SetMass(kg float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Params.MassKg = kg
}

// SetCdA sets the drag area for rides started from now on, as SetMass.
func (s *Session) SetCdA(cda float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Params.CdA = cda
}

// Params is the simulation rides started now get.
func (s *Session) Params() sim.Params {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Params
}

func (s *Session) find(id string) *course.Course {
	for _, c := range s.courses {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// Start arms a ride on the course, racing the personal best; the clock
// starts at the first pedal stroke. The trainer gets the opening grade
// straight away.
func (s *Session) Start(courseID string) error { return s.start(courseID, nil) }

// StartAgainst arms a ride racing an earlier ride on the course, the one
// that finished at finished: from where that one started, with it as the
// ghost.
func (s *Session) StartAgainst(courseID string, finished time.Time) error {
	return s.start(courseID, &finished)
}

func (s *Session) start(courseID string, against *time.Time) error {
	c := s.find(courseID)
	if c == nil {
		return ErrUnknownCourse
	}
	if st, _ := s.hub.Latest(); st.Workout.Phase.Active() || st.Control.Mode != telemetry.ControlOff {
		return ErrRideActive // a workout or manual control has the trainer
	}
	s.mu.Lock()
	start := math.Max(0, math.Min(s.cfg.StartDistanceM, c.Distance-minRideM))
	ghosts := s.cfg.Ghosts
	s.mu.Unlock()
	// Loading a ghost reads and replays a recording: not under the lock.
	var ghost *Ghost
	switch {
	case against != nil && ghosts == nil:
		return ErrUnknownRide
	case against != nil:
		var err error
		if ghost, start, err = ghosts.Race(c, *against); err != nil {
			return err
		}
	case ghosts != nil:
		var err error
		if ghost, err = ghosts.Ghost(c, start); err != nil {
			s.log.Warn("no ghost for this ride", "course", c.Name, "err", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != nil && s.run.phase.Active() {
		return ErrRideActive
	}
	ele, grade := c.At(start)
	r := &run{c: c, rider: sim.Rider{Params: s.cfg.Params, DistanceM: start}, phase: telemetry.RideArmed, lastEle: ele, startM: start, ghost: ghost}
	if start > 0 {
		r.rider.SpeedMPS = s.cfg.Params.SteadySpeed(rollingStartW, grade)
	}
	s.run = r
	g := s.trainerGrade(c, start, r.rider.SpeedMPS)
	s.target = &g
	s.publish(r, time.Time{})
	attrs := []any{"course", c.Name, "distance_km", round(c.Distance/1000, 2), "gain_m", round(c.Gain, 0), "start_km", round(start/1000, 2)}
	if ghost != nil {
		attrs = append(attrs, "ghost", ghost.Label, "ghost_time", ghost.Elapsed.Round(100*time.Millisecond))
	}
	s.log.Info("ride armed", attrs...)
	return nil
}

// Stop aborts a ride under way, or dismisses a finished one. The trainer
// is set flat.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run
	if r == nil {
		return nil
	}
	flat := 0.0
	s.target = &flat
	if r.phase.Active() {
		r.phase = telemetry.RideAborted
		s.publish(r, time.Now())
		s.log.Info("ride aborted", "course", r.c.Name, "distance_km", round(r.rider.DistanceM/1000, 2))
	} else {
		s.hub.Update(func(st *telemetry.State) bool {
			st.Ride = telemetry.Ride{Changed: st.Time, DifficultyPct: s.cfg.Difficulty * 100}
			return true
		})
	}
	s.run = nil
	return nil
}

// Run advances the ride and drives the trainer until ctx is done.
func (s *Session) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Go(func() { s.sendGrades(ctx) })
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

// tick moves the ride forward to now using the trainer's latest power.
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
	if r.phase == telemetry.RideArmed {
		if power <= 0 {
			return
		}
		r.phase, r.start, r.last = telemetry.RideRiding, now, now
		s.log.Info("ride started", "course", r.c.Name)
	}

	dt := math.Min(now.Sub(r.last).Seconds(), 1) // a stalled loop doesn't teleport the rider
	r.last = now
	_, grade := r.c.At(r.rider.DistanceM)
	r.rider.Step(power, grade, dt)
	r.energyJ += power * dt
	if ele, _ := r.c.At(r.rider.DistanceM); ele > r.lastEle {
		r.climbed += ele - r.lastEle
		r.lastEle = ele
	} else {
		r.lastEle = ele
	}

	finish := now
	if over := r.rider.DistanceM - r.c.Distance; over >= 0 {
		// Interpolate the moment the line was crossed.
		if r.rider.SpeedMPS > 0 {
			finish = now.Add(-time.Duration(over / r.rider.SpeedMPS * float64(time.Second)))
		}
		r.rider.DistanceM = r.c.Distance
		r.phase = telemetry.RideFinished
		flat := 0.0
		s.target = &flat
		elapsed := finish.Sub(r.start)
		s.log.Info("ride finished", "course", r.c.Name, "time", elapsed.Round(time.Second),
			"avg_power_w", round(r.energyJ/elapsed.Seconds(), 0), "climbed_m", round(r.climbed, 0))
	} else {
		g := s.trainerGrade(r.c, r.rider.DistanceM, r.rider.SpeedMPS)
		s.target = &g
	}
	s.publish(r, finish)
}

// trainerGrade is the grade to send at distance d moving at speed v.
func (s *Session) trainerGrade(c *course.Course, d, v float64) float64 {
	_, ahead := c.At(d + v*s.cfg.Lookahead.Seconds())
	return sim.TrainerGrade(ahead, s.cfg.Difficulty, s.cfg.MaxGradePct)
}

// publish copies the ride into the hub; now is the ride clock's reading
// time (zero before the start).
func (s *Session) publish(r *run, now time.Time) {
	ele, grade := r.c.At(r.rider.DistanceM)
	lat, lon := r.c.Position(r.rider.DistanceM)
	var elapsed time.Duration
	if !r.start.IsZero() && !now.IsZero() {
		elapsed = now.Sub(r.start)
	}
	avg := 0.0
	if elapsed > 0 {
		avg = r.energyJ / elapsed.Seconds()
	}
	target := 0.0
	if s.target != nil {
		target = *s.target
	}
	var ghost telemetry.RideGhost
	if g := r.ghost; g != nil {
		ghost = telemetry.RideGhost{Label: g.Label, Elapsed: g.Elapsed, DistanceM: g.DistanceAt(elapsed)}
		if r.phase != telemetry.RideArmed {
			ghost.Gap = g.Gap(elapsed, r.rider.DistanceM)
		}
	}
	s.hub.Update(func(st *telemetry.State) bool {
		changed := st.Ride.Changed
		if st.Ride.Phase != r.phase || st.Ride.CourseID != r.c.ID {
			changed = st.Time
		}
		st.Ride = telemetry.Ride{
			Phase:           r.phase,
			CourseID:        r.c.ID,
			CourseName:      r.c.Name,
			CourseDistanceM: r.c.Distance,
			CourseGainM:     r.c.Gain,
			DistanceM:       r.rider.DistanceM,
			ElevationM:      ele,
			GradePct:        grade,
			TrainerGradePct: target,
			SpeedMPS:        r.rider.SpeedMPS,
			ClimbedM:        r.climbed,
			AvgPowerW:       avg,
			Elapsed:         elapsed,
			Changed:         changed,
			DifficultyPct:   s.cfg.Difficulty * 100,
			StartDistanceM:  r.startM,
			Lat:             lat,
			Lon:             lon,
			Sim:             r.rider.Params,
			Ghost:           ghost,
		}
		return true
	})
}

// sendGrades applies the target grade to the trainer, outside the tick loop
// so a slow acknowledgement never stalls the clock.
func (s *Session) sendGrades(ctx context.Context) {
	tick := time.NewTicker(tickInterval)
	defer tick.Stop()
	var (
		sent     float64
		haveSent bool
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
		var target *float64
		if s.target != nil {
			t := *s.target
			target = &t
		}
		active := s.run != nil && s.run.phase.Active()
		s.mu.Unlock()
		if target == nil {
			continue
		}
		stale := active && time.Since(sentAt) > gradeResend
		if haveSent && math.Abs(*target-sent) < gradeEpsilon && !stale {
			continue
		}
		if err := s.trainer.SetGrade(ctx, *target); err != nil {
			if err.Error() != lastErr && ctx.Err() == nil {
				s.log.Warn("setting trainer grade", "grade", round(*target, 1), "err", err)
				lastErr = err.Error()
			}
			continue
		}
		lastErr = ""
		sent, haveSent, sentAt = *target, true, time.Now()
		s.log.Debug("trainer grade", "grade", round(sent, 2))
		if !active && sent == 0 {
			// Ride over and trainer flat: hand the trainer back.
			s.mu.Lock()
			if s.run == nil || !s.run.phase.Active() {
				s.target = nil
			}
			s.mu.Unlock()
		}
	}
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}
