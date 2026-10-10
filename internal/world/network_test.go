package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestRoadsFromTheMap(t *testing.T) {
	// A recorded ride 1 km north, 2 m east of the road it rides on (GPS is
	// like that), with a road crossing at 500 m.
	var pts []course.Point
	kx := 111195 * math.Cos(52*math.Pi/180)
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5 + 2/kx, Ele: 10})
	}
	c, err := course.New("north", "North", pts)
	if err != nil {
		t.Fatal(err)
	}
	e0, _ := c.Project(52, 5) // the road's east, about -2 m
	ways := []scenery.Way{
		{ID: 1, Line: [][2]float64{{e0, -60}, {e0, 500}, {e0, 1060}}, Nodes: []int64{10, 11, 12},
			Name: "Noordweg", Class: "secondary", WidthM: 6, Sidewalk: [2]bool{true, true}},
		{ID: 2, Line: [][2]float64{{e0 - 100, 500}, {e0, 500}, {e0 + 100, 500}}, Nodes: []int64{20, 11, 21},
			Name: "Dwarsweg", Class: "tertiary", WidthM: 7, Surface: scenery.SurfacePaving},
	}
	on := make([]int, 201)
	sc := &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}

	w, err := Build(c, Options{Scenery: sc, Generator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// The rider is on the road's centre line, not on the GPS's.
	xyz := w.Manifest.Path.XYZ
	for _, d := range []float64{100, 500, 900} {
		i := int(d / pathStep)
		if x := xyz[3*i]; math.Abs(x-e0) > 0.1 {
			t.Errorf("at %.0f m the rider is %.2f m east, want on the road at %.2f", d, x, e0)
		}
		if wd := w.Manifest.Path.WidthM[i]; wd != 6 {
			t.Errorf("at %.0f m the road is %.1f m wide, want 6", d, wd)
		}
	}

	// Both roads drawn from their own lines, once each; a junction where
	// they cross, in the main road's asphalt though the brick side road is
	// wider; the sidewalks stop there, and both roads' surfaces at its
	// middle.
	c2 := c
	o := Options{Scenery: sc}
	o.defaults()
	line := courseLine(c2, sc)
	route := sampleRoad(c2, line, fineStep)
	applyRoads(route, nil, o.RoadWidthM, o.CellM)
	tr := &terrain{o: o, c: c2, coarse: route, cover: &cover{banks: map[int]*banks{}}}
	n := newNetwork(c2, o, line, route, tr)
	var network, routeBuilt int
	for _, l := range n.lines {
		if l.route {
			routeBuilt++
		} else {
			network++
		}
	}
	if network != 2 || routeBuilt != 0 {
		t.Fatalf("%d map roads and %d built along the route, want 2 and 0", network, routeBuilt)
	}
	ps := networkPatches(n.lines, ways)
	if len(ps) != 1 || !inPoly(ps[0].poly, e0, 500) || !inPoly(ps[0].poly, e0+5, 500) {
		t.Fatalf("junctions %+v, want one at the crossing", ps)
	}
	if ps[0].surface != scenery.SurfaceAsphalt {
		t.Errorf("junction surface %v, want the main road's asphalt", ps[0].surface)
	}
	if h := ps[0].height(e0, 500); math.Abs(h-10) > 0.3 {
		t.Errorf("junction at %.2f m, want the roads' 10", h)
	}
	// Each road ends at the junction in a cut whose two corners are
	// corners of its outline, at the road's height: no step, no gap.
	cutRoads(n.lines, ps)
	if len(ps[0].cuts) != 4 {
		t.Errorf("%d roads end at the junction, want 4", len(ps[0].cuts))
	}
	for _, c := range ps[0].cuts {
		for _, q := range [][2]float64{c.p0, c.p1} {
			if !ps[0].hasCorner(q) {
				t.Errorf("a road's end corner %.2f, %.2f not on the junction's outline", q[0], q[1])
			}
			if h := ps[0].height(q[0], q[1]); h != c.ele {
				t.Errorf("junction at %.4f m at a road's end, the road at %.4f", h, c.ele)
			}
		}
		if s := sampleBetween(c.l, c.d); s.onPatch || math.Abs(s.d-c.d) > 1e-9 {
			t.Errorf("no road sample ending at the cut at %.1f m", c.d)
		}
	}
	for _, l := range n.lines {
		if s := closestSample(l, e0, 500); !s.onPatch {
			t.Errorf("way %d's surface goes on across the junction", s.way)
		}
	}
	// The junction carries Noordweg's sidewalks round each corner, from
	// its ends (Dwarsweg has none: each goes halfway round and stops).
	if ws := ps[0].walks; len(ws) != 4 {
		t.Errorf("%d sidewalks at the junction, want one at each corner", len(ws))
	} else {
		for _, w := range ws {
			if w.fromA == w.fromB {
				t.Errorf("a corner's sidewalk joins roads' sidewalks at %v and %v, want at one end", w.fromA, w.fromB)
			}
		}
	}
}

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

func TestCourseStaysOnTheRoad(t *testing.T) {
	// A road due north; the route runs on a cycle path 10 m east of it
	// (not in the map data), except for 20 m where the map matched it to
	// the road, and from 700 m it leaves the road for a path 60 m away.
	road := &roadLine{}
	for d := 0.0; d <= 1000; d += fineStep {
		road.samples = append(road.samples, sample{d: d, e: 0, n: d, hw: 3, way: 7})
	}
	var route []sample
	for d := 0.0; d <= 1000; d += fineStep {
		e := 10.0
		if d > 700 {
			e = 60
		}
		route = append(route, sample{d: d, e: e, n: d})
	}
	mapOn := func(d float64) int {
		if d > 400 && d < 420 {
			return 7
		}
		return -1
	}
	got := courseRoads(route, mapOn, map[int]bool{7: true}, []*roadLine{road}, nil)
	at := func(d float64) int { return got[int(d/fineStep)] }
	if at(100) != 7 || at(410) != 7 || at(650) != 7 {
		t.Errorf("beside the road the course is on %d, %d, %d; want the road (7)", at(100), at(410), at(650))
	}
	if at(850) != -1 {
		t.Errorf("60 m from any road the course is on %d, want its own way (-1)", at(850))
	}
	// No hops: a road-path-road blip shorter than 30 m is smoothed away.
	v := []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1, -1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	smoothRuns(v, 15)
	for i, x := range v {
		if x != 1 {
			t.Fatalf("a short blip left at %d: %v", i, v)
		}
	}
}

func TestJunctionsHaveOneHeight(t *testing.T) {
	// Two roads meeting at node 5, at 10 m and 10.6 m there.
	a := &roadLine{nodes: map[int64][]float64{5: {100}}}
	b := &roadLine{nodes: map[int64][]float64{5: {0}}}
	for d := 0.0; d <= 200; d += fineStep {
		a.samples = append(a.samples, sample{d: d, ele: 10})
	}
	for d := 0.0; d <= 100; d += fineStep {
		b.samples = append(b.samples, sample{d: d, ele: 10.6})
	}
	joinHeights([]*roadLine{a, b})
	if ha, hb := sampleAt(a, 100).ele, sampleAt(b, 0).ele; math.Abs(ha-10.3) > 1e-9 || math.Abs(hb-10.3) > 1e-9 {
		t.Errorf("at the junction %.2f and %.2f m, want both 10.3", ha, hb)
	}
	if h := sampleAt(b, 30).ele; math.Abs(h-10.6) > 1e-9 {
		t.Errorf("30 m along the side road: %.2f m, want its own 10.6", h)
	}
}

func TestBridgesSpanStraight(t *testing.T) {
	// A road over a 4 m deep canal 20 m wide, the bridge tagged: the
	// ground model has the canal, the road doesn't.
	c := northCourse(t)
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		if n > 490 && n < 510 {
			return 6, true
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	l := &roadLine{}
	for d := 400.0; d <= 600; d += fineStep {
		w := 0
		if d > 485 && d < 515 {
			w = 1
		}
		l.samples = append(l.samples, sample{d: d, e: 20, n: d, way: w})
	}
	heights(l, tr, []scenery.Way{{}, {Bridge: true}})
	if h := sampleAt(l, 500).ele; math.Abs(h-10) > 0.3 {
		t.Errorf("on the bridge: %.2f m, want level with its ends (10)", h)
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

// closestSample is l's sample nearest e, n.
func closestSample(l *roadLine, e, n float64) sample {
	best, bd := l.samples[0], math.Inf(1)
	for _, s := range l.samples {
		if d := math.Hypot(s.e-e, s.n-n); d < bd {
			best, bd = s, d
		}
	}
	return best
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
	ps := networkPatches([]*roadLine{main, join}, nil)
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

func TestConnectorOverOpenGround(t *testing.T) {
	// Three roads north, at east 0, 12 and 62 m. The rider's path goes up
	// the first, steps 12 m across to the second (kept on the road later:
	// no connector), then jumps 50 m over grass to the third: a connector
	// there, only over the grass between the roads.
	road := func(e float64) *roadLine {
		l := &roadLine{}
		for n := -50.0; n <= 350; n += fineStep {
			l.samples = append(l.samples, sample{d: n + 50, e: e, n: n, ele: 10, hw: 2.5, edge: 2.5, way: 0})
		}
		return l
	}
	nw := &network{lines: []*roadLine{road(0), road(12), road(62)}, o: Options{RoadWidthM: 5, CellM: 5}}
	var d, x, y, z []float64
	for i := range 61 {
		e := 0.0
		switch {
		case i > 40:
			e = 62
		case i > 20:
			e = 12
		}
		d, x, y, z = append(d, float64(i)*pathStep), append(x, e), append(y, float64(i)*pathStep), append(z, 10)
	}
	got := nw.connectors(d, x, y, z, nil)
	if len(got) != 1 {
		t.Fatalf("%d connectors, want 1 (over the grass to the third road)", len(got))
	}
	ss := got[0].samples
	a, b := ss[0], ss[len(ss)-1]
	if a.e < 12 || a.e > 16 || b.e < 58 || b.e > 62 {
		t.Errorf("connector from %.1f to %.1f m east, want from just inside the second road's edge (14.5) to the third's (59.5)", a.e, b.e)
	}
	for _, s := range ss {
		if s.way != -1 || s.hw <= 0 || math.Abs(s.ele-(10-0.01)) > 0.02 {
			t.Fatalf("connector sample %+v: want an own way just under the roads' height", s)
		}
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

// The course changes roads only where they meet: a route down the middle
// of a canal with a road on either bank (Grebbeberg course, 55-62 km),
// matched to one bank, then the other, keeps to the first; where a bridge
// joins the banks, it may change there.
func TestCourseChangesRoadsWhereTheyMeet(t *testing.T) {
	bank := func(e float64, way int) *roadLine {
		l := &roadLine{}
		for d := 0.0; d <= 1000; d += fineStep {
			l.samples = append(l.samples, sample{d: d, e: e, n: d, hw: 2.5, way: way})
		}
		return l
	}
	west, east := bank(-10, 1), bank(10, 2)
	var route []sample
	for d := 0.0; d <= 1000; d += fineStep {
		route = append(route, sample{d: d, e: 0, n: d})
	}
	mapOn := func(d float64) int { // the map's matching: by a hair, one bank then the other
		if int(d/150)%2 == 1 {
			return 2
		}
		return 1
	}
	drawn := map[int]bool{1: true, 2: true}
	got := courseRoads(route, mapOn, drawn, []*roadLine{west, east}, nil)
	for i, k := range got {
		if k != 1 {
			t.Fatalf("at %.0f m the course is on %d, want the west bank (1) all along", route[i].d, k)
		}
	}
	// The west bank's road ends at 500 m: the course goes on along the east.
	west.samples = west.samples[:int(500/fineStep)+1]
	mapOn = func(d float64) int {
		if d >= 500 {
			return 2
		}
		return 1
	}
	got = courseRoads(route, mapOn, drawn, []*roadLine{west, east}, nil)
	if got[int(450/fineStep)] != 1 || got[int(600/fineStep)] != 2 {
		t.Errorf("the west road ending at 500 m: on %d at 450 m and %d at 600 m, want 1 then 2", got[int(450/fineStep)], got[int(600/fineStep)])
	}
}

// Gaps under the rider's path get the course's own way: openRuns finds
// where the path has no drawn road for 3 m or more (a road that stops 12 m
// short of the next one), not where a roundabout's disc will be drawn.
func TestOpenRunsFindGapsUnderThePath(t *testing.T) {
	road := func(n0, n1 float64, way int) *roadLine {
		l := &roadLine{}
		for n := n0; n <= n1; n += fineStep {
			l.samples = append(l.samples, sample{d: n - n0, e: 0, n: n, hw: 2.5, edge: 2.5, way: way})
		}
		return l
	}
	nw := &network{lines: []*roadLine{road(0, 200, 1), road(212, 500, 2)}}
	var d, x, y []float64
	for n := 0.0; n <= 500; n += pathStep {
		d, x, y = append(d, n), append(x, 0.5), append(y, n)
	}
	f, any := nw.openRuns(d, x, y, nil)
	if !any {
		t.Fatal("no gap found")
	}
	for i, n := range d {
		gap := n > 195 && n < 215
		if (f[i] == 0) != gap && !(n >= 190 && n <= 220) { // the run's neighbours may be marked too
			t.Errorf("at %.0f m: f %v, want gap %v", n, f[i], gap)
		}
		if gap && f[i] != 0 {
			t.Errorf("at %.0f m in the gap: f %v", n, f[i])
		}
	}
	// A roundabout there: its disc covers the gap later.
	if _, any := nw.openRuns(d, x, y, []roundabout{{c: [2]float64{0, 206}, r: 8, hw: 3}}); any {
		t.Error("a gap found on a roundabout's disc")
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
	ps := networkPatches([]*roadLine{main, service, side, link}, nil)
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
