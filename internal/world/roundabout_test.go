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
