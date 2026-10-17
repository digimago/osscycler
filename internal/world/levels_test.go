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

// Where only one road at a junction has a sidewalk, a French junction
// draws none of its own (the road's goes on along its span to the other
// road's mouth); elsewhere it goes halfway round.
func TestJunctionWalkStopsAtTheCorner(t *testing.T) {
	var p patch // no roads: the heights don't matter here, only whether it is drawn
	w := walkChain{kerb: [][2]float64{{0, 0}, {3, 0}, {6, 0}, {7, 1}, {7, 4}}, fromA: true}
	for _, corner := range []bool{false, true} {
		var walk, kerb gltf.Primitive
		walkMesh(p, w, &walk, &kerb, corner)
		if drawn := len(walk.Indices) > 0; drawn == corner {
			t.Errorf("corner %v: the junction's own sidewalk drawn %v", corner, drawn)
		}
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
	markTerraces([]*roadLine{road, lane})
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
	markTerraces([]*roadLine{level, line(0+3+walkM+3, 10.1, false)})
	if level.samples[10].terrace[1] != 0 {
		t.Error("a road level with the one beside it got a terrace")
	}
}
