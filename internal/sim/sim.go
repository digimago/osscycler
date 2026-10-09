// Package sim computes virtual speed from power on a grade. It has no
// trainer or renderer dependencies so it can be tested and replayed alone.
package sim

import "math"

const gravity = 9.80665 // m/s²

// Params describe the rider, bike and air.
type Params struct {
	MassKg        float64 // rider + bike
	CdA           float64 // drag area, m²
	Crr           float64 // rolling resistance coefficient
	AirDensity    float64 // kg/m³
	DrivetrainEff float64 // share of pedal power reaching the wheel
}

// DefaultCdA is the drag area of the reference rider (RefHeightM,
// RefRiderKg) on the hoods.
const (
	DefaultCdA = 0.32
	RefHeightM = 1.80
	RefRiderKg = 75.0
)

// FrontalArea estimates a rider's frontal area on a bike, m², from height
// and weight (Bassett et al., 1999, "Comparing cycling world hour
// records, 1967-1996", Med Sci Sports Exerc 31(11)).
func FrontalArea(heightM, riderKg float64) float64 {
	return 0.0293*math.Pow(heightM, 0.725)*math.Pow(riderKg, 0.425) + 0.0604
}

// CdAFor scales DefaultCdA to a rider's size: a taller or heavier rider
// pushes more air. The reference rider gets DefaultCdA exactly.
func CdAFor(heightM, riderKg float64) float64 {
	return DefaultCdA * FrontalArea(heightM, riderKg) / FrontalArea(RefHeightM, RefRiderKg)
}

// DefaultParams are road-bike values: on the hoods, good tyres on smooth
// tarmac, sea level at 15 °C, the reference rider's drag area.
func DefaultParams(riderKg, bikeKg float64) Params {
	return Params{
		MassKg:        riderKg + bikeKg,
		CdA:           DefaultCdA,
		Crr:           0.004,
		AirDensity:    1.225,
		DrivetrainEff: 0.976,
	}
}

// minDriveSpeed caps the drive force at standstill, where power/speed
// would be infinite.
const minDriveSpeed = 0.5 // m/s

// maxSubstep keeps the explicit integration stable.
const maxSubstep = 0.05 // s

// Rider is the simulated bike's motion along a course.
type Rider struct {
	Params
	SpeedMPS  float64
	DistanceM float64
}

// Step advances dt seconds with the rider putting out powerW on gradePct.
// Speed never goes negative: without power on a climb the bike stops.
func (r *Rider) Step(powerW, gradePct, dt float64) {
	theta := math.Atan(gradePct / 100)
	m := r.MassKg
	slope := m * gravity * math.Sin(theta)
	rolling := r.Crr * m * gravity * math.Cos(theta)
	wheelPower := powerW * r.DrivetrainEff
	for dt > 0 {
		h := math.Min(dt, maxSubstep)
		dt -= h
		v := r.SpeedMPS
		drive := wheelPower / math.Max(v, minDriveSpeed)
		aero := 0.5 * r.AirDensity * r.CdA * v * v
		resist := slope + aero
		if v > 0 || drive > slope+rolling {
			resist += rolling // rolling resistance only opposes motion
		}
		v = math.Max(0, v+(drive-resist)/m*h)
		r.DistanceM += (r.SpeedMPS + v) / 2 * h
		r.SpeedMPS = v
	}
}

// SteadySpeed returns the speed at which powerW exactly balances the
// resistance on gradePct, or 0 if it can't move the bike. Useful for tests
// and for showing what a target power means on a climb.
func (p Params) SteadySpeed(powerW, gradePct float64) float64 {
	theta := math.Atan(gradePct / 100)
	slope := p.MassKg * gravity * math.Sin(theta)
	rolling := p.Crr * p.MassKg * gravity * math.Cos(theta)
	need := func(v float64) float64 {
		return (slope+rolling)*v + 0.5*p.AirDensity*p.CdA*v*v*v
	}
	have := powerW * p.DrivetrainEff
	lo, hi := 0.0, 40.0
	if need(hi) < have {
		return hi
	}
	for range 100 {
		mid := (lo + hi) / 2
		if need(mid) < have {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// DescentFactor is the share of a descent's grade sent to the trainer
// before difficulty, as Zwift does, so riders don't spin out downhill.
const DescentFactor = 0.5

// TrainerGrade is the grade to send a trainer for a course grade, following
// Zwift's trainer difficulty: climbs are scaled by difficulty (0..1);
// descents are halved and then scaled, so at the default 50 % a -10 %
// descent is sent as -2.5 %. The result is capped at the trainer's maximum.
// A trainer that can't drive the flywheel (Flux 2) is expected to render a
// negative grade as lighter-than-flat resistance; not yet verified.
func TrainerGrade(gradePct, difficulty, maxPct float64) float64 {
	if gradePct < 0 {
		return gradePct * DescentFactor * difficulty
	}
	return math.Min(gradePct*difficulty, maxPct)
}
