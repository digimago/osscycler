package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// Along a French mountain road the ground model decides the roadside
// (owner's shots of the D13B): where it falls away steeply a barrier
// stands at the edge, where it rises steeply a retaining wall; where it
// lies level, neither. A Dutch world gets none of it.
func TestRoadsideFollowsTheSlope(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// East of the road the land falls 1 in 1 from 3 m out, west it rises
	// 1 in 1, both only between 300 and 700 m north.
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		if y < 300 || y > 700 {
			return 10, true
		}
		switch {
		case x > 3:
			return 10 - (x - 3), true
		case x < -3:
			return 10 + (-x - 3), true
		}
		return 10, true
	}
	build := func(region string) (east, west, other int) {
		tr := testTerrain(c, Options{Region: region, Elevation: elev})
		w := &World{doc: gltf.New("test")}
		if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
		for _, p := range w.prims {
			for v := 0; v+2 < len(p.Positions); v += 3 {
				x, n := float64(p.Positions[v]), -float64(p.Positions[v+2])
				switch {
				case n < 290 || n > 710:
					other++
				case x > 3:
					east++
				case x < -3:
					west++
				}
			}
		}
		return
	}
	east, west, other := build(RegionFR)
	if east == 0 || west == 0 {
		t.Errorf("a falling slope east and a rising one west: %d and %d roadside vertices, want both", east, west)
	}
	// Off the slopes only snow poles and bend posts (the road is low and
	// straight: none).
	if other > 0 {
		t.Errorf("%d roadside vertices where the land lies level", other)
	}
	if e, w, o := build(RegionNL); e+w+o > 0 {
		t.Errorf("a Dutch world has %d roadside vertices", e+w+o)
	}
}

func TestRuns(t *testing.T) {
	on := []bool{true, true, false, true, true, true, false, false, false, false, true, true, true, true, true, false, true}
	// 0-5 and 10-16, gaps of one closed; then a lone one too short.
	on = append(on, false, false, false, false, true)
	got := runs(on)
	if len(got) != 2 || got[0] != (sideRun{0, 5}) || got[1] != (sideRun{10, 16}) {
		t.Errorf("runs = %v", got)
	}
}

// A barrier stands where the land falls away from the road itself, not
// where a car park lies beside the road and the drop is beyond it (the
// Hohneck car parks, 15.33 km of the Route des Américains: a guardrail
// stood on the asphalt in front of the car park).
func TestNoBarrierBesideACarPark(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		if y < 300 || y > 700 || x <= 3 {
			return 10, true
		}
		return 10 - (x - 3), true // falling 1 in 1 east of the road
	}
	build := func(carPark bool) (east int) {
		tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev})
		if carPark {
			// A car park from the road's edge 12 m out, along the slope.
			p := gltf.Primitive{Material: 1}
			for _, q := range [][3]float64{{3.5, 10, -300}, {12, 10, -300}, {12, 10, -700}, {3.5, 10, -700}} {
				p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
			}
			p.Indices = []uint32{0, 1, 2, 0, 2, 3}
			cut := newCutter()
			cut.add(p)
			tr.surfaces = cut
		}
		w := &World{doc: gltf.New("test")}
		if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
			t.Fatal(err)
		}
		for _, p := range w.prims {
			for v := 0; v+2 < len(p.Positions); v += 3 {
				if x, n := float64(p.Positions[v]), -float64(p.Positions[v+2]); x > 3 && n > 300 && n < 700 {
					east++
				}
			}
		}
		return
	}
	if build(false) == 0 {
		t.Fatal("no barrier where the land falls away east of the road")
	}
	if n := build(true); n > 0 {
		t.Errorf("%d roadside vertices east of the road with a car park beside it, want no barrier there", n)
	}
}
