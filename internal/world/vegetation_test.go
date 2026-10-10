package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestPlants(t *testing.T) {
	// Forest west of the road, meadow east; a house in the forest.
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-300, -100, 0, 1100)},
		{Type: "way", ID: 2, Tags: map[string]string{"landuse": "meadow"}, Geometry: ring(0, -100, 300, 1100)},
	}})
	house := scenery.Footprint{ID: 9, Outline: [][2]float64{{-40, 500}, {-25, 500}, {-25, 510}, {-40, 510}}, Kind: scenery.KindHouse, WallM: 5, RoofM: 3}
	o := Options{Land: lm}
	tr := testTerrain(c, o)
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	keys := tr.chunks()
	ps := plants(tr, s, lm, keys, []scenery.Footprint{house}, nil, nil, newRouteGrid(tr.coarse, 400))

	var forest, meadow int
	for _, p := range ps {
		e, n := float64(p.x), -float64(p.z)
		if n > 0 && n < 1000 && math.Abs(e) < tr.fine[0].edge+clearRoadM-0.01 { // beside the road
			t.Fatalf("a plant on the road at %.1f, %.1f", e, n)
		}
		if inPoly(house.Outline, e, n) {
			t.Fatalf("a plant in the house at %.1f, %.1f", e, n)
		}
		if math.Abs(float64(p.y)-s.at(e, n)) > 0.01 {
			t.Fatalf("a plant at %.2f m, the ground at %.2f", p.y, s.at(e, n))
		}
		if n > 0 && n < 1000 && math.Abs(e) < 100 {
			if e < 0 {
				forest++
			} else {
				meadow++
			}
		}
	}
	if forest < 10*meadow || forest < 1000 {
		t.Errorf("%d plants in 1 ha of forest, %d in as much meadow; want the forest far denser", forest, meadow)
	}
	// The same every build.
	again := plants(tr, s, lm, keys, []scenery.Footprint{house}, nil, nil, newRouteGrid(tr.coarse, 400))
	if len(again) != len(ps) || again[len(ps)/2] != ps[len(ps)/2] {
		t.Error("plants differ between builds")
	}
	// Packed: groups cover every plant once, in the file's order.
	buf, groups := packPlants(ps, tr.o.ChunkM)
	total := 0
	for _, g := range groups {
		if g.Offset != total || g.Template != "template "+g.Kind {
			t.Fatalf("group %+v at %d", g, total)
		}
		total += g.Count
	}
	if total != len(ps) || len(buf) != 4*instanceFloats*total {
		t.Errorf("%d plants in groups, %d bytes; want %d and %d", total, len(buf), len(ps), 4*instanceFloats*len(ps))
	}
	if x := math.Float32frombits(binary.LittleEndian.Uint32(buf)); math.IsNaN(float64(x)) {
		t.Error("garbage in the file")
	}
}

func TestHedgeBetweenRoadsSideBySide(t *testing.T) {
	// The course's road north, and 9 m east of it a service road (3 m
	// wide) for 200 m: a low hornbeam hedge down the strip between, none
	// east of the service road or beside the road where it runs alone. No
	// land use: nothing else grows near.
	c := northCourse(t)
	tr := testTerrain(c, Options{})
	for i := range tr.fine {
		tr.fine[i].line = 0
	}
	for d := 300.0; d <= 500; d += fineStep {
		tr.fine = append(tr.fine, sample{d: d, e: 9, n: d, hw: 1.5, edge: 1.5, line: 1})
	}
	gap := 9 - 1.5 - tr.fine[0].edge
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	lm := scenery.NewLandMap(c, &scenery.Data{})
	var hedge, elsewhere int
	for _, p := range plants(tr, s, lm, nil, nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
		e, n := float64(p.x), -float64(p.z)
		switch {
		case p.kind == kHedge+hHornbeam && n > 299 && n < 503 && math.Abs(e-(tr.fine[0].edge+gap/2)) < 0.01:
			hedge++
		case p.kind >= kBush && math.Abs(e) < 20 && n > 0 && n < 1000:
			elsewhere++
		}
	}
	if hedge < 150 || elsewhere > 0 {
		t.Errorf("%d hedge segments in the strip between the roads, %d plants elsewhere near; want ~200 and none", hedge, elsewhere)
	}
}

func TestGardenHedges(t *testing.T) {
	// The road north through a village east of it: houses 8 m back from
	// the road every 15 m from 200 to 800 m. Hedges along the front
	// gardens (a mix of kinds, some plots open, gates), facing the road,
	// at one distance from it; none where no house stands behind, none
	// west (no village).
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "residential"},
			Geometry: []scenery.LatLon{p(0, -100), p(300, -100), p(300, 1100), p(0, 1100), p(0, -100)}},
	}})
	tr := testTerrain(c, Options{Land: lm})
	for i := range tr.fine {
		tr.fine[i].line = 0
	}
	front := tr.fine[0].edge + 1.2
	var fps []scenery.Footprint
	for n := 200.0; n < 800; n += 15 {
		x := front + 8
		fps = append(fps, scenery.Footprint{ID: int64(n), Outline: [][2]float64{{x, n}, {x + 10, n}, {x + 10, n + 9}, {x, n + 9}}, Kind: scenery.KindHouse, WallM: 5, RoofM: 3})
	}
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	sp := newSpots(tr, lm, fps, nil)
	got := hedges(tr, s, sp, newWayPlaces(nil, tr.fine))
	kinds := map[int]int{}
	for _, h := range got {
		e, n := float64(h.x), -float64(h.z)
		if math.Abs(e-front) > 0.01 || n < 180 || n > 820 {
			t.Fatalf("hedge at %.1f, %.1f; want them %.1f m east, beside the houses", e, n, front)
		}
		if math.Abs(math.Cos(float64(h.yaw)-math.Pi/2)) < 0.999 {
			t.Fatalf("hedge yaw %.2f; want along the road", h.yaw)
		}
		kinds[h.kind-kHedge]++
	}
	if len(got) < 150 || len(got) > 500 || len(kinds) < 3 {
		t.Errorf("%d hedge segments of %d kinds along 600 m of front gardens; want 150-500 (plots open, gates) of at least 3", len(got), len(kinds))
	}
}

func TestGroundMap(t *testing.T) {
	// The road north, forest west of it, meadow east; a house east.
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-300, -100, 0, 1100)},
		{Type: "way", ID: 2, Tags: map[string]string{"landuse": "meadow"}, Geometry: ring(0, -100, 300, 1100)},
	}})
	house := scenery.Footprint{ID: 9, Outline: [][2]float64{{20, 500}, {35, 500}, {35, 510}, {20, 510}}, Kind: scenery.KindHouse, WallM: 5, RoofM: 3}
	tr := testTerrain(c, Options{Land: lm})
	keys := tr.chunks()
	data, index := groundMap(newSpots(tr, lm, []scenery.Footprint{house}, nil), tr, &surface{t: tr, lists: map[[2]int]chunkLists{}}, nil, keys, tr.o.ChunkM, newRouteGrid(tr.coarse, 400))
	if len(index) == 0 {
		t.Fatal("no ground map")
	}
	n := int(tr.o.ChunkM / groundCellM)
	// at is the land and the clearance (m) in the cell at e, no.
	at := func(e, no float64) (byte, float64) {
		k := [2]int{int(math.Floor(e / tr.o.ChunkM)), int(math.Floor(no / tr.o.ChunkM))}
		for _, ch := range index {
			if ch.Chunk != k {
				continue
			}
			z, err := zlib.NewReader(bytes.NewReader(data[ch.Offset : ch.Offset+ch.Size]))
			if err != nil {
				t.Fatal(err)
			}
			cells, _ := io.ReadAll(z)
			i := int((e - float64(k[0])*tr.o.ChunkM) / groundCellM)
			j := int((no - float64(k[1])*tr.o.ChunkM) / groundCellM)
			return cells[j*n+i], float64(cells[n*n+j*n+i]) * groundClearStepM
		}
		return 0, 0
	}
	edge := tr.fine[0].edge
	for _, tc := range []struct {
		e, n  float64
		land  byte
		clear float64
		what  string
	}{
		{0.5, 500.5, byte(scenery.LandMeadow) + 1, 0, "the road"},
		{-5.5, 500.5, byte(scenery.LandForest) + 1, 5.5 - edge, "forest beside the road"},
		{5.5, 500.5, byte(scenery.LandMeadow) + 1, 5.5 - edge, "meadow beside the road"},
		{27.5, 505.5, byte(scenery.LandMeadow) + 1, 0, "in the house"},
		{18.5, 505.5, byte(scenery.LandMeadow) + 1, 1.5, "by the house's wall"},
		{150.5, 500.5, 0, 0, "beyond the map's reach of the route"},
	} {
		land, clear := at(tc.e, tc.n)
		if land != tc.land || math.Abs(clear-tc.clear) > groundClearStepM/2+1e-9 {
			t.Errorf("%s (%.1f, %.1f): %s, %.2f m clear; want %s, %.2f", tc.what, tc.e, tc.n,
				GroundClasses[land], clear, GroundClasses[tc.land], tc.clear)
		}
	}
}

func TestTreesClearRoadsAbove(t *testing.T) {
	// A road 10 m up crossing over x = 0 (a bridge), and one at ground
	// level: a tree 3 m beside the bridge's line goes, its twin beside the
	// ground-level road stays, and so does a bush under the bridge.
	var fine []sample
	for i := -10; i <= 10; i++ {
		fine = append(fine, sample{e: float64(i) * fineStep, n: 0, ele: 10, edge: 2.5, line: 1})
		fine = append(fine, sample{e: 100 + float64(i)*fineStep, n: 0, ele: 0, edge: 2.5, line: 2})
	}
	oak := treeKind(0, 0, false)
	ps := []plant{
		{kind: oak, x: 0, y: 0, z: -3, s: 1},
		{kind: oak, x: 100, y: 0, z: -3, s: 1},
		{kind: kBush, x: 0, y: 0, z: -3, s: 1},
	}
	got := clearOverhead(ps, fine)
	if len(got) != 2 || got[0].x != 100 || got[1].kind != kBush {
		t.Errorf("kept %+v, want the tree beside the ground-level road and the bush", got)
	}
}

func TestAvenuesKeyedByPlace(t *testing.T) {
	// One map way north through open land, drawn twice: once as the only
	// line from its start, once as a second line starting 37 m further
	// on (another route's clip). Where both draw it, the avenue trees are
	// the same trees in the same places.
	c := northCourse(t)
	way := scenery.Way{ID: 42, Line: [][2]float64{{0, -100}, {0, 900}}}
	build := func(from float64, line int) map[[3]float32]bool {
		tr := testTerrain(c, Options{})
		tr.fine = nil
		if line > 0 {
			tr.fine = append(tr.fine, sample{e: 5000, n: 5000, hw: 2, edge: 2, line: 0, way: -1}, sample{d: 2, e: 5000, n: 5002, hw: 2, edge: 2, line: 0, way: -1})
		}
		for d := 0.0; from+d <= 800; d += fineStep {
			tr.fine = append(tr.fine, sample{d: d, e: 0, n: from + d, hw: 2.5, edge: 2.5, line: line, way: 0})
		}
		s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
		lm := scenery.NewLandMap(c, &scenery.Data{})
		got := map[[3]float32]bool{}
		for _, p := range plants(tr, s, lm, nil, nil, nil, []scenery.Way{way}, newRouteGrid(tr.coarse, 400)) {
			if n := -p.z; p.kind < speciesKinds && n > 100 && n < 700 && math.Abs(float64(p.x)) < 6 {
				got[[3]float32{p.x, p.z, float32(p.kind)}] = true
			}
		}
		return got
	}
	a, b := build(0, 0), build(37, 1)
	if len(a) < 20 {
		t.Fatalf("%d avenue trees from 100 to 700 m, want an avenue", len(a))
	}
	for k := range a {
		if !b[k] {
			t.Fatalf("tree at %v east, %v north (kind %v) moved when the line started elsewhere", k[0], -k[1], k[2])
		}
	}
	if len(a) != len(b) {
		t.Errorf("%d trees against %d", len(a), len(b))
	}
}

func TestLandSurfaceIsTheDrawnLand(t *testing.T) {
	// A bank: one triangle rising 2 m over 4 m east, and a road triangle
	// (not land) above it: heights come from the land, interpolated over
	// its triangle; none off it.
	land := gltf.Primitive{Positions: []float32{0, 0, 0, 4, 2, 0, 0, 0, -4}, Indices: []uint32{0, 1, 2}, Material: 0}
	road := gltf.Primitive{Positions: []float32{0, 5, 0, 4, 5, 0, 0, 5, -4}, Indices: []uint32{0, 1, 2}, Material: 1}
	ls := newLandSurface([]gltf.Primitive{land, road}, func(m int) bool { return m == 0 })
	if h, ok := ls.at(2, 1); !ok || math.Abs(h-1) > 1e-6 {
		t.Errorf("at 2 east 1 north: %.3f %v, want 1 on the bank", h, ok)
	}
	if _, ok := ls.at(3.5, 3.5); ok {
		t.Error("land found off the triangle")
	}
}

// Grown woods are as dense as real ones (owner, 2026-10-10): 150-250 trees
// a hectare at an average stand, mixed and pure alike.
func TestWoodsHaveGrownWoodsDensity(t *testing.T) {
	mean := standDensity(0.5)
	perHa := func(m2 ...float64) float64 {
		n := 0.0
		for _, a := range m2 {
			n += 10000 / a
		}
		return n * mean
	}
	for name, n := range map[string]float64{
		"mixed": perHa(forestTreeM2, forestTreeM2),
		"pure":  perHa(pureTreeM2, pureOtherM2),
	} {
		if n < 150 || n > 260 {
			t.Errorf("%s wood: %.0f trees a hectare, want 150-250", name, n)
		}
	}
}

// Bushes keep their whole spread off the road, as young trees their crown
// (owner, 2026-10-10: a rhododendron spread over a cycle path): no bush
// near a road reaches over it.
func TestBushesKeepTheirSpreadOffTheRoad(t *testing.T) {
	for i, b := range allBushes {
		if r := bushReach(i); r <= 0.2 || r > 4 {
			t.Errorf("%s reaches %.2f m from its middle", b.name, r)
		}
	}
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: []scenery.LatLon{p(-300, -100), p(300, -100), p(300, 1100), p(-300, 1100), p(-300, -100)}},
	}})
	tr := testTerrain(c, Options{Land: lm})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	ps := plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400))
	n := 0
	for _, q := range ps {
		if q.kind < kBush || q.kind >= kHeather {
			continue
		}
		n++
		e, no := float64(q.x), -float64(q.z)
		if reach := bushReach(q.kind-kBush) * float64(q.s); !roadClear(tr, e, no, reach) {
			t.Fatalf("a %s %.1f m across at %.1f, %.1f reaches over the road", allBushes[q.kind-kBush].name, 2*reach, e, no)
		}
	}
	if n < 20 {
		t.Errorf("%d bushes in the forest", n)
	}
}

// By the sea trees grow low (owner, 2026-10-10: dune woods stood far too
// tall): the same forest 300 m from the shore is seaLow of its height
// inland, and 6 km inland it is as tall as without a coast.
func TestTreesByTheSeaGrowLow(t *testing.T) {
	if f := seaFactor(300); f != seaLow {
		t.Errorf("300 m from the sea: %.2f", f)
	}
	if f := seaFactor(math.Inf(1)); f != 1 {
		t.Errorf("inland: %.2f", f)
	}
	if a, b := seaFactor(2000), seaFactor(4000); !(seaLow < a && a < b && b < 1) {
		t.Errorf("not growing inland: %.2f at 2 km, %.2f at 4 km", a, b)
	}
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: []scenery.LatLon{p(-300, -100), p(300, -100), p(300, 1100), p(-300, 1100), p(-300, -100)}},
	}})
	sizes := func(sea func(lat, lon float64) float64) map[[2]float32]float32 {
		tr := testTerrain(c, Options{Land: lm, SeaDistance: sea})
		s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
		out := map[[2]float32]float32{}
		for _, q := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
			if q.kind < speciesKinds && q.kind%(treeVariants+1) == formForest {
				out[[2]float32{q.x, q.z}] = q.s
			}
		}
		return out
	}
	inland := sizes(nil)
	shore := sizes(func(float64, float64) float64 { return 300 })
	n := 0
	for k, s := range shore {
		if in, ok := inland[k]; ok {
			n++
			if math.Abs(float64(s/in)-seaLow) > 1e-4 {
				t.Fatalf("a tree by the sea at %.2f of its inland size", s/in)
			}
		}
	}
	if n < 100 {
		t.Errorf("%d trees compared", n)
	}
}

// Pollard willows grow in rows along ditches between fields (owner,
// 2026-10-10), on one bank, and nowhere else.
func TestWillowsAlongDitches(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	rect := func(id int64, tags map[string]string, x0, y0, x1, y1 float64) scenery.Element {
		return scenery.Element{Type: "way", ID: id, Tags: tags, Geometry: []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		rect(1, map[string]string{"landuse": "farmland"}, 10, -100, 300, 1100),
		rect(2, map[string]string{"natural": "water", "water": "ditch"}, 40, -100, 42.5, 1100), // a ditch 2.5 m wide
	}})
	tr := testTerrain(c, Options{Land: lm})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	w := speciesIndex("willow")
	n, east, leaning := 0, 0, 0
	for _, q := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
		if q.kind >= speciesKinds || q.kind/(treeVariants+1) != w {
			continue
		}
		n++
		e := float64(q.x)
		if e < 38 || e > 44.5 {
			t.Fatalf("a willow at %.1f m east, not on the ditch's bank (40-42.5 m)", e)
		}
		if e > 42.5 {
			east++
		}
		// A leaning one faces its ditch: (cos yaw, sin yaw) towards it,
		// give or take 25°.
		if q.kind%(treeVariants+1) == formForest {
			leaning++
			toWater := math.Atan2(0, 41.25-e)
			if d := math.Abs(math.Remainder(float64(q.yaw)-toWater, 2*math.Pi)); d > 26*math.Pi/180 {
				t.Errorf("a willow at %.1f m east leans %.0f° away from its ditch", e, d*180/math.Pi)
			}
		}
	}
	if leaning == 0 || leaning == n {
		t.Errorf("%d of %d willows lean", leaning, n)
	}
	if n < 10 {
		t.Errorf("%d willows along 1.2 km of ditch", n)
	}
	if east != n && east != 0 {
		t.Errorf("willows on both banks: %d east of %d", east, n)
	}
}
