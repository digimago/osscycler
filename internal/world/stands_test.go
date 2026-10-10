package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/scenery"
)

func TestForestStands(t *testing.T) {
	// Forest on both sides of the road: its density varies by stand, the
	// mature trees' sizes too, and young trees (the open form, small)
	// come up only where the stand is thin.
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: []scenery.LatLon{p(-300, -100), p(300, -100), p(300, 1100), p(-300, 1100), p(-300, -100)}},
	}})
	tr := testTerrain(c, Options{Land: lm})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	ps := plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400))

	thin, dense := 0, 0
	var young, youngDense int
	minS, maxS := math.Inf(1), 0.0
	for _, q := range ps {
		e, n := float64(q.x), -float64(q.z)
		if n < 0 || n > 1000 || math.Abs(e) > 100 || q.kind >= speciesKinds {
			continue
		}
		v := q.kind % (treeVariants + 1)
		st := standField(e, n)
		if v == formOpen {
			young++
			if st >= regenBelow {
				youngDense++
			}
			if !roadClear(tr, e, n, clearRoadM) {
				t.Fatalf("a young tree on the road at %.1f, %.1f", e, n)
			}
			continue
		}
		minS, maxS = math.Min(minS, float64(q.s)), math.Max(maxS, float64(q.s))
		switch {
		case st < 0.25:
			thin++
		case st > 0.75:
			dense++
		}
	}
	// Trees per 400 m² of each kind of stand, by its area (the stand field
	// every 2 m): counting per 20 m cell that has a tree left out the empty
	// cells, which thin stands have more of.
	var thinM2, denseM2 float64
	for e := -99.0; e < 100; e += 2 {
		for n := 1.0; n < 1000; n += 2 {
			switch st := standField(e, n); {
			case st < 0.25:
				thinM2 += 4
			case st > 0.75:
				denseM2 += 4
			}
		}
	}
	per400 := func(trees int, m2 float64) float64 { return 400 * float64(trees) / math.Max(m2, 1) }
	if young < 50 || youngDense > 0 {
		t.Errorf("%d young trees in 20 ha of forest, %d of them in dense stands; want some, only in thin ones", young, youngDense)
	}
	if thinM2 == 0 || denseM2 == 0 || per400(dense, denseM2) < 1.5*per400(thin, thinM2) {
		t.Errorf("mature trees per 400 m²: %.1f in thin stands (%.0f m²), %.1f in dense ones (%.0f m²); want dense ones far denser",
			per400(thin, thinM2), thinM2, per400(dense, denseM2), denseM2)
	}
	if maxS/minS < 1.9 {
		t.Errorf("mature trees from %.2f to %.2f ×; want stands of different ages", minS, maxS)
	}
}

// roadClear tells whether e, n is m clear of every road in tr.
func roadClear(tr *terrain, e, n, m float64) bool {
	return math.Abs(e) >= tr.fine[0].edge+m-0.01 || n < 0 || n > 1000
}
