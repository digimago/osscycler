package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// Stands: a forest is not one even planting (owner, 2026-10-09: at 3.0 km
// on the Posbank Loop every tree was the same size, a colonnade). It is
// stands of their own density and age, tens of metres across; where the
// canopy is thinner, light reaches the floor and young trees come up in
// it (natural regeneration). A smooth seeded field over the place gives
// each spot its stand: how dense (fewer mature trees where it is low),
// how old (their size), and, where thin, the odd young tree.
const (
	standM      = 45.0 // stands about this far across
	standFineM  = 15.0 // and patchy within
	standMinDen = 0.5  // mature trees in the thinnest stand, × the land's rule
	standMaxDen = 1.15 // and in the densest
	// Young trees: where a stand's field is under regenBelow, one per
	// regenM2 of floor in its thinnest parts, fewer as it closes up.
	regenBelow = 0.55
	regenM2    = 70.0
)

// standField is the stand at e, n in [0, 1]: low thin, high dense. Value
// noise on two world-fixed grids (keyed by the cell, so the same every
// build and whatever else is built), smoothed between corners.
func standField(e, n float64) float64 {
	v := 0.7*valueNoise(e/standM, n/standM, 0x57a4d) + 0.3*valueNoise(e/standFineM, n/standFineM, 0x9a7c1)
	// Spread the middle-heavy sum back over [0, 1].
	return smoothstep(math.Max(0, math.Min(1, (v-0.2)/0.6)))
}

// standAge scales a stand's mature trees: some stands older, some younger.
func standAge(e, n float64) float64 {
	return 0.8 + 0.35*valueNoise(e/standM, n/standM, 0xa6e5)
}

// valueNoise is smooth noise in [0, 1] with features about a unit across.
func valueNoise(x, y float64, seed uint64) float64 {
	x0, y0 := math.Floor(x), math.Floor(y)
	fx, fy := smoothstep(x-x0), smoothstep(y-y0)
	at := func(i, j float64) float64 {
		return unit(splitmix(splitmix(seed^uint64(int64(i))<<32) ^ uint64(int64(j))))
	}
	a := at(x0, y0) + fx*(at(x0+1, y0)-at(x0, y0))
	b := at(x0, y0+1) + fx*(at(x0+1, y0+1)-at(x0, y0+1))
	return a + fy*(b-a)
}

// standDensity is how many of the land's mature trees a forest stand at
// e, n keeps, × its rule.
func standDensity(stand float64) float64 {
	return standMinDen + (standMaxDen-standMinDen)*stand
}

// regenChance is the chance that an empty plantCellM cell in a stand
// grows a young tree.
func regenChance(stand float64) float64 {
	if stand >= regenBelow {
		return 0
	}
	return plantCellM * plantCellM / regenM2 * (1 - stand/regenBelow)
}

// youngTree picks a young tree for a forest spot (h its seed, leaf the
// forest's leaf_type): the stand's own species mostly, birch now and then
// (a pioneer, the first in a gap), and its size (× the species' grown
// height), from a sapling to a pole. It grows in the open form: branches
// low, as young trees have.
func youngTree(h uint64, leaf string) (sp int, scale float64) {
	mix := speciesByLand[scenery.LandForest]
	table := mix.broad
	switch {
	case unit(h) < 0.25:
		table = map[string]float64{"birch": 1}
	case leaf == "needleleaved", leaf != "broadleaved" && unit(h>>16) < 0.5:
		table = mix.conifer
	}
	return pickSpecies(table, unit(h>>32)), 0.22 + 0.3*unit(h>>48)
}
