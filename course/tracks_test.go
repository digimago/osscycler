package course

import (
	"math"
	"testing"
)

func TestTracks(t *testing.T) {
	ts := Tracks()
	if len(ts) != 2 {
		t.Fatalf("%d tracks", len(ts))
	}
	for _, c := range ts {
		if !c.Loop || !c.Builtin {
			t.Errorf("%s: loop %v, builtin %v", c.ID, c.Loop, c.Builtin)
		}
		// The loop closes: the end is where the start is, at the same height.
		la0, lo0 := c.Position(0)
		la1, lo1 := c.Position(c.Distance - 0.001)
		if d := haversine(Point{Lat: la0, Lon: lo0}, Point{Lat: la1, Lon: lo1}); d > 0.1 {
			t.Errorf("%s: end %.2f m from the start", c.ID, d)
		}
		e0, _ := c.At(0)
		e1, _ := c.At(c.Distance - 0.001)
		if math.Abs(e0-e1) > 0.05 {
			t.Errorf("%s: start at %.2f m, end at %.2f m", c.ID, e0, e1)
		}
	}

	oval, eight := ts[0], ts[1]
	if math.Abs(oval.Distance-400) > 0.1 || oval.Gain != 0 || oval.MaxGrade != 0 {
		t.Errorf("oval: %.2f m, gain %.1f, max grade %.2f", oval.Distance, oval.Gain, oval.MaxGrade)
	}
	if math.Abs(eight.Distance-5000) > 0.5 {
		t.Errorf("figure 8: %.2f m", eight.Distance)
	}
	ele, _ := eight.Profile()
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, e := range ele {
		lo, hi = math.Min(lo, e), math.Max(hi, e)
	}
	if hi-lo < 39.5 || hi-lo > 40.5 {
		t.Errorf("figure 8 spans %.2f m (%.1f to %.1f), want 40", hi-lo, lo, hi)
	}
	if eight.MaxGrade < 4 || eight.MaxGrade > 7 || eight.MinGrade < -7 {
		t.Errorf("figure 8 grades %.1f %% to %.1f %%", eight.MinGrade, eight.MaxGrade)
	}
	// The roads cross in the middle, a quarter and three quarters round:
	// the second pass on a bridge well over the first.
	east, north := eight.Track()
	q1, q3 := len(east)/4, 3*len(east)/4
	if d := math.Hypot(east[q1]-east[q3], north[q1]-north[q3]); d > 5 {
		t.Errorf("crossing points %.1f m apart", d)
	}
	under, _ := eight.At(eight.Distance / 4)
	over, _ := eight.At(3 * eight.Distance / 4)
	if over-under < 8 {
		t.Errorf("bridge clearance %.1f m (%.1f over %.1f)", over-under, over, under)
	}
}

func TestLoopWraps(t *testing.T) {
	c := Tracks()[1]
	for _, d := range []float64{1234, 3999} {
		e, g := c.At(d)
		e2, g2 := c.At(d + c.Distance)
		e3, g3 := c.At(d - c.Distance)
		if e != e2 || g != g2 || e != e3 || g != g3 {
			t.Errorf("At(%v) differs a lap on or back", d)
		}
		la, lo := c.Position(d)
		la2, lo2 := c.Position(d + c.Distance)
		if la != la2 || lo != lo2 {
			t.Errorf("Position(%v) differs a lap on", d)
		}
	}
}
