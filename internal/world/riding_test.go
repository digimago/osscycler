package world

import (
	"math"
	"testing"
)

func TestRidingLine(t *testing.T) {
	// 300 m north, a left-hand bend (west) at 300 m, 300 m on, a right-hand
	// bend (north again) at 600 m, 300 m on; the corners rounded with a
	// 15 m radius or so by many points, as a router gives them.
	var d, x, y []float64
	add := func(px, py float64) {
		if n := len(x); n > 0 {
			d = append(d, d[n-1]+math.Hypot(px-x[n-1], py-y[n-1]))
		} else {
			d = append(d, 0)
		}
		x, y = append(x, px), append(y, py)
	}
	add(0, 0)
	for a := 0.0; a <= 90; a += 10 { // left: centre at (-15, 300)
		r := a * math.Pi / 180
		add(-15+15*math.Cos(r), 300+15*math.Sin(r))
	}
	for a := 0.0; a <= 90; a += 10 { // right: centre at (-300, 330)
		r := a * math.Pi / 180
		add(-300-15*math.Sin(r), 330-15*math.Cos(r))
	}
	add(-315, 615)
	line := newSmoothLine(d, x, y, false, nil)
	L := line.length()
	var ds, widths []float64
	for s := 0.0; s <= L; s += pathStep {
		ds, widths = append(ds, s), append(widths, 6)
	}
	lane := ridingLine(line, ds, widths, true)
	lo, hi, mid := room(6)
	at := func(s float64) float64 { return lane[int(math.Round(s/pathStep))] }

	if o := at(100); math.Abs(o-mid) > 0.05 {
		t.Errorf("on the straight: %.2f m right of the centre, want the middle of the half (%.2f)", o, mid)
	}
	leftBend, rightBend := d[5], d[15] // the bends' middles
	// Near the limits: the line smooths over ±30 m, more than this short
	// bend, and a bend takes half the room either side of the middle.
	if want := mid - bendShare*(mid-lo); at(leftBend) > want+0.25 {
		t.Errorf("through the left-hand bend: %.2f m right of the centre, want towards the centre line (%.2f)", at(leftBend), want)
	}
	if want := mid + bendShare*(hi-mid); at(rightBend) < want-0.25 {
		t.Errorf("through the right-hand bend: %.2f m, want towards the edge (%.2f)", at(rightBend), want)
	}
	for i := range lane {
		if lane[i] < lo-1e-9 || lane[i] > hi+1e-9 {
			t.Fatalf("at %.0f m: %.2f, outside %.2f..%.2f", ds[i], lane[i], lo, hi)
		}
		if i > 0 && math.Abs(lane[i]-lane[i-1]) > driftPerM*pathStep+1e-9 {
			t.Fatalf("the line jumps %.2f m at %.0f m", lane[i]-lane[i-1], ds[i])
		}
	}
	// Riders keeping left ride the mirror image.
	left := ridingLine(line, ds, widths, false)
	if o := left[int(math.Round(100/pathStep))]; math.Abs(o+mid) > 0.05 {
		t.Errorf("keeping left on the straight: %.2f, want %.2f", o, -mid)
	}
}

func TestRidingLineAnticipatesNarrowing(t *testing.T) {
	// 400 m straight, 10 m wide to 200 m, then 5 m: the rider is in the
	// narrow road's room before it narrows, and gets there gently.
	var d, x, y []float64
	for s := 0.0; s <= 400; s += 10 {
		d, x, y = append(d, s), append(x, 0), append(y, s)
	}
	line := newSmoothLine(d, x, y, false, nil)
	var ds, widths []float64
	for s := 0.0; s <= 400; s += pathStep {
		w := 10.0
		if s >= 200 {
			w = 5
		}
		ds, widths = append(ds, s), append(widths, w)
	}
	lane := ridingLine(line, ds, widths, true)
	_, hi, _ := room(5)
	for i, s := range ds {
		if s >= 200-pathStep && lane[i] > hi+1e-9 {
			t.Errorf("at %.0f m: %.2f m, outside the narrow road's %.2f", s, lane[i], hi)
		}
		if i > 0 && math.Abs(lane[i]-lane[i-1]) > driftPerM*pathStep+1e-9 {
			t.Errorf("at %.0f m the line moves %.2f m", s, lane[i]-lane[i-1])
		}
	}
}

// A sharp turn right at the start of a path (its sharpest point lead
// points in) is left as it is rather than crashing the build (a clip of
// the Grebbeberg course from 55 km, 2026-10-10: index out of range).
func TestSmoothTurnsAtThePathsStart(t *testing.T) {
	lead := int(math.Round(turnLeadM / pathStep))
	var x, y []float64
	for i := 0; i <= lead; i++ {
		x, y = append(x, float64(i-lead)*pathStep), append(y, 0)
	}
	for i := 1; i <= 3*lead; i++ {
		x, y = append(x, 0), append(y, float64(i)*pathStep)
	}
	smoothTurns(x, y, pathStep, false)
}

// A spike in the path goes (the Posbank Loop at 5.425 km: one point 0.74 m
// aside); a bend stays a bend.
func TestDespike(t *testing.T) {
	var x, y []float64
	for i := range 20 {
		x, y = append(x, float64(i)*5), append(y, 0)
	}
	y[10] = 0.74
	despike(x, y, false)
	if math.Abs(y[10]) > 1e-9 {
		t.Errorf("the spike left at %.2f m", y[10])
	}
	// A bend of 15 m radius, points 5 m apart: every point off its
	// neighbours' line by about 0.8 m, and kept.
	var bx, by []float64
	for i := range 20 {
		a := float64(i) * 5 / 15
		bx, by = append(bx, 15*math.Sin(a)), append(by, 15-15*math.Cos(a))
	}
	ox, oy := append([]float64(nil), bx...), append([]float64(nil), by...)
	despike(bx, by, false)
	for i := range bx {
		if bx[i] != ox[i] || by[i] != oy[i] {
			t.Fatalf("the bend moved at point %d", i)
		}
	}
}
