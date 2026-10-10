package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/scenery"
)

// ring is a roundabout of radius r round (cx, cy) in four ways.
func ring(cx, cy, r float64, firstNode int64) []scenery.Way {
	var ws []scenery.Way
	for q := range 4 {
		var line [][2]float64
		var nodes []int64
		for k := 0; k <= 4; k++ {
			a := (float64(q) + float64(k)/4) * math.Pi / 2
			line = append(line, [2]float64{cx + r*math.Cos(a), cy + r*math.Sin(a)})
			nodes = append(nodes, firstNode+int64((4*q+k)%16))
		}
		ws = append(ws, scenery.Way{Line: line, Nodes: nodes, Class: "secondary", WidthM: 7, Roundabout: true})
	}
	return ws
}

func TestRoundabouts(t *testing.T) {
	ws := append(ring(0, 0, 25, 100), ring(500, 0, 12, 200)...)
	ws = append(ws, scenery.Way{Line: [][2]float64{{0, 25}, {0, 100}}, Nodes: []int64{100, 7}, Class: "residential", WidthM: 5})
	rbs := findRoundabouts(ws, true)
	if len(rbs) != 2 {
		t.Fatalf("%d roundabouts, want 2", len(rbs))
	}
	big, small := rbs[0], rbs[1]
	if big.c[0] > small.c[0] {
		big, small = small, big
	}
	if !big.island || small.island || math.Abs(big.r-25) > 1 || math.Abs(small.r-12) > 1 {
		t.Errorf("roundabouts %+v and %+v; want r 25 with an island, r 12 flat", big, small)
	}
	for _, w := range ws[:8] {
		if drawable(w, true) {
			t.Fatal("a roundabout's ring is drawn as its ways")
		}
	}

	// A path straight through each, north to south.
	for _, rb := range []roundabout{big, small} {
		var x, y, z, w []float64
		for n := 60.0; n >= -60; n -= pathStep {
			x, y, z, w = append(x, rb.c[0]), append(y, rb.c[1]+n), append(z, 0), append(w, 5)
		}
		rideAround(x, y, z, w, []roundabout{rb})
		mid := len(x) / 2
		switch {
		case rb.island && !(x[mid] < rb.c[0]-rb.r/2):
			t.Errorf("round the island at %.1f east of its middle halfway; want west of it (anticlockwise from the north)", x[mid]-rb.c[0])
		case !rb.island && x[mid] != rb.c[0]:
			t.Errorf("across the flat one at %.1f east of its middle; want straight through", x[mid]-rb.c[0])
		}
	}
}

// A route that leaves the road for the cycle path round a roundabout (the
// Velp loop at 2.45 km: its carriageways are closed to bikes) rides the
// roundabout instead, as on a closed course: in along the west arm, over
// it and out along the east arm, without the hop up its side.
func TestSkirtingRouteRidesTheRoundabout(t *testing.T) {
	arm := func(from, to [2]float64) *roadLine {
		l := &roadLine{}
		length := math.Hypot(to[0]-from[0], to[1]-from[1])
		for at := 0.0; at <= length+1e-9; at += fineStep {
			f := at / length
			l.samples = append(l.samples, sample{d: at, e: from[0] + f*(to[0]-from[0]), n: from[1] + f*(to[1]-from[1]), hw: 3})
		}
		return l
	}
	rb := roundabout{r: 15, hw: 3, arms: []rbArm{
		{l: arm([2]float64{-18.5, 0}, [2]float64{-200, 0}), d: 0, dir: 1},
		{l: arm([2]float64{18.5, 0}, [2]float64{200, 0}), d: 0, dir: 1},
		{l: arm([2]float64{0, -18.5}, [2]float64{0, -200}), d: 0, dir: 1},
	}}
	// pathOver puts points every pathStep along the corners given.
	pathOver := func(corners ...[2]float64) (x, y, z, w []float64) {
		at := 0.0
		for k := 1; k < len(corners); k++ {
			a, b := corners[k-1], corners[k]
			l := math.Hypot(b[0]-a[0], b[1]-a[1])
			for ; at < l; at += pathStep {
				x, y = append(x, a[0]+at/l*(b[0]-a[0])), append(y, a[1]+at/l*(b[1]-a[1]))
				z, w = append(z, 0), append(w, 5)
			}
			at -= l
		}
		return
	}
	// Off the west arm onto a path 30 m south of the middle, back onto
	// the east arm.
	x, y, z, w := pathOver([2]float64{-200, 0}, [2]float64{-60, 0}, [2]float64{-35, -30}, [2]float64{35, -30}, [2]float64{60, 0}, [2]float64{200, 0})
	rbs := []roundabout{rb}
	skirtRound(x, y, z, w, rbs)
	passThrough(x, y, z, rbs)
	for i := range x {
		if math.Abs(y[i]) > rb.hw+0.5 {
			t.Fatalf("at %.0f, %.0f: off the arms and the roundabout; want in along the west arm and out along the east", x[i], y[i])
		}
	}
	// A road 30 m south that never touches the arms is left alone.
	x, y, z, w = pathOver([2]float64{-200, -30}, [2]float64{200, -30})
	skirtRound(x, y, z, w, rbs)
	for i := range y {
		if y[i] != -30 {
			t.Fatalf("a road passing by moved to %.0f, %.0f", x[i], y[i])
		}
	}
}
