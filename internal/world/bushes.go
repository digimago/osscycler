package world

import (
	"math"
	"sync"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Bushes by species (owner, 2026-10-09: as with trees, bushes differ in
// look and size: holly is nothing like prunus or ribes). Each species has
// one template in its own shape and colours, low-poly as there are many;
// sizes vary per bush (scale) within the species' own range. Shapes and
// heights from what they are in the Netherlands, grown free (clipped
// hedges are hedges.go's):
//   - holly (Ilex aquifolium): evergreen, dense, dark and glossy, an
//     upright cone; under oak and beech on the Veluwe, 2-5 m.
//   - bird cherry (Prunus serotina, Amerikaanse vogelkers): the forests'
//     commonest shrub, a loose vase of several stems, fresh green, 2-4 m.
//   - cherry laurel (Prunus laurocerasus): gardens' evergreen, a broad
//     dense glossy mound, 1.5-3 m.
//   - currant (Ribes): a small open bush of arching stems, 1-1.5 m.
//   - hazel (Corylus avellana): a fountain of many stems, 3-6 m, at
//     forest edges and in hedgerows.
//   - hawthorn (Crataegus): a dense round small tree of a shrub, 2-4 m,
//     on meadow edges.
//   - broom (Cytisus scoparius): heath, an upright bundle of green
//     twigs, 1-2 m.
//   - juniper (Juniperus communis): heath, grey-green columns, 1-4 m.
//   - rhododendron: estates and gardens, a broad dark dome, 1.5-3 m.
//   - box (Buxus): gardens, small clipped balls, 0.4-0.9 m.
//
// and the dunes' (dunes.go): sea buckthorn, bramble, wild privet,
// creeping willow, elder.
type bushSpecies struct {
	name       string
	drawM      float64
	minS, maxS float64 // size range × the template
	leaf, wood uint32
	build      func(p *gltf.Primitive, leaf, wood [3]float32)
}

var allBushes = []bushSpecies{
	{name: "holly", drawM: 300, minS: 0.6, maxS: 1.4, leaf: 0x1f3a1e, wood: 0x6e6a58, build: hollyTemplate},
	{name: "bird cherry", drawM: 300, minS: 0.6, maxS: 1.3, leaf: 0x5a8434, wood: 0x5a4636, build: birdCherryTemplate},
	{name: "laurel", drawM: 250, minS: 0.7, maxS: 1.3, leaf: 0x2c5626, wood: 0x5a4a3a, build: laurelTemplate},
	{name: "currant", drawM: 150, minS: 0.7, maxS: 1.2, leaf: 0x6c9c3c, wood: 0x6a5240, build: currantTemplate},
	{name: "hazel", drawM: 300, minS: 0.6, maxS: 1.3, leaf: 0x587e30, wood: 0x6a5444, build: hazelTemplate},
	{name: "hawthorn", drawM: 300, minS: 0.6, maxS: 1.3, leaf: 0x46682c, wood: 0x4e4236, build: hawthornTemplate},
	{name: "broom", drawM: 150, minS: 0.6, maxS: 1.2, leaf: 0x3e5e24, wood: 0x3e5e24, build: broomTemplate},
	{name: "juniper", drawM: 250, minS: 0.4, maxS: 1.4, leaf: 0x4c614e, wood: 0x5a4a3a, build: juniperTemplate},
	{name: "rhododendron", drawM: 250, minS: 0.7, maxS: 1.3, leaf: 0x2a4628, wood: 0x5a4a3a, build: rhododendronTemplate},
	{name: "box", drawM: 120, minS: 0.6, maxS: 1.3, leaf: 0x3a5c28, wood: 0x3a5c28, build: boxTemplate},
	{name: "sea buckthorn", drawM: 300, minS: 0.5, maxS: 1.3, leaf: 0x6f7f5e, wood: 0x4e4234, build: seaBuckthornTemplate},
	{name: "bramble", drawM: 150, minS: 0.6, maxS: 1.2, leaf: 0x34522a, wood: 0x6a3c38, build: brambleTemplate},
	{name: "privet", drawM: 250, minS: 0.7, maxS: 1.2, leaf: 0x2e4c24, wood: 0x5a4a3a, build: privetTemplate},
	{name: "creeping willow", drawM: 150, minS: 0.6, maxS: 1.4, leaf: 0x74845c, wood: 0x6a5a48, build: creepingWillowTemplate},
	{name: "elder", drawM: 300, minS: 0.7, maxS: 1.2, leaf: 0x4e7a34, wood: 0x7a7060, build: elderTemplate},
}

// bushesByLand: the species of each land use (weights).
var bushesByLand = map[scenery.Land]map[string]float64{
	scenery.LandForest: {"bird cherry": 35, "holly": 25, "hazel": 18, "currant": 8, "hawthorn": 8, "rhododendron": 6},
	scenery.LandHeath:  {"broom": 45, "juniper": 40, "bird cherry": 10, "hawthorn": 5},
	scenery.LandBuilt:  {"laurel": 24, "box": 20, "rhododendron": 14, "currant": 12, "holly": 10, "hazel": 10, "hawthorn": 10},
	scenery.LandMeadow: {"hawthorn": 55, "hazel": 30, "bird cherry": 15},
	// Dunes: thickets of sea buckthorn with brambles and privet, creeping
	// willow on the grey dune, the odd elder and hawthorn.
	scenery.LandDuneScrub: {"sea buckthorn": 42, "bramble": 20, "privet": 14, "creeping willow": 10, "hawthorn": 10, "elder": 4},
	scenery.LandDuneGrass: {"creeping willow": 37, "bramble": 26, "sea buckthorn": 25, "hawthorn": 10, "elder": 2},
	scenery.LandDuneSand:  {"sea buckthorn": 60, "creeping willow": 25, "bramble": 15},
	scenery.LandNone:      {"hawthorn": 55, "hazel": 30, "bird cherry": 15},
}

// pickBush picks a bush for land l with seed h: its kind and size (× the
// template); ok false where the land grows none.
// bushReach is how far bush species i's template reaches out from its
// middle at size 1, measured from the template once.
func bushReach(i int) float64 {
	bushReachOnce.Do(func() {
		bushReaches = make([]float64, len(allBushes))
		for k, b := range allBushes {
			var p gltf.Primitive
			b.build(&p, srgbLinear(b.leaf), srgbLinear(b.wood))
			for v := 0; v+2 < len(p.Positions); v += 3 {
				bushReaches[k] = math.Max(bushReaches[k], math.Hypot(float64(p.Positions[v]), float64(p.Positions[v+2])))
			}
		}
	})
	return bushReaches[i]
}

var (
	bushReachOnce sync.Once
	bushReaches   []float64
)

func pickBush(l scenery.Land, h uint64) (kind int, scale float64, ok bool) {
	mix := bushesByLand[l]
	if mix == nil {
		mix = bushesByLand[scenery.LandNone]
	}
	total := 0.0
	for _, b := range allBushes { // in a fixed order
		total += mix[b.name]
	}
	r := unit(h) * total
	for i, b := range allBushes {
		if w := mix[b.name]; r < w {
			// The template's own spread (add's 0.75-1.25) on top of the
			// species' range.
			return kBush + i, b.minS + (b.maxS-b.minS)*unit(h>>16), true
		}
		r -= mix[b.name]
	}
	return 0, 0, false
}

// bushTemplate adds bush species b's template.
func bushTemplate(p *gltf.Primitive, b bushSpecies) {
	b.build(p, srgbLinear(b.leaf), srgbLinear(b.wood))
}

// clump is a low-poly leaf clump for bushes: fewer faces than a tree's.
func clump(p *gltf.Primitive, x, y, z, r, hh float64, c [3]float32, lift float64) {
	blobAt(p, x, y, z, r, hh, c, lift, 3, 6)
}

// around is the point at angle a (radians) and distance d round the stem.
func around(a, d float64) (float64, float64) { return d * math.Cos(a), d * math.Sin(a) }

func hollyTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 3 m: a short stem, then a cone dense to the ground, of tiers
	// overlapping so far they read as one; dark, a touch lighter
	// towards the top.
	cone(p, 0, 0, 0.08, 0.04, 0.5, 5, wood)
	tiers := [][2]float64{{0.6, 0.95}, {1.05, 0.86}, {1.5, 0.74}, {1.95, 0.6}, {2.4, 0.44}, {2.8, 0.26}}
	for i, t := range tiers {
		clump(p, 0.05*float64(i%2), t[0], -0.04*float64(i%2), t[1], 0.5, shade(leaf, 0.9+0.04*float64(i)), 0.3)
	}
}

func birdCherryTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 3 m: four stems leaning out from one foot, each with a loose flat
	// clump; the middle open, light through it.
	for i := range 4 {
		a := 2*math.Pi*float64(i)/4 + 0.4
		x, z := around(a, 0.75+0.15*float64(i%2))
		y := 2.1 + 0.3*float64(i%3)
		limb(p, 0, 0, 0, x, y, z, 0.06, 0.03, 4, wood)
		clump(p, x, y+0.2, z, 0.8, 0.55, shade(leaf, 0.9+0.08*float64(i%3)), 0.4)
	}
	clump(p, 0, 2.7, 0, 0.6, 0.45, shade(leaf, 1.05), 0.4)
}

func laurelTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 2 m: a broad dense mound of big glossy leaves, to the ground.
	clump(p, 0, 0.9, 0, 1.5, 0.95, leaf, 0.6)
	for i := range 3 {
		x, z := around(2*math.Pi*float64(i)/3, 0.7)
		clump(p, x, 0.8+0.25*float64(i%2), z, 0.9, 0.7, shade(leaf, 0.92+0.08*float64(i)), 0.6)
	}
}

func currantTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 1.2 m: a few arching stems from the ground, small clumps at their
	// ends; open, the stems show.
	for i := range 5 {
		a := 2*math.Pi*float64(i)/5 + 0.3
		x, z := around(a, 0.45)
		y := 0.8 + 0.25*float64(i%3)
		limb(p, 0, 0, 0, x, y, z, 0.025, 0.012, 3, wood)
		clump(p, x*1.05, y, z*1.05, 0.32, 0.25, shade(leaf, 0.92+0.06*float64(i%3)), 0.3)
	}
}

func hazelTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 4 m: a fountain of many straight stems, wide at the top, clumps along
	// their upper half, bare grey-brown below.
	for i := range 6 {
		a := 2*math.Pi*float64(i)/6 + 0.2
		x, z := around(a, 1.1+0.3*float64(i%2))
		y := 3.2 + 0.5*float64(i%3)
		limb(p, 0, 0, 0, x, y, z, 0.05, 0.025, 4, wood)
		clump(p, x, y, z, 0.75, 0.6, shade(leaf, 0.9+0.07*float64(i%3)), 0.3)
		if i%2 == 0 {
			clump(p, x*0.6, y*0.65, z*0.6, 0.6, 0.45, shade(leaf, 0.88), 0.3)
		}
	}
}

func hawthornTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 3 m: a short gnarled stem and a dense round crown low over it.
	limb(p, 0, 0, 0, 0.15, 1.0, 0.05, 0.12, 0.08, 5, wood)
	clump(p, 0.15, 1.9, 0.05, 1.3, 1.0, leaf, 0.5)
	for i := range 3 {
		x, z := around(2*math.Pi*float64(i)/3+0.5, 0.7)
		clump(p, x+0.15, 1.5+0.3*float64(i%2), z, 0.75, 0.6, shade(leaf, 0.9+0.07*float64(i)), 0.4)
	}
}

func broomTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 1.5 m: a bundle of thin green twigs, upright and fanning a little;
	// see-through, no leafy mass.
	for i := range 9 {
		a := 2*math.Pi*float64(i)/9 + 0.15*float64(i%3)
		x, z := around(a, 0.25+0.1*float64(i%3))
		limb(p, 0, 0, 0, x, 1.2+0.3*float64(i%4)/3, z, 0.04, 0.01, 3, shade(leaf, 0.9+0.05*float64(i%3)))
	}
	clump(p, 0, 0.55, 0, 0.35, 0.45, shade(leaf, 0.85), 0)
}

func juniperTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 2.5 m: two or three grey-green columns of different heights, close
	// together, as junipers on the Veluwe's heaths stand.
	for _, c := range [][3]float64{{0, 0, 2.5}, {0.35, 0.15, 1.8}, {-0.2, 0.3, 1.3}} {
		r := 0.18 + 0.1*c[2]
		blobAt(p, c[0], c[2]*0.5, c[1], r, c[2]*0.52, shade(leaf, 0.85+0.07*c[2]), 0, 3, 6)
	}
}

func rhododendronTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 2 m: a broad dark dome of separate leafy masses, wider than tall.
	for i := range 5 {
		x, z := around(2*math.Pi*float64(i)/5, 0.85)
		clump(p, x, 0.95+0.2*float64(i%2), z, 0.85, 0.65, shade(leaf, 0.9+0.06*float64(i%3)), 0.5)
	}
	clump(p, 0, 1.4, 0, 0.9, 0.6, shade(leaf, 1.05), 0.5)
}

func boxTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 0.7 m: a clipped ball, small leaves, close and smooth.
	blobAt(p, 0, 0.36, 0, 0.4, 0.36, leaf, 0, 4, 8)
}
