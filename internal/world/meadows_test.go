package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/scenery"
)

// A meadow by the road is fenced along it, posts and wire with a gate in
// a long stretch; not where a ditch lies between road and meadow (the
// ditch is the fence), nor beside other land (owner, 2026-10-12).
func TestMeadowsAreFencedAlongTheRoad(t *testing.T) {
	c := northCourse(t) // 1 km north, x = 0
	kx := 111195 * math.Cos(52*math.Pi/180)
	ll := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	rect := func(id int64, x0, y0, x1, y1 float64, kv ...string) scenery.Element {
		tags := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			tags[kv[i]] = kv[i+1]
		}
		return scenery.Element{Type: "way", ID: id, Tags: tags,
			Geometry: []scenery.LatLon{ll(x0, y0), ll(x1, y0), ll(x1, y1), ll(x0, y1), ll(x0, y0)}}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		rect(1, 0, 100, 200, 400, "landuse", "meadow"),                  // east: meadow to the road
		rect(2, -200, 100, 0, 400, "landuse", "meadow"),                 // west: meadow
		rect(3, -6, 100, -4, 400, "natural", "water", "water", "ditch"), // west: a ditch between
		rect(4, 0, 500, 200, 700, "landuse", "farmland"),                // east further on: a field
	}})
	tr := testTerrain(c, Options{Land: lm})
	fences := meadowFences(tr, lm, nil, nil)
	var east, west float64
	gates := 0
	for _, f := range fences {
		if !f.Wire {
			t.Fatal("a meadow fence that isn't wire")
		}
		mid := polyAt(f.Line, polyLength(f.Line)/2)
		if mid[1] < 100 || mid[1] > 400 {
			t.Errorf("a fence at n %.0f, beside no meadow", mid[1])
		}
		if mid[0] > 0 {
			east += polyLength(f.Line)
		} else {
			west += polyLength(f.Line)
		}
		if f.GateToM > f.GateFromM {
			gates++
		}
	}
	if east < 250 || west > 0 || gates == 0 {
		t.Errorf("fenced %.0f m east (want most of 300), %.0f m west (want none: the ditch), %d gates", east, west, gates)
	}
}
