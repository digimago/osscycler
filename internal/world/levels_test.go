package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// A lane climbing beside a road on a mountainside (owner, 2026-10-16,
// 1.34 and 1.48 km of the Route des Américains, the Rue du Hohneck).

// The ground model steps a metre where the lane runs onto a bank; samples
// crowd round the step (finishRoad adds them through bends). Smoothed by
// distance, the lane still climbs gently there: counted in samples, the
// smoothing had shrunk to a few metres and left a 40 % ramp.
func TestSmoothByDistanceIgnoresCrowding(t *testing.T) {
	var ss []sample
	for d := 0.0; d <= 60; {
		ss = append(ss, sample{d: d})
		if d > 25 && d < 35 {
			d += 0.25
		} else {
			d += fineStep
		}
	}
	base := make([]float64, len(ss))
	for i, s := range ss {
		if s.d > 30 {
			base[i] = 1.2
		}
	}
	smoothByDistance(ss, base)
	worst := 0.0
	for i := 1; i < len(ss); i++ {
		worst = math.Max(worst, (ss[i].ele-ss[i-1].ele)/(ss[i].d-ss[i-1].d))
	}
	if worst > 0.1 {
		t.Errorf("steepest %.0f %%, want the step spread over ±%.0f m (under 10 %%)", 100*worst, heightSpanM)
	}
}

func levelLine(e0, ele float64) *roadLine {
	l := &roadLine{}
	for i := 0; i <= 30; i++ {
		d := float64(i) * fineStep
		l.samples = append(l.samples, sample{d: d, e: e0, n: d, ele: ele, hw: 2, edge: 2})
	}
	return l
}

// A road a metre above another, their edges a metre apart, stays a level
// of its own with a level limit: neither joined to the other's height
// nor reaching over to it; without one (the Dutch style), it is joined.
func TestRoadsALevelApartStayTwo(t *testing.T) {
	for _, c := range []struct {
		levelM float64
		joined bool
	}{{0, true}, {abreastStepM, false}} {
		low, high := levelLine(0, 0), levelLine(5, 1)
		joinAlongside([]*roadLine{low, high}, 5, c.levelM)
		s := high.samples[15]
		joined := math.Abs(s.ele) < 0.1 || s.butt[0] || s.butt[1]
		if joined != c.joined {
			t.Errorf("level limit %.2f: upper road joined %v (ele %.2f), want %v", c.levelM, joined, s.ele, c.joined)
		}
	}
}

// A road climbing more than bridgeM above another beside it isn't a deck
// over it (with beside); a road crossing is.
func TestRoadBesideIsNoDeck(t *testing.T) {
	for _, beside := range []bool{false, true} {
		low, high := levelLine(0, 0), levelLine(4, 3)
		markDecks([]*roadLine{low, high}, 0, beside)
		aloft := high.samples[15].aloft
		if aloft == beside {
			t.Errorf("beside %v: upper road on a deck %v, want %v", beside, aloft, !beside)
		}
	}
}

// The lane at 1.34 km as a world: a main road along the course and a lane
// beside it on a step 1.5 m up the hillside, 1 m between their edges.
// The lane keeps its own height (not pulled down onto the main road, nor
// a deck over it), a wall stands at its edge facing the main road, and
// nothing between them is a hole (owner, 2026-10-16: the wall stood in
// the lane's asphalt, the land a cliff, slits under the roads).
func TestGraphLaneBesideTheRoadBehindAWall(t *testing.T) {
	c := graphCourse(t, "lane", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		if n > 130 && n < 270 && e > 4 {
			return 11.5, true
		}
		return 10, true
	}
	// The lane leaves the main road and comes back to it, a loop climbing
	// onto the step beside it, as the one at 1.34 km.
	lane := [][2]float64{{0, 100}, {3, 108}, {5.5, 118}, {6.5, 130}}
	for n := 140.0; n <= 260; n += 10 {
		lane = append(lane, [2]float64{6.5, n})
	}
	lane = append(lane, [2]float64{6.5, 270}, [2]float64{5.5, 282}, [2]float64{3, 292}, [2]float64{0, 300})
	laneNodes := []int64{10}
	for i := 1; i < len(lane)-1; i++ {
		laneNodes = append(laneNodes, int64(100+i))
	}
	laneNodes = append(laneNodes, 11)
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 100}}, Nodes: []int64{1, 10}, Name: "Main", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, 100}, {0, 300}}, Nodes: []int64{10, 11}, Name: "Main", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, 300}, {0, 440}}, Nodes: []int64{11, 2}, Name: "Main", Class: "secondary", WidthM: 7},
		{Line: lane, Nodes: laneNodes, Name: "Lane", Class: "residential", WidthM: 4},
	}
	on := routeOn(c.Distance, func(d float64) int {
		switch {
		case d < 100:
			return 0
		case d < 300:
			return 1
		}
		return 2
	})
	w, err := BuildGraph(c, Options{Generator: "test", Region: RegionFR, Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	laneY, wall := math.Inf(1), 0
	for _, p := range w.prims {
		name := w.doc.MaterialName(p.Material)
		for v := 0; v+2 < len(p.Positions); v += 3 {
			x, y, n := float64(p.Positions[v]), float64(p.Positions[v+1]), -float64(p.Positions[v+2])
			if n < 160 || n > 240 {
				continue
			}
			switch {
			case name == "bridge" || name == "bridges":
				t.Fatalf("a deck at %.1f, %.0f: the lane beside the road taken for a bridge", x, n)
			case riddenMaterials[name] && x > 5.5 && x < 7.5:
				laneY = math.Min(laneY, y)
			case name == "roadside" && x > 3.5 && x < 5.5 && y > 10.5:
				wall++
			}
		}
	}
	if laneY < 11.2 {
		t.Errorf("the lane comes down to %.2f m beside the main road, want its own 11.5", laneY)
	}
	if wall == 0 {
		t.Error("no wall between the lane and the main road")
	}
	if r := w.Check(); r.HolesM2 > 0.1 {
		t.Errorf("holes %.2f m² (%v), want none between the roads", r.HolesM2, r.Holes)
	}
}

// A minor road meeting a major one (owner's photo of the lane off the Rue
// du Hohneck, 2026-10-16): the node is at the major road's height, and
// the minor road keeps it, flat, out past the major road's edge and a
// metre more before it climbs.
func TestMinorRoadMouthIsFlat(t *testing.T) {
	ways := []scenery.Way{
		{Line: [][2]float64{{-100, 0}, {0, 0}, {100, 0}}, Nodes: []int64{1, 2, 3}, Name: "Main", Class: "secondary", WidthM: 6},
		{Line: [][2]float64{{0, 0}, {0, 100}}, Nodes: []int64{2, 4}, Name: "Side", Class: "residential", WidthM: 4},
	}
	ls := networkLines(ways, nil, func(e, n float64) bool { return true })
	for _, l := range ls {
		for i := range l.samples {
			s := &l.samples[i]
			if s.way == 0 {
				s.ele = 10
			} else {
				s.ele = 10 + 0.1*s.d // the side road climbs 10 % from the node
			}
		}
	}
	g := newGnet(ls, ways, nil)
	g.anchorHeights(ways)
	if math.Abs(g.ele[2]-10) > 0.01 {
		t.Errorf("the junction at %.2f m, want the main road's 10", g.ele[2])
	}
	for _, l := range g.lines {
		if l.samples[0].way != 1 {
			continue
		}
		reach := 3 + gMouthFlatM // the main road's half width, and the flat metre
		for _, s := range l.samples {
			if s.d <= reach-0.01 && math.Abs(s.ele-10) > 0.01 {
				t.Errorf("the side road at %.1f m from the node is %.2f m high, want flat at 10", s.d, s.ele)
			}
		}
		if s := sampleBetween(l, 40); s.ele < 13 {
			t.Errorf("40 m up the side road at %.2f m: it should climb on beyond its mouth", s.ele)
		}
	}
}

// A French village's residential lanes have no sidewalks guessed from the
// built-up land beside them; its through roads keep theirs, and tagged
// sidewalks stay. The map data, shared, is left as it was.
func TestFrenchLanesHaveNoGuessedSidewalks(t *testing.T) {
	ways := []scenery.Way{
		{Class: "residential", Sidewalk: [2]bool{true, true}, WalkGuessed: true},
		{Class: "secondary", Sidewalk: [2]bool{true, true}, WalkGuessed: true},
		{Class: "residential", Sidewalk: [2]bool{true, false}},
	}
	sc := &scenery.Scenery{Ways: ways}
	for _, c := range []struct {
		region string
		want   [3]bool
	}{{RegionFR, [3]bool{false, true, true}}, {RegionNL, [3]bool{true, true, true}}} {
		o := Options{Region: c.region, Scenery: sc}
		o.regionScenery()
		for i, w := range o.Scenery.Ways {
			if got := w.Sidewalk[0]; got != c.want[i] {
				t.Errorf("%s: way %d (%s) sidewalk %v, want %v", c.region, i, w.Class, got, c.want[i])
			}
		}
	}
	if !sc.Ways[0].Sidewalk[0] {
		t.Error("the shared map data was changed")
	}
}

// Where only one road at a junction has a sidewalk, a Dutch junction's
// sidewalk goes halfway round and stops; a French one runs the whole way
// round the corner to the other road's edge (owner, 2026-10-18), unless
// the road's own sidewalk runs on across the junction as a dropped kerb
// (a service road's mouth): then the junction draws none of its own.
func TestJunctionWalkRoundsTheCorner(t *testing.T) {
	var p patch // no roads: the heights don't matter here, only what is drawn
	w := walkChain{kerb: [][2]float64{{0, 0}, {3, 0}, {6, 0}, {7, 1}, {7, 4}}, fromA: true}
	reach := func(corner bool, drop bool) (drawn bool, far float64) {
		p.dropWalks = drop
		var walk, kerb gltf.Primitive
		walkMesh(p, w, &walk, &kerb, corner)
		for i := 0; i+2 < len(walk.Positions); i += 3 {
			far = math.Max(far, math.Hypot(float64(walk.Positions[i]), float64(-walk.Positions[i+2])))
		}
		return len(walk.Indices) > 0, far
	}
	if drawn, far := reach(false, false); !drawn || far > 7.5 {
		t.Errorf("Dutch: drawn %v, reaching %.1f m from the start; want about halfway round (under 7.5 m)", drawn, far)
	}
	if drawn, far := reach(true, false); !drawn || far < 7.5 {
		t.Errorf("French: drawn %v, reaching %.1f m; want the whole way round to the other road (over 7.5 m)", drawn, far)
	}
	if drawn, _ := reach(true, true); drawn {
		t.Error("French, the road's sidewalk running on across the mouth: the junction drew one of its own too")
	}
}

// A lane 1.2 m above a road, its edge 1.5 m behind the road's sidewalk,
// is a terrace (owner's photos of the Rue du Hohneck): the road's samples
// along it carry the band, the sidewalk's width, the lane's height and
// its offset, and the road's own sidewalk is off there; where the lane
// is level with the road, nothing.
func TestMarkTerraces(t *testing.T) {
	line := func(e0, ele float64, walk bool) *roadLine {
		l := &roadLine{}
		for i := 0; i <= 20; i++ {
			d := float64(i) * fineStep
			s := sample{d: d, e: e0, n: d, ele: ele, hw: 3, edge: 3}
			s.walk[1] = walk
			l.samples = append(l.samples, s)
		}
		return l
	}
	road, lane := line(0, 10, true), line(0+3+walkM+1.5+1.5, 11.2, false)
	markTerraces([]*roadLine{road, lane}, nil)
	s := road.samples[10]
	if s.terrace[1] == 0 || s.walk[1] || s.terraceWalk[1] != walkM {
		t.Fatalf("the road's sample: terrace %.2f walk %v terraceWalk %.2f; want a terrace with its sidewalk's width, the sidewalk off", s.terrace[1], s.walk[1], s.terraceWalk[1])
	}
	if math.Abs(s.terrace[1]-(3+walkM)) > 1e-9 || math.Abs(s.terraceBack[1]-11.2) > 1e-9 || s.terraceBehind[1] <= s.terrace[1] {
		t.Errorf("band %.2f back %.2f behind %.2f; want the sidewalk's edge %.2f, the lane's 11.2, an offset beyond the band", s.terrace[1], s.terraceBack[1], s.terraceBehind[1], 3+walkM)
	}
	if lane.samples[10].terrace[0] != 0 || lane.samples[10].terrace[1] != 0 {
		t.Error("the lane, the higher road, got a terrace of its own")
	}
	level := line(0, 10, true)
	markTerraces([]*roadLine{level, line(0+3+walkM+3, 10.1, false)}, nil)
	if level.samples[10].terrace[1] != 0 {
		t.Error("a road level with the one beside it got a terrace")
	}
}

// A terrace's asphalt filler ramps only at a mouth, where a road crosses
// the sidewalk's line or a junction takes it; where the terrace ends
// because the road above drifts out of reach, the land takes over from
// the wall's end (10.92 km of the Route des Américains: the filler, flat
// at the lower road's level, ran out under the road above).
func TestTerraceApronOnlyAtAMouth(t *testing.T) {
	line := func(e0, n0, n1, ele float64, walk bool) *roadLine {
		l := &roadLine{}
		for n := n0; n <= n1+1e-9; n += fineStep {
			s := sample{d: n - n0, e: e0, n: n, ele: ele, hw: 3, edge: 3}
			s.walk[1] = walk
			l.samples = append(l.samples, s)
		}
		return l
	}
	m := roadMaterials{surface: [5]int{1, 1, 1, 1, 1}, walk: 2, kerb: 3}
	band := 3 + walkM
	// The lower road's level beyond the terrace's band: a filler would be
	// the only thing drawn there under 10.5 m.
	lowBeyond := func(ps map[[2]int][]gltf.Primitive, n0, n1 float64) int {
		c := 0
		for _, pp := range ps {
			for _, p := range pp {
				for v := 0; v+2 < len(p.Positions); v += 3 {
					e, y, n := float64(p.Positions[v]), float64(p.Positions[v+1]), -float64(p.Positions[v+2])
					if n >= n0 && n <= n1 && e > band+0.3 && y < 10.5 {
						c++
					}
				}
			}
		}
		return c
	}
	// The higher road alongside from 10 to 30 m only: the terrace ends in
	// the open at both ends.
	road, lane := line(0, 0, 60, 10, true), line(band+1.5+1.5, 10, 30, 11.2, false)
	markTerraces([]*roadLine{road, lane}, nil)
	if road.samples[10].terrace[1] == 0 {
		t.Fatal("no terrace beside the higher road")
	}
	if c := lowBeyond(terracePrims([]*roadLine{road, lane}, m, 250), 0, 60); c > 0 {
		t.Errorf("%d filler vertices at the lower road's level beyond the terrace's band: the terrace ends in the open, where the land takes over", c)
	}
	// A road crossing the sidewalk's line at 32 m, at the road's level: a
	// mouth, and the filler ramps up to the slab there.
	road, lane = line(0, 0, 60, 10, true), line(band+1.5+1.5, 10, 34, 11.2, false)
	cross := &roadLine{}
	for e := 3.0; e <= 12; e += fineStep {
		cross.samples = append(cross.samples, sample{d: e - 3, e: e, n: 32, ele: 10, hw: 2, edge: 2})
	}
	markTerraces([]*roadLine{road, lane, cross}, nil)
	if !road.samples[16].terraceMouth[1] {
		t.Fatal("the crossing road at 32 m isn't a mouth to the terrace")
	}
	for k := range road.samples {
		if road.samples[k].terraceMouth[1] {
			road.samples[k].walk[1] = false // the junction takes the sidewalk at the mouth
		}
	}
	if c := lowBeyond(terracePrims([]*roadLine{road, lane, cross}, m, 250), 24, 36); c == 0 {
		t.Error("no filler ramps from the mouth up to the slab")
	}
}

// Two roads alongside each other a step apart in height, too far apart
// for a wall between them: the land between is a straight bank from the
// higher one's edge down to the lower's, not level at each edge with a
// cliff in the middle (owner, 2026-10-17: the Route des Crêtes coming down
// beside the Route des Américains at 10.91 km).
func TestGraphBankBetweenRoadsAStepApart(t *testing.T) {
	c := graphCourse(t, "bank", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		if e > 6 {
			return 11.5, true
		}
		return 10, true
	}
	// The higher road's edge 4.5 m from the main road's: beyond a wall's
	// reach (terraceReachM), so a bank.
	upper := [][2]float64{{10, -40}, {10, 440}}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 440}}, Nodes: []int64{1, 2}, Name: "Main", Class: "secondary", WidthM: 7},
		{Line: upper, Nodes: []int64{3, 4}, Name: "Upper", Class: "residential", WidthM: 4},
	}
	on := routeOn(c.Distance, func(float64) int { return 0 })
	w, err := BuildGraph(c, Options{Generator: "test", Region: RegionFR, Elevation: ground, Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	var vs [][3]float64 // the land between the roads' edges, mid-course
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "terrain" {
			continue
		}
		for v := 0; v+2 < len(p.Positions); v += 3 {
			x, y, n := float64(p.Positions[v]), float64(p.Positions[v+1]), -float64(p.Positions[v+2])
			if n > 150 && n < 250 && x > 3.3 && x < 8.2 {
				vs = append(vs, [3]float64{x, y, n})
			}
		}
	}
	if len(vs) < 20 {
		t.Fatalf("%d land vertices between the roads, want a bank of them", len(vs))
	}
	worst := 0.0
	for i, a := range vs {
		for _, b := range vs[i+1:] {
			if d := math.Hypot(a[0]-b[0], a[2]-b[2]); d > 0.1 && d < 0.7 {
				worst = math.Max(worst, math.Abs(a[1]-b[1]))
			}
		}
	}
	if worst > 0.6 {
		t.Errorf("the land between the roads steps %.2f m within 0.7 m: a cliff, want a bank", worst)
	}
	if r := w.Check(); r.HolesM2 > 0.1 || r.LandOverRoadM2 > 0.05 {
		t.Errorf("holes %.2f m², land over road %.2f m² between the roads", r.HolesM2, r.LandOverRoadM2)
	}
}

// A terrace's asphalt filler comes down with the wall's top within the end
// wedges (its far edge stays the road above's), so it ends no higher than
// the wall's end over the bank beside it.
func TestTerraceFillerComesDownAtItsEnds(t *testing.T) {
	line := func(e0, n0, n1, ele float64, walk bool) *roadLine {
		l := &roadLine{}
		for n := n0; n <= n1+1e-9; n += fineStep {
			s := sample{d: n - n0, e: e0, n: n, ele: ele, hw: 3, edge: 3}
			s.walk[1] = walk
			l.samples = append(l.samples, s)
		}
		return l
	}
	band := 3 + walkM
	road, lane := line(0, 0, 60, 10, true), line(band+1.5+1.5, 10, 40, 11.2, false)
	markTerraces([]*roadLine{road, lane}, nil)
	m := roadMaterials{surface: [5]int{1, 1, 1, 1, 1}, walk: 2, kerb: 3}
	lo, hi := math.Inf(1), math.Inf(-1) // the filler's edge at the wall's back
	for _, pp := range terracePrims([]*roadLine{road, lane}, m, 250) {
		for _, p := range pp {
			if p.Material != 1 {
				continue
			}
			for v := 0; v+2 < len(p.Positions); v += 3 {
				if e := float64(p.Positions[v]); math.Abs(e-(band+terraceCopeM)) < 0.05 {
					y := float64(p.Positions[v+1])
					lo, hi = math.Min(lo, y), math.Max(hi, y)
				}
			}
		}
	}
	if hi < 11.1 {
		t.Errorf("the filler's near edge reaches %.2f m at most, want the road above's 11.2 along the wall", hi)
	}
	if lo > 10+kerbM+terraceEndM {
		t.Errorf("the filler's near edge comes down to %.2f m, want the wall's end height %.2f", lo, 10+kerbM+terraceEndM)
	}
}

// Two roads alongside, level at first and then a step apart: the join's
// reach over to the other road (joinAlongside) tapers out where they are
// level, and never onto the stretch where the other is a level of its own.
func TestJoinAlongsideStopsAtALevelApart(t *testing.T) {
	main, side := &roadLine{}, &roadLine{}
	for n := 0.0; n <= 100; n += fineStep {
		main.samples = append(main.samples, sample{d: n, e: 0, n: n, ele: 10, hw: 3.5})
		ele := 10.0
		if n >= 50 {
			ele = 11.5
		}
		side.samples = append(side.samples, sample{d: n, e: 6.5, n: n, ele: ele, hw: 2})
	}
	joinAlongside([]*roadLine{main, side}, 5, abreastStepM)
	joinedLevel, wideApart := false, 0.0
	for _, s := range main.samples {
		switch {
		case s.n < 44:
			joinedLevel = joinedLevel || s.wide[1] > 0.5
		case s.n >= 50:
			wideApart = math.Max(wideApart, s.wide[1])
		}
	}
	if !joinedLevel {
		t.Error("the roads aren't joined where level")
	}
	if wideApart > 0 {
		t.Errorf("the main road reaches %.2f m over towards the road a level above it", wideApart)
	}
}
