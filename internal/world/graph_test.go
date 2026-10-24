package world

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// graphCourse is a course through points given in metres east and north of
// its start (as map data has them, scenery.Way.Line).
func graphCourse(t *testing.T, id string, en [][2]float64) *course.Course {
	t.Helper()
	const m = 1 / 111195.0
	var pts []course.Point
	for _, p := range en {
		pts = append(pts, course.Point{Lat: 52 + p[1]*m, Lon: 5 + p[0]*m/math.Cos(52*math.Pi/180), Ele: 10})
	}
	c, err := course.New(id, id, pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// along is the course's points every step from a to b.
func along(a, b [2]float64, step float64) [][2]float64 {
	n := max(1, int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1])/step)))
	var out [][2]float64
	for i := range n {
		f := float64(i) / float64(n)
		out = append(out, [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])})
	}
	return out
}

// routeOn is the map's own match every 5 m for a course of length L.
func routeOn(L float64, way func(d float64) int) []int {
	out := make([]int, int(L/5)+1)
	for i := range out {
		out[i] = way(float64(i) * 5)
	}
	return out
}

// The course changes roads only where the network joins them: a route
// down the middle between two roads 12 m apart, which the map's own match
// puts on one, then the other, every 150 m, stays on one road until a
// link joins them, and changes there at most once.
func TestGraphCourseChangesRoadsOnlyWhereTheyMeet(t *testing.T) {
	c := graphCourse(t, "between", append(along([2]float64{0, 0}, [2]float64{0, 1000}, 20), [2]float64{0, 1000}))
	ways := []scenery.Way{
		{Line: [][2]float64{{-6, -40}, {-6, 500}, {-6, 1040}}, Nodes: []int64{1, 2, 3}, Name: "West", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{6, -40}, {6, 500}, {6, 1040}}, Nodes: []int64{4, 5, 6}, Name: "East", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{-6, 500}, {6, 500}}, Nodes: []int64{2, 5}, Name: "Link", Class: "residential", WidthM: 5},
	}
	on := routeOn(c.Distance, func(d float64) int { return int(d/150) % 2 })
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	changes := 0
	for i := 1; 3*i < len(xyz); i++ {
		a, b := xyz[3*i-3], xyz[3*i]
		if a*b < 0 {
			changes++
			if n := -xyz[3*i+2]; math.Abs(n-500) > 40 {
				t.Errorf("the course changes roads at %.0f m north, not at the link (500 m)", n)
			}
		}
		if math.Abs(b) < 3 && math.Abs(-xyz[3*i+2]-500) > 40 {
			t.Errorf("at %.0f m north the rider is between the roads (%.1f m east)", -xyz[3*i+2], b)
		}
	}
	if changes > 1 {
		t.Errorf("the course changes roads %d times", changes)
	}
	if r := w.Check(); r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
}

// A route on the cycle path round a flat roundabout rides over it: in
// along one road, across the roundabout, out along the other, on the
// road all the way (owner, 2026-10-10: a route skirting a roundabout
// rides it).
func TestGraphRidesOverTheRoundabout(t *testing.T) {
	const r = 12.0
	var ring [][2]float64
	var ringNodes []int64
	for k := 0; k <= 16; k++ {
		a := -math.Pi/2 + 2*math.Pi*float64(k%16)/16
		ring = append(ring, [2]float64{r * math.Cos(a), 300 + r*math.Sin(a)})
		ringNodes = append(ringNodes, int64(100+k%16))
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 300 - r}}, Nodes: []int64{1, 100}, Name: "South", Class: "tertiary", WidthM: 6},
		{Line: [][2]float64{{r, 300}, {400, 300}}, Nodes: []int64{104, 2}, Name: "East", Class: "tertiary", WidthM: 6},
		{Line: ring, Nodes: ringNodes, Class: "tertiary", WidthM: 6, Roundabout: true, Oneway: 1},
	}
	// The route: north on the cycle path 10 m east of the road, round the
	// roundabout's south-east 22 m from its middle, east on the cycle path
	// 10 m north of the road.
	var pts [][2]float64
	pts = append(pts, along([2]float64{10, 0}, [2]float64{10, 280}, 20)...)
	a0, a1 := math.Atan2(-20, 10), math.Atan2(10, 20)
	for k := 0; k <= 8; k++ {
		a := a0 + (a1-a0)*float64(k)/8
		pts = append(pts, [2]float64{22.4 * math.Cos(a), 300 + 22.4*math.Sin(a)})
	}
	pts = append(pts, along([2]float64{30, 310}, [2]float64{380, 310}, 20)...)
	pts = append(pts, [2]float64{380, 310})
	// Map data is in metres from the course's start.
	for i := range ways {
		for j := range ways[i].Line {
			ways[i].Line[j][0] -= 10
		}
	}
	c := graphCourse(t, "roundabout", pts)
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Check(); r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
	xyz := w.Manifest.Path.XYZ
	closest := math.Inf(1)
	for i := 0; 3*i < len(xyz); i++ {
		closest = math.Min(closest, math.Hypot(xyz[3*i]+10, -xyz[3*i+2]-300))
	}
	if closest > r {
		t.Errorf("the rider comes no nearer the roundabout's middle than %.1f m: not over it", closest)
	}
}

// A roundabout on a hillside lies on a plane tilted no more than
// rbMaxTilt, its roads eased onto it, and the rider rides round on its
// surface (owner, 2026-10-23: on the Lakes Route's ring on a 14.6 % plane
// the rider went 3 m up and down over a quarter of it, the ring's edge
// over its island).
func TestRoundaboutOnAHillsideLiesNearLevel(t *testing.T) {
	const r = 22.0 // an island
	var ring [][2]float64
	var ringNodes []int64
	for k := 0; k <= 16; k++ {
		a := -math.Pi/2 + 2*math.Pi*float64(k%16)/16
		ring = append(ring, [2]float64{r * math.Cos(a), 300 + r*math.Sin(a)})
		ringNodes = append(ringNodes, int64(100+k%16))
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 300 - r}}, Nodes: []int64{1, 100}, Name: "South", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{r, 300}, {300, 300}}, Nodes: []int64{104, 2}, Name: "East", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, 300 + r}, {0, 600}}, Nodes: []int64{108, 3}, Name: "North", Class: "primary", WidthM: 7},
		{Line: [][2]float64{{-r, 300}, {-400, 300}}, Nodes: []int64{112, 4}, Name: "West", Class: "secondary", WidthM: 7},
		{Line: ring, Nodes: ringNodes, Class: "secondary", WidthM: 8, Roundabout: true, Oneway: 1},
	}
	// In from the south, round past the east and north roads, out west.
	var pts [][2]float64
	pts = append(pts, along([2]float64{0, 0}, [2]float64{0, 300 - r}, 20)...)
	for k := 0; k <= 12; k++ {
		a := -math.Pi/2 + 1.5*math.Pi*float64(k)/12
		pts = append(pts, [2]float64{r * math.Cos(a), 300 + r*math.Sin(a)})
	}
	pts = append(pts, along([2]float64{-r - 5, 300}, [2]float64{-380, 300}, 20)...)
	pts = append(pts, [2]float64{-380, 300})
	c := graphCourse(t, "hillside roundabout", pts)
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		return 10 + 0.12*n, true // rising 12 % to the north
	}
	on := routeOn(c.Distance, func(d float64) int {
		switch {
		case d < 300-r:
			return 0
		case d < 300-r+1.5*math.Pi*r:
			return 4
		}
		return 3
	})
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	// The ring's surface round its centre line: no more than the tilt
	// allows across it.
	lo, hi := math.Inf(1), math.Inf(-1)
	for k := 0; k < 32; k++ {
		a := 2 * math.Pi * float64(k) / 32
		y, ok := surfaceTop(w, "road", r*math.Cos(a), 300+r*math.Sin(a))
		if !ok {
			t.Fatalf("no ring at %.0f°", a*180/math.Pi)
		}
		lo, hi = math.Min(lo, y), math.Max(hi, y)
	}
	if hi-lo > rbMaxTilt*2*r+0.1 {
		t.Errorf("the ring rises %.2f m across, more than %.0f %% over its %.0f m", hi-lo, 100*rbMaxTilt, 2*r)
	}
	// The rider on it, all the way round.
	xyz := w.Manifest.Path.XYZ
	round := 0
	for i := 0; 3*i < len(xyz); i++ {
		e, n, y := xyz[3*i], -xyz[3*i+2], xyz[3*i+1]
		if math.Abs(math.Hypot(e, n-300)-r) > 3 {
			continue
		}
		round++
		// Within 0.15 m: where the turn onto the ring is swept up to 3 m off
		// the network's line, the path keeps the line's height, 0.12 m off
		// the plane at 4 %.
		if top, ok := surfaceTop(w, "road", e, n); !ok || math.Abs(y-top) > 0.15 {
			t.Errorf("the rider at %.0f, %.0f is at %.2f m, the ring's surface at %.2f (%v)", e, n, y, top, ok)
		}
	}
	if round < 10 {
		t.Errorf("only %d path points on the ring, want the course round it", round)
	}
}

// The land meets the road: a road in a cutting 3 m deep, its banks rising
// within metres of its edge, has no holes beside it and no land over it.
func TestGraphLandMeetsTheRoad(t *testing.T) {
	c := graphCourse(t, "cutting", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 10 + 3*smoothstep(math.Max(0, math.Abs(e)-4)/4), true
	}
	ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 440}}, Nodes: []int64{1, 2}, Name: "Holle Weg", Class: "unclassified", WidthM: 5}}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Check(); r.HolesM2 > 0.1 || r.LandOverRoadM2 > 0.1 || r.OffRoadM > 0 {
		t.Errorf("check: holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v",
			r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad)
	}
	// The banks keep their height a few metres out: the land gives way to
	// the road only beside it.
	drawn := newLandSurface(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "terrain" })
	if h, ok := drawn.at(20, 200); !ok || h < 12.9 {
		t.Errorf("the bank 20 m east is at %.2f m (drawn %v), want the ground model's 13", h, ok)
	}
}

// Roads meeting at a node have one height there, the mean of theirs, and
// each keeps its own shape between its nodes.
func TestGraphNodesShareAHeight(t *testing.T) {
	ways := []scenery.Way{
		{Line: [][2]float64{{-100, 0}, {0, 0}, {100, 0}}, Nodes: []int64{1, 2, 3}, Name: "Main", WidthM: 6},
		{Line: [][2]float64{{0, 0}, {0, 100}}, Nodes: []int64{2, 4}, Name: "Side", WidthM: 4},
	}
	ls := networkLines(ways, nil, func(e, n float64, _ bool) bool { return true })
	for _, l := range ls {
		for i := range l.samples {
			s := &l.samples[i]
			if s.way == 0 {
				s.ele = 10 + s.d/100 // the main road climbs 1 %
			} else {
				s.ele = 13 // the side road stands 2 m over the main road's node
			}
		}
	}
	g := newGnet(ls, ways, nil)
	g.anchorHeights(ways)
	var at []float64
	for k, l := range g.lines {
		for _, st := range g.stops[k] {
			if st.node == 2 {
				at = append(at, sampleBetween(l, st.d).ele)
			}
		}
	}
	if len(at) != 2 || math.Abs(at[0]-at[1]) > 0.01 {
		t.Errorf("heights at the junction %v, want one", at)
	}
	if math.Abs(g.ele[2]-12) > 0.01 {
		t.Errorf("the junction at %.2f m, want 12 (the mean of 11 and 13)", g.ele[2])
	}
}

// A way of the course's own (a cycle path the map lacks) meets the road
// it joins, rather than stopping a metre short of it with grass between
// (owner's marks, 2026-10-11: the Posbank Loop at 10.63, 12.27 and
// 13.17 km on the graph builder's world).
func TestGraphOwnWayMeetsTheRoad(t *testing.T) {
	// North on a path the map lacks, then east along the road at 300 m.
	pts := append(along([2]float64{0, 0}, [2]float64{0, 300}, 20), along([2]float64{0, 300}, [2]float64{400, 300}, 20)...)
	c := graphCourse(t, "ownway", append(pts, [2]float64{400, 300}))
	ways := []scenery.Way{{Line: [][2]float64{{-200, 300}, {500, 300}}, Nodes: []int64{1, 2}, Name: "Schaapsallee", Class: "unclassified", WidthM: 5}}
	on := routeOn(c.Distance, func(d float64) int {
		if d > 300 {
			return 0
		}
		return -1
	})
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	road := newRoadIndex(w.prims, func(mat int) bool { return riddenMaterials[w.doc.MaterialName(mat)] })
	for n := 250.0; n <= 300; n += 0.5 {
		if !road.on(0, n) {
			t.Fatalf("nothing to ride at %.1f m north on the path's line, %.1f m short of the road's middle", n, 300-n)
		}
	}
}

// The course's line stepping back for a moment doesn't turn the course
// round: its way at a step is its direction over metres (routeWay), not
// at a point. courseLine steps back near a junction turn, where it keeps
// one of the route's points and drops its neighbours, so its distances
// are shorter than the points' spacing and a rounded corner overreaches
// (owner's marks #113-116, 2026-10-11: the rider swerved 2 m off the
// Imboschweg and back at 14.42 km on the Posbank Loop, every road there
// facing the wrong way for one step).
func TestGraphGlitchInTheRouteDoesntTurnTheCourse(t *testing.T) {
	// A corner of 0.28 rad, then 19.56 m to the next point by a distance
	// of 21.85: as at 14.42 km on the Posbank Loop.
	a := 0.28
	l := newSmoothLine([]float64{0, 100, 121.85, 221.85}, []float64{0, 0, 19.56 * math.Sin(a), 119.56 * math.Sin(a)},
		[]float64{-100, 0, 19.56 * math.Cos(a), 119.56 * math.Cos(a)}, false, nil)
	back := false
	for at := 1.0; at < 220; at += 0.25 {
		de, dn, _ := l.heading(at)
		wx, wy := routeWay(l, at)
		back = back || de*wx+dn*wy < 0
		if fx, fy := l.at(at + gWayM); (fx-l.x[0])*wx+(fy-l.y[0])*wy < 0 {
			t.Fatalf("at %.2f the route's way %.2f, %.2f points back to its start", at, wx, wy)
		}
	}
	if !back {
		t.Skip("the line no longer steps back: this test needs another glitch")
	}
}

// A road that ends right at another road, pointing at it, without a node
// in common (the path it really meets, beside a main road closed to bikes,
// isn't in the map data) joins that road there: one junction, one height,
// no step (owner's mark, 2026-10-11: the Bentincklaan ends 5 m from the
// Hoofdstraat's middle in Rheden, 30.26 km on the Posbank Loop, its end
// standing over the main road as a wall).
func TestGraphRoadEndingAtAnotherJoinsIt(t *testing.T) {
	ways := []scenery.Way{
		{Line: [][2]float64{{-200, 0}, {200, 0}}, Nodes: []int64{1, 2}, Name: "Hoofdstraat", Class: "secondary", WidthM: 7, Sidepath: true},
		{Line: [][2]float64{{-100, 100}, {-4, 5}}, Nodes: []int64{3, 4}, Name: "Bentincklaan", Class: "unclassified", WidthM: 5},
		// Ends beside the main road, running along it: not joined.
		{Line: [][2]float64{{100, 30}, {150, 8}}, Nodes: []int64{5, 6}, Name: "Ventweg", Class: "service", WidthM: 3},
	}
	got := joinNearEnds(ways, nil)
	if got[1].Nodes[len(got[1].Nodes)-1] == 4 {
		t.Fatalf("the Bentincklaan still ends at its own node: %v", got[1].Nodes)
	}
	end := got[1].Nodes[len(got[1].Nodes)-1]
	found := false
	for _, n := range got[0].Nodes {
		found = found || n == end
	}
	if !found {
		t.Errorf("the Hoofdstraat doesn't pass the Bentincklaan's new end node: %v, %v", got[0].Nodes, got[1].Nodes)
	}
	if p := got[1].Line[len(got[1].Line)-1]; math.Abs(p[1]) > 0.01 {
		t.Errorf("the Bentincklaan ends at %v, not on the Hoofdstraat's line", p)
	}
	if got[2].Nodes[len(got[2].Nodes)-1] != 6 {
		t.Errorf("the Ventweg, ending beside the main road and along it, was joined to it")
	}
	if len(ways[0].Nodes) != 2 || len(ways[1].Nodes) != 2 {
		t.Error("joinNearEnds changed the ways it was given")
	}

	// In a world, with the ground 0.4 m higher where the side road ends:
	// the side road meets the main road at its height.
	c := graphCourse(t, "rheden", append(along([2]float64{-150, 0}, [2]float64{150, 0}, 20), [2]float64{150, 0}))
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		return 10 + 0.4*smoothstep(math.Max(0, math.Min(1, (n-2)/4))), true
	}
	shift := func(ws []scenery.Way) []scenery.Way {
		out := append([]scenery.Way(nil), ws...)
		for i := range out {
			out[i].Line = append([][2]float64(nil), out[i].Line...)
			for j := range out[i].Line {
				out[i].Line[j][0] += 150
			}
		}
		return out
	}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: shift(ways[:2]), RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	road := newSurfaceIndex(w.prims, func(mat int) bool { return riddenMaterials[w.doc.MaterialName(mat)] }, 200)
	// Along the side road's line, from the main road's middle outwards.
	dx, dy := -96.0/math.Hypot(96, 95), 95.0/math.Hypot(96, 95)
	prev, ok := road.at(146, 0)
	if !ok {
		t.Fatal("no road where the side road meets the main road")
	}
	for s := 0.25; s <= 8; s += 0.25 {
		h, ok := road.at(146+dx*s, dy*s)
		if !ok {
			t.Fatalf("no road %.2f m up the side road", s)
		}
		if math.Abs(h-prev) > 0.08 {
			t.Fatalf("a step of %.2f m %.2f m up the side road", h-prev, s)
		}
		prev = h
	}
}

// A ditch beside the road, mapped as pieces with dams between (the
// entrances to the plots), is water from its own outline: no land stands
// through it, and the dams stay land (owner's marks, 2026-10-11: along the
// dike road on the polder ride, 1.09-1.43 km, the graph builder's 5 m water
// cells had the land standing through the ditch in wedges).
func TestGraphDitchBesideTheRoad(t *testing.T) {
	c := graphCourse(t, "ditch", append(along([2]float64{0, 0}, [2]float64{0, 300}, 20), [2]float64{0, 300}))
	const lat0, lon0 = 52.0, 5.0
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	ll := func(e, n float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + n/111195, Lon: lon0 + e/kx} }
	piece := func(id int64, n0, n1 float64) scenery.Element {
		return scenery.Element{Type: "way", ID: id, Tags: map[string]string{"natural": "water"},
			Geometry: []scenery.LatLon{ll(4, n0), ll(8, n0), ll(8, n1), ll(4, n1), ll(4, n0)}}
	}
	// Two pieces of ditch 4-8 m right of the road, a dam from 140 to 146 m.
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{piece(1, -20, 140), piece(2, 146, 320)}})
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		if e > 4 && e < 8 && (n < 140 || n > 146) {
			return 0, false // no heights on water, as AHN
		}
		return 10, true
	}
	ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 340}}, Nodes: []int64{1, 2}, Name: "Dike road", Class: "unclassified", WidthM: 5}}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Land: lm, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	land := newLandSurface(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "terrain" })
	water := newSurfaceIndex(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "water" }, 1000)
	// The ditch is mapped to the top of its bank, 1.5 m from the road's
	// edge (2.5 m): its water keeps a verge and a bank from the road
	// (ditchBank: owner, 2026-10-12, a bank a wall within a metre of the
	// asphalt read as water level with the road), from 4.8 m.
	for n := 20.0; n <= 280; n += 1 {
		for e := 5.0; e <= 7.5; e += 0.5 {
			wh, wet := water.at(e, n)
			lh, dry := land.at(e, n)
			if n >= 139.5 && n <= 146.5 && (n < 140.5 || n > 145.5) {
				continue // the ditch's ends: its surface reaches a little past them, under the land
			}
			if n > 140 && n < 146 {
				if !dry || wet && wh > lh {
					t.Fatalf("the dam at %.0f m isn't land: land %v %.2f, water %v %.2f", n, dry, lh, wet, wh)
				}
				continue
			}
			if !wet {
				t.Fatalf("no water in the ditch at %.1f m right, %.0f m along", e, n)
			}
			if dry && lh > wh {
				t.Fatalf("land stands %.2f m through the ditch at %.1f m right, %.0f m along", lh-wh, e, n)
			}
		}
		if n > 130 && n < 156 {
			continue // the dam and the ditch's ends
		}
		// The verge, level with the road's edge; then the bank, falling
		// all the way to the water and nowhere as a wall.
		road, _ := land.at(2.7, n)
		if v, _ := land.at(3.1, n); math.Abs(v-road) > 0.1 {
			t.Fatalf("the verge at %.0f m along stands %.2f m off the road's edge", n, v-road)
		}
		wh, _ := water.at(5.5, n)
		prev := road
		for e := 3.3; e <= 4.8; e += 0.25 {
			h, ok := land.at(e, n)
			if !ok {
				t.Fatalf("no land on the bank at %.2f m right, %.0f m along", e, n)
			}
			if h > prev+0.02 || prev-h > 0.45 {
				t.Fatalf("the bank at %.2f m right, %.0f m along: %.2f after %.2f (a step or a wall)", e, n, h, prev)
			}
			prev = h
		}
		if prev > road-0.5*(road-wh) {
			t.Fatalf("the bank at %.0f m along ends %.2f m over the water, %.2f under the road", n, prev-wh, road-prev)
		}
	}
}

// A long canal beside the road, its outline a few far-apart points, is
// water all across: its outline's triangles are hundreds of metres long,
// and the land must still go round them (owner's photo, 2026-10-11: land
// stood through the canal beside the main street, 0.35 km into the the polder ride
// ride, where the land's outline left out triangles over 200 m).
func TestGraphLongCanalBesideTheRoad(t *testing.T) {
	c := graphCourse(t, "canal", append(along([2]float64{0, 0}, [2]float64{0, 700}, 20), [2]float64{0, 700}))
	const lat0, lon0 = 52.0, 5.0
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	ll := func(e, n float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + n/111195, Lon: lon0 + e/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{{Type: "way", ID: 1, Tags: map[string]string{"natural": "water"},
		Geometry: []scenery.LatLon{ll(6, -20), ll(20, -20), ll(20, 720), ll(6, 720), ll(6, -20)}}}})
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		if e > 6 && e < 20 {
			return 0, false
		}
		return 10, true
	}
	ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 740}}, Nodes: []int64{1, 2}, Name: "Main street", Class: "secondary", WidthM: 7}}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Land: lm, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	land := newLandSurface(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "terrain" })
	water := newSurfaceIndex(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "water" }, 5000)
	for n := 20.0; n <= 680; n += 2 {
		for e := 7.0; e <= 19; e += 1 {
			wh, wet := water.at(e, n)
			if !wet {
				t.Fatalf("no water in the canal at %.0f m right, %.0f m along", e, n)
			}
			if lh, dry := land.at(e, n); dry && lh > wh {
				t.Fatalf("land stands %.2f m through the canal at %.0f m right, %.0f m along", lh-wh, e, n)
			}
		}
	}
}

// A water with islands in it is one ring with its islands left out, which
// ear clipping covers exactly (owner, 2026-10-11: the canal by the
// main street, an outline with three islands, fell back to 5 m cells).
func TestWaterWithIslandsIsOneRing(t *testing.T) {
	outer := [][2]float64{{0, 0}, {100, 0}, {100, 30}, {0, 30}}
	island := func(x0 float64) [][2]float64 {
		return [][2]float64{{x0, 10}, {x0 + 10, 10}, {x0 + 10, 20}, {x0, 20}}
	}
	r, ok := withIslands([][][2]float64{island(20), outer, island(60)})
	if !ok {
		t.Fatal("not joined")
	}
	want := 100*30 - 2*100.0
	if got := math.Abs(ringArea(r)); math.Abs(got-want) > 1e-6 {
		t.Errorf("the ring's area %.1f m², want %.1f (the water less its islands)", got, want)
	}
	sum := 0.0
	for _, tr := range earClip(r) {
		sum += math.Abs(ringArea([][2]float64{r[tr[0]], r[tr[1]], r[tr[2]]}))
	}
	if math.Abs(sum-want) > 1e-3 {
		t.Errorf("ear clipping covers %.1f m², want %.1f", sum, want)
	}
	if _, ok := withIslands([][][2]float64{outer, {{200, 0}, {210, 0}, {210, 10}}}); ok {
		t.Error("a ring outside the outline was taken for an island")
	}
}

// A road through a tunnel keeps low under what lies over it: straight from
// end to end, as a bridge spans over (owner's mark, 2026-10-11: on the
// polder ride at 5.17 km the dike road went up and over the railway
// embankment, -19 % in a step, where it goes under the track through the
// tunnel).
func TestGraphRoadGoesUnderNotOverInATunnel(t *testing.T) {
	c := graphCourse(t, "tunnel", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		// A railway embankment 4 m high over 180-220 m.
		return 10 + 4*smoothstep(math.Max(0, math.Min(1, (20-math.Abs(n-200))/8))), true
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 180}}, Nodes: []int64{1, 2}, Name: "Dike", Class: "tertiary", WidthM: 6},
		{Line: [][2]float64{{0, 180}, {0, 220}}, Nodes: []int64{2, 3}, Name: "Dike tunnel", Class: "tertiary", WidthM: 6, Tunnel: true},
		{Line: [][2]float64{{0, 220}, {0, 440}}, Nodes: []int64{3, 4}, Name: "Dike", Class: "tertiary", WidthM: 6},
	}
	on := routeOn(c.Distance, func(d float64) int {
		switch {
		case d < 180:
			return 0
		case d < 220:
			return 1
		}
		return 2
	})
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 0; 3*i < len(xyz); i++ {
		if n := -xyz[3*i+2]; n > 185 && n < 215 && xyz[3*i+1] > 10.5 {
			t.Fatalf("at %.0f m the road is %.2f m high, over the embankment rather than under it", n, xyz[3*i+1])
		}
	}
}

// Trees in a town are its street and park trees, by water those that like
// it wet, and conifers only back from the street in gardens (owner,
// 2026-10-11: firs and pines stood in the main street's verges at
// the polder ride, where birches grow by the canal).
func TestTownTrees(t *testing.T) {
	c := graphCourse(t, "town", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	const lat0, lon0 = 52.0, 5.0
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	ll := func(e, n float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + n/111195, Lon: lon0 + e/kx} }
	box := func(id int64, tags map[string]string, e0, n0, e1, n1 float64) scenery.Element {
		return scenery.Element{Type: "way", ID: id, Tags: tags, Geometry: []scenery.LatLon{ll(e0, n0), ll(e1, n0), ll(e1, n1), ll(e0, n1), ll(e0, n0)}}
	}
	data := &scenery.Data{Elements: []scenery.Element{
		box(1, map[string]string{"landuse": "residential"}, -300, -300, 300, 700),
		box(2, map[string]string{"natural": "water"}, 20, 100, 40, 300),
	}}
	ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 440}}, Nodes: []int64{1, 2}, Name: "Main street", Class: "secondary", WidthM: 7}}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := Build(c, Options{Generator: "test", Land: scenery.NewLandMap(c, data), Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	town, wet := map[string]bool{}, map[string]bool{}
	for k := range townMix {
		town[k] = true
	}
	for k := range townWaterMix {
		wet[k] = true
	}
	conifers, nearWater, trees := 0, 0, 0
	for _, g := range w.Manifest.Instances.Groups {
		name := strings.TrimRight(strings.TrimSuffix(g.Kind, " far"), " 0123456789")
		if speciesIndex(name) < 0 {
			continue
		}
		for i := g.Offset; i < g.Offset+g.Count; i++ {
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(w.instances[4*instanceFloats*i:])))
			n := -float64(math.Float32frombits(binary.LittleEndian.Uint32(w.instances[4*instanceFloats*i+8:])))
			trees++
			sp := allSpecies[speciesIndex(name)]
			if (sp.fir || sp.pine) && math.Abs(x) < gardenM+3.5 && n > -25 && n < 425 { // along the street (it runs -40 to 440 m)
				conifers++
			}
			if x > 6 && x < 19 && n > 110 && n < 290 {
				nearWater++
				if !wet[name] {
					t.Errorf("a %s %.0f m from the water's edge, want one of %v", name, 20-x, townWaterMix)
				}
			} else if x > -100 && x < 100 && n > -250 && n < 650 && !(sp.fir || sp.pine) && !town[name] && !wet[name] {
				t.Errorf("a %s in the town at %.0f, %.0f, want one of its street trees", name, x, n)
			}
		}
	}
	if trees == 0 || nearWater == 0 {
		t.Fatalf("%d trees, %d by the water: nothing to judge", trees, nearWater)
	}
	if conifers > 0 {
		t.Errorf("%d conifers within %.0f m of the street", conifers, gardenM)
	}
}

// A carriageway split's junction doesn't reach past the next junction
// along the carriageways, and two junctions a short street apart stay
// two: no road paved over the verge between them (owner, 2026-10-11:
// a triangle of road west of the main street at the side street, the polder ride
// ride 10.4 km).
func TestJunctionsStopAtTheNext(t *testing.T) {
	ways := []scenery.Way{
		// One road splitting at (0, 0) into two carriageways 6 m apart.
		{Line: [][2]float64{{0, -200}, {0, 0}}, Nodes: []int64{1, 2}, Name: "Main street", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, 0}, {-3, 40}, {-3, 200}}, Nodes: []int64{2, 3, 4}, Name: "Main street", Class: "secondary", WidthM: 6, Oneway: 1},
		{Line: [][2]float64{{3, 200}, {3, 40}, {0, 0}}, Nodes: []int64{5, 6, 2}, Name: "Main street", Class: "secondary", WidthM: 5, Oneway: 1},
		// A street crossing both 40 m north of the split, and a street
		// alongside 17 m west.
		{Line: [][2]float64{{-80, 40}, {-17, 40}, {-3, 40}, {3, 40}, {80, 40}}, Nodes: []int64{7, 8, 3, 6, 9}, Name: "Side street", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{-17, -100}, {-17, 40}, {-17, 200}}, Nodes: []int64{10, 8, 11}, Name: "the next side street", Class: "residential", WidthM: 5},
	}
	ls := networkLines(ways, nil, func(e, n float64, _ bool) bool { return true })
	for _, l := range ls {
		finishRoad(l.samples, 5)
	}
	ps := networkPatches(ls, ways, 0)
	for _, p := range ps {
		for _, q := range [][2]float64{{-10, 20}, {-10, 10}, {-12, 30}} {
			if inPoly(p.poly, q[0], q[1]) {
				t.Errorf("a junction (node %v, %d nodes) paves the verge at %v", p.node, len(p.nodes), q)
			}
		}
	}
}

// A short link onto a roundabout is drawn: the ring counts as a road at
// its end (owner, 2026-10-11: a residential street stopped short of the
// roundabout on the polder ride, 10.2 km, its two short links left out).
func TestShortLinkOntoARoundaboutIsDrawn(t *testing.T) {
	var ring [][2]float64
	var nodes []int64
	for k := 0; k <= 12; k++ {
		a := 2 * math.Pi * float64(k%12) / 12
		ring = append(ring, [2]float64{12 * math.Cos(a), 12 * math.Sin(a)})
		nodes = append(nodes, int64(100+k%12))
	}
	ways := []scenery.Way{
		{Line: ring, Nodes: nodes, Class: "secondary", WidthM: 6, Roundabout: true, Oneway: 1},
		{Line: [][2]float64{{12, 0}, {20, 0}}, Nodes: []int64{100, 1}, Name: "Street", Class: "residential", WidthM: 5, Oneway: 1},
		{Line: [][2]float64{{20, 0}, {120, 0}}, Nodes: []int64{1, 2}, Name: "Street", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{20, 0}, {20, 30}}, Nodes: []int64{1, 3}, Name: "Zijstraat", Class: "residential", WidthM: 5},
	}
	ls := networkLines(ways, nil, func(e, n float64, _ bool) bool { return true })
	drawn := map[int]bool{}
	for _, l := range ls {
		for _, s := range l.samples {
			drawn[s.way] = true
		}
	}
	if !drawn[1] {
		t.Error("the 8 m link onto the roundabout isn't drawn")
	}
}

// A turn at a node is swept wide (owner's A/B, 2026-10-12: the graph
// builder turned 101° in one 5 m step at 25.95 km on the Posbank Loop,
// where the classic builder's sweep gives 56° over 20 m). The road the
// course turns onto passes the node with a slight kink, so the two roads'
// lines meet the node a hair apart, as they do on the map.
func TestGraphTurnAtANodeIsRounded(t *testing.T) {
	pts := along([2]float64{0, 0}, [2]float64{0, 200}, 20)
	pts = append(pts, along([2]float64{0, 200}, [2]float64{300, 200}, 20)...)
	pts = append(pts, [2]float64{300, 200})
	c := graphCourse(t, "corner", pts)
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 200}}, Nodes: []int64{1, 2}, Name: "North", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{-100, 190}, {0, 200}, {340, 200}}, Nodes: []int64{3, 2, 4}, Name: "East", Class: "residential", WidthM: 5},
	}
	on := routeOn(c.Distance, func(d float64) int {
		if d < 200 {
			return 0
		}
		return 1
	})
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.TurnDeg > 40 {
		t.Errorf("the rider turns %.0f° in one step at the corner %v, want a sweep under 40°", r.TurnDeg, r.Turns)
	}
	if r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
}

// Leaving the roads is smooth (owner's A/B, 2026-10-12: at 26.74 km on the
// Posbank Loop the course leaves the Ellecomsedijk for the cycle tunnel
// under the N348, which the map data lacks: the rider stepped 16 m sideways
// in two 56° turns, and then rode the 5 m ground model's bumps under the
// viaduct, 2.1 m up and down between path points). The course rides the
// cycle path 15 m beside the road (matched onto it), turns off it at a
// right angle, and goes on over ground that rises and falls 1.5 m every
// 25 m.
func TestGraphLeavingTheRoadIsSmooth(t *testing.T) {
	pts := along([2]float64{15, 0}, [2]float64{15, 200}, 20)
	pts = append(pts, along([2]float64{15, 200}, [2]float64{80, 200}, 20)...)
	pts = append(pts, along([2]float64{80, 200}, [2]float64{80, 500}, 20)...)
	pts = append(pts, [2]float64{80, 500})
	c := graphCourse(t, "tunnel", pts)
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		if e < 20 {
			return 10, true
		}
		return 10 + 1.5*math.Sin(n/4), true
	}
	ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 240}}, Nodes: []int64{1, 2}, Name: "Dijk", Class: "unclassified", WidthM: 5}}
	on := routeOn(c.Distance, func(d float64) int {
		if d < 200 {
			return 0
		}
		return -1
	})
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.StepM > 0.8 {
		t.Errorf("the rider's height steps %.2f m between path points %v, want under 0.8 (the ground model smoothed)", r.StepM, r.Steps)
	}
	if r.TurnDeg > 45 {
		t.Errorf("the rider turns %.0f° in one step %v leaving the road, want a sweep under 45°", r.TurnDeg, r.Turns)
	}
	if r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
}

// A dogleg of a few metres at a road change (two nodes 3.6 m apart, the
// Z through a 3 m piece of the Beekhuizenseweg at 15.36 km on the Velp
// loop) is one turn, swept through: the other node's point, 2.5 m off the
// chord, is not kept for the rounding to follow.
func TestGraphDoglegAtANodeIsOneTurn(t *testing.T) {
	pts := along([2]float64{0, 0}, [2]float64{0, 200}, 20)
	pts = append(pts, along([2]float64{4, 203}, [2]float64{300, 203}, 20)...)
	pts = append(pts, [2]float64{300, 203})
	c := graphCourse(t, "dogleg", pts)
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 200}}, Nodes: []int64{1, 2}, Name: "North", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{0, 200}, {4, 203}}, Nodes: []int64{2, 5}, Name: "Link", Class: "residential", WidthM: 5},
		{Line: [][2]float64{{-100, 193}, {4, 203}, {340, 203}}, Nodes: []int64{3, 5, 4}, Name: "East", Class: "residential", WidthM: 5},
	}
	on := routeOn(c.Distance, func(d float64) int {
		if d < 200 {
			return 0
		}
		return 2
	})
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.TurnDeg > 40 {
		t.Errorf("the rider turns %.0f° in one step at the dogleg %v, want a sweep under 40°", r.TurnDeg, r.Turns)
	}
	if r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
}

// The course's own way goes under a bridge the map has (owner, 2026-10-12:
// the Posbank Loop takes the cycle tunnel under the N348 at 26.8 km, which
// the map data lacks; the own way rode the ground model, the embankment's
// fill, and was hidden under the bridges' carriageways: 9 m off road, land
// 0.84 m over it). The ground model carries the embankment, 4 m high and
// 24 m wide, that the viaduct (bridge=yes) runs along.
func TestGraphOwnWayGoesUnderABridge(t *testing.T) {
	c := graphCourse(t, "underpass", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		// The fill under the viaduct rises 0.4 m above its ends.
		return 10 + 4*smoothstep(math.Max(0, 12-math.Abs(n-200))/4) + 0.4*math.Exp(-e*e/100), true
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{-150, 200}, {-20, 200}}, Nodes: []int64{1, 2}, Name: "N1", Class: "primary", WidthM: 8},
		{Line: [][2]float64{{-20, 200}, {20, 200}}, Nodes: []int64{2, 3}, Name: "N1", Class: "primary", WidthM: 8, Bridge: true},
		{Line: [][2]float64{{20, 200}, {150, 200}}, Nodes: []int64{3, 4}, Name: "N1", Class: "primary", WidthM: 8},
	}
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.OffRoadM > 0 || r.LandOverRoadM2 > 0.1 {
		t.Errorf("off road %.0f m %v, land over road %.2f m² %v: the own way doesn't go under the viaduct", r.OffRoadM, r.OffRoad, r.LandOverRoadM2, r.LandOverRoad)
	}
	if r.StepM > 0.5 {
		t.Errorf("the rider's height steps %.2f m %v, want ramps", r.StepM, r.Steps)
	}
	xyz := w.Manifest.Path.XYZ
	under := xyz[3*40+1] // the path point at 200 m: height
	if under > 11.5 {
		t.Errorf("under the viaduct the rider is at %.1f m, want 3 m under the bridge's 14, not up on the embankment", under)
	}
}

// Far from the route, flat land is laid in 50 m blocks, and hilly land
// keeps its shape: the land as drawn stays within gFarTolM of the ground
// model everywhere (owner, 2026-10-12: fewer triangles far off, towards
// hills on the horizon).
func TestGraphFarLandIsSimplifiedWhereFlat(t *testing.T) {
	build := func(id string, ground func(e, n float64) float64) *World {
		c := graphCourse(t, id, append(along([2]float64{0, 0}, [2]float64{0, 600}, 20), [2]float64{0, 600}))
		el := func(lat, lon float64) (float64, bool) {
			e, n := c.Project(lat, lon)
			return ground(e, n), true
		}
		ways := []scenery.Way{{Line: [][2]float64{{0, -40}, {0, 640}}, Nodes: []int64{1, 2}, Name: "Weg", Class: "unclassified", WidthM: 5}}
		on := routeOn(c.Distance, func(float64) int { return 0 })
		w, err := Build(c, Options{Generator: "test", Elevation: el, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	terrainTris := func(w *World) int {
		n := 0
		for _, p := range w.prims {
			if w.doc.MaterialName(p.Material) == "terrain" {
				n += len(p.Indices) / 3
			}
		}
		return n
	}
	flat := build("flat", func(e, n float64) float64 { return 10 + 0.001*e })
	hills := build("hills", func(e, n float64) float64 { return 10 + 3*math.Sin(e/30)*math.Cos(n/40) })
	// A 300 m corridor either side: 120 m of near land (10 m root cells
	// beyond 60 m) and 180 m of far land, which the blocks take with a
	// fiftieth of the triangles.
	if tf, th := terrainTris(flat), terrainTris(hills); tf > th*2/3 {
		t.Errorf("the flat land has %d terrain triangles, the hilly %d: the far blocks don't merge", tf, th)
	}
	drawn := newLandSurface(hills.prims, func(m int) bool { return hills.doc.MaterialName(m) == "terrain" })
	worst := 0.0
	for n := 100.0; n <= 500; n += 7 {
		for e := 130.0; e <= 280; e += 9 {
			if h, ok := drawn.at(e, n); ok {
				worst = math.Max(worst, math.Abs(h-(10+3*math.Sin(e/30)*math.Cos(n/40))))
			}
		}
	}
	if worst > gFarTolM+0.3 {
		t.Errorf("the far hills as drawn are %.2f m off the ground model, want within %.2f (+0.3 for the cells' own interpolation)", worst, gFarTolM)
	}
}

// The course doesn't dip into a stub that reaches its line and come back
// (the Dunes course at 14.88 and 17.35 km: a cycle path the map lacks
// runs beside a dual road, and the crossing pieces and stubs between the
// carriageways end on it; the matcher rode up them and straight back,
// 180° in a step): a stub's end is a dead end, a way back over a twin
// line is a hairpin, and no leg of a way through the network runs against
// the course. The course here rides a cycle path 12 m east of the road, a
// stub reaches it, and the path stays on the road.
func TestGraphNoDipIntoAStub(t *testing.T) {
	c := graphCourse(t, "stub", append(along([2]float64{12, 0}, [2]float64{12, 400}, 20), [2]float64{12, 400}))
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 200}}, Nodes: []int64{1, 2}, Name: "Weg", Class: "unclassified", WidthM: 5, Sidepath: true},
		{Line: [][2]float64{{0, 200}, {0, 440}}, Nodes: []int64{2, 3}, Name: "Weg", Class: "unclassified", WidthM: 5, Sidepath: true},
		{Line: [][2]float64{{0, 200}, {11, 200}}, Nodes: []int64{2, 4}, Name: "Stub", Class: "service", WidthM: 3},
		{Line: [][2]float64{{0, 200}, {11, 200}}, Nodes: []int64{2, 4}, Name: "Stub twin", Class: "service", WidthM: 3},
	}
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.TurnDeg > 45 {
		t.Errorf("the rider turns %.0f° in one step %v: into the stub and back", r.TurnDeg, r.Turns)
	}
	if r.OffRoadM > 0 {
		t.Errorf("off road %.0f m %v", r.OffRoadM, r.OffRoad)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 0; 3*i < len(xyz); i++ {
		if xyz[3*i] > 4 {
			t.Errorf("at %.0f m north the rider is %.1f m east: on the stub", -xyz[3*i+2], xyz[3*i])
			break
		}
	}
}

// The line through the network loses its spurs: the course placed at a
// stub's end and then back on the road it came from traces out along the
// stub and straight back over it; the spur goes, and the steps on it stand
// at its foot (the Grebbeberg course at 69.6 and 88.6 km: 180° in a step).
func TestTraceRemovesSpurs(t *testing.T) {
	ways := []scenery.Way{
		{Line: [][2]float64{{0, 0}, {0, 100}, {0, 200}}, Nodes: []int64{1, 2, 3}, Name: "Weg", Class: "unclassified", WidthM: 5},
		// The stub bows a little, so the way back from its end is the twin.
		{Line: [][2]float64{{0, 100}, {5, 100.4}, {10, 100}}, Nodes: []int64{2, 5, 4}, Name: "Stub", Class: "service", WidthM: 3},
		{Line: [][2]float64{{0, 100}, {10, 100}}, Nodes: []int64{2, 4}, Name: "Stub twin", Class: "service", WidthM: 3},
	}
	ls := networkLines(ways, nil, func(e, n float64, _ bool) bool { return true })
	g := newGnet(ls, ways, nil)
	road, stub := -1, -1
	for k, l := range g.lines[:g.virtual] {
		switch l.samples[0].way {
		case 0:
			road = k
		case 1:
			stub = k
		}
	}
	if road < 0 || stub < 0 {
		t.Fatalf("lines: road %d, stub %d", road, stub)
	}
	at := func(k int, d float64) gstate {
		s := sampleBetween(g.lines[k], d)
		return gstate{on: true, line: k, t: d, dir: 1, x: s.e, y: s.n, z: s.ele, w: 2 * s.hw}
	}
	st := []gstate{at(road, 80), at(road, 90), at(stub, 10), at(road, 105), at(road, 110), at(road, 120)}
	out := g.trace(st)
	for i := 1; i+1 < len(out.pts); i++ {
		a, b, c := out.pts[i-1], out.pts[i], out.pts[i+1]
		ux, uy, vx, vy := b.x-a.x, b.y-a.y, c.x-b.x, c.y-b.y
		if l := math.Hypot(ux, uy) * math.Hypot(vx, vy); l > 0 && (ux*vx+uy*vy)/l < -0.95 {
			t.Errorf("the line turns straight back at (%.1f, %.1f): a spur left in", b.x, b.y)
		}
	}
	for i := 1; i < len(out.at); i++ {
		if out.at[i] < out.at[i-1] {
			t.Errorf("step %d lies at %.1f m along the line, before step %d at %.1f", i, out.at[i], i-1, out.at[i-1])
		}
	}
}

// A course on a path the map lacks, crossing a dual road's two
// carriageways at a slant, keeps its way across the median between them:
// the carriageways cover its way only where they lie, not half its width
// beyond, and the few metres between two roads it crosses aren't handed to
// them (owner's mark, 2026-10-11: grass under the rider for ~10 m where
// the Dunes course crosses the Parnassiaweg at 15.27 km).
func TestGraphPathAcrossADualRoadKeepsItsWay(t *testing.T) {
	pts := along([2]float64{0, 0}, [2]float64{240, 180}, 20)
	c := graphCourse(t, "crossing", append(pts, [2]float64{240, 180}))
	ways := []scenery.Way{
		{Line: [][2]float64{{116, -100}, {116, 300}}, Nodes: []int64{1, 2}, Name: "Parnassiaweg", Class: "unclassified", WidthM: 3.1, Oneway: 1},
		{Line: [][2]float64{{124, 300}, {124, -100}}, Nodes: []int64{3, 4}, Name: "Parnassiaweg", Class: "unclassified", WidthM: 3.1, Oneway: 1},
	}
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	road := newRoadIndex(w.prims, func(mat int) bool { return riddenMaterials[w.doc.MaterialName(mat)] })
	for x := 110.0; x <= 130; x += 0.5 {
		if y := x * 0.75; !road.on(x, y) {
			t.Fatalf("nothing to ride at %.1f m east on the path's line (the carriageways at 116 and 124)", x)
		}
	}
}

// A sharp turn off the roads (paths the map data lacks) is swept wide:
// nothing drawn bounds it there, and within the usual window a 70° turn
// was a 14 m radius, which the 3D view leaned into at its 35° limit
// (owner, 2026-10-13: the Posbank Loop's turn onto the Lange Juffer at
// 10.07 km; round it wider).
func TestGraphFreeTurnIsSweptWide(t *testing.T) {
	a := 70 * math.Pi / 180
	end := [2]float64{-300 * math.Sin(a), 200 + 300*math.Cos(a)}
	pts := along([2]float64{0, 0}, [2]float64{0, 200}, 20)
	pts = append(pts, along([2]float64{0, 200}, end, 20)...)
	pts = append(pts, end)
	c := graphCourse(t, "freeturn", pts)
	// A road 120 m off, so the course is matched (off the roads).
	ways := []scenery.Way{{Line: [][2]float64{{120, -100}, {120, 400}}, Nodes: []int64{1, 2}, Name: "Elders", Class: "residential", WidthM: 5}}
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	// 13° per 5 m step is a 22 m radius.
	if r := w.Check(); r.TurnDeg > 13 {
		t.Errorf("the rider turns %.1f° in one step %v, want a sweep of 13° or less", r.TurnDeg, r.Turns)
	}
}

// The path's step in the manifest is the points' own spacing, the course's
// length over their number, not the nominal 5 m: a renderer placing the
// rider at d/StepM was metres off by a course's end (1.7 m at the lapped
// Posbank Loop's, which the 3D view hopped at each lap's wrap; owner,
// 2026-10-13). Both builders.
func TestPathStepIsThePointsSpacing(t *testing.T) {
	pts := along([2]float64{0, 0}, [2]float64{0, 1003}, 20)
	c := graphCourse(t, "step", append(pts, [2]float64{0, 1003}))
	for _, build := range []func(*course.Course, Options) (*World, error){Build, Build} {
		w, err := build(c, Options{Generator: "test"})
		if err != nil {
			t.Fatal(err)
		}
		n := len(w.Manifest.Path.XYZ) / 3
		if got := w.Manifest.Path.StepM * float64(n-1); math.Abs(got-c.Distance) > 1e-6 {
			t.Errorf("%s: %d points every %.5f m cover %.3f m of a %.3f m course", w.Manifest.Builder, n, w.Manifest.Path.StepM, got, c.Distance)
		}
	}
}

// A lapped course's tail ends on the lap's own path (owner, 2026-10-13):
// past the turn into the lap the tail's points ease onto the lap's, so
// wrapping back by a lap at the course's end lands where the rider was.
func TestTailOntoLap(t *testing.T) {
	raw, err := course.New("square", "Square", func() []course.Point {
		m := 1 / 111195.0
		k := m / math.Cos(52*math.Pi/180)
		var pts []course.Point
		for _, s := range [][4]float64{{0, 0, 500, 0}, {500, 0, 500, 500}, {500, 500, 1000, 500}, {1000, 500, 1000, 0}, {1000, 0, 500, 0}, {500, 0, 0, 0}} {
			for i := range 50 {
				f := float64(i) / 50
				pts = append(pts, course.Point{Lat: 52 + (s[1]+f*(s[3]-s[1]))*m, Lon: 5 + (s[0]+f*(s[2]-s[0]))*k, Ele: 10})
			}
		}
		return append(pts, course.Point{Lat: 52, Lon: 5, Ele: 10})
	}())
	if err != nil {
		t.Fatal(err)
	}
	c := raw.Lapped()
	l := c.Lap
	if l == nil {
		t.Fatal("not lapped")
	}
	// A path every 5 m along the course, the tail 1.5 m behind the lap
	// along the road (as a turn rounded otherwise leaves it).
	n := int(c.Distance/5) + 1
	step := c.Distance / float64(n-1)
	pd, px, py, pz, lanes := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range pd {
		pd[i] = float64(i) * step
		d := pd[i]
		if d > l.EndM {
			d = d - l.Length() - 1.5
		}
		px[i], py[i] = c.Project(c.Position(d))
		pz[i], lanes[i] = d/100, 1+d/1e4
	}
	tailOntoLap(c, pd, px, py, pz, lanes)
	lerp := func(v []float64, d float64) float64 {
		f := d / step
		i := min(int(f), len(v)-2)
		return v[i] + (f-float64(i))*(v[i+1]-v[i])
	}
	end, lapAt := pd[n-1], pd[n-1]-l.Length()
	if g := math.Hypot(px[n-1]-lerp(px, lapAt), py[n-1]-lerp(py, lapAt)); g > 0.01 {
		t.Errorf("the course's end is %.2f m from the lap's point %.0f m in", g, end-l.EndM)
	}
	if math.Abs(pz[n-1]-lerp(pz, lapAt)) > 1e-6 || math.Abs(lanes[n-1]-lerp(lanes, lapAt)) > 1e-6 {
		t.Errorf("height %.3f / %.3f, lane %.3f / %.3f", pz[n-1], lerp(pz, lapAt), lanes[n-1], lerp(lanes, lapAt))
	}
	// The turn into the lap is left as it was.
	i := int((l.EndM+10)/step) + 1
	want, _ := c.Project(c.Position(pd[i] - l.Length() - 1.5))
	if math.Abs(px[i]-want) > 1e-9 {
		t.Errorf("the tail's turn moved")
	}
}

// Land between two roads close beside it on opposite sides lies at the
// lower one's level (owner, 2026-10-16, Rue du Hohneck at 1.34 km: a
// lane climbing beside the main road); on one side only, or wide apart,
// it eases as before. Two roads a step apart in height are level only
// where a wall closes the step (1.52 km, where the lane parts from the
// main road: the land had stood as a bare cliff up to it).
func TestLandBetweenCloseRoadsIsLevel(t *testing.T) {
	const wall = abreastReachM
	main := gPiece{k: [2]int{1, 0}, gap: 0.3, ele: 10, qe: 0, qn: -2, de: 1}
	lane := gPiece{k: [2]int{2, 0}, gap: 0.5, ele: 11.2, qe: 0, qn: 3, de: 1}
	if low, ok := island([]gPiece{main, lane}, 0, 0, wall); !ok || low != 10 {
		t.Errorf("between the main road and the lane: %v, %v; want the main road's 10 m", low, ok)
	}
	if _, ok := island([]gPiece{main, lane}, 0, 0, 0); ok {
		t.Error("a step with no wall made a level strip (a cliff up to the lane)")
	}
	level := lane
	level.ele = 10.2
	if _, ok := island([]gPiece{main, level}, 0, 0, 0); !ok {
		t.Error("two roads at one level, no wall needed: no level strip")
	}
	parted := lane
	parted.gap = abreastReachM
	if _, ok := island([]gPiece{main, parted}, 0, 0, wall); ok {
		t.Error("a step past the wall's reach made a level strip")
	}
	across := lane
	across.de, across.dn = 0.3, 0.95
	if _, ok := island([]gPiece{main, across}, 0, 0, wall); ok {
		t.Error("a step to a road crossing (no wall across its mouth) made a level strip")
	}
	same := gPiece{k: [2]int{2, 0}, gap: 3, ele: 11.2, qe: 0, qn: -4, de: 1}
	if _, ok := island([]gPiece{main, same}, 0, 0, wall); ok {
		t.Error("two roads on one side made an island")
	}
	far := lane
	far.gap = 8
	if _, ok := island([]gPiece{main, far}, 0, 0, wall); ok {
		t.Error("roads 9.5 m apart made an island")
	}
}

// A loop whose own way runs across its start and finish (a map road
// elsewhere on it) has one way there, not two ending at the line with
// grass between them (2026-10-22, the figure 8 after its village was
// mapped: 2 m off the road at 5.0 km).
func TestLoopOwnWayCrossesItsLine(t *testing.T) {
	c := course.Tracks()[0] // the oval, its line at the home straight's start
	el := scenery.Element{Type: "way", ID: 1, Tags: map[string]string{"highway": "unclassified"}}
	for d := 210.0; d <= 280; d += 5 { // along the back straight
		lat, lon := c.Position(d)
		el.Geometry = append(el.Geometry, scenery.LatLon{Lat: lat, Lon: lon})
		el.Nodes = append(el.Nodes, int64(len(el.Nodes)+1))
	}
	data := &scenery.Data{Elements: []scenery.Element{el}}
	w, err := Build(c, Options{Generator: "test", Land: scenery.NewLandMap(c, data), Scenery: scenery.Build(c, data)})
	if err != nil {
		t.Fatal(err)
	}
	if r := w.Check(); r.OffRoadM > 0 {
		t.Errorf("the riding line %.1f m off the road: %+v", r.OffRoadM, r.OffRoad)
	}
}

// A dual carriageway's two short bridges over the course's own way (owner,
// 2026-10-22, the N348 over the Posbank Loop's cycle underpass at 26.8
// km): the way stays down between them, as low as under them, with no
// bump; each deck rests on a bank seat (nothing of it reaches down to the
// way, where its abutment had stood out of the bank as a pillar), and the
// slopes under the decks are paved.
func TestGraphOwnWayUnderTwoShortBridges(t *testing.T) {
	c := graphCourse(t, "underpass2", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		// An embankment 4 m high from 172 to 233 m, both carriageways on it.
		return 10 + 4*smoothstep(math.Max(0, math.Min(1, (30.5-math.Abs(n-202.5))/6))), true
	}
	var ways []scenery.Way
	for k, n := range []float64{190, 215} {
		id := int64(10 * (k + 1))
		ways = append(ways,
			scenery.Way{Line: [][2]float64{{-150, n}, {-15, n}}, Nodes: []int64{id, id + 1}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1},
			scenery.Way{Line: [][2]float64{{-15, n}, {15, n}}, Nodes: []int64{id + 1, id + 2}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1, Bridge: true, Layer: 1},
			scenery.Way{Line: [][2]float64{{15, n}, {150, n}}, Nodes: []int64{id + 2, id + 3}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1})
	}
	on := routeOn(c.Distance, func(float64) int { return -1 })
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	height := func(n float64) float64 {
		best, h := math.Inf(1), 0.0
		for i := 0; 3*i < len(xyz); i++ {
			if d := math.Abs(-xyz[3*i+2] - n); d < best {
				best, h = d, xyz[3*i+1]
			}
		}
		return h
	}
	a, b := height(190), height(215)
	for n := 190.0; n <= 215; n += 5 {
		if h := height(n); h > math.Max(a, b)+0.02 {
			t.Errorf("between the bridges at %.0f m the way is %.2f m high, over %.2f and %.2f under them: a bump", n, h, a, b)
		}
	}
	low := math.Inf(1)
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "bridge" {
			continue
		}
		for v := 1; v < len(p.Positions); v += 3 {
			low = math.Min(low, float64(p.Positions[v]))
		}
	}
	deck, _ := surfaceTop(w, "road", 8, 190) // the bridge's road, beside the way
	// The lowest of a short span is its bank seat's foot, whatever the
	// ground at its ends.
	if seat := deck - deckDepthM - bankSeatM; math.Abs(low-seat) > 0.02 {
		t.Errorf("the bridges reach down to %.2f m, not a bank seat's %.2f: an abutment down to the ground", low, seat)
	}
	// Beside the way under the deck: paved (owner, 2026-10-22), the
	// paving its own surface, nothing over it but the deck.
	for _, e := range []float64{-4, 4} {
		pave, ok := surfaceTop(w, "slope paving", e, 190)
		if !ok {
			t.Fatalf("the land under the bridge at %.0f, 190 isn't paved", e)
		}
		if land, ok := surfaceTop(w, "terrain", e, 190); ok && land > pave+0.01 {
			t.Errorf("land %.2f m over the paving under the bridge at %.0f, 190", land-pave, e)
		}
	}
}

// A cycle underpass the map lacks, under a dual carriageway's two short
// bridges on an embankment, takes service vehicles and two cycle paths,
// and its banks stand as packed sand does (owner, 2026-10-23: the Posbank
// Loop's under the N348 was 2.2 m under the decks, 3.5 m across, its
// banks at 50-60°).
func TestCycleUnderpassTakesServiceVehicles(t *testing.T) {
	c := graphCourse(t, "underpass3", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		return 10 + 4*smoothstep(math.Max(0, math.Min(1, (30.5-math.Abs(n-202.5))/6))), true
	}
	var ways []scenery.Way
	for k, n := range []float64{190, 215} {
		id := int64(10 * (k + 1))
		ways = append(ways,
			scenery.Way{Line: [][2]float64{{-150, n}, {-15, n}}, Nodes: []int64{id, id + 1}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1},
			scenery.Way{Line: [][2]float64{{-15, n}, {15, n}}, Nodes: []int64{id + 1, id + 2}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1, Bridge: true, Layer: 1},
			scenery.Way{Line: [][2]float64{{15, n}, {150, n}}, Nodes: []int64{id + 2, id + 3}, Name: "N1", Class: "primary", WidthM: 7, Oneway: 1})
	}
	w, err := Build(c, Options{Generator: "test", Elevation: ground, RoadWidthM: 3,
		Scenery: &scenery.Scenery{Ways: ways, RouteOn: routeOn(c.Distance, func(float64) int { return -1 }), RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	xyz := w.Manifest.Path.XYZ
	height := func(n float64) float64 {
		best, h := math.Inf(1), 0.0
		for i := 0; 3*i < len(xyz); i++ {
			if d := math.Abs(-xyz[3*i+2] - n); d < best {
				best, h = d, xyz[3*i+1]
			}
		}
		return h
	}
	maxGrade := math.Tan(47 * math.Pi / 180)
	// The land, paved under the decks.
	landTop := func(e, n float64) (float64, bool) {
		a, okA := surfaceTop(w, "terrain", e, n)
		b, okB := surfaceTop(w, "slope paving", e, n)
		switch {
		case okA && okB:
			return math.Max(a, b), true
		case okB:
			return b, true
		}
		return a, okA
	}
	// Two cycle paths wide, between the decks and on the ramp down (no deck
	// over it there to find instead).
	for _, n := range []float64{202.5, 175} {
		for _, side := range []float64{-1, 1} {
			if y, ok := surfaceTop(w, "road", side*1.8, n); !ok || math.Abs(y-height(n)) > 0.3 {
				t.Errorf("no way 1.8 m %s of its middle at %.1f m (%.2f, %v): narrower than two cycle paths", map[float64]string{-1: "left", 1: "right"}[side], n, y, ok)
			}
		}
	}
	// Paved through the open strip between the decks too, no land over it
	// (owner, 2026-10-23: one paved plane each side for the underpass).
	for _, side := range []float64{-1, 1} {
		pave, ok := surfaceTop(w, "slope paving", side*4, 202.5)
		if !ok {
			t.Errorf("no paving 4 m %s of the way between the decks", map[float64]string{-1: "left", 1: "right"}[side])
		} else if land, ok := surfaceTop(w, "terrain", side*4, 202.5); ok && land > pave+0.01 {
			t.Errorf("land %.2f m over the paving between the decks, %.0f m out", land-pave, side*4)
		}
	}
	for _, n := range []float64{190, 215} {
		path := height(n)
		deck, _ := surfaceTop(w, "road", 8, n) // the bridge's road, beside the way
		if clear := deck - deckDepthM - path; clear < gUnderClearM-0.05 {
			t.Errorf("under the bridge at %.0f m: %.2f m under its slab, want %.1f", n, clear, gUnderClearM)
		}
		for _, side := range []float64{-1, 1} {
			last := math.NaN()
			for e := 1.0; e <= 10; e++ {
				land, ok := landTop(side*e, n)
				if !ok {
					last = math.NaN()
					continue
				}
				if !math.IsNaN(last) && land-last > maxGrade+0.05 {
					t.Errorf("the bank at %.0f m, %.0f m out: rises %.2f m in a metre, steeper than packed sand", n, side*e, land-last)
				}
				last = land
			}
		}
	}
}

// A block is closed on all six sides (owner, 2026-10-22: a pier's
// crosshead had no bottom face).
func TestBoxIsClosed(t *testing.T) {
	var p gltf.Primitive
	f := deckFrame{c: [3]float64{0, 10, 0}, r: [3]float64{1, 0, 0}, along: [3]float64{0, 0, -1}}
	box(&p, f, -1, 1, -0.5, 0.5, 8, -0.5, [3]float32{1, 1, 1})
	if n := len(p.Indices) / 3; n != 12 {
		t.Fatalf("%d triangles, want 12 (six faces)", n)
	}
	down := false
	for v := 1; v < len(p.Normals); v += 3 {
		down = down || p.Normals[v] < -0.99
	}
	if !down {
		t.Error("no face looks down")
	}
}
