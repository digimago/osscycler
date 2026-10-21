package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestChainsJoinARoadSplitByTheMap(t *testing.T) {
	// One street in two ways (the map splits where tags change), then a
	// different street on: two lines.
	ways := []scenery.Way{
		{Line: [][2]float64{{0, 0}, {0, 100}}, Nodes: []int64{1, 2}, Name: "A"},
		{Line: [][2]float64{{0, 200}, {0, 100}}, Nodes: []int64{3, 2}, Name: "A"}, // drawn the other way
		{Line: [][2]float64{{0, 200}, {100, 200}}, Nodes: []int64{3, 4}, Name: "B"},
	}
	cs := chains(ways, nil)
	if len(cs) != 2 {
		t.Fatalf("%d chains, want 2", len(cs))
	}
	a := cs[0]
	if len(a) != 3 || a[0].node != 1 || a[2].node != 3 || !a[1].reversed || a[0].reversed {
		t.Errorf("street A: %+v", a)
	}
}

func TestGroundAtAJunction(t *testing.T) {
	// Two roads crossing, apart in height by less than a bridge: under
	// the higher one near the crossing the ground goes down towards the
	// lower, as far as the higher road's skirt hides; a little lower (a
	// service road beside a main road) it lies under both.
	for _, tc := range []struct{ upper, want float64 }{
		{10.5, 10.5 - skirtM},
		{10.15, 10 - sinkM},
	} {
		tr := testTerrain(northCourse(t), Options{})
		var fine []sample
		for d := -50.0; d <= 50; d += fineStep {
			fine = append(fine, sample{d: d + 50, e: 0, n: 500 + d, ele: 10, hw: 3, edge: 3, flat: 12, line: 0})
		}
		for d := -50.0; d <= 50; d += fineStep {
			fine = append(fine, sample{d: d + 50, e: d, n: 500, ele: tc.upper, hw: 3, edge: 3, flat: 12, line: 1})
		}
		tr.fine, tr.loops = fine, []bool{false, false}
		segs, near := tr.lists([2]int{0, 2})
		if h := tr.height(5, 500, segs, near); math.Abs(h-tc.want) > 0.01 {
			t.Errorf("under the road at %.2f m, 5 m from the crossing: %.2f m, want %.2f", tc.upper, h, tc.want)
		}
		if h := tr.height(0, 530, segs, near); math.Abs(h-(10-sinkM)) > 0.01 {
			t.Errorf("under the lower road: %.2f m, want %.2f", h, 10-sinkM)
		}
	}
}

func TestRiderKeepsToTheMainRoad(t *testing.T) {
	// A main road north; beside it, 12 m east, an access road (a ventweg)
	// broken at 400-440 m where a side road joins. The route follows the
	// access road (a router's bike profile likes it): the course is the
	// main road, at a steady pace, with no hop where the access road breaks.
	var pts []course.Point
	kx := 111195 * math.Cos(52*math.Pi/180)
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5 + 12/kx, Ele: 10})
	}
	c, err := course.New("vent", "Vent", pts)
	if err != nil {
		t.Fatal(err)
	}
	e0, _ := c.Project(52, 5) // the main road's east, about -12 m
	ways := []scenery.Way{
		{Line: [][2]float64{{e0, -60}, {e0, 1060}}, Nodes: []int64{1, 2}, Name: "Hoofdweg", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, -60}, {0, 400}}, Nodes: []int64{3, 4}, Name: "Hoofdweg", Class: "residential", WidthM: 4},
		{Line: [][2]float64{{0, 440}, {0, 1060}}, Nodes: []int64{5, 6}, Name: "Hoofdweg", Class: "residential", WidthM: 4},
	}
	on := make([]int, 201)
	for i := range on {
		switch d := float64(i) * 5; {
		case d < 400:
			on[i] = 1
		case d > 440:
			on[i] = 2
		default:
			on[i] = 0 // the route crosses over to the main road for the gap
		}
	}
	w, err := Build(c, Options{Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}, Generator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 0; 3*i < len(xyz); i++ {
		if d := float64(i) * pathStep; d > 20 && d < 980 && math.Abs(xyz[3*i]-e0) > 0.5 {
			t.Fatalf("at %.0f m the rider is %.1f m east, want on the main road at %.1f", d, xyz[3*i], e0)
		}
		if i > 0 {
			if gap := math.Hypot(xyz[3*i]-xyz[3*i-3], xyz[3*i+2]-xyz[3*i-1]); gap < 3 || gap > 7 {
				t.Fatalf("at %.0f m the rider moves %.1f m in one %.0f m step", float64(i)*pathStep, gap, pathStep)
			}
		}
	}
}

func TestJunctionCoversRoadsJoining(t *testing.T) {
	// A road north, 4 m wide; another joins it at 100 m from 10 m east at
	// its start (a carriageway joining): the junction is one surface back
	// to where they stop overlapping, about 40 m before the node.
	line := func(from, to [2]float64, node float64) *roadLine {
		l := &roadLine{nodes: map[int64][]float64{1: {node}}}
		length := math.Hypot(to[0]-from[0], to[1]-from[1])
		var d, x, y []float64
		for at := 0.0; at <= length+1e-9; at += fineStep {
			f := at / length
			e, n := from[0]+f*(to[0]-from[0]), from[1]+f*(to[1]-from[1])
			l.samples = append(l.samples, sample{d: at, e: e, n: n, hw: 2, ele: 10})
			d, x, y = append(d, at), append(x, e), append(y, n)
		}
		l.smooth = newSmoothLine(d, x, y, false, nil)
		return l
	}
	main := line([2]float64{0, 0}, [2]float64{0, 200}, 100)
	join := line([2]float64{10, 0}, [2]float64{0, 100}, math.Hypot(10, 100))
	ps := networkPatches([]*roadLine{main, join}, nil, 0)
	if len(ps) != 1 {
		t.Fatalf("%d junctions, want 1", len(ps))
	}
	if !inPoly(ps[0].poly, 0, 70) || !inPoly(ps[0].poly, 4, 70) {
		t.Errorf("30 m before the node, where the roads overlap, not on the junction")
	}
	if inPoly(ps[0].poly, 0, 40) {
		t.Errorf("60 m before the node, where the roads are apart, on the junction")
	}
	if inPoly(ps[0].poly, 0, 130) {
		t.Errorf("30 m past the node, on the junction")
	}
}

func TestShortLinksAreDrawn(t *testing.T) {
	// A main road broken at two junctions 8 m apart (the map splits it
	// there): the 8 m piece between them is drawn; a 6 m stub off it that
	// leads nowhere is not.
	ways := []scenery.Way{
		{Line: [][2]float64{{-100, 0}, {0, 0}}, Nodes: []int64{1, 2}, Name: "Grebbeweg", WidthM: 6},
		{Line: [][2]float64{{0, 0}, {-8, 0}}, Nodes: []int64{2, 3}, Name: "Herenstraat", WidthM: 6},
		{Line: [][2]float64{{-8, 0}, {-108, 0}}, Nodes: []int64{3, 4}, Name: "Herenstraat", WidthM: 6},
		{Line: [][2]float64{{0, 0}, {0, 60}}, Nodes: []int64{2, 5}, Name: "Spoorbaanweg", WidthM: 4},
		{Line: [][2]float64{{-8, 0}, {-8, 60}}, Nodes: []int64{3, 6}, Name: "Spoorbaanweg", WidthM: 4},
		{Line: [][2]float64{{-50, 0}, {-50, -6}}, Nodes: []int64{7, 8}, Name: "Stub", WidthM: 4},
	}
	ways[2].Line = [][2]float64{{-8, 0}, {-50, 0}, {-108, 0}}
	ways[2].Nodes = []int64{3, 7, 4}
	ls := networkLines(ways, nil, func(e, n float64) bool { return true })
	drawn := map[int]bool{}
	for _, l := range ls {
		for _, s := range l.samples {
			drawn[s.way] = true
		}
	}
	if !drawn[1] {
		t.Error("the 8 m link between the junctions isn't drawn")
	}
	if drawn[5] {
		t.Error("the 6 m stub is drawn")
	}
}

// A road the course builds itself takes sharp turns as the rider does:
// on a preview of the Amsterdam Water Supply Dunes course (no map data)
// the road kept a 9 m dogleg at 2.36 km and a right angle at 9.92 km
// while the rider's path swept across the grass beside it.
func TestOwnRoadTakesTurnsAsTheRiderDoes(t *testing.T) {
	const m = 1 / 111195.0
	lon := func(e float64) float64 { return 5 + e*m/math.Cos(52*math.Pi/180) }
	var pts []course.Point
	add := func(e, n float64) { pts = append(pts, course.Point{Lat: 52 + n*m, Lon: lon(e), Ele: 10}) }
	for n := 0.0; n <= 300; n += 20 {
		add(0, n)
	}
	// The dogleg: right 9 m, then on north.
	add(2.6, 302)
	add(-1, 311)
	for n := 330.0; n <= 600; n += 20 {
		add(-1, n)
	}
	// A right angle, then east.
	for e := 20.0; e <= 300; e += 20 {
		add(-1+e, 600)
	}
	c, err := course.New("dogleg", "Dogleg", pts)
	if err != nil {
		t.Fatal(err)
	}
	w, err := Build(c, Options{Generator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Check(); r.OffRoadM > 0 {
		t.Errorf("the riding line off road for %.0f m %v", r.OffRoadM, r.OffRoad)
	}
}

// A junction cuts roads running alongside each other abreast (the Velp
// loop at 3.0 km): a service road 1 m beside a main road has a side road
// at e 5 and a link to the main road at e 20; the junction (one, merged)
// reaches along the service road past the side road, and the main road
// beside it must be cut as far, or the strip between them shows as a
// wedge of ground between two road surfaces.
func TestJunctionCutsRoadsAlongsideAbreast(t *testing.T) {
	line := func(from, to [2]float64, hw float64, nodes map[int64]float64) *roadLine {
		l := &roadLine{nodes: map[int64][]float64{}}
		for k, v := range nodes {
			l.nodes[k] = []float64{v}
		}
		length := math.Hypot(to[0]-from[0], to[1]-from[1])
		var d, x, y []float64
		for k := 0; ; k++ {
			at := math.Min(float64(k)*fineStep, length)
			f := at / length
			e, n := from[0]+f*(to[0]-from[0]), from[1]+f*(to[1]-from[1])
			l.samples = append(l.samples, sample{d: at, e: e, n: n, hw: hw, ele: 10})
			d, x, y = append(d, at), append(x, e), append(y, n)
			if at == length {
				break
			}
		}
		l.smooth = newSmoothLine(d, x, y, false, nil)
		return l
	}
	main := line([2]float64{-200, 0}, [2]float64{200, 0}, 3.5, map[int64]float64{2: 220})
	service := line([2]float64{-200, -7}, [2]float64{26, -7}, 2.5, map[int64]float64{3: 205, 4: 220})
	side := line([2]float64{5, -7}, [2]float64{5, -100}, 2.5, map[int64]float64{3: 0})
	link := line([2]float64{20, 0}, [2]float64{20, -7}, 2.5, map[int64]float64{2: 0, 4: 7})
	ps := networkPatches([]*roadLine{main, service, side, link}, nil, 0)
	var j *patch
	for k := range ps {
		if inPoly(ps[k].poly, 5, -7) {
			j = &ps[k]
		}
	}
	if j == nil {
		t.Fatal("no junction at the side road")
	}
	for e := -2.5; e <= 15; e += 2.5 {
		if !inPoly(j.poly, e, -4) {
			t.Errorf("at e %.1f the strip between the roads is not on the junction", e)
		}
	}
}

// The rider never changes onto a road metres below the one they are on
// (Amsterdam Water Supply Dunes at 10.0 km: the course was matched to the
// beach access road 8 m below the boulevard at Zandvoort, and the rider
// dropped 7.5 m in 5 m onto it and climbed back up). Here the course is
// matched to a road in a cutting 12 m beside the main road it rides,
// joining it 60 m before and after.
func TestRiderNeverDropsOntoARoadBelow(t *testing.T) {
	var pts []course.Point
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 16})
	}
	c, err := course.New("cut", "Cutting", pts)
	if err != nil {
		t.Fatal(err)
	}
	ground := func(lat, lon float64) (float64, bool) {
		// Below the boulevard to the east, rising to it at 600-660 m.
		e, n := c.Project(lat, lon)
		if e < 2 {
			return 16, true
		}
		return 16 - 8*math.Min(1, math.Max(0, (660-n)/60)), true
	}
	ways := []scenery.Way{
		// One-way the other way, as the boulevard there: the course takes
		// the road down where it can.
		{Line: [][2]float64{{0, 1060}, {0, 660}, {0, -60}}, Nodes: []int64{2, 6, 1}, Name: "Boulevard", Class: "tertiary", WidthM: 7, Oneway: 1},
		// From the beach below, alongside, up to the boulevard: it meets
		// the boulevard only at its north end.
		{Line: [][2]float64{{60, 380}, {12, 400}, {12, 600}, {0, 660}}, Nodes: []int64{7, 3, 4, 6}, Name: "Strandafgang", Class: "service", WidthM: 4},
	}
	on := make([]int, 201)
	for i := range on {
		if d := float64(i) * 5; d >= 400 && d <= 660 {
			on[i] = 1
		}
	}
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 1; 3*i < len(xyz); i++ {
		if dz := math.Abs(xyz[3*i+1] - xyz[3*i-2]); dz > 2 {
			t.Fatalf("at %.0f m the rider's height steps %.1f m in one %.0f m step", float64(i)*pathStep, dz, pathStep)
		}
	}
}
