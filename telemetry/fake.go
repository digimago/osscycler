package telemetry

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/digimago/osscycler/fec"
)

// Fake feeds a hub with a synthetic ride at 4 Hz, so renderers can be
// developed without a trainer. It also plays through a spin-down
// calibration when asked, and starts out asking for one.
type Fake struct {
	hub *Hub

	mu       sync.Mutex
	calStart time.Time // zero when no calibration runs
	grade    float64   // last SetGrade; the fake rider works harder uphill
	erg      float64   // last SetTargetPower; 0 = not in ERG mode
	level    float64   // last SetResistance; the fake rider works harder on a higher level
}

// SetGrade makes the fake rider push about 15 W more per percent of grade.
// It leaves ERG mode, as a real trainer does on a grade page.
func (f *Fake) SetGrade(_ context.Context, gradePct float64) error {
	f.mu.Lock()
	f.grade, f.erg, f.level = gradePct, 0, 0
	f.mu.Unlock()
	return nil
}

// SetResistance makes the fake rider push about 2 W more per percent of
// brake level. It leaves ERG and simulation mode.
func (f *Fake) SetResistance(_ context.Context, pct float64) error {
	f.mu.Lock()
	f.level, f.erg, f.grade = pct, 0, 0
	f.mu.Unlock()
	return nil
}

// SetTargetPower puts the fake trainer in ERG mode: the rider's power
// follows the target with a little noise.
func (f *Fake) SetTargetPower(_ context.Context, watts float64) error {
	f.mu.Lock()
	f.erg, f.grade, f.level = watts, 0, 0
	f.mu.Unlock()
	return nil
}

func NewFake(hub *Hub) *Fake { return &Fake{hub: hub} }

// SetUser is accepted and ignored: the fake rider has no weight to feel.
func (f *Fake) SetUser(context.Context, fec.UserConfig) error { return nil }

// Timings of the simulated spin-down: speed up for fakeRampUp, then coast.
const (
	fakeTargetSpeed = 9.7 // m/s, about 35 km/h
	fakeRampUp      = 6 * time.Second
	fakeCoast       = 5 * time.Second
	fakeSpinDownMS  = 2980
)

// The fake's mutex is never held while calling into the hub: Run takes them
// the other way round.

func (f *Fake) StartSpinDown(context.Context) error {
	f.mu.Lock()
	if !f.calStart.IsZero() {
		f.mu.Unlock()
		return ErrCalibrationActive
	}
	f.calStart = time.Now()
	f.mu.Unlock()
	f.hub.Update(func(s *State) bool {
		s.Trainer.Calibration = Calibration{}
		s.Trainer.Calibration.setPhase(CalRequested, s.Time, "")
		return true
	})
	return nil
}

func (f *Fake) CancelCalibration(context.Context) error {
	f.mu.Lock()
	f.calStart = time.Time{}
	f.mu.Unlock()
	f.hub.Update(func(s *State) bool {
		if !s.Trainer.Calibration.Phase.Active() {
			return false
		}
		s.Trainer.Calibration.setPhase(CalCancelled, s.Time, "")
		return true
	})
	return nil
}

// Run produces data until ctx is cancelled.
func (f *Fake) Run(ctx context.Context) error {
	f.hub.Update(func(s *State) bool {
		s.Trainer.Sensor = Sensor{Status: StatusConnected, DeviceNumber: 1, ManufacturerID: 255, SWVersion: "fake"}
		s.HeartRate.Sensor = Sensor{Status: StatusConnected, DeviceNumber: 2, ManufacturerID: 255, SWVersion: "fake"}
		s.Trainer.State = fec.StateInUse
		s.Trainer.Flags = fec.ResistanceCalibrationRequired
		return true
	})
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		t := time.Since(start).Seconds()
		power := 200 + 60*math.Sin(t/20)
		// Flat-road speed from power: aero drag plus rolling resistance for
		// ~90 kg. Good enough to look plausible.
		speed := math.Cbrt(power / 0.25)
		hr := 110 + 40*(1-math.Exp(-t/120)) + 5*math.Sin(t/15)

		f.mu.Lock()
		calStart, grade, erg, level := f.calStart, f.grade, f.erg, f.level
		f.mu.Unlock()
		power += 15*grade + 2*level
		if erg > 0 {
			power = erg + 4*math.Sin(t*1.7)
		}
		var calElapsed time.Duration
		if !calStart.IsZero() {
			calElapsed = time.Since(calStart)
			// Ramp up past the target speed, then coast down without power.
			if calElapsed < fakeRampUp {
				speed = 6 + 4.5*calElapsed.Seconds()/fakeRampUp.Seconds()
				power = 350
			} else {
				speed = math.Max(0, 10.5*(1-(calElapsed-fakeRampUp).Seconds()/fakeCoast.Seconds()))
				power = 0
			}
		}

		var calDone bool
		f.hub.Update(func(s *State) bool {
			tr := &s.Trainer
			tr.Sensor.LastSeen, s.HeartRate.Sensor.LastSeen = s.Time, s.Time
			tr.PowerW = Some(uint16(power))
			tr.CadenceRPM = Some(uint8(88 + 4*math.Sin(t/7)))
			if power == 0 {
				tr.CadenceRPM = Some[uint8](0)
			}
			tr.SpeedMPS = Some(speed)
			tr.DistanceM += speed * 0.25
			tr.Elapsed = time.Duration(t * float64(time.Second))
			s.HeartRate.BPM = Some(uint8(hr))
			if !calStart.IsZero() {
				calDone = calibrationStep(s, calElapsed, speed)
			}
			return true
		})
		if calDone {
			f.mu.Lock()
			if f.calStart.Equal(calStart) {
				f.calStart = time.Time{}
			}
			f.mu.Unlock()
		}
	}
}

// calibrationStep mimics the trainer's calibration pages and reports
// whether the calibration finished.
func calibrationStep(s *State, elapsed time.Duration, speed float64) bool {
	c := &s.Trainer.Calibration
	if !c.Phase.Active() {
		return true // cancelled meanwhile
	}
	if elapsed >= fakeRampUp+fakeCoast {
		c.SpinDownMS = Some[uint16](fakeSpinDownMS)
		c.setPhase(CalSucceeded, s.Time, "")
		s.Trainer.Flags &^= fec.ResistanceCalibrationRequired
		return true
	}
	if c.Phase != CalInProgress {
		c.setPhase(CalInProgress, s.Time, "")
	}
	c.TargetSpeedMPS = Some(fakeTargetSpeed)
	c.TemperatureC = Some(24.5)
	c.TemperatureCondition = fec.ConditionOK
	c.TargetSpinDownMS = Some[uint16](3000)
	c.SpeedCondition = fec.ConditionTooLow
	if speed >= fakeTargetSpeed || elapsed >= fakeRampUp {
		c.SpeedCondition = fec.ConditionOK
	}
	return false
}
