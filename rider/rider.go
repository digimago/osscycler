// Package rider applies the rider's profile across the core: the
// trainer's user config, the simulated mass and drag, the ERG FTP and the
// difficulty. It saves changes (except those forced by flags), publishes
// the profile for renderers, and refuses what the profile can't support
// yet (a course ride without a weight, a workout without an FTP), so
// onboarding is the stack's rule and every renderer follows it the same way.
package rider

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/erg"
	"github.com/digimago/osscycler/fec"
	"github.com/digimago/osscycler/profile"
	"github.com/digimago/osscycler/ride"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
)

// UserSetter sends the rider's user config to the trainer.
type UserSetter interface {
	SetUser(ctx context.Context, u fec.UserConfig) error
}

// Service is the core's owner of the rider profile.
type Service struct {
	m        *profile.Manager
	hub      *telemetry.Hub
	trainer  UserSetter
	rides    *ride.Session // nil without courses
	workouts *erg.Session  // nil without a workout library
	log      *slog.Logger

	mu      sync.Mutex
	user    fec.UserConfig // bike and wheel; the weight follows the profile
	cdaFlag float64        // -cda given: this drag area, whatever the rider's size
}

// New publishes the profile and returns the service. user carries the
// bike and wheel; its weight is taken from the profile.
func New(m *profile.Manager, hub *telemetry.Hub, trainer UserSetter, rides *ride.Session, workouts *erg.Session,
	user fec.UserConfig, log *slog.Logger) *Service {
	user.UserWeightKg = m.Get().WeightKg
	s := &Service{m: m, hub: hub, trainer: trainer, rides: rides, workouts: workouts, log: log, user: user}
	s.applyDrag()
	s.publish()
	if p := m.Get(); !p.Complete() {
		log.Info("rider profile incomplete: renderers will ask for it", "weight_kg", p.WeightKg, "ftp_w", p.FTPW, "path", m.Path())
	}
	return s
}

// User is the trainer user config for the profile in effect.
func (s *Service) User() fec.UserConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user
}

// SuggestedFTP is a starting FTP for onboarding: 2.5 W/kg, rounded to
// 5 W, a fair guess for a recreational rider; 200 W without a weight.
func SuggestedFTP(weightKg float64) float64 {
	if weightKg <= 0 {
		return 200
	}
	return math.Max(profile.MinFTPW, math.Round(weightKg*2.5/5)*5)
}

// FixCdA uses this drag area whatever the rider's size, as -cda asks.
func (s *Service) FixCdA(cda float64) {
	s.mu.Lock()
	s.cdaFlag = cda
	s.mu.Unlock()
	s.applyDrag()
	s.publish()
}

// cda is the drag area for the profile in effect: from the rider's height
// and weight when both are known, else the reference rider's.
func (s *Service) cda() float64 {
	s.mu.Lock()
	fixed := s.cdaFlag
	s.mu.Unlock()
	p := s.m.Get()
	switch {
	case fixed > 0:
		return fixed
	case p.HeightCm > 0 && p.WeightKg > 0:
		return sim.CdAFor(p.HeightCm/100, p.WeightKg)
	}
	return sim.DefaultCdA
}

// applyDrag gives rides started from now on the drag area in effect.
func (s *Service) applyDrag() {
	if s.rides != nil {
		s.rides.SetCdA(s.cda())
	}
}

// SetProfile changes the weight, FTP and/or height (nil leaves one as it
// is), checking all before applying any, and returns the profile in
// effect. Out-of-range values match profile.ErrOutOfRange.
func (s *Service) SetProfile(ctx context.Context, weightKg, ftpW, heightCm *float64) (telemetry.Profile, error) {
	for _, c := range []struct {
		f profile.Field
		v *float64
	}{{profile.Weight, weightKg}, {profile.FTP, ftpW}, {profile.Height, heightCm}} {
		if c.v != nil {
			if err := profile.Check(c.f, *c.v); err != nil {
				return s.state(), err
			}
		}
	}
	if heightCm != nil {
		if err := s.set(profile.Height, *heightCm); err != nil {
			return s.state(), err
		}
		s.applyDrag()
		s.publish()
	}
	if weightKg != nil {
		if err := s.setWeight(ctx, *weightKg); err != nil {
			return s.state(), err
		}
	}
	if ftpW != nil {
		if err := s.set(profile.FTP, *ftpW); err != nil {
			return s.state(), err
		}
		if s.workouts != nil {
			s.workouts.SetFTP(*ftpW)
		}
	}
	return s.state(), nil
}

// setWeight saves the weight and applies it: the trainer gets a new user
// config now (or at its next pairing), and rides started from now on are
// simulated with the new mass.
func (s *Service) setWeight(ctx context.Context, kg float64) error {
	if err := s.set(profile.Weight, kg); err != nil {
		return err
	}
	s.mu.Lock()
	s.user.UserWeightKg = kg
	u := s.user
	s.mu.Unlock()
	if s.rides != nil {
		s.rides.SetMass(kg + u.BikeWeightKg)
	}
	s.applyDrag() // a heavier rider is also a bigger one
	s.publish()
	if err := s.trainer.SetUser(ctx, u); err != nil {
		s.log.Warn("trainer didn't take the new weight; it gets it at the next pairing", "err", err)
	}
	return nil
}

// set saves one field (unless forced this run) and publishes.
func (s *Service) set(f profile.Field, v float64) error {
	_, err := s.m.Set(f, v)
	s.publish()
	if err != nil {
		s.log.Error("saving the rider profile failed", "path", s.m.Path(), "err", err)
	}
	return err
}

// state is the profile as the state stream carries it.
func (s *Service) state() telemetry.Profile {
	p := s.m.Get()
	return telemetry.Profile{
		Known: true, Complete: p.Complete(), NeedWeight: p.WeightKg <= 0, NeedFTP: p.FTPW <= 0,
		WeightKg: p.WeightKg, FTPW: p.FTPW, DifficultyPct: p.DifficultyPct, SuggestedFTPW: SuggestedFTP(p.WeightKg),
		HeightCm: p.HeightCm, CdA: s.cda(),
		WeightForced: s.m.Forced(profile.Weight), FTPForced: s.m.Forced(profile.FTP),
		DifficultyForced: s.m.Forced(profile.Difficulty), HeightForced: s.m.Forced(profile.Height),
		CdAForced: s.cdaForced(), Path: s.m.Path(),
	}
}

func (s *Service) cdaForced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cdaFlag > 0
}

func (s *Service) publish() {
	p := s.state()
	s.hub.Update(func(st *telemetry.State) bool {
		if st.Profile == p {
			return false
		}
		st.Profile = p
		return true
	})
}

// Rides is the course-ride session with the profile's rules: a ride needs
// the rider's weight, and difficulty changes are saved.
type Rides struct {
	*ride.Session
	s *Service
}

// Rides wraps the ride session for the API.
func (s *Service) Rides() Rides { return Rides{s.rides, s} }

func (r Rides) Start(courseID string) error {
	if r.s.m.Get().WeightKg <= 0 {
		return fmt.Errorf("%w: a course ride needs the rider's weight", profile.ErrIncomplete)
	}
	return r.Session.Start(courseID)
}

func (r Rides) StartAgainst(courseID string, finished time.Time) error {
	if r.s.m.Get().WeightKg <= 0 {
		return fmt.Errorf("%w: a course ride needs the rider's weight", profile.ErrIncomplete)
	}
	return r.Session.StartAgainst(courseID, finished)
}

func (r Rides) SetDifficulty(pct float64) float64 {
	v := r.Session.SetDifficulty(pct)
	r.s.set(profile.Difficulty, v)
	return v
}

// Workouts is the ERG session with the profile's rules: a workout needs
// the FTP, and FTP changes are saved.
type Workouts struct {
	*erg.Session
	s *Service
}

// Workouts wraps the workout session for the API.
func (s *Service) Workouts() Workouts { return Workouts{s.workouts, s} }

func (w Workouts) Start(id string) error {
	if w.s.m.Get().FTPW <= 0 {
		return fmt.Errorf("%w: a workout needs the rider's FTP", profile.ErrIncomplete)
	}
	return w.Session.Start(id)
}

func (w Workouts) SetFTP(watts float64) float64 {
	v := w.Session.SetFTP(watts)
	w.s.set(profile.FTP, v)
	return v
}
