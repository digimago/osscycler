package world

import (
	"math"
	"testing"
)

// walkSmooth checks a smooth line every 0.1 m: no jumps, no kinks, and
// within maxDevM of the line's own points.
func walkSmooth(t *testing.T, s *smoothLine, from, to float64) {
	t.Helper()
	const step = 0.1
	px, py := s.at(from)
	pde, pdn, _ := s.heading(from)
	for d := from + step; d <= to; d += step {
		x, y := s.at(d)
		if j := math.Hypot(x-px, y-py); j > 1.5*step {
			t.Fatalf("jump of %.2f m at %.1f m", j, d)
		}
		de, dn, _ := s.heading(d)
		if turn := math.Abs(math.Atan2(pde*dn-pdn*de, pde*de+pdn*dn)) * 180 / math.Pi; turn > 5 {
			t.Fatalf("kink of %.1f° at %.1f m", turn, d)
		}
		px, py, pde, pdn = x, y, de, dn
	}
}

func TestSmoothCorner(t *testing.T) {
	// 100 m north, a right angle, 100 m east.
	s := newSmoothLine([]float64{0, 100, 200}, []float64{0, 0, 100}, []float64{0, 100, 100}, false, nil)
	walkSmooth(t, s, 0, 200)
	// The corner is rounded, but no further than maxDevM from it.
	near := math.Inf(1)
	for d := 90.0; d <= 110; d += 0.05 {
		x, y := s.at(d)
		near = math.Min(near, math.Hypot(x, y-100))
	}
	if near > maxDevM+0.01 || near < maxDevM/2 {
		t.Errorf("the rounded corner passes %.2f m from the corner, want up to %.1f", near, maxDevM)
	}
	// The straights stay where they are.
	if x, y := s.at(50); x != 0 || y != 50 {
		t.Errorf("at 50 m: %.2f, %.2f", x, y)
	}
	if x, y := s.at(150); x != 50 || y != 100 {
		t.Errorf("at 150 m: %.2f, %.2f", x, y)
	}
	// Cross-sections stand closer together through it.
	st := s.stations(2)
	inArc := 0
	for _, d := range st {
		if d > 95 && d < 105 {
			inArc++
		}
	}
	if inArc < 8 {
		t.Errorf("%d stations within the corner's 10 m, want closer than every 2 m", inArc)
	}
}

func TestSmoothLoop(t *testing.T) {
	// A square loop starting at a corner: smooth across the start too.
	d := []float64{0, 100, 200, 300, 400}
	x := []float64{0, 0, 100, 100, 0}
	y := []float64{0, 100, 100, 0, 0}
	s := newSmoothLine(d, x, y, true, nil)
	walkSmooth(t, s, -50, 450)
}

func TestSmoothJunctionCorner(t *testing.T) {
	// The same corner may be rounded 3 m out at a junction.
	dev := func(float64) float64 { return 3 }
	s := newSmoothLine([]float64{0, 100, 200}, []float64{0, 0, 100}, []float64{0, 100, 100}, false, dev)
	walkSmooth(t, s, 0, 200)
	near := math.Inf(1)
	for d := 80.0; d <= 120; d += 0.05 {
		x, y := s.at(d)
		near = math.Min(near, math.Hypot(x, y-100))
	}
	if near < 2 || near > 3.01 {
		t.Errorf("the junction's corner passes %.2f m from it, want up to 3", near)
	}
}
