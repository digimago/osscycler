package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// northCourse runs 1 km due north, flat at 10 m.
func northCourse(t *testing.T) *course.Course {
	t.Helper()
	var pts []course.Point
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 10})
	}
	c, err := course.New("north", "North", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEarClip(t *testing.T) {
	// An L, clockwise, with a repeated point: its area in triangles.
	l := [][2]float64{{0, 0}, {0, 20}, {10, 20}, {10, 10}, {10, 10}, {30, 10}, {30, 0}}
	tris := earClip(l)
	area := 0.0
	for _, tr := range tris {
		a, b, c := l[tr[0]], l[tr[1]], l[tr[2]]
		cr := (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
		if cr <= 0 {
			t.Errorf("triangle %v not counter-clockwise", tr)
		}
		area += cr / 2
	}
	if math.Abs(area-400) > 1e-9 {
		t.Errorf("triangles cover %.1f m², want 400", area)
	}
}

func TestRoadFromTheMap(t *testing.T) {
	// 8 m wide with sidewalks and cycle lanes for 500 m, then 4 m of
	// gravel; in a valley whose sides the ground model raises.
	c := northCourse(t)
	sc := &scenery.Scenery{Roads: []scenery.RoadStretch{
		{FromM: 0, ToM: 500, Class: "secondary", WidthM: 8, Sidewalk: [2]bool{true, true}, CycleLane: [2]bool{true, true}},
		{FromM: 500, ToM: c.Distance, Class: "unclassified", WidthM: 4, Surface: scenery.SurfaceUnpaved},
	}}
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 10 + 0.1*math.Abs(e), true
	}
	tr := testTerrain(c, Options{Elevation: ground, Scenery: sc})

	// The ground lies flat under the wide road and its sidewalks, 12 m
	// out; beside the narrow one it is already rising there.
	wide, narrow := tr.at(12, 250), tr.at(12, 750)
	if math.Abs(wide-(10-sinkM)) > 0.01 || narrow < wide+0.02 {
		t.Errorf("12 m out: %.3f m beside the wide road, %.3f beside the narrow one; want flat at %.2f, then rising", wide, narrow, 10-sinkM)
	}
	// A cell further, the ground is the model's: the valley's sides
	// start right beside the road.
	if h := tr.at(20, 750); math.Abs(h-12) > 0.01 {
		t.Errorf("20 m out beside the narrow road: %.3f m, want the model's 12", h)
	}

	w, err := Build(c, Options{Elevation: ground, Scenery: sc, Generator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	width := func(d float64) float64 { return w.Manifest.Path.WidthM[int(d/pathStep)] }
	if width(250) != 8 || width(750) != 4 || w.Manifest.Road.Keep != "right" {
		t.Errorf("path widths %.1f and %.1f, keep %q; want 8, 4, right", width(250), width(750), w.Manifest.Road.Keep)
	}
	// The width tapers over a few metres, not in a step.
	if m := width(500); m <= 4 || m >= 8 {
		t.Errorf("width at the change %.1f, want between", m)
	}

	m := addRoadMaterials(w.doc, &styleNL)
	ps := roadMesh(tr.fine[:200], tr.fine, 0, courseLine(c, nil), false, m, nil) // the first 400 m
	got := map[int]bool{}
	for _, p := range ps {
		got[p.Material] = true
	}
	for name, mat := range map[string]int{"asphalt": m.surface[scenery.SurfaceAsphalt], "cycle lane": m.lane, "sidewalk": m.walk, "kerb": m.kerb} {
		if !got[mat] {
			t.Errorf("the wide road has no %s", name)
		}
	}
	ps = roadMesh(tr.fine[300:], tr.fine, 300, courseLine(c, nil), false, m, nil)
	if len(ps) != 1 || ps[0].Material != m.surface[scenery.SurfaceUnpaved] {
		t.Errorf("the gravel road: %d primitives", len(ps))
	}
}

func TestCarParkLiesOnTheGround(t *testing.T) {
	// A car park west of 600 m, on ground that slopes away from the road.
	c := northCourse(t)
	sc := &scenery.Scenery{Parking: []scenery.Parking{{DistanceM: 600,
		Outline: [][2]float64{{-20, 580}, {-60, 580}, {-60, 620}, {-40, 620}, {-40, 640}, {-20, 640}}}}}
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 10 + 0.05*math.Abs(e), true
	}
	tr := testTerrain(c, Options{Elevation: ground, Scenery: sc})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	p := parkingMesh(s, sc.Parking[0].Outline, 0)
	if len(p.Indices) == 0 {
		t.Fatal("no car park")
	}
	for k := 0; k < len(p.Positions); k += 3 {
		e, y, n := float64(p.Positions[k]), float64(p.Positions[k+1]), -float64(p.Positions[k+2])
		if d := y - s.at(e, n); math.Abs(d-parkingLiftM) > 0.01 {
			t.Fatalf("car park vertex at %.1f, %.1f lies %.3f m above the ground, want %.2f", e, n, d, parkingLiftM)
		}
	}
}

func TestTidyStrips(t *testing.T) {
	fine := make([]sample, 30)
	set := func(from, to int, open bool) {
		for i := from; i < to; i++ {
			fine[i].walk[1], fine[i].open[1] = !open, open
		}
	}
	set(0, 10, false) // a sidewalk,
	set(10, 12, true) // a flicker of 2 samples off (but not opened)
	fine[10].open[1], fine[11].open[1] = false, false
	set(12, 20, false)
	set(20, 22, true)  // a junction's opening: stays
	set(22, 24, false) // a 2-sample piece: goes
	set(24, 30, true)
	tidyStrips(fine)
	var got []byte
	for _, s := range fine {
		if s.walk[1] {
			got = append(got, 'x')
		} else {
			got = append(got, '.')
		}
	}
	if want := "xxxxxxxxxxxxxxxxxxxx.........."; string(got) != want {
		t.Errorf("sidewalk %s, want %s", got, want)
	}
}

func TestRoadRiddenTwiceIsDrawnOnce(t *testing.T) {
	// 500 m north and back the same way: the way back is the same road.
	var pts []course.Point
	for i := 0; i <= 50; i++ {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 10})
	}
	for i := 49; i >= 0; i-- {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5.000001, Ele: 10})
	}
	c, err := course.New("back", "Back", pts)
	if err != nil {
		t.Fatal(err)
	}
	fine := sampleRoad(c, courseLine(c, nil), fineStep)
	applyRoads(fine, nil, 5, 5)
	l := &roadLine{samples: fine, route: true}
	markPassesAgain([]*roadLine{l})
	again := 0
	for _, s := range l.samples {
		if s.again {
			again++
			if s.d < 500 {
				t.Fatalf("the way out marked as ridden again at %.0f m", s.d)
			}
		}
	}
	if want := len(fine) / 2; again < want-30 { // all but the turn
		t.Errorf("%d samples of the way back marked, want about %d", again, want)
	}
}

func TestLoopClosesOnItself(t *testing.T) {
	// A loop's last sample is its first again: the same pass, drawn, so
	// the road is closed at the line.
	var fine []sample
	const r, n = 100.0, 315
	for i := 0; i <= n; i++ {
		a := 2 * math.Pi * float64(i) / n
		fine = append(fine, sample{d: 2 * math.Pi * r * float64(i) / n, e: r * math.Sin(a), n: r * math.Cos(a)})
	}
	l := &roadLine{samples: fine, loop: true, route: true}
	markPassesAgain([]*roadLine{l})
	for _, s := range l.samples {
		if s.again {
			t.Fatalf("the loop marked as ridden again at %.0f m", s.d)
		}
	}
}

// A sidewalk's strip is closed at its short ends: where it starts or
// stops without a dropped kerb (a junction's edge, the end of what is
// drawn), a kerb face stands across it from the road's edge to the outer
// face's foot, so the hollow slab never shows (owner, 2026-10-12: on the
// Velp loop sidewalks began or ended without a solid end, to look into).
func TestSidewalkEndsAreClosed(t *testing.T) {
	c := northCourse(t)
	sc := &scenery.Scenery{Roads: []scenery.RoadStretch{
		{FromM: 0, ToM: c.Distance, Class: "residential", WidthM: 6, Sidewalk: [2]bool{true, true}}}}
	tr := testTerrain(c, Options{Scenery: sc})
	m := addRoadMaterials(gltf.New("test"), &styleNL)
	// The first 400 m: the strip starts at the first cross-section and
	// stops at the last, each end a face across both sidewalks.
	ps := roadMesh(tr.fine[:200], tr.fine, 0, courseLine(c, nil), false, m, nil)
	across := 0
	for _, p := range ps {
		if p.Material != m.kerb {
			continue
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			var v [3][3]float64
			for k := range 3 {
				j := p.Indices[i+k]
				v[k] = [3]float64{float64(p.Positions[3*j]), float64(p.Positions[3*j+1]), float64(p.Positions[3*j+2])}
			}
			u, w := [3]float64{v[1][0] - v[0][0], v[1][1] - v[0][1], v[1][2] - v[0][2]}, [3]float64{v[2][0] - v[0][0], v[2][1] - v[0][1], v[2][2] - v[0][2]}
			n := [3]float64{u[1]*w[2] - u[2]*w[1], u[2]*w[0] - u[0]*w[2], u[0]*w[1] - u[1]*w[0]}
			if l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]); l > 0 && math.Abs(n[2])/l > 0.9 {
				across++ // facing along the road (north: glTF -z)
			}
		}
	}
	if across != 8 {
		t.Errorf("%d kerb triangles face along the road, want 8: two per end of each sidewalk", across)
	}
}

// A junction's surface is fat triangles, not a fan of slivers from one
// corner (owner, 2026-10-13: diagonal streaks across a junction, Dunes
// course 13.02 km): the outline's triangles flipped to Delaunay's cover
// the same area (flips stay inside convex quadrilaterals, so inside the
// outline), and the thinnest is far fatter than ear clipping's.
func TestJunctionTrianglesAreFat(t *testing.T) {
	// A crossing's outline: four 6 m wide arms 20 m long, a point every
	// 2 m along each side, as a junction's outline has.
	var poly [][2]float64
	add := func(a, b [2]float64) {
		n := int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1]) / 2))
		for i := range n {
			f := float64(i) / float64(n)
			poly = append(poly, [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])})
		}
	}
	corners := [][2]float64{{3, -20}, {3, -3}, {20, -3}, {20, 3}, {3, 3}, {3, 20}, {-3, 20}, {-3, 3}, {-20, 3}, {-20, -3}, {-3, -3}, {-3, -20}}
	for i := range corners {
		add(corners[i], corners[(i+1)%len(corners)])
	}
	minAngle := func(tris [][3]int) (float64, float64) {
		worst, area := 180.0, 0.0
		for _, tr := range tris {
			a, b, c := poly[tr[0]], poly[tr[1]], poly[tr[2]]
			area += ((b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])) / 2
			for k := range 3 {
				p, q, r := poly[tr[k]], poly[tr[(k+1)%3]], poly[tr[(k+2)%3]]
				u := [2]float64{q[0] - p[0], q[1] - p[1]}
				v := [2]float64{r[0] - p[0], r[1] - p[1]}
				ang := math.Abs(math.Atan2(u[0]*v[1]-u[1]*v[0], u[0]*v[0]+u[1]*v[1])) * 180 / math.Pi
				worst = math.Min(worst, ang)
			}
		}
		return worst, area
	}
	ear := earClip(poly)
	fat := delaunayFlip(poly, ear)
	ea, earArea := minAngle(ear)
	fa, fatArea := minAngle(fat)
	if math.Abs(fatArea-earArea) > 1e-6 || math.Abs(fatArea-(4*17*6+36)) > 1e-6 {
		t.Fatalf("area %.2f, ear clipping's %.2f", fatArea, earArea)
	}
	if fa < 5 || fa < 10*ea {
		t.Errorf("thinnest corner %.1f° (ear clipping's %.1f°)", fa, ea)
	}
}

// A car park's outline running just off a road's edge is taken onto the
// road (parkingOntoRoads): a map outline a few centimetres outside the
// drawn carriageway left a slit between the two surfaces too narrow for
// the land to fill (the Route des Crêtes' car parks, 15.3-15.5 km of the
// Route des Américains).
func TestParkingOutlineMeetsTheRoad(t *testing.T) {
	// A road 3.5 m half wide along n, its right edge at e 3.5, 10 m high.
	road := gltf.Primitive{Material: 1}
	for _, q := range [][3]float64{{-3.5, 10, 50}, {3.5, 10, 50}, {3.5, 10, -50}, {-3.5, 10, -50}} {
		road.Positions = append(road.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
	}
	road.Indices = []uint32{0, 1, 2, 0, 2, 3}
	rs := newSurfaceIndex([]gltf.Primitive{road}, func(int) bool { return true }, 200)
	// The car park: its west edge 0.2 m off the road's, from n -20 to 20.
	outline := [][2]float64{{3.7, -20}, {13.7, -20}, {13.7, 20}, {3.7, 20}}
	out := parkingOntoRoads(outline, rs)
	west, far := 0, 0
	for _, q := range out {
		switch {
		case q[0] < 5:
			west++
			if q[0] > 3.5 || q[0] < 3.1 {
				t.Errorf("a west point at e %.2f, want it just in over the road's edge at 3.5", q[0])
			}
		case math.Abs(q[0]-13.7) < 1e-9:
			far++
		default:
			t.Errorf("a point at %v, neither on the west edge nor the far one", q)
		}
	}
	if west < 2 || far != 2 {
		t.Errorf("%d west points and %d far ones; want the west edge on the road (its ends at least) and the far edge as it was", west, far)
	}
	if len(out) > 8 {
		t.Errorf("%d points, want the collinear ones along the road dropped", len(out))
	}
}

// The course's own way isn't drawn where it crosses a car park or a paved
// area: the car park is the surface there (an own way over the Hohneck
// col's apron showed as a dark seam, 16.1 km of the Route des Américains).
func TestOwnWayHiddenInACarPark(t *testing.T) {
	own := &roadLine{route: true}
	road := &roadLine{}
	for n := 0.0; n <= 100; n += fineStep {
		own.samples = append(own.samples, sample{d: n, e: 0, n: n, ele: 10, hw: 1.75})
		road.samples = append(road.samples, sample{d: n, e: 20, n: n, ele: 10, hw: 3})
	}
	sc := &scenery.Scenery{Parking: []scenery.Parking{{Outline: [][2]float64{{-10, 41}, {19, 41}, {19, 59}, {-10, 59}}}}}
	hideOwnWaysInLots([]*roadLine{own, road}, nil, sc)
	for _, s := range own.samples {
		if in := s.n > 40 && s.n < 60; s.onPatch != in {
			t.Errorf("own way at %.0f m: onPatch %v, want %v (inside the car park)", s.n, s.onPatch, in)
		}
	}
	for _, s := range road.samples {
		if s.onPatch {
			t.Error("a map road's samples hidden in the car park")
		}
		// The road's left edge (e 17) lies in the car park from 41 to 59 m
		// (its right edge, e 23, outside it):
		// no skirt there, the surface goes on.
		if in := s.n > 41 && s.n < 59; s.butt[0] != in || s.butt[1] {
			t.Errorf("road at %.0f m: butt %v, want left %v and right false", s.n, s.butt, in)
		}
	}
}

// A car park's rim is drawn along its outline where land lies beside it,
// not where the outline runs under a road: the road is the surface there
// (a rim across the Hohneck col's apron stood as a line on the asphalt).
func TestRimNotUnderARoad(t *testing.T) {
	road := gltf.Primitive{Material: 1}
	for _, q := range [][3]float64{{-3.5, 10, 50}, {3.5, 10, 50}, {3.5, 10, -50}, {-3.5, 10, -50}} {
		road.Positions = append(road.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
	}
	road.Indices = []uint32{0, 1, 2, 0, 2, 3}
	roads := newSurfaceIndex([]gltf.Primitive{road}, func(int) bool { return true }, 200)
	land := newSurfaceIndex(nil, func(int) bool { return true }, 200)
	// The car park reaches from inside the road (e 0) out to e 20.
	outline := [][2]float64{{0, -10}, {20, -10}, {20, 10}, {0, 10}}
	p := rimMesh(outline, func(e, n float64) float64 { return 9.5 }, land, roads, 2)
	for v := 0; v+2 < len(p.Positions); v += 3 {
		if e := float64(p.Positions[v]); e < 3.4 {
			t.Fatalf("a rim vertex at e %.2f, under the road", e)
		}
	}
}

// Roads inside a car park (its aisles, a road's last metres inside it)
// are taken out of the network: the car park is the surface there (the
// Hohneck's summit car park: a loop ridden in it was matched onto the
// service way through it and came out as a hairpin on that line).
func TestTrimLinesInLots(t *testing.T) {
	road := &roadLine{}
	for n := 0.0; n <= 100; n += fineStep {
		road.samples = append(road.samples, sample{d: n, e: 0, n: n, ele: 10, hw: 2})
	}
	aisle := &roadLine{}
	for e := 10.0; e <= 30; e += fineStep {
		aisle.samples = append(aisle.samples, sample{d: e - 10, e: e, n: 70, ele: 10, hw: 1.5})
	}
	own := &roadLine{route: true}
	for n := 0.0; n <= 100; n += fineStep {
		own.samples = append(own.samples, sample{d: n, e: 20, n: n, ele: 10, hw: 1.75})
	}
	// The car park from n 60 on, over e 5..40: the road's last 40 m and
	// the whole aisle lie in it.
	lots := []scenery.Parking{{Outline: [][2]float64{{-5, 60}, {40, 60}, {40, 120}, {-5, 120}}}}
	out := trimLinesInLots([]*roadLine{road, aisle, own}, lots)
	if len(out) != 2 {
		t.Fatalf("%d lines, want the road's outside piece and the course's own line", len(out))
	}
	for _, l := range out {
		if l.route {
			if len(l.samples) != len(own.samples) {
				t.Error("the course's own line was trimmed")
			}
			continue
		}
		if last := l.samples[len(l.samples)-1]; last.n >= 60 || len(l.samples) < 25 {
			t.Errorf("the road's piece ends at %.0f m with %d samples, want outside the car park, 30 samples", last.n, len(l.samples))
		}
	}
}

// An apron (a paved junction area mapped as a highway) keeps the roads
// running over it; only a car park's aisles are trimmed.
func TestApronKeepsItsRoads(t *testing.T) {
	road := &roadLine{}
	for n := 0.0; n <= 100; n += fineStep {
		road.samples = append(road.samples, sample{d: n, e: 0, n: n, ele: 10, hw: 2})
	}
	lots := []scenery.Parking{{Outline: [][2]float64{{-10, 40}, {10, 40}, {10, 60}, {-10, 60}}, Apron: true}}
	out := trimLinesInLots([]*roadLine{road}, lots)
	if len(out) != 1 || len(out[0].samples) != len(road.samples) {
		t.Errorf("the road through an apron was trimmed: %d lines", len(out))
	}
}

// A car park's surface is cut along a grid with shared vertices: a long
// strip beside a road on a slope comes out smooth, each vertex one height
// and one normal, and no fan of slivers (owner, 2026-10-18, the car parks
// beside the Route des Crêtes at 19.7 km rippled with light and dark
// bands).
func TestCarParkSurfaceIsAGrid(t *testing.T) {
	// A strip 6 m wide and 300 m long, its long sides sampled every
	// half metre with a wobble (as parkingOntoRoads leaves them), on a 9 %
	// slope across a 2 m swell.
	var outline [][2]float64
	for x := 0.0; x <= 300; x += 0.5 {
		outline = append(outline, [2]float64{x, 0.02 * math.Sin(x)})
	}
	for x := 300.0; x >= 0; x -= 0.5 {
		outline = append(outline, [2]float64{x, 6 + 0.02*math.Cos(x)})
	}
	height := func(e, n float64) float64 { return 0.09*e + 2*math.Sin(e/40) }
	p := gridDraped(height, outline, 0, 0, 1)
	area, worst := 0.0, 0.0
	slivers := 0
	for i := 0; i+2 < len(p.Indices); i += 3 {
		var v [3][2]float64
		for k := range 3 {
			v[k] = pc2(p, p.Indices[i+k])
		}
		a := math.Abs((v[1][0]-v[0][0])*(v[2][1]-v[0][1])-(v[1][1]-v[0][1])*(v[2][0]-v[0][0])) / 2
		area += a
		long := 0.0
		for k := range 3 {
			long = math.Max(long, math.Hypot(v[(k+1)%3][0]-v[k][0], v[(k+1)%3][1]-v[k][1]))
		}
		worst = math.Max(worst, long)
		// Away from the outline (cells cut by it may leave a thin strip
		// along a grid line, in the surface's own plane).
		cn := (v[0][1] + v[1][1] + v[2][1]) / 3
		if 2*a/long < 0.05 && long > 0.5 && cn > 1 && cn < 5 {
			slivers++
		}
	}
	if math.Abs(area-1800) > 5 {
		t.Errorf("the surface covers %.0f m², want the outline's 1800", area)
	}
	if worst > 1.5 {
		t.Errorf("a triangle %.2f m long, want the grid's cells (under 1.5 m)", worst)
	}
	if slivers > 0 {
		t.Errorf("%d slivers over half a metre long inside the car park, want none", slivers)
	}
	// Vertices shared: one per grid point (~2,100) and per outline point
	// (~1,200), not three per triangle.
	if n := len(p.Positions) / 3; n > 4000 {
		t.Errorf("%d vertices for %d triangles: not shared", n, len(p.Indices)/3)
	}
}
