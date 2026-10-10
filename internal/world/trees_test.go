package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// firstHit is the nearest triangle of p a ray from o along d meets: how
// far, and whether it faces the ray (counter-clockwise towards it, the
// side a renderer culling back faces draws).
func firstHit(p *gltf.Primitive, o, d [3]float64) (float64, bool, bool) {
	at := func(i uint32) [3]float64 {
		return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
	}
	sub := func(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
	cross := func(a, b [3]float64) [3]float64 {
		return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
	}
	dot := func(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
	best, facing, hit := math.Inf(1), false, false
	for i := 0; i+2 < len(p.Indices); i += 3 {
		a, b, c := at(p.Indices[i]), at(p.Indices[i+1]), at(p.Indices[i+2])
		e1, e2 := sub(b, a), sub(c, a)
		h := cross(d, e2)
		det := dot(e1, h)
		if math.Abs(det) < 1e-12 {
			continue
		}
		s := sub(o, a)
		u := dot(s, h) / det
		q := cross(s, e1)
		v := dot(d, q) / det
		t := dot(e2, q) / det
		if u < 0 || v < 0 || u+v > 1 || t <= 1e-6 || t >= best {
			continue
		}
		best, hit = t, true
		facing = dot(cross(e1, e2), d) < 0
	}
	return best, facing, hit
}

func TestFirsAreClosedBelow(t *testing.T) {
	// Seen from under its lowest tier, a fir shows its underside, not the
	// sky through an open cone.
	for i, sp := range allSpecies {
		if !sp.fir {
			continue
		}
		for v := range treeVariants {
			var p gltf.Primitive
			addTreeTemplate(&p, sp, v, false)
			for k := range 12 {
				a := 2 * math.Pi * float64(k) / 12
				r := 0.6 + 0.4*float64(k%3) // between the trunk and the tier's edge
				_, facing, hit := firstHit(&p, [3]float64{r * math.Cos(a), 0.3, r * math.Sin(a)}, [3]float64{0, 1, 0})
				if !hit || !facing {
					t.Errorf("%s form %d: looking up at %.1f m from the trunk: hit %v, facing %v", allSpecies[i].name, v, r, hit, facing)
				}
			}
		}
	}
}

func TestTrunksKeepTheirGirth(t *testing.T) {
	// Leonardo's rule: up to the first branch the trunk carries the whole
	// crown, so halfway up its bare stretch it is nearly as thick as at
	// the foot.
	for _, sp := range allSpecies {
		if sp.fir {
			continue
		}
		for v := range treeVariants {
			var p gltf.Primitive
			addTreeTemplate(&p, sp, v, false)
			bare := sp.bare
			if v == formForest {
				bare = math.Max(sp.bare, math.Min(sp.bare+(sp.h-sp.bare)*0.3, sp.h*0.65))
			}
			y := bare / 2
			dist, _, hit := firstHit(&p, [3]float64{5, y, 0.01}, [3]float64{-1, 0, 0})
			if !hit {
				t.Fatalf("%s form %d: no trunk at %.1f m", sp.name, v, y)
			}
			if r := 5 - dist; r < 0.85*sp.trunkR {
				t.Errorf("%s form %d: trunk %.2f m thick at %.1f m, %.2f at the foot", sp.name, v, 2*r, y, 2*sp.trunkR)
			}
			if bare > 0.66*sp.h {
				t.Errorf("%s form %d: bare to %.1f of %.0f m", sp.name, v, bare, sp.h)
			}
		}
	}
}

// A tree's crown reach covers what its template draws: young trees are
// kept that far off roads, and young firs, whose crown had counted as
// nothing, spread their lowest tier over a forest path at head height
// (Amsterdam Water Supply Dunes, 1.56 and 1.66 km).
func TestCrownReachCoversTheTree(t *testing.T) {
	for _, sp := range allSpecies {
		for v := range treeVariants {
			var p gltf.Primitive
			addTreeTemplate(&p, sp, v, false)
			out := 0.0
			for i := 0; i+2 < len(p.Positions); i += 3 {
				out = math.Max(out, math.Hypot(float64(p.Positions[i]), float64(p.Positions[i+2])))
			}
			if out > sp.crownReach()+0.05 {
				t.Errorf("%s form %d reaches %.2f m, crownReach %.2f m", sp.name, v, out, sp.crownReach())
			}
		}
	}
}

// Trees as riders know them (owner, 2026-10-10): firs narrow (about a third
// of their height across in the open, under a quarter in a stand), grown
// broadleaves thick in the trunk at breast height (1.3 m).
func TestTreesHaveRealMeasures(t *testing.T) {
	dbh := map[string]float64{"oak": 0.95, "beech": 0.75, "ash": 0.65, "birch": 0.32}
	for _, sp := range allSpecies {
		for v := range treeVariants {
			var p gltf.Primitive
			addTreeTemplate(&p, sp, v, false)
			if sp.fir {
				out := 0.0
				for i := 0; i+2 < len(p.Positions); i += 3 {
					out = math.Max(out, math.Hypot(float64(p.Positions[i]), float64(p.Positions[i+2])))
				}
				want := map[int]float64{formOpen: 0.36, formForest: 0.26}[v]
				if 2*out > want*sp.h {
					t.Errorf("fir form %d: %.1f m across at %.0f m tall, want at most %.2f of its height", v, 2*out, sp.h, want)
				}
				continue
			}
			min, ok := dbh[sp.name]
			if !ok {
				continue
			}
			dist, _, hit := firstHit(&p, [3]float64{5, 1.3, 0.01}, [3]float64{-1, 0, 0})
			if !hit || 2*(5-dist) < min {
				t.Errorf("%s form %d: %.2f m across at breast height, want at least %.2f", sp.name, v, 2*(5-dist), min)
			}
		}
	}
}

// The far (simple) pollard's crown rests on its head (owner, 2026-10-10:
// it floated above it): its lowest leaves are no higher than the head's
// top.
func TestFarPollardCrownRestsOnItsHead(t *testing.T) {
	sp := allSpecies[speciesIndex("willow")]
	var p gltf.Primitive
	addTreeTemplate(&p, sp, formOpen, true)
	low := math.Inf(1)
	for v := 0; v+2 < len(p.Positions); v += 3 {
		if p.Colors[v+1] > p.Colors[v] { // green: the leaves (the bark is brown)
			low = math.Min(low, float64(p.Positions[v+1]))
		}
	}
	if top := sp.bare + sp.trunkR*0.9; low > top {
		t.Errorf("the crown starts %.2f m above the head's top", low-top)
	}
}

// The leaning willow leans towards +x by willowLeanDeg, its foot in place.
func TestLeaningWillow(t *testing.T) {
	sp := allSpecies[speciesIndex("willow")]
	var up, lean gltf.Primitive
	addTreeTemplate(&up, sp, formOpen, false)
	addTreeTemplate(&lean, sp, formForest, false)
	mid := func(p *gltf.Primitive, y0, y1 float64) float64 {
		s, n := 0.0, 0
		for v := 0; v+2 < len(p.Positions); v += 3 {
			if y := float64(p.Positions[v+1]); y >= y0 && y < y1 {
				s += float64(p.Positions[v])
				n++
			}
		}
		return s / float64(max(n, 1))
	}
	if d := mid(&lean, 0, 0.3) - mid(&up, 0, 0.3); math.Abs(d) > 0.05 {
		t.Errorf("the foot moved %.2f m", d)
	}
	want := sp.bare * math.Tan(willowLeanDeg*math.Pi/180)
	if d := mid(&lean, sp.bare-0.2, sp.bare+0.2) - mid(&up, sp.bare-0.2, sp.bare+0.2); math.Abs(d-want) > 0.05 {
		t.Errorf("the head leans %.2f m, want %.2f", d, want)
	}
}
