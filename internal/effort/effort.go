// Package effort sums up a ride from its samples, one a second of riding
// time: averages, the best power over set durations (the power curve) and
// the time spent in each power and heart rate zone. The live graph and the
// end-of-ride summary both come from it (owner, 2026-10-10).
package effort

import "math"

// Sample is one second of riding: readings are -1 when there was none.
type Sample struct {
	T       float64 // riding time, s (pauses left out)
	PowerW  int
	HR      int // bpm
	Cadence int // rpm
	SpeedMS float64
	DistM   float64 // since the start of the activity
}

// CurveDurations are the durations of the power curve, s.
var CurveDurations = []int{5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600}

// PowerZones are Zwift's: upper bounds as a fraction of FTP for Z1-Z5,
// Z6 above (the TUI's and the 3D view's zone colours).
var PowerZones = []float64{0.60, 0.76, 0.90, 1.05, 1.19}

// HRZones are upper bounds as a fraction of maximum heart rate for Z1-Z4,
// Z5 above.
var HRZones = []float64{0.60, 0.70, 0.80, 0.90}

// Best is the highest average power held for DurationS seconds.
type Best struct {
	DurationS int
	PowerW    float64
}

// Summary is what a ride took.
type Summary struct {
	TimerS        float64 // riding time
	DistanceM     float64
	AvgPowerW     float64 // over seconds with a reading; 0 when none
	MaxPowerW     float64
	NormPowerW    float64 // normalized power (30 s rolling, fourth power); 0 under 30 s
	WorkKJ        float64
	AvgHR, MaxHR  float64 // 0 when no heart rate was read
	AvgCadenceRPM float64 // over seconds pedalling
	// Curve has a Best for each of CurveDurations the ride lasted.
	Curve []Best
	// PowerZoneS is the time in each power zone, s (len(PowerZones)+1),
	// nil without an FTP or power.
	PowerZoneS []float64
	// HRZoneS is the time in each heart rate zone, s (len(HRZones)+1), nil
	// without a maximum heart rate or heart rate.
	HRZoneS []float64
	// The FTP and maximum heart rate the zones were taken from.
	FTPW, MaxHRBpm float64
}

// Summarize sums up samples (one a second, in order) with the rider's FTP
// and maximum heart rate (0: unknown, those zones are left out).
func Summarize(ss []Sample, ftpW, maxHR float64) Summary {
	s := Summary{TimerS: float64(len(ss)), FTPW: ftpW, MaxHRBpm: maxHR}
	var powerN, hrN, cadN int
	var powerSum, hrSum, cadSum float64
	watts := make([]float64, len(ss)) // no reading counts as 0 for the curve
	if len(ss) > 0 {
		s.DistanceM = ss[len(ss)-1].DistM
	}
	for i, x := range ss {
		if x.PowerW >= 0 {
			p := float64(x.PowerW)
			watts[i] = p
			powerSum += p
			powerN++
			s.MaxPowerW = math.Max(s.MaxPowerW, p)
		}
		if x.HR > 0 {
			hrSum += float64(x.HR)
			hrN++
			s.MaxHR = math.Max(s.MaxHR, float64(x.HR))
		}
		if x.Cadence > 0 {
			cadSum += float64(x.Cadence)
			cadN++
		}
	}
	if powerN > 0 {
		s.AvgPowerW = powerSum / float64(powerN)
		s.WorkKJ = powerSum / 1000
		s.NormPowerW = normalized(watts)
		s.Curve = curve(watts)
		if ftpW > 0 {
			s.PowerZoneS = make([]float64, len(PowerZones)+1)
			for _, x := range ss {
				if x.PowerW >= 0 {
					s.PowerZoneS[zone(float64(x.PowerW)/ftpW, PowerZones)]++
				}
			}
		}
	}
	if hrN > 0 {
		s.AvgHR = hrSum / float64(hrN)
		if maxHR > 0 {
			s.HRZoneS = make([]float64, len(HRZones)+1)
			for _, x := range ss {
				if x.HR > 0 {
					s.HRZoneS[zone(float64(x.HR)/maxHR, HRZones)]++
				}
			}
		}
	}
	if cadN > 0 {
		s.AvgCadenceRPM = cadSum / float64(cadN)
	}
	return s
}

// zone is the index of the first bound frac is under, or len(bounds).
func zone(frac float64, bounds []float64) int {
	for i, b := range bounds {
		if frac < b {
			return i
		}
	}
	return len(bounds)
}

// curve is the best average for each of CurveDurations the ride lasted.
func curve(w []float64) []Best {
	sum := make([]float64, len(w)+1)
	for i, v := range w {
		sum[i+1] = sum[i] + v
	}
	var out []Best
	for _, d := range CurveDurations {
		if d > len(w) {
			break
		}
		best := 0.0
		for i := d; i <= len(w); i++ {
			best = math.Max(best, sum[i]-sum[i-d])
		}
		out = append(out, Best{DurationS: d, PowerW: best / float64(d)})
	}
	return out
}

// normalized is the normalized power: the fourth root of the mean fourth
// power of the 30 s rolling average; 0 for under 30 s.
func normalized(w []float64) float64 {
	const n = 30
	if len(w) < n {
		return 0
	}
	run, sum4 := 0.0, 0.0
	for i, v := range w {
		run += v
		if i >= n {
			run -= w[i-n]
		}
		if i >= n-1 {
			a := run / n
			sum4 += a * a * a * a
		}
	}
	return math.Pow(sum4/float64(len(w)-n+1), 0.25)
}
