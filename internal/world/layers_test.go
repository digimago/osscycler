package world

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// A road moved to clear another holds its height across the other road,
// eases in and out no steeper than its grade, and between two holds near
// each other runs straight from one to the other (the figure 8's motorway
// from over its southern arc to under its northern one).
func TestLayerCorrection(t *testing.T) {
	const grade = layerMotorwayGrade
	corr := layerCorrection([]layerHold{{d: 600, c: -5, p: 10}, {d: 100, c: 5, p: 10}}, grade)
	for _, x := range []struct{ d, want float64 }{
		{100, 5}, {109, 5}, {600, -5}, {591, -5},
		{350, 0}, // halfway along the straight between them
		{90 - math.Pi/2*5/grade - 1, 0}, {610 + math.Pi/2*5/grade + 1, 0},
	} {
		if got := corr(x.d); math.Abs(got-x.want) > 1e-6 {
			t.Errorf("at %.0f m: %.3f, want %.3f", x.d, got, x.want)
		}
	}
	for d := -300.0; d < 1000; d++ {
		if s := math.Abs(corr(d+1) - corr(d)); s > grade+1e-3 {
			t.Fatalf("at %.0f m the correction changes %.3f in a metre, more than its grade %.2f", d, s, grade)
		}
	}
}

// layerWorld builds a course north along a mapped road crossed at 200 m by
// a dual carriageway motorway running east to west through a forest, with
// no ground model: the road in a tunnel under it (tunnel), or on a bridge
// over it.
func layerWorld(t *testing.T, tunnel bool) *World {
	t.Helper()
	c := graphCourse(t, "layers", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	under := scenery.Way{Line: [][2]float64{{0, 180}, {0, 220}}, Nodes: []int64{2, 3}, Name: "Bosweg", Class: "unclassified", WidthM: 5}
	if tunnel {
		under.Tunnel, under.Layer = true, -1
	} else {
		under.Bridge, under.Layer = true, 1
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 180}}, Nodes: []int64{1, 2}, Name: "Bosweg", Class: "unclassified", WidthM: 5},
		under,
		{Line: [][2]float64{{0, 220}, {0, 440}}, Nodes: []int64{3, 4}, Name: "Bosweg", Class: "unclassified", WidthM: 5},
		{Line: [][2]float64{{-300, 192}, {300, 192}}, Nodes: []int64{10, 11}, Class: "motorway", WidthM: 11, Motorway: true, Oneway: 1},
		{Line: [][2]float64{{300, 208}, {-300, 208}}, Nodes: []int64{12, 13}, Class: "motorway", WidthM: 11, Motorway: true, Oneway: 1},
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
	const lat0, lon0 = 52.0, 5.0
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	ll := func(e, n float64) scenery.LatLon { return scenery.LatLon{Lat: lat0 + n/111195, Lon: lon0 + e/kx} }
	data := &scenery.Data{Elements: []scenery.Element{{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest", "leaf_type": "broadleaved"},
		Geometry: []scenery.LatLon{ll(-400, -300), ll(400, -300), ll(400, 700), ll(-400, 700), ll(-400, -300)}}}}
	w, err := Build(c, Options{Generator: "test", Land: scenery.NewLandMap(c, data), Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// surfaceTop is the highest surface of material name at e, n in w.
func surfaceTop(w *World, name string, e, n float64) (float64, bool) {
	var ps []gltf.Primitive
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) == name {
			ps = append(ps, p)
		}
	}
	if len(ps) == 0 {
		return 0, false
	}
	return newSurfaceIndex(ps, func(int) bool { return true }, 200).top(e, n, true)
}

// A motorway crossing a road the course rides, with no ground model to
// set them apart, lies over it on an embankment where the map has the road
// in a tunnel (owner, 2026-10-22: the figure 8's motorway): the course
// keeps its height through the tunnel, the motorway clears it, the land
// closes over the tunnel's roof, the tunnel has its tube and portals, and
// nothing grows in the motorway's clear zone.
func TestMotorwayPassesOverATunnel(t *testing.T) {
	w := layerWorld(t, true)
	if r := w.Check(); r.HolesM2 > 0.1 || r.LandOverRoadM2 > 0.05 || r.OffRoadM > 0 {
		t.Errorf("holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v",
			r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 0; 3*i < len(xyz); i++ {
		if y := xyz[3*i+1]; math.Abs(y-10) > 0.05 {
			t.Fatalf("at %.0f m north the course is %.2f m high, want the profile's 10 m", -xyz[3*i+2], y)
		}
	}
	for _, n := range []float64{192, 208} {
		if y, ok := surfaceTop(w, "road", 0, n); !ok || y < 10+layerM-0.01 {
			t.Errorf("the motorway over the tunnel at %.0f m: %.2f m (%v), want at least %.2f", n, y, ok, 10+layerM)
		}
	}
	if y, ok := surfaceTop(w, "terrain", 0, 200); !ok || y < roofTop(10, layerClearM)-0.05 {
		t.Errorf("the land between the carriageways over the tunnel: %.2f m (%v), want its roof's top %.2f", y, ok, roofTop(10, layerClearM))
	}
	if y, ok := surfaceTop(w, "tunnel", 0, 200); !ok || math.Abs(y-roofTop(10, layerClearM)) > 0.01 {
		t.Errorf("the tunnel's roof: %.2f m (%v), want %.2f", y, ok, roofTop(10, layerClearM))
	}
	// The portals' headwalls stand over the opening at both ends.
	for _, n := range []float64{180 - headwallT/2, 220 + headwallT/2} {
		if y, ok := surfaceTop(w, "tunnel", 0, n); !ok || y < roofTop(10, layerClearM)+parapetM-0.01 {
			t.Errorf("the headwall at %.1f m: %.2f m (%v), want its parapet at %.2f", n, y, ok, roofTop(10, layerClearM)+parapetM)
		}
	}
	plants := 0
	for _, g := range w.Manifest.Instances.Groups {
		for i := g.Offset; i < g.Offset+g.Count; i++ {
			e := float64(math.Float32frombits(binary.LittleEndian.Uint32(w.instances[4*instanceFloats*i:])))
			n := -float64(math.Float32frombits(binary.LittleEndian.Uint32(w.instances[4*instanceFloats*i+8:])))
			plants++
			// Within the motorway's reach (drawn to motorwayClipM from the
			// course), in its clear zone.
			if math.Abs(e) < motorwayClipM-10 && n > 192-5.5-motorwayClearM+0.5 && n < 208+5.5+motorwayClearM-0.5 {
				t.Fatalf("a %s at %.1f m east, %.1f m north, in the motorway's clear zone", g.Kind, e, n)
			}
		}
	}
	if plants < 100 {
		t.Errorf("%d plants in the forest, want it planted", plants)
	}
}

// Where the map has the road the course rides on a bridge over the
// motorway, the motorway goes under it, in a cutting, and the bridge is a
// deck; the course keeps its height.
func TestMotorwayPassesUnderABridge(t *testing.T) {
	w := layerWorld(t, false)
	if r := w.Check(); r.HolesM2 > 0.1 || r.LandOverRoadM2 > 0.05 || r.OffRoadM > 0 {
		t.Errorf("holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v",
			r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad)
	}
	xyz := w.Manifest.Path.XYZ
	for i := 0; 3*i < len(xyz); i++ {
		if y := xyz[3*i+1]; math.Abs(y-10) > 0.05 {
			t.Fatalf("at %.0f m north the course is %.2f m high, want the profile's 10 m", -xyz[3*i+2], y)
		}
	}
	for _, e := range []float64{-4, 4} { // beside the bridge's deck
		if y, ok := surfaceTop(w, "road", e, 192); !ok || y > 10-layerM+0.01 {
			t.Errorf("the motorway under the bridge at %.0f m east: %.2f m (%v), want at most %.2f", e, y, ok, 10-layerM)
		}
	}
	if _, ok := surfaceTop(w, "bridge", 0, 200); !ok {
		t.Error("no bridge deck over the motorway")
	}
	if _, ok := surfaceTop(w, "tunnel", 0, 200); ok {
		t.Error("a tunnel where the road is on a bridge")
	}
}

// A tunnel's headroom is what its cover allows: under a railway
// embankment 4 m over the road, the land over it stays the embankment's
// (no mound over the roof), the roof under it.
func TestTunnelHeadroomFromItsCover(t *testing.T) {
	c := graphCourse(t, "tunnel", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		return 10 + 4*smoothstep(math.Max(0, math.Min(1, (20-math.Abs(n-200))/8))), true
	}
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 180}}, Nodes: []int64{1, 2}, Name: "Dike", Class: "tertiary", WidthM: 6},
		{Line: [][2]float64{{0, 180}, {0, 220}}, Nodes: []int64{2, 3}, Name: "Dike tunnel", Class: "tertiary", WidthM: 6, Tunnel: true, Layer: -1},
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
	road, _ := surfaceTop(w, "road", 0, 200)
	roof, ok := surfaceTop(w, "tunnel", 0, 200)
	if !ok || roof-road < tunnelMinClearM+tunnelRoofM-0.01 {
		t.Fatalf("the tunnel's roof %.2f m (%v) over its road at %.2f m, want at least %.1f m of headroom", roof, ok, road, tunnelMinClearM)
	}
	if land, _ := surfaceTop(w, "terrain", 0, 200); land > 14.1 {
		t.Errorf("the land over the tunnel at %.2f m, over the embankment's 14 m", land)
	}
	if r := w.Check(); r.HolesM2 > 0.1 || r.LandOverRoadM2 > 0.05 || r.OffRoadM > 0 {
		t.Errorf("holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v",
			r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad)
	}
}

// A motorway that must go under the course's bridge moves even where a
// slip road joins it near the crossing: the slip road takes the change at
// their node and eases it out along it, its far end staying (owner,
// 2026-10-22: held still at the interchange's nodes, the A13 by Rodange
// ran through the bridge the course crosses it on).
func TestMotorwayMovesWithItsSlipRoad(t *testing.T) {
	c := graphCourse(t, "slip", append(along([2]float64{0, 0}, [2]float64{0, 400}, 20), [2]float64{0, 400}))
	ways := []scenery.Way{
		{Line: [][2]float64{{0, -40}, {0, 180}}, Nodes: []int64{1, 2}, Name: "Bosweg", Class: "unclassified", WidthM: 5},
		{Line: [][2]float64{{0, 180}, {0, 220}}, Nodes: []int64{2, 3}, Name: "Bosweg", Class: "unclassified", WidthM: 5, Bridge: true, Layer: 1},
		{Line: [][2]float64{{0, 220}, {0, 440}}, Nodes: []int64{3, 4}, Name: "Bosweg", Class: "unclassified", WidthM: 5},
		{Line: [][2]float64{{-300, 200}, {60, 200}, {300, 200}}, Nodes: []int64{10, 14, 11}, Class: "motorway", WidthM: 11, Motorway: true, Oneway: 1},
		{Line: [][2]float64{{60, 200}, {90, 215}, {120, 240}}, Nodes: []int64{14, 15, 16}, Class: "motorway", WidthM: 6, Motorway: true, Oneway: 1},
		{Line: [][2]float64{{120, 160}, {120, 240}, {120, 320}}, Nodes: []int64{20, 16, 21}, Name: "Dorpsweg", Class: "tertiary", WidthM: 6},
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
	w, err := Build(c, Options{Generator: "test", Scenery: &scenery.Scenery{Ways: ways, RouteOn: on, RouteStepM: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if y, ok := surfaceTop(w, "road", -6, 200); !ok || y > 10-layerM+0.01 {
		t.Errorf("the motorway beside the bridge: %.2f m (%v), want at most %.2f", y, ok, 10-layerM)
	}
	// The road the slip road leads to keeps its height.
	for _, n := range []float64{170, 290} {
		if y, ok := surfaceTop(w, "road", 120, n); !ok || math.Abs(y-10) > 0.05 {
			t.Errorf("the road the slip road joins, at %.0f m north: %.2f m (%v), want it where it was, 10 m", n, y, ok)
		}
	}
}
