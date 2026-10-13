package course

import (
	"math"
	"testing"
)

// pointsAlong is a track through en (metres east, north of 52°N 5°E),
// every 10 m, 20 m up.
func pointsAlong(en ...[2]float64) []Point {
	var pts []Point
	for k := 0; k+1 < len(en); k++ {
		a, b := en[k], en[k+1]
		n := max(1, int(math.Hypot(b[0]-a[0], b[1]-a[1])/10))
		for i := range n {
			f := float64(i) / float64(n)
			e, no := a[0]+f*(b[0]-a[0]), a[1]+f*(b[1]-a[1])
			pts = append(pts, Point{Lat: 52 + no/metresPerDegree, Lon: 5 + e/(metresPerDegree*math.Cos(52*math.Pi/180)), Ele: 20 + no/100})
		}
	}
	last := en[len(en)-1]
	return append(pts, Point{Lat: 52 + last[1]/metresPerDegree, Lon: 5 + last[0]/(metresPerDegree*math.Cos(52*math.Pi/180)), Ele: 20 + last[1]/100})
}

// A course that comes back to its start is lapped (owner, 2026-10-13):
// its run-in, ridden out and back, then the lap from where it leaves the
// run-in to where it joins it again; the course ends LapTailM into the lap
// again, and past its end wraps back by a lap, to the same place.
func TestLappedCourse(t *testing.T) {
	// A 500 m run-in east, a 500 m square lap north of it, back west.
	c, err := New("square", "Square", pointsAlong([2]float64{0, 0}, [2]float64{500, 0}, [2]float64{500, 500},
		[2]float64{1000, 500}, [2]float64{1000, 0}, [2]float64{500, 0}, [2]float64{0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	lc := c.Lapped()
	l := lc.Lap
	if l == nil || math.Abs(l.StartM-500) > 25 || math.Abs(l.EndM-2500) > 25 {
		t.Fatalf("lap %+v, want 500..2500 m", l)
	}
	if math.Abs(lc.Distance-l.EndM-LapTailM) > lc.Spacing {
		t.Errorf("the course is %.0f m, want the lap's end and %.0f m", lc.Distance, LapTailM)
	}
	at := func(d float64) [2]float64 {
		e, n := lc.Project(lc.Position(d))
		return [2]float64{e, n}
	}
	gap := func(a, b [2]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
	// The tail is the lap's start again: the end of the course lies where
	// the lap began the same way in, and past the end wraps there.
	if g := gap(at(lc.Distance), at(l.StartM+lc.Distance-l.EndM)); g > 1 {
		t.Errorf("the course's end is %.1f m from the lap's start as far in", g)
	}
	if g := gap(at(lc.Distance+250), at(l.StartM+lc.Distance-l.EndM+250)); g > 1 {
		t.Errorf("past the end, %.1f m off the lap", g)
	}
	e1, _ := lc.At(lc.Distance + 250)
	e2, _ := lc.At(l.StartM + lc.Distance - l.EndM + 250)
	if math.Abs(e1-e2) > 0.01 {
		t.Errorf("elevation past the end %.2f, in the lap %.2f", e1, e2)
	}
	// The run-in back to the start is left out: nothing after the tail.
	if g := gap(at(lc.Distance), at(0)); g < 400 {
		t.Errorf("the course ends %.0f m from its start: the run-in back is still in it", g)
	}

	// A one-way course has no lap.
	ow, err := New("line", "Line", pointsAlong([2]float64{0, 0}, [2]float64{3000, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if ow.Lapped().Lap != nil {
		t.Errorf("a one-way course lapped: %+v", ow.Lapped().Lap)
	}
}
