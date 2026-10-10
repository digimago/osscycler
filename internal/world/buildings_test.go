package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestBuildings(t *testing.T) {
	// Flat ground at 10 m, sloping 10 % east beyond 20 m.
	c := northCourse(t)
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 10 + 0.1*math.Max(0, e-20), true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	ys := func(p *gltf.Primitive) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for k := 1; k < len(p.Positions); k += 3 {
			lo, hi = math.Min(lo, float64(p.Positions[k])), math.Max(hi, float64(p.Positions[k]))
		}
		return lo, hi
	}

	// A 12 by 8 m house, 5 m walls and a 4 m roof: a gable, its ridge 9 m
	// over the highest ground under it.
	house := scenery.Footprint{ID: 1, Kind: scenery.KindHouse, WallM: 5, RoofM: 4,
		Outline: [][2]float64{{-30, 400}, {-18, 400}, {-18, 408}, {-30, 408}}}
	var p gltf.Primitive
	buildingMesh(&p, s, house)
	top := math.Inf(-1)
	for _, q := range house.Outline {
		top = math.Max(top, s.at(q[0], q[1]))
	}
	ridge := top + house.WallM + house.RoofM
	if lo, hi := ys(&p); math.Abs(hi-ridge) > 0.01 || lo > top-plinthM+0.01 {
		t.Errorf("house from %.2f to %.2f m, want from below the ground to the ridge at %.2f", lo, hi, ridge)
	}
	// The ridge runs along the long side (east-west): its ends lie at the
	// short walls' middle, north 404.
	for k := 0; k < len(p.Positions); k += 3 {
		if y := float64(p.Positions[k+1]); math.Abs(y-ridge) < 0.01 {
			if n := -float64(p.Positions[k+2]); math.Abs(n-404) > 0.01 {
				t.Errorf("a ridge point at north %.2f, want 404 (along the long side)", n)
			}
		}
	}

	// A flat-roofed block on the slope: the roof level, walls from below
	// the lowest ground.
	block := scenery.Footprint{ID: 2, Kind: scenery.KindFlat, WallM: 12,
		Outline: [][2]float64{{30, 500}, {50, 500}, {50, 520}, {30, 520}}}
	p = gltf.Primitive{}
	buildingMesh(&p, s, block)
	lo, hi := ys(&p)
	glo, ghi := math.Inf(1), math.Inf(-1)
	for _, q := range block.Outline {
		g := s.at(q[0], q[1])
		glo, ghi = math.Min(glo, g), math.Max(ghi, g)
	}
	if ghi-glo < 0.5 {
		t.Fatalf("the fixture's slope is gone: ground %.2f to %.2f", glo, ghi)
	}
	if math.Abs(lo-(glo-plinthM)) > 0.01 || math.Abs(hi-(ghi+12)) > 0.01 {
		t.Errorf("block from %.2f to %.2f m, want %.2f (below its lowest ground) to %.2f (12 m over its highest)", lo, hi, glo-plinthM, ghi+12)
	}
	if len(p.Colors) != len(p.Positions) {
		t.Errorf("%d colours for %d positions", len(p.Colors)/3, len(p.Positions)/3)
	}

	// Colours: the map's tags win; otherwise the same building, the same
	// colours, every build.
	tagged := house
	tagged.WallColour, tagged.RoofColour = "#ffffff", "red"
	wall, roof := colours(tagged, 7)
	if wall != srgbLinear(0xffffff) || roof != srgbLinear(namedColor["red"]) {
		t.Errorf("tagged colours %v %v", wall, roof)
	}
	a1, b1 := colours(house, splitmix(1))
	a2, b2 := colours(house, splitmix(1))
	if a1 != a2 || b1 != b2 {
		t.Error("colours differ between builds")
	}
}
