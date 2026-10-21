package effort

import (
	"math"
	"testing"
)

func TestSummarize(t *testing.T) {
	// 10 min: 5 at 150 W, 2 at 300 W, 3 at 200 W; heart rate 120 then 170;
	// a gap in the power reading for 10 s.
	var ss []Sample
	for i := range 600 {
		p, hr := 150, 120
		switch {
		case i >= 300 && i < 420:
			p, hr = 300, 170
		case i >= 420:
			p = 200
		}
		if i >= 100 && i < 110 {
			p = -1
		}
		ss = append(ss, Sample{T: float64(i), PowerW: p, HR: hr, Cadence: 90, SpeedMS: 8, DistM: 8 * float64(i+1)})
	}
	s := Summarize(ss, 200, 190)
	if s.TimerS != 600 || s.DistanceM != 4800 {
		t.Errorf("timer %.0f s, distance %.0f m; want 600 s, 4800 m", s.TimerS, s.DistanceM)
	}
	wantAvg := (290*150.0 + 120*300 + 180*200) / 590
	if math.Abs(s.AvgPowerW-wantAvg) > 0.01 || s.MaxPowerW != 300 {
		t.Errorf("average %.1f W, max %.0f W; want %.1f, 300", s.AvgPowerW, s.MaxPowerW, wantAvg)
	}
	if s.MaxHR != 170 || s.AvgCadenceRPM != 90 {
		t.Errorf("max HR %.0f, cadence %.0f; want 170, 90", s.MaxHR, s.AvgCadenceRPM)
	}
	best := map[int]float64{}
	for _, b := range s.Curve {
		best[b.DurationS] = b.PowerW
	}
	if best[60] != 300 || best[120] != 300 || len(s.Curve) != 7 {
		t.Errorf("curve %v; want 300 W over 1 and 2 min, durations up to 10 min", s.Curve)
	}
	if want := (300*120.0 + 200*180) / 300; math.Abs(best[300]-want) > 0.01 {
		t.Errorf("best 5 min %.1f W, want %.1f", best[300], want)
	}
	// FTP 200: 150 W is Z2 (0.75), 300 W Z6 (1.5), 200 W Z4 (1.0).
	if z := s.PowerZoneS; len(z) != 6 || z[1] != 290 || z[5] != 120 || z[3] != 180 {
		t.Errorf("power zones %v; want 290 s in Z2, 180 in Z4, 120 in Z6", z)
	}
	// Max HR 190: 120 is 0.63 (Z2), 170 is 0.89 (Z4).
	if z := s.HRZoneS; len(z) != 5 || z[1] != 480 || z[3] != 120 {
		t.Errorf("heart rate zones %v; want 480 s in Z2, 120 in Z4", z)
	}
	if s.NormPowerW < s.AvgPowerW {
		t.Errorf("normalized power %.0f under the average %.0f", s.NormPowerW, s.AvgPowerW)
	}
}

func TestSummarizeWithoutFTPOrMaxHR(t *testing.T) {
	ss := []Sample{{PowerW: 100, HR: 100}, {PowerW: 100, HR: 100}}
	s := Summarize(ss, 0, 0)
	if s.PowerZoneS != nil || s.HRZoneS != nil {
		t.Errorf("zones without FTP or max HR: %v, %v", s.PowerZoneS, s.HRZoneS)
	}
	if len(Summarize(nil, 200, 190).Curve) != 0 {
		t.Error("a curve with no samples")
	}
}
