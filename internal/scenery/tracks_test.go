package scenery

import (
	"math"
	"reflect"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func TestTrackScenery(t *testing.T) {
	for _, c := range course.Tracks() {
		tr := TrackScenery(c)
		if tr == nil || len(tr.Data.Elements) == 0 {
			t.Fatalf("%s: no scenery", c.ID)
		}
		if again := TrackScenery(c); !reflect.DeepEqual(again.Data, tr.Data) {
			t.Errorf("%s: not the same twice", c.ID)
		}
		fps := Footprints(c, tr.Data)
		if len(fps) == 0 {
			t.Errorf("%s: no buildings", c.ID)
		}
		lm := NewLandMap(c, tr.Data)
		seen := map[Land]bool{}
		for d := 0.0; d < c.Distance; d += 25 {
			lat, lon := c.Position(d)
			e, n := c.Project(lat, lon)
			for _, off := range []float64{-40, 40} {
				l, _ := lm.Area(e+off, n)
				seen[l] = true
			}
		}
		switch c.ID {
		case "figure-8":
			for _, l := range []Land{LandFarmland, LandForest, LandHeath, LandMeadow} {
				if !seen[l] {
					t.Errorf("figure 8: no %v beside the road", l)
				}
			}
		case "oval-400":
			if len(tr.Lawns) == 0 || len(tr.Roads) != 1 || tr.Roads[0].Surface != SurfaceTrack {
				t.Errorf("oval: lawns %d, roads %+v", len(tr.Lawns), tr.Roads)
			}
		}
	}
	if TrackScenery(course.Included()[len(course.Included())-1]) != nil {
		t.Error("a real course got made-up scenery")
	}
}

// The polder's gates stand at field entrances, set back from the road with
// a strip of bare earth between, not across a ditch's end by the road
// (owner, 2026-10-12). Each strip begins at the road's edge, keeps clear
// of the ditches, has its gate across its far end, and nothing grows on it.
func TestFieldEntrances(t *testing.T) {
	var c *course.Course
	for _, x := range course.Tracks() {
		if x.ID == "figure-8" {
			c = x
		}
	}
	tr := TrackScenery(c)
	g := newTrackGen(c)
	var ditches [][][2]float64
	for _, el := range tr.Data.Elements {
		if el.Tags["water"] == "ditch" {
			var ring [][2]float64
			for _, p := range el.Geometry {
				e, n := c.Project(p.Lat, p.Lon)
				ring = append(ring, [2]float64{e, n})
			}
			ditches = append(ditches, ring)
		}
	}
	if len(tr.Fences) < 10 || len(ditches) == 0 {
		t.Fatalf("%d entrances, %d ditches", len(tr.Fences), len(ditches))
	}
	lawns := map[[2]float64]bool{}
	for _, l := range tr.Lawns {
		lawns[l[0]] = true
	}
	for i, f := range tr.Fences {
		if len(f.Entry) != 4 {
			t.Fatalf("fence %d: no entrance strip", i)
		}
		near := [2]float64{(f.Entry[0][0] + f.Entry[1][0]) / 2, (f.Entry[0][1] + f.Entry[1][1]) / 2}
		far := [2]float64{(f.Entry[2][0] + f.Entry[3][0]) / 2, (f.Entry[2][1] + f.Entry[3][1]) / 2}
		if d := g.dist(near); d > entryEdgeM+0.5 {
			t.Errorf("fence %d: the strip begins %.1f m off the road's line", i, d)
		}
		// The gate across the strip, entryGateM off the road's edge.
		mid := [2]float64{(f.Line[0][0] + f.Line[1][0]) / 2, (f.Line[0][1] + f.Line[1][1]) / 2}
		if d := g.dist(mid); d < entryEdgeM+entryGateM-0.5 || d > entryEdgeM+entryGateM+0.5 {
			t.Errorf("fence %d: the gate %.1f m off the road's line", i, d)
		}
		for _, q := range append([][2]float64{near, far}, f.Entry...) {
			for _, ring := range ditches {
				if inRing(q, ring) {
					t.Errorf("fence %d: its strip runs into a ditch at %.0f, %.0f", i, q[0], q[1])
				}
			}
		}
		if !lawns[f.Entry[0]] {
			t.Errorf("fence %d: plants may grow on its strip", i)
		}
	}
}

func inRing(p [2]float64, ring [][2]float64) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

// The figure 8 has a village and a river on mapped roads (owner,
// 2026-10-22: the demo track has every kind of thing a world is built
// from): its streets meet at shared nodes, the roundabout is one ring of
// one lane, the river has two bridges over it, and the route is on the
// map's roads in the village and over the northern bridge, its own way
// everywhere else (the crossing above all).
func TestFigureEightVillage(t *testing.T) {
	c := course.Tracks()[1]
	data := TrackScenery(c).Data
	nodes := map[string]map[int64]bool{} // by street name
	ring := map[int64]bool{}
	var bridges, rings, parking, river, school int
	for _, el := range data.Elements {
		tg := el.Tags
		switch {
		case tg["junction"] == "roundabout":
			rings++
			for _, n := range el.Nodes {
				ring[n] = true
			}
		case tg["highway"] != "":
			if nodes[tg["name"]] == nil {
				nodes[tg["name"]] = map[int64]bool{}
			}
			for _, n := range el.Nodes {
				nodes[tg["name"]][n] = true
			}
			if tg["bridge"] == "yes" && tg["name"] != "Heideweg" { // the motorway's (TestFigureEightMotorway)
				bridges++
			}
		case tg["amenity"] == "parking":
			parking++
		case tg["water"] == "river":
			river++
		case tg["building"] == "school":
			school++
		}
	}
	if rings != 4 || len(ring) != 16 || bridges != 2 || parking != 1 || river < 2 || school != 1 {
		t.Errorf("%d ring pieces of %d nodes, %d bridges, %d car parks, %d river pieces, %d schools", rings, len(ring), bridges, parking, river, school)
	}
	meets := func(a map[int64]bool, b map[int64]bool) bool {
		for n := range a {
			if b[n] {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"Dorpsstraat", "Kerkstraat", "Polderweg"} {
		if !meets(nodes[name], ring) {
			t.Errorf("%s doesn't meet the roundabout", name)
		}
	}
	for _, name := range []string{"Schoolstraat", ""} { // "": the car park's entrance
		if !meets(nodes[name], nodes["Dorpsstraat"]) {
			t.Errorf("%q doesn't meet Dorpsstraat", name)
		}
	}
	if len(nodes["Rivierdijk"]) == 0 {
		t.Error("no road over the river on the northern arc")
	}
	sc := Build(c, data)
	on := func(d float64) bool { return sc.RouteOn[int(d/sc.RouteStepM)] >= 0 }
	south, north := motorwayCrossings(c)
	for d := 0.0; d < c.Distance; d += 50 {
		mapped := d > villageFromM+20 && d < villageToM-20 || d > 560 && d < 620
		free := d < 520 || d > 660 && d < villageFromM-20 || d > villageToM+20
		if math.Abs(d-south) < crossMappedM+20 || math.Abs(d-north) < crossMappedM+20 {
			continue
		}
		if mapped && !on(d) || free && on(d) {
			t.Errorf("at %.0f m the route is on a map road: %v", d, on(d))
		}
	}
}

// motorwayCrossings is where the figure 8 crosses its motorway's axis.
func motorwayCrossings(c *course.Course) (south, north float64) {
	g := newTrackGen(c)
	x := (g.lo[0]+g.hi[0])/2 + motorwayX
	return g.riverAt(x, 1250, 2500), g.riverAt(x, 2500, 3750)
}

// The figure 8's motorway (owner, 2026-10-22): two one-way carriageways
// straight across the forest lobe, on an embankment over the southern arc
// and in a cutting under the northern one; the course goes through a
// tunnel under the first and over a bridge above the second, on mapped
// roads there and never on the motorway.
func TestFigureEightMotorway(t *testing.T) {
	c := course.Tracks()[1]
	data := TrackScenery(c).Data
	var north, south, bank, cut, tunnel, bridge int
	for _, el := range data.Elements {
		tg := el.Tags
		switch {
		case tg["highway"] == "motorway":
			if tg["oneway"] != "yes" {
				t.Errorf("motorway way %d isn't one-way", el.ID)
			}
			g := el.Geometry
			if g[len(g)-1].Lat > g[0].Lat {
				north++
			} else {
				south++
			}
			if tg["embankment"] == "yes" {
				bank++
			}
			if tg["cutting"] == "yes" {
				cut++
			}
		case tg["name"] == "Bosweg" && tg["tunnel"] == "yes" && tg["layer"] == "-1":
			tunnel++
		case tg["name"] == "Heideweg" && tg["bridge"] == "yes" && tg["layer"] == "1":
			bridge++
		}
	}
	if north != 5 || south != 5 || bank != 2 || cut != 2 || tunnel != 1 || bridge != 1 {
		t.Errorf("%d northbound and %d southbound pieces, %d on the embankment, %d in the cutting, %d tunnels, %d bridges",
			north, south, bank, cut, tunnel, bridge)
	}
	sc := Build(c, data)
	s, n := motorwayCrossings(c)
	for d := 0.0; d < c.Distance; d += sc.RouteStepM {
		k := sc.RouteOn[min(int(d/sc.RouteStepM), len(sc.RouteOn)-1)]
		if k >= 0 && sc.Ways[k].Motorway {
			t.Fatalf("at %.0f m the route is on the motorway", d)
		}
		for _, x := range []struct {
			at             float64
			tunnel, bridge bool
		}{{s, true, false}, {n, false, true}} {
			if math.Abs(d-x.at) < 5 && (k < 0 || sc.Ways[k].Tunnel != x.tunnel || sc.Ways[k].Bridge != x.bridge) {
				t.Errorf("at %.0f m the route isn't on the crossing's mapped road (%d)", d, k)
			}
		}
	}
}
