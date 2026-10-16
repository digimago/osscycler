package world

import (
	"math"
	"testing"

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
