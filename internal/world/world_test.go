package world

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestBuildTracks(t *testing.T) {
	for _, c := range course.Tracks() {
		t.Run(c.ID, func(t *testing.T) {
			w, err := Build(c, Options{Generator: "test"})
			if err != nil {
				t.Fatal(err)
			}
			m := w.Manifest
			if m.Terrain.Source != "course profile" || m.Terrain.Chunks == 0 || w.Vertices == 0 {
				t.Errorf("terrain %+v, %d vertices", m.Terrain, w.Vertices)
			}
			want := 3 * (int(math.Ceil(c.Distance/pathStep)) + 1)
			if len(m.Path.XYZ) != want {
				t.Errorf("path has %d numbers, want %d", len(m.Path.XYZ), want)
			}
			line, bridge := false, false
			for _, p := range w.prims {
				line = line || w.doc.MaterialName(p.Material) == "start line"
				bridge = bridge || w.doc.MaterialName(p.Material) == "bridge"
			}
			if !line {
				t.Error("no start line on the loop")
			}
			if want := c.ID == "figure-8"; bridge != want {
				t.Errorf("bridge %v, want %v (the figure 8 crosses itself over a bridge)", bridge, want)
			}
			ele0, _ := c.At(0)
			// At the start, give or take its corner rounded off.
			if x, y, z := m.Path.XYZ[0], m.Path.XYZ[1], m.Path.XYZ[2]; math.Hypot(x, z) > maxDevM || math.Abs(y-ele0) > 0.01 {
				t.Errorf("path starts at %v %v %v, want 0 %v 0", x, y, z, ele0)
			}
			if c.Loop {
				n := len(m.Path.XYZ)
				if d := math.Hypot(m.Path.XYZ[n-3], m.Path.XYZ[n-1]); d > 0.5 {
					t.Errorf("loop path ends %.2f m from its start", d)
				}
			}

			if r := w.Check(); r.HolesM2 > 0.5 || r.LandOverRoadM2 > 0.1 || r.OffRoadM > 0 {
				t.Errorf("check: holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v",
					r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad)
			}

			dir := t.TempDir()
			if err := w.Write(dir); err != nil {
				t.Fatal(err)
			}
			glb, err := os.ReadFile(filepath.Join(dir, ModelFile))
			if err != nil || string(glb[:4]) != "glTF" {
				t.Fatalf("model: %v", err)
			}
			b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
			if err != nil {
				t.Fatal(err)
			}
			var back Manifest
			if err := json.Unmarshal(b, &back); err != nil || back.Format != Format || back.Course.ID != c.ID {
				t.Errorf("manifest %+v: %v", back.Course, err)
			}
		})
	}
}

// testTerrain sets up the terrain of c as Build does.
func testTerrain(c *course.Course, o Options) *terrain {
	o.defaults()
	fine := sampleRoad(c, courseLine(c, nil), fineStep)
	var roads []scenery.RoadStretch
	if o.Scenery != nil {
		roads = o.Scenery.Roads
	}
	applyRoads(fine, roads, o.RoadWidthM, o.CellM)
	var coarse []sample
	for i := 0; i < len(fine); i += coarseStep {
		coarse = append(coarse, fine[i])
	}
	if o.Elevation != nil {
		groundOffsets(c, coarse, o.Elevation)
	}
	return &terrain{o: o, c: c, fine: fine, coarse: coarse, flattenM: o.RoadWidthM/2 + flatPast(o.CellM),
		cover: &cover{m: o.Land, banks: map[int]*banks{}}, loops: []bool{c.Loop}}
}

func (t *terrain) at(e, n float64) float64 {
	var segs []int
	for i := 0; i+1 < len(t.fine); i++ {
		segs = append(segs, i)
	}
	return t.height(e, n, segs, t.coarse)
}

func TestBridge(t *testing.T) {
	// The figure of eight crosses itself a quarter of the way round, under
	// the bridge it comes back over at three quarters, 10 m higher.
	c := course.Tracks()[1]
	tr := testTerrain(c, Options{})
	e, n := c.Project(c.Position(c.Distance / 4))
	under, _ := c.At(c.Distance / 4)
	over, _ := c.At(3 * c.Distance / 4)
	if over-under < 8 {
		t.Fatalf("the fixture changed: roads at %.1f and %.1f m", under, over)
	}
	if h := tr.at(e, n); math.Abs(h-(under-sinkM)) > 0.3 {
		t.Errorf("ground at the crossing %.2f m, want the lower road's %.2f", h, under-sinkM)
	}
}

func TestGroundModel(t *testing.T) {
	// 2 km due north, flat at 10 m, in a valley whose floor the ground model
	// puts 5 m higher than the course: a constant offset, which the terrain
	// drops so it meets the road.
	var pts []course.Point
	const lat0, lon0 = 52.0, 5.0
	for i := range 201 {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 10})
	}
	c, err := course.New("valley", "Valley", pts)
	if err != nil {
		t.Fatal(err)
	}
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 15 + 0.1*math.Abs(e), true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	for _, k := range []struct{ e, want, tol float64 }{
		{0, 10 - sinkM, 0.01}, // under the road
		{5, 10 - sinkM, 0.01}, // the shoulder
		{100, 20, 0.05},       // beyond the blend: the ground model, 5 m down
		{-200, 30, 0.05},      // the other side
	} {
		if h := tr.at(k.e, 1000); math.Abs(h-k.want) > k.tol {
			t.Errorf("%v m east of the road: %.2f m, want %.2f", k.e, h, k.want)
		}
	}
	// Rising monotonically through the blend.
	prev := tr.at(0, 1000)
	for e := 1.0; e <= 60; e++ {
		h := tr.at(e, 1000)
		if h < prev-1e-9 {
			t.Fatalf("terrain dips at %v m: %.3f after %.3f", e, h, prev)
		}
		prev = h
	}
}

func TestNoCliffsBetweenPasses(t *testing.T) {
	// Near the figure of eight's crossing the two passes run close together
	// 10 m apart in height, as on a switchback. Off the road the ground must
	// stay continuous: no step between neighbouring points 0.5 m apart
	// steeper than 1.5:1 (the embankment between the passes is about 1:1;
	// before passes were blended the ground stepped 10 m here). Within 25 m of the crossing itself the ground drops
	// from the upper road to the lower: the bridge's abutment, a wall.
	c := course.Tracks()[1]
	tr := testTerrain(c, Options{})
	e0, n0 := c.Project(c.Position(c.Distance / 4))
	var segs []int
	for i := 0; i+1 < len(tr.fine); i++ {
		if math.Hypot(tr.fine[i].e-e0, tr.fine[i].n-n0) < 200 {
			segs = append(segs, i)
		}
	}
	onRoad := func(e, n float64) bool {
		for _, i := range segs {
			if math.Hypot(tr.fine[i].e-e, tr.fine[i].n-n) < tr.flattenM+fineStep {
				return true
			}
		}
		return false
	}
	at := func(e, n float64) float64 { return tr.height(e, n, segs, tr.coarse) }
	const step = 0.5
	worst := 0.0
	for de := -80.0; de <= 80; de += step {
		for dn := -80.0; dn <= 80; dn += step {
			e, n := e0+de, n0+dn
			if math.Hypot(de, dn) < 25 || onRoad(e, n) || onRoad(e+step, n) || onRoad(e, n+step) {
				continue
			}
			h := at(e, n)
			worst = max(worst, math.Abs(at(e+step, n)-h), math.Abs(at(e, n+step)-h))
		}
	}
	if worst > 1.5*step {
		t.Errorf("ground steps %.2f m between points %.1f m apart", worst, step)
	}
}

func TestLandCover(t *testing.T) {
	// 2 km due north, flat at 10 m. Forest on the left all along; a pond
	// right of the road from 900 to 1100 m, 60 to 160 m out, where the
	// ground model has no heights (as AHN over water). The ground rises
	// eastwards, so the pond's banks lie between 10.6 and 11.6 m.
	var pts []course.Point
	const lat0, lon0 = 52.0, 5.0
	for i := range 201 {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 10})
	}
	c, err := course.New("pond", "Pond", pts)
	if err != nil {
		t.Fatal(err)
	}
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + y/111195, Lon: lon0 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	inPond := func(e, n float64) bool { return e > 60 && e < 160 && n > 900 && n < 1100 }
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		if inPond(e, n) {
			return 0, false
		}
		return 10 + 0.01*math.Max(0, e), true
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-400, -100, -3, 2100)},
		{Type: "way", ID: 2, Tags: map[string]string{"natural": "water"}, Geometry: ring(60, 900, 160, 1100)},
	}})
	tr := testTerrain(c, Options{Elevation: ground, Land: lm})

	g, w := tr.chunk([2]int{0, 4}, 0, 1) // 0..250 m east, 1000..1250 m north
	// The vertex at e, no, by position (blocks far from the route have
	// fewer: lod.go).
	vertex := func(e, no float64) int {
		for v := 0; 3*v < len(g.Positions); v++ {
			if math.Abs(float64(g.Positions[3*v])-e) < 0.01 && math.Abs(float64(-g.Positions[3*v+2])-no) < 0.01 {
				return v
			}
		}
		t.Fatalf("no terrain vertex at %.0f, %.0f", e, no)
		return -1
	}
	color := func(v int) [3]float32 { return [3]float32{g.Colors[3*v], g.Colors[3*v+1], g.Colors[3*v+2]} }
	if got := color(vertex(200, 1200)); got != landColors[scenery.LandNone] {
		t.Errorf("open ground: colour %v, want grass", got)
	}
	if len(w.Positions) == 0 {
		t.Fatal("no water in the pond's chunk")
	}
	level := float64(w.Positions[1])
	for k := 1; k < len(w.Positions); k += 3 {
		if float64(w.Positions[k]) != level {
			t.Fatalf("water not flat: %v and %v", w.Positions[k], level)
		}
	}
	if level < 10.6-freeboardM-0.1 || level >= 10.6 {
		t.Errorf("water level %.2f m, want just below the lowest bank (10.6 m)", level)
	}
	if h := float64(g.Positions[3*vertex(110, 1050)+1]); h > level-bedM+0.01 {
		t.Errorf("ground under the pond at %.2f m, above the bed (level %.2f)", h, level)
	}
	for _, e := range []float64{55, 165} {
		if bank := float64(g.Positions[3*vertex(e, 1050)+1]); bank <= level {
			t.Errorf("bank %v m out at %.2f m, not above the water at %.2f", e, bank, level)
		}
	}

	// The forest on the left, the verge beside the road grass.
	g, _ = tr.chunk([2]int{-1, 4}, 0, 1) // 250..0 m west
	if got := color(vertex(-100, 1100)); got != landColors[scenery.LandForest] {
		t.Errorf("forest: colour %v, want the forest floor", got)
	}
	if got := color(vertex(0, 1100)); got != landColors[verdure] {
		t.Errorf("the road's edge: colour %v, want the verge", got)
	}
}

func TestRiverFalls(t *testing.T) {
	// A road 2 km due north rising 1 m per km, a river beside it 60 to
	// 120 m out, its banks 0.6 m above the road's height there.
	var pts []course.Point
	const lat0, lon0 = 52.0, 5.0
	for i := range 201 {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 10 + float64(i)*0.01})
	}
	c, err := course.New("river", "River", pts)
	if err != nil {
		t.Fatal(err)
	}
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + y/111195, Lon: lon0 + x/kx} }
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		if e > 60 && e < 120 {
			return 0, false // water
		}
		return 10 + 0.001*n + 0.01*math.Max(0, e), true
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{{Type: "way", ID: 1,
		Tags:     map[string]string{"natural": "water", "water": "river"},
		Geometry: []scenery.LatLon{p(60, -300), p(120, -300), p(120, 2300), p(60, 2300), p(60, -300)}}}})
	tr := testTerrain(c, Options{Elevation: ground, Land: lm})

	levelAt := func(key [2]int) float64 {
		_, w := tr.chunk(key, 0, 1)
		if len(w.Positions) == 0 {
			t.Fatalf("no water in chunk %v", key)
		}
		var sum float64
		for k := 1; k < len(w.Positions); k += 3 {
			sum += float64(w.Positions[k])
		}
		return sum / float64(len(w.Positions)/3)
	}
	low, high := levelAt([2]int{0, 1}), levelAt([2]int{0, 5}) // around 375 m and 1375 m
	if d := high - low; d < 0.7 || d > 1.3 {
		t.Errorf("the river falls %.2f m over 1 km, want about 1 m (%.2f to %.2f)", d, low, high)
	}

	// Chunks meet: the ground on their shared edge is the same on both
	// sides, water or not.
	a, _ := tr.chunk([2]int{0, 1}, 0, 1)
	b, _ := tr.chunk([2]int{0, 2}, 0, 1)
	edge := 2 * tr.o.ChunkM
	for _, pair := range [][2]gltf.Primitive{{a, b}, {b, a}} {
		for _, v := range edgeVertices(pair[0], edge) {
			if other := edgeHeight(pair[1], edge, v[0]); math.Abs(other-v[1]) > 1e-4 {
				t.Fatalf("crack at %v m east: %.3f against %.3f", v[0], v[1], other)
			}
		}
	}
}

// edgeVertices are p's vertices on the line north = no: east and height,
// west to east.
func edgeVertices(p gltf.Primitive, no float64) [][2]float64 {
	var out [][2]float64
	for v := 0; 3*v < len(p.Positions); v++ {
		if math.Abs(float64(-p.Positions[3*v+2])-no) < 1e-3 {
			out = append(out, [2]float64{float64(p.Positions[3*v]), float64(p.Positions[3*v+1])})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// edgeHeight is p's edge along north = no at east e, between its
// vertices there.
func edgeHeight(p gltf.Primitive, no, e float64) float64 {
	vs := edgeVertices(p, no)
	for k := 0; k+1 < len(vs); k++ {
		if a, b := vs[k], vs[k+1]; e >= a[0]-1e-3 && e <= b[0]+1e-3 {
			return a[1] + (e-a[0])/math.Max(b[0]-a[0], 1e-9)*(b[1]-a[1])
		}
	}
	return math.NaN()
}

func TestWaterLiesBelowTheRoadBesideIt(t *testing.T) {
	// Without a ground model the figure 8's ditches took their level from
	// the profile's shape alone and stood up to 0.5 m above the road they
	// run up to (owner, 2026-10-09, 1 km): the banks are the ground as
	// drawn now, the road's shaping included.
	c := course.Tracks()[1]
	ts := scenery.TrackScenery(c)
	o := Options{Generator: "test", Land: scenery.NewLandMap(c, ts.Data), Scenery: scenery.Build(c, ts.Data),
		Buildings: scenery.Footprints(c, ts.Data), Lawns: ts.Lawns, Fences: ts.Fences}
	if ts.Roads != nil {
		o.Scenery.Roads = ts.Roads
	}
	w, err := Build(c, o)
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	worst, n := math.Inf(-1), 0
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "water" {
			continue
		}
		for k := 0; k+2 < len(p.Positions); k += 3 {
			x, y, z := float64(p.Positions[k]), float64(p.Positions[k+1]), float64(p.Positions[k+2])
			best, road := math.Inf(1), 0.0
			for i := 0; i+2 < len(xyz); i += 3 {
				if d := math.Hypot(xyz[i]-x, xyz[i+2]-z); d < best {
					best, road = d, xyz[i+1]
				}
			}
			if best < 15 {
				n++
				worst = max(worst, y-road)
			}
		}
	}
	if n == 0 {
		t.Fatal("no water within 15 m of the road")
	}
	if worst > -0.1 {
		t.Errorf("water within 15 m of the road stands %+.2f m from it, want below by 0.1 m or more", worst)
	}
}
