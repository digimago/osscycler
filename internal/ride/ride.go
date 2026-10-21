// Package ride runs course rides: it moves a simulated rider along a
// course using the trainer's power, keeps the clock for a timed effort, and
// sends the trainer the grade ahead.
//
// A loop (a built-in test track) has no finish: the rider goes round until
// they stop, each lap timed and raced against the best lap so far. A
// workout or manual control may run meanwhile; it drives the trainer, and
// the ride only moves the rider round the loop.
package ride

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

var (
	ErrUnknownCourse = errors.New("ride: unknown course")
	ErrRideActive    = errors.New("ride: a ride, workout or manual control is already under way")
	ErrNoRide        = errors.New("ride: no course ride under way")
	ErrNoOtherWay    = errors.New("ride: this course can't be ridden the other way")
	ErrOnRunIn       = errors.New("ride: no way back along the course from the run-in: turn round once you're on the lap")
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
	// trainer's response lag lines up with the course: 2 s (owner,
	// 2026-10-10, on the Flux 2: with 1 s every change of resistance came
	// 1-2 s late, on grades and in ERG alike).
	Lookahead time.Duration
	// StartDistanceM starts rides this far into the course, with a rolling
	// start, to practise a section (and for the demo). Clamped to leave
	// some course to ride.
	StartDistanceM float64
	// Ghosts, if set, gives each ride an earlier one to race (the PB).
	Ghosts GhostSource
	// StartSpeedMPS starts rides at this speed instead (replaying a lap of
	// a loop, which the rider entered at speed).
	StartSpeedMPS float64
}

// rollingStartW is the power whose steady speed a rolling start begins at.
const rollingStartW = 200

// minRideM is the least course left after a start offset.
const minRideM = 100

// DefaultConfig suits a Tacx Flux 2 (16 % maximum) with Zwift's default
// trainer difficulty of 50 %.
func DefaultConfig(params sim.Params) Config {
	return Config{Params: params, Difficulty: 0.5, MaxGradePct: 16, Lookahead: 2 * time.Second}
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
	// chosen: the ghost is a ride the rider picked, kept all ride long;
	// otherwise on a loop each lap faster than it becomes the ghost.
	chosen bool
	// untimed: the rider turned round onto this course partway along
	// (TurnAround); joined: on its lap, partway round, untimed until the
	// lap's line (lap 0, lapDist how far into the lap).
	untimed, joined bool

	// Lapped courses only (course.Lap).
	lap                   int       // under way, from 1; 0 on the run-in
	lapStart              time.Time // when it began (the ride clock's start for lap 1 on a test track)
	lapDist               float64   // how far into it
	lapEnergy, lapClimbed float64
	lapSpeed0             float64      // speed crossing the line into it
	lapTrace              []TracePoint // this lap, on its own clock
	lastLap, bestLap      telemetry.Lap
	// yielded: a workout or manual control has the trainer.
	yielded bool
	// The core paused (State.Paused): since when (zero: not paused), and
	// whether the ride and the lap under way were ever paused.
	pausedAt          time.Time
	paused, lapPaused bool
}

// drivenElsewhere reports whether a workout or manual control has the
// trainer. A workout's free segments leave it to the loop's grade.
func drivenElsewhere(st telemetry.State) bool {
	return (st.Workout.Phase.Active() && !st.Workout.Free) || st.Control.Mode != telemetry.ControlOff
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
	if r := s.run; r != nil && r.phase.Active() && !r.yielded {
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
	st, _ := s.hub.Latest()
	if drivenElsewhere(st) && c.Lap == nil {
		return ErrRideActive // a workout or manual control has the trainer
	}
	s.mu.Lock()
	start := math.Max(0, math.Min(s.cfg.StartDistanceM, c.Distance-minRideM))
	switch {
	case c.Loop:
		start = 0 // laps count from the line
	case c.Lap != nil:
		start = math.Min(start, c.Lap.StartM) // on the run-in: laps count from the lap's start
	}
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
	r := &run{c: c, rider: sim.Rider{Params: s.cfg.Params, DistanceM: start}, phase: telemetry.RideArmed, lastEle: ele, startM: start,
		ghost: ghost, chosen: against != nil, yielded: c.Lap != nil && drivenElsewhere(st)}
	if c.Lap != nil && start >= c.Lap.StartM {
		r.lap = 1 // the ride starts on the lap (a test track's): it begins at the first pedal stroke
	}
	switch {
	case s.cfg.StartSpeedMPS > 0:
		r.rider.SpeedMPS = s.cfg.StartSpeedMPS
	case start > 0:
		r.rider.SpeedMPS = s.cfg.Params.SteadySpeed(rollingStartW, grade)
	}
	r.lapSpeed0 = r.rider.SpeedMPS
	s.run = r
	s.target = nil
	if !r.yielded {
		g := s.trainerGrade(c, start, r.rider.SpeedMPS)
		s.target = &g
	}
	s.publish(r, time.Time{})
	attrs := []any{"course", c.Name, "distance_km", round(c.Distance/1000, 2), "gain_m", round(c.Gain, 0), "start_km", round(start/1000, 2), "loop", c.Lap != nil}
	if ghost != nil {
		attrs = append(attrs, "ghost", ghost.Label, "ghost_time", ghost.Elapsed.Round(100*time.Millisecond))
	}
	s.log.Info("ride armed", attrs...)
	return nil
}

// Stop aborts a ride under way (how a loop ends), or dismisses a finished
// one. The trainer is set flat, unless a workout or manual control has it.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.run
	if r == nil {
		return nil
	}
	flat := 0.0
	s.target = &flat
	if r.yielded {
		s.target = nil
	}
	switch {
	case r.phase.Active() && r.c.Lap != nil:
		r.phase = telemetry.RideAborted
		s.publish(r, time.Now())
		attrs := []any{"course", r.c.Name, "laps", max(0, r.lap-1)}
		if r.bestLap.N > 0 {
			attrs = append(attrs, "best_lap", r.bestLap.Elapsed.Round(100*time.Millisecond), "best_lap_n", r.bestLap.N)
		}
		s.log.Info("loop ride ended", attrs...)
	case r.phase.Active():
		r.phase = telemetry.RideAborted
		s.publish(r, time.Now())
		s.log.Info("ride aborted", "course", r.c.Name, "distance_km", round(r.rider.DistanceM/1000, 2))
	default:
		s.hub.Update(func(st *telemetry.State) bool {
			st.Ride = telemetry.Ride{Changed: st.Time, DifficultyPct: s.cfg.Difficulty * 100}
			return true
		})
	}
	s.run = nil
	return nil
}

// otherWay is the course that rides c the other way: its reverse, or the
// forward course of a reverse; nil when it has none.
func (s *Session) otherWay(c *course.Course) *course.Course {
	for _, o := range s.courses {
		if (c.Forward != nil && o.ID == c.Forward.ID) || (o.Forward != nil && o.Forward.ID == c.ID) {
			return o
		}
	}
	return nil
}

// TurnAround turns the rider round during a course ride (owner,
// 2026-10-20): the ride so far ends with no result, and the rider rides
// on from the same spot, at the same speed, along the course's other way,
// untimed. Timing lines time it from there as on any ride: on a lapped
// course the lap's line starts a timed lap (the rest of the lap the rider
// joined is untimed, its ghost waiting at the line); a one-way course
// finishes with no time that counts. On a lapped course's run-in out the
// other way has no way back to the start (Opposite): refused with
// ErrOnRunIn (owner, 2026-10-20: ending the ride there was a surprise),
// the ride going on. It returns the course ridden on now. A pause holds
// on.
func (s *Session) TurnAround() (string, error) {
	s.mu.Lock()
	r := s.run
	if r == nil || !r.phase.Active() {
		s.mu.Unlock()
		return "", ErrNoRide
	}
	o := s.otherWay(r.c)
	if o == nil {
		s.mu.Unlock()
		return "", ErrNoOtherWay
	}
	at, ok := r.c.Opposite(o, r.rider.DistanceM)
	ghosts := s.cfg.Ghosts
	s.mu.Unlock()
	if !ok {
		return "", ErrOnRunIn
	}
	// The other way's lap ghost: from its lap's start, read outside the
	// lock as start does.
	var ghost *Ghost
	if o.Lap != nil && ghosts != nil {
		var err error
		if ghost, err = ghosts.Ghost(o, o.Lap.StartM); err != nil {
			s.log.Warn("no ghost for this ride", "course", o.Name, "err", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != r || !r.phase.Active() {
		return "", ErrNoRide // ended meanwhile
	}
	now := r.last // on the ticks' clock, as the ride's own times
	if now.IsZero() {
		now = time.Now()
	}
	ele, _ := o.At(at)
	n := &run{c: o, rider: sim.Rider{Params: r.rider.Params, DistanceM: at, SpeedMPS: r.rider.SpeedMPS},
		phase: r.phase, lastEle: ele, startM: at, ghost: ghost, untimed: true, yielded: r.yielded,
		pausedAt: r.pausedAt, paused: r.paused}
	if n.phase == telemetry.RideRiding {
		n.start, n.last = now, now
		if !n.pausedAt.IsZero() {
			n.start = n.pausedAt // its clock stands until the pause ends
		}
	}
	if l := o.Lap; l != nil && at >= l.StartM {
		n.joined, n.lapDist = true, at-l.StartM
		if at > l.EndM { // its tail: the lap's start again
			n.lapDist = at - l.EndM
		}
	}
	n.lapSpeed0 = n.rider.SpeedMPS
	s.run = n
	switch {
	case n.yielded:
		s.target = nil
	case !n.pausedAt.IsZero():
		flat := 0.0
		s.target = &flat
	default:
		g := s.trainerGrade(o, at, n.rider.SpeedMPS)
		s.target = &g
	}
	clock := time.Time{}
	if n.phase == telemetry.RideRiding {
		clock = now
		if !n.pausedAt.IsZero() {
			clock = n.pausedAt
		}
	}
	s.publish(n, clock)
	s.log.Info("turned round", "from", r.c.Name, "from_km", round(r.rider.DistanceM/1000, 2), "course", o.Name, "at_km", round(at/1000, 2))
	return o.ID, nil
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
	elsewhere := drivenElsewhere(st)
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
	if st.Paused {
		s.hold(r, now)
		return
	}
	if !r.pausedAt.IsZero() {
		s.carryOn(r, now)
	}
	if r.phase == telemetry.RideArmed {
		if power <= 0 {
			return
		}
		r.phase, r.start, r.last = telemetry.RideRiding, now, now
		if l := r.c.Lap; l != nil && r.rider.DistanceM >= l.StartM && !r.joined {
			s.beginLap(r, now, r.rider.DistanceM-l.StartM, 1) // the ride starts on the lap
		}
		s.log.Info("ride started", "course", r.c.Name)
	}
	if r.c.Lap != nil && elsewhere != r.yielded {
		r.yielded = elsewhere
		s.log.Info("loop ride", "trainer", map[bool]string{true: "driven by a workout or manual control", false: "follows the grade"}[elsewhere])
	}

	dt := math.Min(now.Sub(r.last).Seconds(), 1) // a stalled loop doesn't teleport the rider
	r.last = now
	_, grade := r.c.At(r.rider.DistanceM)
	before := r.rider.DistanceM
	r.rider.Step(power, grade, dt)
	moved := r.rider.DistanceM - before
	r.energyJ += power * dt
	r.lapEnergy += power * dt
	if ele, _ := r.c.At(r.rider.DistanceM); ele > r.lastEle {
		r.climbed += ele - r.lastEle
		r.lapClimbed += ele - r.lastEle
		r.lastEle = ele
	} else {
		r.lastEle = ele
	}

	finish := now
	if r.c.Lap != nil {
		s.laps(r, now, moved)
		s.target = nil
		if !r.yielded {
			g := s.trainerGrade(r.c, r.rider.DistanceM, r.rider.SpeedMPS)
			s.target = &g
		}
	} else if over := r.rider.DistanceM - r.c.Distance; over >= 0 {
		// Interpolate the moment the line was crossed.
		if r.rider.SpeedMPS > 0 {
			finish = now.Add(-time.Duration(over / r.rider.SpeedMPS * float64(time.Second)))
		}
		r.rider.DistanceM = r.c.Distance
		r.phase = telemetry.RideFinished
		flat := 0.0
		s.target = &flat
		elapsed := finish.Sub(r.start)
		s.log.Info("ride finished", "course", r.c.Name, "time", elapsed.Round(time.Second), "untimed", r.untimed,
			"avg_power_w", round(r.energyJ/elapsed.Seconds(), 0), "climbed_m", round(r.climbed, 0))
	} else {
		g := s.trainerGrade(r.c, r.rider.DistanceM, r.rider.SpeedMPS)
		s.target = &g
	}
	s.publish(r, finish)
}

// hold keeps a ride still while the core is paused: the rider stopped,
// the clock (and the ghost with it) standing, the trainer flat.
func (s *Session) hold(r *run, now time.Time) {
	if r.pausedAt.IsZero() {
		r.pausedAt = now
		r.rider.SpeedMPS = 0
		if r.phase == telemetry.RideRiding {
			r.paused, r.lapPaused = true, true
		}
		s.log.Info("ride paused", "course", r.c.Name, "distance_km", round(r.rider.DistanceM/1000, 2))
	}
	if !r.yielded {
		flat := 0.0
		s.target = &flat
	}
	clock := time.Time{}
	if r.phase == telemetry.RideRiding {
		clock = r.pausedAt
	}
	s.publish(r, clock)
}

// carryOn resumes a ride after a pause: its clocks move on by the time it
// stood, so the ride's time is riding time; the trainer gets the grade
// again.
func (s *Session) carryOn(r *run, now time.Time) {
	held := now.Sub(r.pausedAt)
	if r.phase == telemetry.RideRiding {
		r.start, r.lapStart = r.start.Add(held), r.lapStart.Add(held)
	}
	r.last, r.pausedAt = now, time.Time{}
	if !r.yielded {
		g := s.trainerGrade(r.c, r.rider.DistanceM, r.rider.SpeedMPS)
		s.target = &g
	}
	s.log.Info("ride resumed", "course", r.c.Name, "after", held.Round(time.Second))
}

// beginLap starts lap n at, over metres into it: the first when the
// rider reaches the lap's start from the run-in (or starts on it).
func (s *Session) beginLap(r *run, at time.Time, over float64, n int) {
	r.lap = n
	r.lapStart, r.lapDist = at, over
	r.lapEnergy, r.lapClimbed, r.lapSpeed0 = 0, 0, r.rider.SpeedMPS
	r.lapTrace = []TracePoint{{0, over}}
	r.lapPaused = false
}

// crossedAt is when the rider passed a point over metres behind them now.
func crossedAt(r *run, now time.Time, over float64) time.Time {
	if r.rider.SpeedMPS > 0 {
		return now.Add(-time.Duration(over / r.rider.SpeedMPS * float64(time.Second)))
	}
	return now
}

// laps follows the rider round a lapped course: onto the lap from the
// run-in, the laps they complete (moved metres this tick), and back by a
// lap at the course's end (course.Lap: the same place, a lap on); it
// keeps the current lap's trace for the ghost.
func (s *Session) laps(r *run, now time.Time, moved float64) {
	l := r.c.Lap
	switch {
	case r.lap == 0 && r.joined:
		// Partway round: the lap's line starts the first timed lap.
		if r.lapDist += moved; r.lapDist >= l.Length() {
			over := r.lapDist - l.Length()
			cross := crossedAt(r, now, over)
			if cross.Before(r.start) {
				cross = r.start
			}
			r.joined = false
			s.beginLap(r, cross, over, 1)
			s.log.Info("over the lap's line: timed from here", "course", r.c.Name)
		}
	case r.lap == 0:
		if r.rider.DistanceM >= l.StartM {
			over := r.rider.DistanceM - l.StartM
			cross := crossedAt(r, now, over)
			if cross.Before(r.start) {
				cross = r.start
			}
			s.beginLap(r, cross, over, 1)
			s.log.Info("onto the lap", "course", r.c.Name)
		}
	default:
		r.lapDist += moved
	}
	for r.lap > 0 && r.lapDist >= l.Length() {
		// Interpolate the moment the line was crossed.
		over := r.lapDist - l.Length()
		cross := crossedAt(r, now, over)
		if cross.Before(r.lapStart) {
			cross = r.lapStart
		}
		t := cross.Sub(r.lapStart)
		done := telemetry.Lap{N: r.lap, Elapsed: t, ClimbedM: r.lapClimbed, StartSpeedMPS: r.lapSpeed0, Finished: cross, Paused: r.lapPaused}
		if t > 0 {
			done.AvgPowerW = r.lapEnergy / t.Seconds()
		}
		r.lastLap = done
		// A paused lap is neither the best nor the one to race.
		if !done.Paused && (r.bestLap.N == 0 || t < r.bestLap.Elapsed) {
			r.bestLap = done
		}
		// A lap faster than the ghost is the one to race from now on.
		if !r.chosen && !done.Paused && t > 0 && (r.ghost == nil || t < r.ghost.Elapsed) {
			trace := append(r.lapTrace, TracePoint{t, l.Length()})
			r.ghost = &Ghost{Label: fmt.Sprintf("PB lap %d", r.lap), Elapsed: t, Trace: trace}
		}
		s.log.Info("lap", "course", r.c.Name, "lap", r.lap, "time", t.Round(100*time.Millisecond),
			"avg_power_w", round(done.AvgPowerW, 0), "best", r.bestLap.N == r.lap)
		s.beginLap(r, cross, over, r.lap+1)
	}
	// Past the course's end: back by a lap, the same place.
	for l.Length() > 0 && r.rider.DistanceM >= r.c.Distance {
		r.rider.DistanceM -= l.Length()
	}
	if r.lap > 0 {
		if at := now.Sub(r.lapStart); at > r.lapTrace[len(r.lapTrace)-1].At {
			r.lapTrace = append(r.lapTrace, TracePoint{at, r.lapDist})
		}
	}
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
	var elapsed, lapElapsed time.Duration
	if !r.start.IsZero() && !now.IsZero() {
		elapsed = now.Sub(r.start)
		lapElapsed = now.Sub(r.lapStart)
	}
	avg := 0.0
	if elapsed > 0 {
		avg = r.energyJ / elapsed.Seconds()
	}
	target := 0.0
	if s.target != nil {
		target = *s.target
	}
	l := r.c.Lap
	if l != nil && r.lap == 0 {
		lapElapsed = 0 // on the run-in
	}
	var ghost telemetry.RideGhost
	if g := r.ghost; g != nil {
		clock, at := elapsed, r.rider.DistanceM
		if l != nil {
			clock, at = lapElapsed, r.lapDist // a lapped course races lap by lap
		}
		ghost = telemetry.RideGhost{Label: g.Label, Elapsed: g.Elapsed, DistanceM: g.DistanceAt(clock)}
		if l != nil {
			ghost.DistanceM += l.StartM // on the course: the lap starts there
		}
		if r.phase != telemetry.RideArmed && (l == nil || r.lap > 0) {
			ghost.Gap = g.Gap(clock, at)
		}
		if r.phase == telemetry.RideRiding && r.pausedAt.IsZero() && (l == nil || r.lap > 0) {
			ghost.SpeedMPS = g.SpeedAt(clock)
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
			Loop:            l != nil,
			LastLap:         r.lastLap,
			BestLap:         r.bestLap,
			Yielded:         r.yielded,
			Paused:          r.paused,
			LapPaused:       r.lapPaused,
			Untimed:         r.untimed,
		}
		if l != nil {
			st.Ride.Lap, st.Ride.LapElapsed = r.lap, lapElapsed
			st.Ride.LapStartM, st.Ride.LapLengthM = l.StartM, l.Length()
			st.Ride.LapDistanceM = r.lapDist
			switch {
			case r.lap == 0 && r.joined:
				st.Ride.LapDistanceM = r.lapDist - l.Length() // to go to the line: negative
			case r.lap == 0:
				st.Ride.LapDistanceM = r.rider.DistanceM - l.StartM // to go to the lap: negative
			}
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
		if st, _ := s.hub.Latest(); drivenElsewhere(st) {
			continue // a workout or manual control has the trainer now
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
