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
		rect(5, 8, 250, 9.5, 400, "natural", "water", "water", "ditch"), // east from n 250: a roadside ditch 5.5-7 m off the edge
	}})
	tr := testTerrain(c, Options{Land: lm})
	fences := meadowFences(tr, lm, nil, nil, nil)
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
	if east < 120 || east > 160 || west > 0 || gates == 0 {
		t.Errorf("fenced %.0f m east (want most of the 150 m without a ditch), %.0f m west (want none: the ditch), %d gates", east, west, gates)
	}
}

// Avenue trees stand in the verge, the meadow's fence behind them: a
// meadow with trees in it can't be mown (owner, 2026-10-19: the polder
// ride and the figure 8 had the fence 1.2 m off the road and the trees
// 2-3.5 m off it, in the meadow).
func TestMeadowFenceStandsBehindTheAvenue(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ll := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "meadow"},
			Geometry: []scenery.LatLon{ll(0, 100), ll(200, 100), ll(200, 900), ll(0, 900), ll(0, 100)}},
	}})
	way := scenery.Way{ID: 42, Line: [][2]float64{{0, -100}, {0, 1100}}}
	tr := testTerrain(c, Options{Land: lm})
	for i := range tr.fine {
		tr.fine[i].way = 0
	}
	ways := []scenery.Way{way}
	fences := meadowFences(tr, lm, ways, nil, nil)
	var posts [][2]float64
	for _, f := range fences {
		posts = append(posts, f.Line...)
	}
	if len(posts) == 0 {
		t.Fatal("no fence along the meadow")
	}
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	trees := 0
	for _, p := range plants(tr, s, lm, nil, nil, nil, ways, newRouteGrid(tr.coarse, 400)) {
		e, n := float64(p.x), -float64(p.z)
		if p.kind >= speciesKinds || e <= 0 || e > 6 || n < 200 || n > 800 {
			continue // an avenue tree on the meadow's side, away from the fence's ends
		}
		trees++
		for _, q := range posts {
			if math.Abs(q[1]-n) < 2 && q[0] <= e {
				t.Fatalf("an avenue tree %.1f m off the road at %.0f m north stands behind the fence at %.1f m", e, n, q[0])
			}
		}
	}
	if trees < 10 {
		t.Fatalf("%d avenue trees beside the meadow, want an avenue", trees)
	}
}
