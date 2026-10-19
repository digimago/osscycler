package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
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

// A barrier is one run along a slope: gaps up to railGapM where the fall
// eases are closed (while the road is drawn there), and pieces shorter
// than railLeastM are left out (owner, 2026-10-17: short segments a few
// metres apart instead of one longer one).
func TestRailRunsAreContinuous(t *testing.T) {
	n := 260 // samples 0.5 m apart: 130 m
	on, near, dist := make([]bool, n), make([]bool, n), make([]float64, n)
	for i := range n {
		dist[i], near[i] = float64(i)*0.5, true
		on[i] = i >= 20 && i < 60 || i >= 90 && i < 150 || i >= 230 && i < 236 // 20 m, a 15 m gap, 30 m, then 40 m on a 3 m piece
	}
	for i := 100; i < 104; i++ {
		near[i] = false // a mouth inside the second run: stays as it is (on)
	}
	got := railRuns(on, near, dist)
	if len(got) != 1 || got[0].from != 20 || got[0].to != 149 {
		t.Fatalf("runs %v, want one from 20 to 149 (the 15 m gap closed, the 3 m piece dropped)", got)
	}
	// A gap at a junction's mouth (not near) is never closed.
	for i := 60; i < 90; i++ {
		near[i] = false
	}
	if got = railRuns(on, near, dist); len(got) != 2 {
		t.Errorf("runs %v, want two with the mouth between them open", got)
	}
}

// Retaining walls and netted rock face the road on either side of it,
// whatever way the map's line runs (owner, 2026-10-18, the Route des
// Américains at 7.5 km: the rock's faces were wound inside out on one
// side, so the renderer culled them and the netting hung over grass; the
// walls on that side showed their dark backs). Every steep triangle's
// winding faces the road, and so does its coping face up.
func TestWallsFaceTheRoad(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// Both sides rise: 1 in 1 from 3 m out between 200 and 450 m north
	// (a wall), 2 in 1 between 550 and 800 m (rock).
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		d := math.Abs(x) - 3
		switch {
		case d <= 0:
			return 10, true
		case y > 200 && y < 450:
			return 10 + d, true
		case y > 550 && y < 800:
			return 10 + 2*d, true
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev})
	w := &World{doc: gltf.New("test")}
	if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	count := map[string][3]int{} // material: steep triangles facing the road, facing away, tops facing down
	for _, p := range w.prims {
		name := w.doc.MaterialName(p.Material)
		if name != "rock" && name != "stone wall" {
			continue
		}
		pos := func(i uint32) [3]float64 {
			return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			a, b, c := pos(p.Indices[i]), pos(p.Indices[i+1]), pos(p.Indices[i+2])
			n := cross([3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}, [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]})
			l := math.Sqrt(dot3(n, n))
			if l == 0 {
				continue
			}
			n = scale3(n, 1/l)
			cx := (a[0] + b[0] + c[0]) / 3
			cnt := count[name]
			switch {
			case math.Abs(n[1]) > 0.7:
				if n[1] < 0 {
					cnt[2]++
				}
			case n[0]*cx < 0: // towards x = 0, the road
				cnt[0]++
			default:
				cnt[1]++
			}
			count[name] = cnt
		}
	}
	for _, name := range []string{"rock", "stone wall"} {
		cnt := count[name]
		if cnt[0] < 50 {
			t.Errorf("%s: %d steep triangles facing the road, want many", name, cnt[0])
		}
		if cnt[1] > 0 || cnt[2] > 0 {
			t.Errorf("%s: %d steep triangles face away from the road, %d tops face down; want none", name, cnt[1], cnt[2])
		}
	}
}

// Out in the hills a retaining wall runs on over a short gap in the bank
// and a stub of wall is left out (owner, 2026-10-18, the Route des
// Américains: walls in short pieces stood as stone wedges in a row).
func TestWallsRunOnOverShortGaps(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// West of the road the land rises 1 in 1 from 3 m out between 200 and
	// 300 m north and 315 and 420 m (a 15 m dip between), and for 10 m at
	// 700 m.
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		bank := y > 200 && y < 300 || y > 315 && y < 420 || y > 700 && y < 710
		if x < -3 && bank {
			return 10 + (-x - 3), true
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev})
	w := &World{doc: gltf.New("test")}
	if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	walled := map[int]bool{} // metres north with wall
	for _, p := range w.prims {
		if name := w.doc.MaterialName(p.Material); name != "stone wall" && name != "rock" {
			continue
		}
		for v := 0; v+2 < len(p.Positions); v += 3 {
			walled[int(-p.Positions[v+2])] = true
		}
	}
	gap := 0
	for n := 300; n <= 315; n++ {
		if walled[n] {
			gap++
		}
	}
	if gap < 7 { // vertices every 2 m: 8 at most
		t.Errorf("the wall has vertices at %d of the 16 metres of the dip at 300-315 m, want it to run on", gap)
	}
	for n := 690; n <= 720; n++ {
		if walled[n] {
			t.Errorf("a stub of wall at %d m: a 10 m bank gets none", n)
			break
		}
	}
}

// A retaining wall stands no higher than the land it holds up, and comes
// down towards its ends (owner, 2026-10-18, 19.07 km of the Hohneck loop:
// walls stood out of the terrain like buttresses, a 2.6 m wall ending
// square, its fill a ridge over the bank behind).
func TestWallFitsTheBankBehind(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// West of the road, between 200 and 400 m north, a bank rising 1 in 1
	// from 3 m out to a level bench 2.5 m up.
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		if x < -3 && y > 200 && y < 400 {
			return 10 + math.Min(-x-3, 2.5), true
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev})
	w := &World{doc: gltf.New("test")}
	if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	type vtx struct{ n, y float64 }
	var vs []vtx
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range w.prims {
		if name := w.doc.MaterialName(p.Material); name != "stone wall" && name != "rock" {
			continue
		}
		for v := 0; v+2 < len(p.Positions); v += 3 {
			q := vtx{-float64(p.Positions[v+2]), float64(p.Positions[v+1])}
			vs = append(vs, q)
			lo, hi = math.Min(lo, q.n), math.Max(hi, q.n)
		}
	}
	if len(vs) == 0 {
		t.Fatal("no wall along the bank")
	}
	top, endTop := 0.0, 0.0
	for _, q := range vs {
		top = math.Max(top, q.y)
		if math.Min(q.n-lo, hi-q.n) < 1 {
			endTop = math.Max(endTop, q.y)
		}
	}
	if top > 12.5+wallOverM+0.01 {
		t.Errorf("the wall's top stands at %.2f m, over the bench behind it at 12.50", top)
	}
	if endTop > 10+wallLeastH+wallEndSlope*1.5 {
		t.Errorf("within a metre of its ends the wall stands %.2f m high, want it coming down to its ends", endTop-10)
	}
}

// No grass grows in a retaining wall's footprint: on the land under the
// masonry it stood out through the face (owner, 2026-10-18, 20.00 km of
// the Hohneck loop).
func TestNoGrassInAWall(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		if x < -3 && y > 200 && y < 400 {
			return 10 + math.Min(-x-3, 2.5), true
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev})
	w := &World{doc: gltf.New("test")}
	if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if len(tr.walls) == 0 {
		t.Fatal("no wall footprints recorded")
	}
	sp := newSpots(tr, nil, nil, nil)
	in := 0
	for _, wall := range tr.walls {
		var e, n float64
		for _, q := range wall {
			e, n = e+q[0]/4, n+q[1]/4
		}
		if sp.clearance(e, n, 25) > 0 {
			in++
		}
	}
	if in > 0 {
		t.Errorf("%d of %d wall pieces have room for grass in their middle, want none", in, len(tr.walls))
	}
}

// A pasture's fence is cut where it would run through a retaining wall
// (owner, 2026-10-18, 20.00 km of the Hohneck loop: its wire ran along
// the masonry).
func TestFenceKeepsClearOfWalls(t *testing.T) {
	wall := [][2]float64{{-0.5, 10}, {0.5, 10}, {0.5, 20}, {-0.5, 20}}
	f := scenery.Fence{Line: [][2]float64{{0, 0}, {0, 40}}, Wire: true, GateFromM: 25, GateToM: 29}
	out := fencesClearOfWalls([]scenery.Fence{f}, [][][2]float64{wall})
	if len(out) != 2 {
		t.Fatalf("the fence comes out in %d pieces, want 2 either side of the wall", len(out))
	}
	for _, g := range out {
		for _, q := range g.Line {
			if q[1] > 10-fenceWallM && q[1] < 20+fenceWallM {
				t.Errorf("a fence point at %.1f m stands within %.1f m of the wall at 10-20 m", q[1], fenceWallM)
			}
		}
	}
	other := scenery.Fence{Line: [][2]float64{{5, 0}, {5, 40}}, Wire: true, GateFromM: 25, GateToM: 29}
	if got := fencesClearOfWalls([]scenery.Fence{other}, [][][2]float64{wall}); len(got) != 1 || got[0].GateToM != 29 {
		t.Errorf("a fence clear of the wall was changed: %+v", got)
	}
}

// In open land a bank of a few metres is grass, not a wall, up to
// riseOpenWall (owner's Street View shots, 2026-10-18, of the Route des
// Crêtes at 19.07 and 20.00 km: grass banks of 2-3 m above the pastures,
// where walls stood); in the woods the same bank is held by a wall.
func TestBanksInOpenLandAreGrass(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// West of the road the land rises 1.3 in 1 from 3 m out between 200
	// and 400 m north (3.3 m within 3 m of the edge): pasture there, woods
	// between 500 and 700 m with the same bank.
	elev := func(lat, lon float64) (float64, bool) {
		x, y := (lon-5)*kx, (lat-52)*111195
		if x < -3 && (y > 200 && y < 400 || y > 500 && y < 700) {
			return 10 + 1.3*(-x-3), true
		}
		return 10, true
	}
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "meadow"}, Geometry: ring(-300, 150, 0, 450)},
		{Type: "way", ID: 2, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-300, 450, 0, 750)},
	}})
	tr := testTerrain(c, Options{Region: RegionFR, Elevation: elev, Land: lm})
	w := &World{doc: gltf.New("test")}
	if err := w.addRoadside(tr, nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	pasture, woods := 0, 0
	for _, p := range w.prims {
		if name := w.doc.MaterialName(p.Material); name != "stone wall" && name != "rock" {
			continue
		}
		for v := 0; v+2 < len(p.Positions); v += 3 {
			switch n := -float64(p.Positions[v+2]); {
			case n > 200 && n < 400:
				pasture++
			case n > 500 && n < 700:
				woods++
			}
		}
	}
	if pasture > 0 {
		t.Errorf("%d wall vertices along the pasture's 3.3 m bank, want a grass bank", pasture)
	}
	if woods == 0 {
		t.Error("no wall along the same bank in the woods")
	}
}
