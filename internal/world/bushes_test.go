package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestBushSpecies(t *testing.T) {
	// Each land grows its own bushes, in the species' own sizes.
	seen := map[scenery.Land]map[string]int{}
	for _, l := range []scenery.Land{scenery.LandForest, scenery.LandHeath, scenery.LandBuilt, scenery.LandMeadow} {
		seen[l] = map[string]int{}
		for i := range 4000 {
			k, s, ok := pickBush(l, splitmix(uint64(i)))
			if !ok {
				t.Fatalf("no bush on land %v", l)
			}
			b := allBushes[k-kBush]
			if bushesByLand[l][b.name] == 0 || s < b.minS || s > b.maxS {
				t.Fatalf("a %s of %.2f × on land %v", b.name, s, l)
			}
			seen[l][b.name]++
		}
	}
	if seen[scenery.LandForest]["holly"] == 0 || seen[scenery.LandHeath]["juniper"] == 0 || seen[scenery.LandBuilt]["box"] == 0 || seen[scenery.LandHeath]["holly"] > 0 {
		t.Errorf("bushes by land: %v", seen)
	}
	// The templates differ in size and shape: holly tall and narrow, box
	// a small ball, hazel tall and wide.
	type dims struct{ h, w float64 }
	size := map[string]dims{}
	for _, b := range allBushes {
		var p gltf.Primitive
		bushTemplate(&p, b)
		var d dims
		for i := 0; i < len(p.Positions); i += 3 {
			d.h = math.Max(d.h, float64(p.Positions[i+1]))
			d.w = math.Max(d.w, 2*math.Hypot(float64(p.Positions[i]), float64(p.Positions[i+2])))
		}
		if tris := len(p.Indices) / 3; tris > 600 {
			t.Errorf("%s: %d triangles; bushes are many, keep them low-poly", b.name, tris)
		}
		size[b.name] = d
	}
	if h, b, z := size["holly"], size["box"], size["hazel"]; h.h < 2*h.w*0.8 || b.h > 1 || z.h < 3.5 || z.w < 3 {
		t.Errorf("sizes (height, width): %v", size)
	}
}
