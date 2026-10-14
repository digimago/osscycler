package erg

import (
	"math"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
)

// CadenceTolerance is how far the rider's cadence may be off a workout's
// target cadence, as a fraction either way, and still be on target.
const CadenceTolerance = 0.05

// cadenceWindow is what the check averages over: the 5 s of the screens'
// CADENCE 5s tile, so the check agrees with the number they show.
const cadenceWindow = 5 * time.Second

// cadenceSample is the trainer's cadence as one tick read it.
type cadenceSample struct {
	at  time.Time
	rpm float64
	ok  bool // the trainer reported a valid cadence
}

// cadenceAvg is the cadence over the last cadenceWindow, each reading
// weighted by how long it held (as the TUI's tile, tui/cadence.go).
type cadenceAvg struct {
	samples []cadenceSample
	now     time.Time
}

// add records the reading at now, dropping those that no longer count
// (the one before the window holds into it).
func (c *cadenceAvg) add(now time.Time, rpm float64, ok bool) {
	from := now.Add(-cadenceWindow)
	keep := c.samples[:0]
	for i, s := range c.samples {
		if i+1 < len(c.samples) && !c.samples[i+1].at.After(from) {
			continue
		}
		keep = append(keep, s)
	}
	c.samples, c.now = append(keep, cadenceSample{at: now, rpm: rpm, ok: ok}), now
}

// value is the average; ok is false without valid readings.
func (c *cadenceAvg) value() (float64, bool) {
	from := c.now.Add(-cadenceWindow)
	var sum, weight float64
	for i, s := range c.samples {
		to := c.now
		if i+1 < len(c.samples) {
			to = c.samples[i+1].at
		}
		start := s.at
		if start.Before(from) {
			start = from
		}
		if d := to.Sub(start).Seconds(); d > 0 && s.ok {
			sum, weight = sum+s.rpm*d, weight+d
		}
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}

// checkCadence is the rider's cadence rpm against target (0: none).
func checkCadence(rpm float64, ok bool, target int) telemetry.CadenceCheck {
	if target <= 0 || !ok {
		return telemetry.CadenceNone
	}
	t := float64(target)
	switch {
	case rpm < t*(1-CadenceTolerance):
		return telemetry.CadenceLow
	case rpm > t*(1+CadenceTolerance):
		return telemetry.CadenceHigh
	}
	return telemetry.CadenceOn
}

// roundRPM keeps the published average to a tenth of an rpm.
func roundRPM(v float64) float64 { return math.Round(v*10) / 10 }
