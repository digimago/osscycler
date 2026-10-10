package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
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

	m := addRoadMaterials(w.doc)
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
