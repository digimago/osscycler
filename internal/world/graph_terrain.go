package world

import (
	"math"
	"sync"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// The graph builder's land (graph.go): laid last, round what is already
// there. The roads, junctions, roundabouts and car parks are built first;
// the land is a mesh of its own that stops at their outline, so nothing is
// cut out of it and nothing of it lies over a road.
//
// Each chunk is a quadtree of square cells from gRootCells terrain cells
// down to an eighth of that: a cell wholly clear of the roads is a leaf
// (two triangles, or a fan where smaller neighbours put vertices on its
// sides), one wholly on them is left out, and one the outline crosses
// splits until the smallest, where the outline is followed exactly: the
// land's edge runs through points found on the cell's sides by bisection
// (marching squares). Where a cell lies is all that decides its tree, so
// chunks meet without cracks. The outline is the roads' shrunk by
// gTuckM, so the land's edge lies just under the road's edge, a little
// below it, and the road's skirt or kerb covers the seam.
//
// Heights: the ground model (or the course's profile without one), eased
// to the road's height beside it over gBlend (sunken lanes keep their
// banks beyond that, embankments their slopes).

const (
	gRootCells = 2    // a root cell is this many terrain cells across (10 m)
	gDepth     = 4    // levels below the root: its smallest cells an eighth across (1.25 m)
	gTuckM     = 0.4  // the land's edge lies this far in under the road's
	gSinkM     = 0.03 // and this far below it
	gNearM     = 60.0 // within this of the route, cells are at most a terrain cell
	gBlendM    = 8.0  // the ground eases to the road over this far from its edge (with a ground model)
	// A shore (water, no road) is followed in cells this many smallest
	// cells across (1.25 m), a road's edge in the smallest: the land along
	// every ditch of a polder at 0.625 m doubled the figure 8's and the
	// polder ride's terrain (0.24 → 0.57 M, 1.09 → 1.71 M triangles);
	// water meets the land softly. A cell whose inside disagrees with its
	// corners (a ditch narrower than the cell) still splits.
	gWaterS = 2
	// Far from the route, a block of gBlockRoots × gBlockRoots root cells
	// (50 m) with no road, water or outline in it and a ground within
	// gFarTolM of the plane through its corners is one leaf (owner,
	// 2026-10-12: fewer triangles far off, towards hills on the horizon).
	gFarM       = 120.0
	gFarTolM    = 0.75
	gBlockRoots = 5
	// Under a deck the land lies at least this far below its road: the
	// ground model has the fill under a viaduct, up to it and over (the
	// N348's carriageways over the cycle tunnel at 26.8 km on the Posbank
	// Loop: 0.2 m over, 10 m²); the slab is 0.9 m deep.
	gUnderDeckM = 1.0
)

// gland is the land being laid.
type gland struct {
	t     *terrain
	roads *landSurface // what the land goes round (decks excluded: the ground goes on under bridges)
	decks *landSurface // the decks, which the land keeps under (nil: none)
	water int          // the water material: a shore refines to gWaterS, a road's edge to the smallest cell
	// waters indexes the water sheets alone, so the land tells quickly
	// whether a ditch is near (ditchBank); nil: none.
	waters *landSurface
	dry    *landSurface // the roads, junctions and car parks alone
	near   *routeGrid   // the route, for the cells' size
	samps  []sample     // the roads' samples, for the heights
	grid   map[[2]int][]int
	gridM  float64
	blendM float64
	rootM  float64
	fineM  float64
	trees  sync.Map // [2]int root → *gtree
	blocks sync.Map // [2]int block → bool: one leaf (far and flat)
	far    *routeGrid
	nearC  sync.Map // [2]int chunk → []sample: the route's coarse samples that shape it
	// walls is the route within roadsideNearM, where a style with
	// roadside walls stands a wall between two roads a step apart in
	// height (nil: no such walls, roadside.go).
	walls *routeGrid
	// levelM: the style's junctionLevelM; over 0, two roads further apart
	// in height than this beside each other are levels of their own, and
	// the land between them a bank where no wall closes the step.
	levelM float64
}

// gtree is a root cell's leaves: for each of its smallest cells, the
// leaf over it (size in smallest cells, origin, and what it is).
type gtree struct {
	size [gSide * gSide]int8
	org  [gSide * gSide][2]int8
	kind [gSide * gSide]int8
}

// gSide is a root cell's side in smallest cells.
const gSide = 1 << gDepth

const (
	gClear = iota
	gCovered
	gMixed
)

// What covers a lattice point (coverKind): gClear, or by water, or by a road.
const (
	gByWater = iota + 1
	gByRoad
)

func newGland(t *terrain, roads *landSurface, lines []*roadLine) *gland {
	g := &gland{t: t, roads: roads, rootM: gRootCells * t.o.CellM}
	g.fineM = g.rootM / (1 << gDepth)
	g.near = newRouteGrid(t.coarse, gNearM+g.rootM)
	g.far = newRouteGrid(t.coarse, gFarM+gBlockRoots*g.rootM)
	if styleFor(t.o.Region).roadside {
		g.walls = newRouteGrid(t.coarse, roadsideNearM)
	}
	g.levelM = styleFor(t.o.Region).junctionLevelM
	g.blendM = gBlendM
	if t.o.Elevation == nil {
		g.blendM = t.o.BlendM
	}
	g.gridM = g.blendM + 10
	g.grid = map[[2]int][]int{}
	for _, l := range lines {
		for i := 0; i+1 < len(l.samples); i++ {
			a := l.samples[i]
			if a.aloft && l.samples[i+1].aloft {
				continue // a bridge: the ground goes on under it
			}
			k := len(g.samps)
			g.samps = append(g.samps, a, l.samples[i+1])
			key := [2]int{int(math.Floor(a.e / g.gridM)), int(math.Floor(a.n / g.gridM))}
			g.grid[key] = append(g.grid[key], k)
		}
	}
	return g
}

// covered says whether the road (shrunk by gTuckM) is at e, n: coverKind
// other than clear, so the tree, the ring vertices and the crossings all
// agree (water near a road counts as clear, below).
func (g *gland) covered(e, n float64) bool {
	return g.coverKind(e, n) != gClear
}

// coverKind is what covers e, n: gClear nothing, gByWater water only, gByRoad
// a road (or car park) at any of the tuck's points.
func (g *gland) coverKind(e, n float64) int8 {
	kind := int8(gByWater)
	water := false
	var hs [5]float64
	for i, d := range [5][2]float64{{0, 0}, {gTuckM, 0}, {-gTuckM, 0}, {0, gTuckM}, {0, -gTuckM}} {
		m, ok := g.roads.matAt(e+d[0], n+d[1])
		if !ok {
			return gClear
		}
		if m != g.water {
			kind = gByRoad
		} else {
			water = true
		}
		hs[i], _ = g.roads.low(e+d[0], n+d[1])
	}
	// Between two roads a level apart (the style's level), the land runs
	// on into the gap between them, however narrow: the bank between the
	// two (bankBetween) rather than nothing, which from the side showed
	// the sky under the higher road's edge (the Route des Crêtes coming
	// down beside the Route des Américains, 10.92 km).
	if g.levelM > 0 && kind == gByRoad {
		if _, on := g.roads.matAt(e, n); !on {
			if math.Abs(hs[1]-hs[2]) > g.levelM || math.Abs(hs[3]-hs[4]) > g.levelM {
				return gClear
			}
		}
	}
	// Probes on a road and on water both: the seam between the two
	// surfaces is within the tuck, and they may leave a sliver between
	// them narrower than a cell; clear, so the land runs under both and
	// bridges it (a canal along the Benedenkerkseweg at 73.4-75.4 km of
	// the Grebbeberg course: a line of holes along the road's edge, 15 m²).
	if kind == gByRoad && water {
		return gClear
	}
	// Under water but within a cell of a road: clear, so the land runs on
	// under the water's edge to the road's and tucks under both; with every
	// lattice point covered by one or the other, the seam between the two
	// surfaces had been a line of holes (a canal along the Benedenkerkseweg,
	// 73.4-75.4 km of the Grebbeberg course, 15 m²).
	if kind == gByWater {
		for _, r := range [3]float64{0.5, 1, g.fineM + gTuckM} {
			for k := range 8 {
				a := float64(k) * math.Pi / 4
				if m, ok := g.roads.matAt(e+r*math.Cos(a), n+r*math.Sin(a)); ok && m != g.water {
					return gClear
				}
			}
		}
	}
	return kind
}

// lattice is the point i, j of the smallest cells' lattice (global).
func (g *gland) lattice(i, j int) (float64, float64) {
	return float64(i) * g.fineM, float64(j) * g.fineM
}

// roadsNear says whether any road triangle may reach the root cell at
// I, J (in root cells).
func (g *gland) roadsNear(I, J int) bool {
	e0, n0 := float64(I)*g.rootM, float64(J)*g.rootM
	m := 2 * gTuckM
	for gx := int(math.Floor((e0 - m) / landCellM)); gx <= int(math.Floor((e0+g.rootM+m)/landCellM)); gx++ {
		for gz := int(math.Floor((-(n0 + g.rootM) - m) / landCellM)); gz <= int(math.Floor((-n0+m)/landCellM)); gz++ {
			if len(g.roads.cells[[2]int{gx, gz}]) > 0 {
				return true
			}
		}
	}
	return false
}

// tree is root cell I, J's leaves, made on first use.
func (g *gland) tree(I, J int) *gtree {
	key := [2]int{I, J}
	if v, ok := g.trees.Load(key); ok {
		return v.(*gtree)
	}
	tr := &gtree{}
	const side = gSide
	setLeaf := func(i0, j0, s int, kind int8) {
		for j := j0; j < j0+s; j++ {
			for i := i0; i < i0+s; i++ {
				tr.size[j*side+i] = int8(s)
				tr.org[j*side+i] = [2]int8{int8(i0), int8(j0)}
				tr.kind[j*side+i] = kind
			}
		}
	}
	// Near the route the land is at least as fine as the terrain grid.
	ce, cn := (float64(I)+0.5)*g.rootM, (float64(J)+0.5)*g.rootM
	nearRoute := false
	g.near.within(ce, cn, gNearM+g.rootM/2, func(int) { nearRoute = true })
	maxS := side
	if nearRoute {
		maxS = side / gRootCells
	}
	if !g.roadsNear(I, J) {
		for j := 0; j < side; j += maxS {
			for i := 0; i < side; i += maxS {
				setLeaf(i, j, maxS, gClear)
			}
		}
		v, _ := g.trees.LoadOrStore(key, tr)
		return v.(*gtree)
	}
	var cov [side + 1][side + 1]int8
	for j := 0; j <= side; j++ {
		for i := 0; i <= side; i++ {
			e, n := g.lattice(I*side+i, J*side+j)
			cov[j][i] = g.coverKind(e, n)
		}
	}
	// shoreLeaf says whether a cell the water's edge crosses can be one
	// leaf: its sides' middles and its middle agree with its corners (a
	// ditch narrower than the cell would hide inside it).
	shoreLeaf := func(i0, j0, s int) bool {
		h := s / 2
		for _, p := range [5][4]int{{i0 + h, j0, i0, i0 + s}, {i0 + h, j0 + s, i0, i0 + s}, {i0, j0 + h, j0, j0 + s}, {i0 + s, j0 + h, j0, j0 + s}, {i0 + h, j0 + h, -1, -1}} {
			c := cov[p[1]][p[0]] != gClear
			var agree bool
			switch {
			case p[2] < 0:
				for _, q := range [4][2]int{{i0, j0}, {i0 + s, j0}, {i0, j0 + s}, {i0 + s, j0 + s}} {
					agree = agree || (cov[q[1]][q[0]] != gClear) == c
				}
			case p[1] == j0 || p[1] == j0+s:
				agree = (cov[p[1]][p[2]] != gClear) == c || (cov[p[1]][p[3]] != gClear) == c
			default:
				agree = (cov[p[2]][p[0]] != gClear) == c || (cov[p[3]][p[0]] != gClear) == c
			}
			if !agree {
				return false
			}
		}
		return true
	}
	var split func(i0, j0, s int)
	split = func(i0, j0, s int) {
		all, none, road := true, true, false
		for j := j0; j <= j0+s; j++ {
			for i := i0; i <= i0+s; i++ {
				switch cov[j][i] {
				case gClear:
					all = false
				case gByRoad:
					none, road = false, true
				default:
					none = false
				}
			}
		}
		switch {
		case all && s <= maxS:
			setLeaf(i0, j0, s, gCovered)
		case none && s <= maxS:
			setLeaf(i0, j0, s, gClear)
		case s == 1, !road && s <= gWaterS && shoreLeaf(i0, j0, s):
			setLeaf(i0, j0, s, gMixed)
		default:
			h := s / 2
			split(i0, j0, h)
			split(i0+h, j0, h)
			split(i0, j0+h, h)
			split(i0+h, j0+h, h)
		}
	}
	split(0, 0, side)
	v, _ := g.trees.LoadOrStore(key, tr)
	return v.(*gtree)
}

// merged says whether block BI, BJ (in blocks of gBlockRoots root cells)
// is one leaf: far from the route, no road or water reaching it, and its
// ground within gFarTolM of the plane through its corners.
func (g *gland) merged(BI, BJ int) bool {
	key := [2]int{BI, BJ}
	if v, ok := g.blocks.Load(key); ok {
		return v.(bool)
	}
	m := g.mergeable(BI, BJ)
	v, _ := g.blocks.LoadOrStore(key, m)
	return v.(bool)
}

func (g *gland) mergeable(BI, BJ int) bool {
	blockM := gBlockRoots * g.rootM
	e0, n0 := float64(BI)*blockM, float64(BJ)*blockM
	nearRoute := false
	g.far.within(e0+blockM/2, n0+blockM/2, gFarM+blockM/2, func(int) { nearRoute = true })
	if nearRoute {
		return false
	}
	for J := 0; J < gBlockRoots; J++ {
		for I := 0; I < gBlockRoots; I++ {
			if g.roadsNear(BI*gBlockRoots+I, BJ*gBlockRoots+J) {
				return false
			}
		}
	}
	// The ground at every root corner against the plane through the
	// block's corners (bilinear), and no water anywhere in it.
	var c [2][2]float64
	for j := range 2 {
		for i := range 2 {
			c[j][i] = g.heightAt(e0+float64(i)*blockM, n0+float64(j)*blockM)
		}
	}
	for J := 0; J <= gBlockRoots; J++ {
		for I := 0; I <= gBlockRoots; I++ {
			e, n := e0+float64(I)*g.rootM, n0+float64(J)*g.rootM
			if land, _ := g.t.cover.at(e, n); land == scenery.LandWater {
				return false
			}
			fi, fj := float64(I)/gBlockRoots, float64(J)/gBlockRoots
			plane := (c[0][0]*(1-fi)+c[0][1]*fi)*(1-fj) + (c[1][0]*(1-fi)+c[1][1]*fi)*fj
			if math.Abs(g.heightAt(e, n)-plane) > gFarTolM {
				return false
			}
		}
	}
	return true
}

// inMerged says whether the smallest cell i, j lies in a merged block.
func (g *gland) inMerged(i, j int) bool {
	bs := gBlockRoots * gSide
	return g.merged(floorDiv(i, bs), floorDiv(j, bs))
}

// leafAt is the leaf over the smallest cell i, j (global): its origin,
// size and kind.
func (g *gland) leafAt(i, j int) (oi, oj, s int, kind int8) {
	const side = gSide
	I, J := floorDiv(i, side), floorDiv(j, side)
	tr := g.tree(I, J)
	k := mod(j, side)*side + mod(i, side)
	return I*side + int(tr.org[k][0]), J*side + int(tr.org[k][1]), int(tr.size[k]), tr.kind[k]
}

// vertexAt says whether lattice point i, j is a corner of any leaf
// around it: on a leaf's side, it is a vertex the leaf must take in.
func (g *gland) vertexAt(i, j int) bool {
	bs := gBlockRoots * gSide
	for _, c := range [4][2]int{{i, j}, {i - 1, j}, {i, j - 1}, {i - 1, j - 1}} {
		if g.inMerged(c[0], c[1]) {
			// A merged block's only vertices are its corners.
			if mod(i, bs) == 0 && mod(j, bs) == 0 {
				return true
			}
			continue
		}
		oi, oj, s, _ := g.leafAt(c[0], c[1])
		if (i == oi || i == oi+s) && (j == oj || j == oj+s) {
			return true
		}
	}
	return false
}

// heightAt is the land's height at e, n: the ground model (or the profile)
// eased to the roads near it, just under them; under water, its bed.
func (g *gland) heightAt(e, n float64) float64 {
	return g.height(e, n, true)
}

// relief is the meso relief the ground model is too coarse for (owner,
// 2026-10-15: a detail terrain over the coarse one, in mountains and
// hills first): hummocks and lumps of 0.3-1.5 m, a seeded field at 40 m
// and 14 m, as strong as the style says (reliefM) where the ground slopes
// (none on the flat: a valley floor or a meadow stays calm, full from a
// 20 % slope), none within the road's blend and a few metres beyond, nor
// by water; so roads, junctions and shores stay as they are.
func (g *gland) relief(e, n, gap float64, near []sample) float64 {
	t := g.t
	amp := styleFor(t.o.Region).reliefM
	if amp <= 0 || t.o.Elevation == nil || gap < gBlendM {
		return 0
	}
	fade := smooth01((gap - gBlendM) / 6)
	// Smooth in town: gardens and lawns (owner's shot of the Rue du
	// Hohneck at 0.89 km: a mown bank where the hummocks had striped it).
	for _, d := range [5][2]float64{{0, 0}, {15, 0}, {-15, 0}, {0, 15}, {0, -15}} {
		if l, _ := t.cover.at(e+d[0], n+d[1]); l == scenery.LandBuilt {
			return 0
		}
	}
	for _, d := range [4][2]float64{{8, 0}, {-8, 0}, {0, 8}, {0, -8}} {
		if l, _ := t.cover.at(e+d[0], n+d[1]); l == scenery.LandWater {
			return 0
		}
	}
	if l, _ := t.cover.at(e, n); l == scenery.LandWater {
		return 0
	}
	const r = 12.0
	gx := (t.base(e+r, n, near) - t.base(e-r, n, near)) / (2 * r)
	gy := (t.base(e, n+r, near) - t.base(e, n-r, near)) / (2 * r)
	steep := smooth01((math.Hypot(gx, gy) - 0.04) / 0.16)
	// Hummocks 14 m across on swells 40 m across (a 5 m cell near the
	// route carries the first; finer would need finer land).
	field := 0.35*(valueNoise(e/40, n/40, 0x6e11ef)-0.5) + 0.65*(valueNoise(e/14, n/14, 0x6e11f0)-0.5)
	return 2 * amp * field * steep * fade
}

// smooth01 eases x, clamped to 0..1, in and out.
func smooth01(x float64) float64 {
	x = math.Max(0, math.Min(1, x))
	return x * x * (3 - 2*x)
}

// dryAt is the land's height at e, n as if no water were there: what a
// water's shore stands at.
func (g *gland) dryAt(e, n float64) float64 {
	return g.height(e, n, false)
}

func (g *gland) height(e, n float64, wet bool) float64 {
	t := g.t
	key := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
	near := g.coarseNear(key)
	base := t.base(e, n, near)
	h, gap, edge := g.blend(e, n, base)
	h += g.relief(e, n, gap, near)
	// Between a road and a ditch: a verge, then the bank down to the water
	// (ditchBank), not the land at the water's level right up to the road.
	if gap < gDitchVergeM+gDitchBankM+1 {
		if bh, be, ok := g.ditchBank(e, n); ok {
			h, edge = bh, be
		}
	}
	// Under water the ground lies below its level; a water drawn from its
	// outline (t.exact) is a surface the land goes round instead.
	if land, id := t.cover.at(e, n); wet && land == scenery.LandWater && gap > g.t.o.CellM/2 && id >= 0 && !t.exact[id] {
		h = math.Min(h, t.cover.level(t, id, e, n)-bedM)
	}
	// A shoulder: within a smallest cell of the road's edge the land
	// stays under it, so no triangle from under the road's edge out to a
	// bank rises through the edge (it had stood 11 cm over a roundabout's
	// edge on the Posbank Loop, 16.48 km).
	if gap < g.fineM+gTuckM && !math.IsNaN(edge) {
		h = math.Min(h, edge-gSinkM)
	}
	// Under a deck, and within a smallest cell of its edge (a triangle from
	// there would rise through it): below it.
	if g.decks != nil {
		k := [2]int{int(math.Floor(e / landCellM)), int(math.Floor(-n / landCellM))}
		near := false
		for gx := k[0] - 1; gx <= k[0]+1 && !near; gx++ {
			for gz := k[1] - 1; gz <= k[1]+1 && !near; gz++ {
				near = len(g.decks.cells[[2]int{gx, gz}]) > 0
			}
		}
		if near {
			low := math.Inf(1)
			if d, ok := g.decks.at(e, n); ok {
				low = d
			}
			for _, r := range [3]float64{0.5, 1, g.fineM + gTuckM} {
				for k := range 8 {
					a := float64(k) * math.Pi / 4
					if d, ok := g.decks.at(e+r*math.Cos(a), n+r*math.Sin(a)); ok {
						low = math.Min(low, d)
					}
				}
			}
			if !math.IsInf(low, 1) {
				h = math.Min(h, low-gUnderDeckM)
			}
		}
	}
	return h
}

// coarseNear are the route's coarse samples that shape chunk key's ground
// (as terrain.lists has them).
func (g *gland) coarseNear(key [2]int) []sample {
	if v, ok := g.nearC.Load(key); ok {
		return v.([]sample)
	}
	t := g.t
	size := t.o.ChunkM
	e0, n0 := float64(key[0])*size, float64(key[1])*size
	var near []sample
	for _, s := range t.coarse {
		if rectDist(s.e, s.n, e0, n0, size) <= t.o.CorridorM+1.5*size {
			near = append(near, s)
		}
	}
	v, _ := g.nearC.LoadOrStore(key, near)
	return v.([]sample)
}

// blend eases base to the roads near e, n: to the drawn surface at the
// nearest road's edge (its junction or sidewalk included), or where that
// can't be found, to its pieces' heights (a line, 50 m of it) weighted so
// the nearest takes over as it comes close; also the distance to the
// nearest road's edge. Easing to the centre lines alone stood the land
// 13 cm over a junction's corner, between two roads at their own heights.
func (g *gland) blend(e, n, base float64) (h, gap, edge float64) {
	type piece = gPiece
	var ps [12]piece
	pieces := ps[:0]
	kx, ky := int(math.Floor(e/g.gridM)), int(math.Floor(n/g.gridM))
	for gx := kx - 1; gx <= kx+1; gx++ {
		for gy := ky - 1; gy <= ky+1; gy++ {
			for _, k := range g.grid[[2]int{gx, gy}] {
				a, b := g.samps[k], g.samps[k+1]
				de, dn := b.e-a.e, b.n-a.n
				f := 0.0
				if l2 := de*de + dn*dn; l2 > 0 {
					f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
				}
				qe, qn := a.e+f*de, a.n+f*dn
				d := math.Hypot(e-qe, n-qn)
				side := 1
				if de*(n-a.n)-dn*(e-a.e) > 0 {
					side = 0
				}
				gap := math.Max(0, d-sideEdge(a, side))
				if gap >= g.blendM {
					continue
				}
				pk := [2]int{a.line, int(a.d / (pieceSegs * fineStep))}
				ele := a.ele + f*(b.ele-a.ele)
				ue, un := 0.0, 0.0
				if l := math.Hypot(de, dn); l > 0 {
					ue, un = de/l, dn/l
				}
				found := false
				for i := range pieces {
					if pieces[i].k == pk {
						if gap < pieces[i].gap {
							pieces[i] = piece{pk, gap, ele, qe, qn, ue, un}
						}
						found = true
						break
					}
				}
				if !found && len(pieces) < cap(pieces) {
					pieces = append(pieces, piece{pk, gap, ele, qe, qn, ue, un})
				}
			}
		}
	}
	var road, sum, wsum float64
	gap = math.Inf(1)
	var near piece
	for _, p := range pieces {
		x := p.gap / g.blendM
		w := (1 - smoothstep(x)) / (x*x + 1e-6)
		sum += w * p.ele
		wsum += w
		if p.gap < near.gap || math.IsInf(gap, 1) {
			near, gap = p, p.gap
		}
	}
	if len(pieces) > 0 {
		road = sum / wsum
	}
	// The drawn surface at the nearest road's edge, looked for all round
	// (junctions, roundabouts and car parks reach where no centre line
	// says: a car park's far edge has none within blendM, and the land
	// stood a lip over it), the lowest where several are as near; its
	// distance is the gap. Further out, just inside the nearest road's edge
	// towards its centre line.
	// The shoulder (height) keeps the land under the lowest surface within
	// a smallest cell and the tuck, not the first found, and the lowest
	// where surfaces overlap (a junction's over a road's end, a step
	// between: the Diepesteeg's banks at 3.58 km stood 8 cm over): beside a junction's
	// sidewalk the first ring found the sidewalk, and the land triangle
	// from there spanned the junction's carriageway under it, 12 cm lower,
	// standing 9 cm over (the Posbank Loop at 27.7 km).
	probed := false
	edge = math.Inf(1)
	if (gap < gProbeM || len(pieces) == 0) && g.roads.anyWithin(e, n, gProbeRings[len(gProbeRings)-1]) {
		for _, r := range gProbeRings {
			if probed && r > g.fineM+gTuckM {
				break
			}
			if !g.roads.anyWithin(e, n, r) {
				continue // nothing this ring could find
			}
			low := math.Inf(1)
			for k := range 8 {
				if h, ok := g.roads.low(e+r*gProbeCos[k], n+r*gProbeSin[k]); ok {
					low = math.Min(low, h)
				}
			}
			if math.IsInf(low, 1) {
				continue
			}
			edge = math.Min(edge, low)
			if !probed {
				road, probed, gap = low, true, math.Min(gap, r)
			}
		}
	}
	if len(pieces) == 0 && !probed {
		return base, math.Inf(1), math.NaN()
	}
	if d := math.Hypot(near.qe-e, near.qn-n); len(pieces) > 0 && !probed && d > 1e-6 {
		for _, in := range [3]float64{0.05, 0.3, 1} {
			r := math.Min(near.gap+in, d)
			if h, ok := g.roads.low(e+(near.qe-e)*r/d, n+(near.qn-n)*r/d); ok {
				road, edge = h, h
				break
			}
		}
	}
	if math.IsInf(edge, 1) {
		edge = road
	}
	// Level with the road's edge out to gTuckM (the land's edge lies that
	// far in under it, and a bank easing from the edge itself stood a few
	// centimetres over the road within that), then eased out to blendM.
	f := smoothstep(math.Max(0, gap-gTuckM) / (g.blendM - gTuckM))
	// Between two roads close beside each other, on opposite sides, the
	// land lies at the lower one's level (owner, 2026-10-16, Rue du
	// Hohneck at 1.34 km: a lane climbs beside the main road behind a
	// wall; eased between them, the strip floated in a tilt with the
	// lane's edge standing over it); the higher road's wall closes the step
	// (roadside.go's abreast walls). Two roads a step apart in height
	// with no wall between them (too far apart, or no walls in this
	// style) have the land eased between them instead: laid level with
	// the lower one, it had stood as a bare cliff up to the higher one
	// (1.52 km, where the lane parts from the main road).
	wallGap := 0.0
	if g.walls != nil {
		g.walls.within(e, n, roadsideNearM, func(int) { wallGap = abreastReachM })
	}
	if low, ok := island(pieces, e, n, wallGap); ok {
		road, f = low, 0
		if !math.IsInf(edge, 1) {
			edge = math.Min(edge, low)
		}
	} else if g.levelM > 0 {
		if h, ok := bankBetween(pieces, e, n, g.levelM); ok {
			return h, gap, edge
		}
	}
	return (road-gSinkM)*(1-f) + base*f, gap, edge
}

// bankBetween is the land's height where e, n lies between two different
// roads' pieces alongside each other on opposite sides, a step apart in
// height (over levelM) with no wall to close it (island) and their edges
// less than gIslandM apart: a straight bank from the higher road's edge
// down to the lower's, tucked under each within gTuckM of it (owner,
// 2026-10-17, the Route des Crêtes coming down beside the Route des
// Américains at 10.91 km: eased to the nearest edge, the strip between
// them lay level at each edge and fell 1.3 m within half a metre in the
// middle, a grass cliff in the rider's eye).
func bankBetween(pieces []gPiece, e, n, levelM float64) (float64, bool) {
	for i, a := range pieces {
		for _, b := range pieces[i+1:] {
			if a.k[0] == b.k[0] || math.Abs(a.ele-b.ele) <= levelM || a.gap+b.gap >= gIslandM {
				continue
			}
			if math.Abs(a.de*b.de+a.dn*b.dn) < abreastCrossCos {
				continue // crossing, not alongside
			}
			if (a.qe-e)*(b.qe-e)+(a.qn-n)*(b.qn-n) >= 0 {
				continue // both to one side
			}
			ga, gb := math.Max(0, a.gap-gTuckM), math.Max(0, b.gap-gTuckM)
			f := 0.5
			if ga+gb > 0 {
				f = ga / (ga + gb)
			}
			return a.ele*(1-f) + b.ele*f - gSinkM, true
		}
	}
	return 0, false
}

// gIslandM: land between two roads narrower than this lies at the lower
// one's level.
const gIslandM = 6.0

// gPiece is a road's piece near a land point (blend): which, how far its
// edge, its height, its nearest point on its centre line.
type gPiece struct {
	k      [2]int
	gap    float64
	ele    float64
	qe, qn float64
	de, dn float64 // the road's way there (unit; 0 for none)
}

// island is the lower road's height when e, n lies between two different
// roads' pieces on opposite sides, their edges less than gIslandM apart;
// for two a step apart in height (over abreastStepM), wherever a wall
// will close the step: their edges less than wallGap apart.
func island(pieces []gPiece, e, n, wallGap float64) (float64, bool) {
	low, ok := math.Inf(1), false
	for i, a := range pieces {
		for _, b := range pieces[i+1:] {
			if a.k[0] == b.k[0] {
				continue
			}
			limit := gIslandM
			if step := math.Abs(a.ele - b.ele); step > abreastStepM {
				// As roadside.go stands its wall: roads alongside each other
				// (not crossing each other, the step under abreastMaxM).
				limit = wallGap
				if step >= abreastMaxM || math.Abs(a.de*b.de+a.dn*b.dn) < abreastCrossCos {
					limit = 0
				}
			}
			if a.gap+b.gap >= limit {
				continue
			}
			if (a.qe-e)*(b.qe-e)+(a.qn-n)*(b.qn-n) < 0 {
				low, ok = math.Min(low, math.Min(a.ele, b.ele)), true
			}
		}
	}
	return low, ok
}

// The land looks for the drawn road round a point within the last ring,
// where a road's centre line is within gProbeM.
var gProbeRings = [...]float64{0.15, 0.3, 0.5, 0.8, 1.2, 1.7}

const gProbeM = 12.0

// The eight probe directions, every 45°, computed once.
var gProbeCos, gProbeSin = func() (c, s [8]float64) {
	for k := range 8 {
		a := float64(k) * math.Pi / 4
		c[k], s[k] = math.Cos(a), math.Sin(a)
	}
	return
}()

// landHeight is a land vertex's height: heightAt, but under a road
// surface (the land's edge is tucked under it) just below that surface,
// whatever the road's centre line says.
func (g *gland) landHeight(e, n float64) float64 {
	h := g.heightAt(e, n)
	if r, ok := g.roads.low(e, n); ok {
		h = math.Min(h, r-gSinkM)
	}
	return h
}

// crossing is where the outline crosses the lattice edge from a (clear)
// to b (covered): bisected.
func (g *gland) crossing(ae, an, be, bn float64) (float64, float64) {
	for range 7 {
		me, mn := (ae+be)/2, (an+bn)/2
		if g.covered(me, mn) {
			be, bn = me, mn
		} else {
			ae, an = me, mn
		}
	}
	return (ae + be) / 2, (an + bn) / 2
}

// edgeHeight is the land's height at a point of its edge: just under the
// road there.
func (g *gland) edgeHeight(e, n, towardE, towardN float64) float64 {
	l := math.Hypot(towardE-e, towardN-n)
	if l > 0 {
		if h, ok := g.roads.low(e+0.05*(towardE-e)/l, n+0.05*(towardN-n)/l); ok {
			return h - gSinkM
		}
	}
	if h, ok := g.roads.low(e, n); ok {
		return h - gSinkM
	}
	return g.heightAt(e, n)
}

// chunk lays the land of one chunk: its mesh, coloured by land use, and
// its water.
func (g *gland) chunk(key [2]int, material, waterMaterial int) (ground, water gltf.Primitive) {
	t := g.t
	const side = gSide
	roots := max(1, int(math.Round(t.o.ChunkM/g.rootM)))
	I0, J0 := key[0]*roots, key[1]*roots
	p := gltf.Primitive{Material: material}
	type vkey struct {
		i, j int // lattice point (×2: a leaf's centre lies between)
		x    int // a crossing: the other end's direction (1-4), 0 a lattice point
	}
	index := map[vkey]uint32{}
	put := func(k vkey, e, n, h float64) uint32 {
		if v, ok := index[k]; ok {
			return v
		}
		v := uint32(len(p.Positions) / 3)
		index[k] = v
		p.Positions = append(p.Positions, float32(e), float32(h), float32(-n))
		p.Normals = append(p.Normals, 0, 1, 0)
		land, _ := t.cover.at(e, n)
		_, gap, _ := g.blend(e, n, 0)
		if gap < verge {
			land = verdure
		}
		c := styleFor(t.o.Region).landColour(land)
		p.Colors = append(p.Colors, c[0], c[1], c[2])
		return v
	}
	point := func(i, j int) uint32 {
		e, n := g.lattice(i, j)
		return put(vkey{2 * i, 2 * j, 0}, e, n, g.landHeight(e, n))
	}
	// cross is the crossing on the edge from lattice point a (clear) to b
	// (covered), keyed by the edge.
	cross := func(ai, aj, bi, bj int) uint32 {
		ae, an := g.lattice(ai, aj)
		be, bn := g.lattice(bi, bj)
		k := vkey{ai + bi, aj + bj, 1}
		if v, ok := index[k]; ok {
			return v
		}
		e, n := g.crossing(ae, an, be, bn)
		return put(k, e, n, g.edgeHeight(e, n, be, bn))
	}
	tri := func(a, b, c uint32) {
		if a != b && b != c && a != c {
			p.Indices = append(p.Indices, a, b, c)
		}
	}
	// Far, flat blocks of root cells as one leaf each: a ring of their
	// corners and the vertices their neighbours have on their sides.
	bs := gBlockRoots * side
	for BJ := floorDiv(J0, gBlockRoots); BJ*gBlockRoots < J0+roots; BJ++ {
		for BI := floorDiv(I0, gBlockRoots); BI*gBlockRoots < I0+roots; BI++ {
			if !g.merged(BI, BJ) {
				continue
			}
			i0, j0 := BI*bs, BJ*bs
			var ring []uint32
			for i := i0; i < i0+bs; i++ {
				if i == i0 || g.vertexAt(i, j0) {
					ring = append(ring, point(i, j0))
				}
			}
			for j := j0; j < j0+bs; j++ {
				if j == j0 || g.vertexAt(i0+bs, j) {
					ring = append(ring, point(i0+bs, j))
				}
			}
			for i := i0 + bs; i > i0; i-- {
				if i == i0+bs || g.vertexAt(i, j0+bs) {
					ring = append(ring, point(i, j0+bs))
				}
			}
			for j := j0 + bs; j > j0; j-- {
				if j == j0+bs || g.vertexAt(i0, j) {
					ring = append(ring, point(i0, j))
				}
			}
			if len(ring) == 4 {
				tri(ring[0], ring[1], ring[2])
				tri(ring[0], ring[2], ring[3])
				continue
			}
			ce, cn := g.lattice(i0, j0)
			ce, cn = ce+float64(bs)*g.fineM/2, cn+float64(bs)*g.fineM/2
			c := put(vkey{2*i0 + bs, 2*j0 + bs, 0}, ce, cn, g.landHeight(ce, cn))
			for a := range ring {
				tri(c, ring[a], ring[(a+1)%len(ring)])
			}
		}
	}
	for J := J0; J < J0+roots; J++ {
		for I := I0; I < I0+roots; I++ {
			if g.merged(floorDiv(I, gBlockRoots), floorDiv(J, gBlockRoots)) {
				continue
			}
			tr := g.tree(I, J)
			for k := range side * side {
				oi, oj := int(tr.org[k][0]), int(tr.org[k][1])
				if mod(k, side) != oi || k/side != oj {
					continue // not the leaf's own first cell
				}
				s := int(tr.size[k])
				i0, j0 := I*side+oi, J*side+oj
				switch tr.kind[k] {
				case gCovered:
				case gClear:
					// Counter-clockwise from above (east, north): along the
					// south side east, the east side north, back west, down.
					var ring []uint32
					for i := i0; i < i0+s; i++ {
						if i == i0 || g.vertexAt(i, j0) {
							ring = append(ring, point(i, j0))
						}
					}
					for j := j0; j < j0+s; j++ {
						if j == j0 || g.vertexAt(i0+s, j) {
							ring = append(ring, point(i0+s, j))
						}
					}
					for i := i0 + s; i > i0; i-- {
						if i == i0+s || g.vertexAt(i, j0+s) {
							ring = append(ring, point(i, j0+s))
						}
					}
					for j := j0 + s; j > j0; j-- {
						if j == j0+s || g.vertexAt(i0, j) {
							ring = append(ring, point(i0, j))
						}
					}
					if len(ring) == 4 {
						tri(ring[0], ring[1], ring[2])
						tri(ring[0], ring[2], ring[3])
						continue
					}
					ce, cn := g.lattice(i0, j0)
					ce, cn = ce+float64(s)*g.fineM/2, cn+float64(s)*g.fineM/2
					c := put(vkey{2*i0 + s, 2*j0 + s, 0}, ce, cn, g.landHeight(ce, cn))
					for a := range ring {
						tri(c, ring[a], ring[(a+1)%len(ring)])
					}
				case gMixed:
					// Marching squares over the leaf: the ring of its corners
					// and the vertices on its sides (the clear case's), each
					// clear or covered, with the crossing between any two that
					// differ; the clear stretches of the ring, a crossing at
					// each end, are the land. Two stretches apart are two
					// pieces when the leaf's middle is covered (a saddle), one
					// joined through it when not.
					type rp struct {
						i, j  int
						clear bool
					}
					var ring []rp
					add := func(i, j int) {
						e, n := g.lattice(i, j)
						ring = append(ring, rp{i, j, !g.covered(e, n)})
					}
					for i := i0; i < i0+s; i++ {
						if i == i0 || g.vertexAt(i, j0) {
							add(i, j0)
						}
					}
					for j := j0; j < j0+s; j++ {
						if j == j0 || g.vertexAt(i0+s, j) {
							add(i0+s, j)
						}
					}
					for i := i0 + s; i > i0; i-- {
						if i == i0+s || g.vertexAt(i, j0+s) {
							add(i, j0+s)
						}
					}
					for j := j0 + s; j > j0; j-- {
						if j == j0+s || g.vertexAt(i0, j) {
							add(i0, j)
						}
					}
					n := len(ring)
					edgePt := func(q int) uint32 {
						a, b := ring[q], ring[(q+1)%n]
						if a.clear {
							return cross(a.i, a.j, b.i, b.j)
						}
						return cross(b.i, b.j, a.i, a.j)
					}
					// The clear stretches: each from the ring vertex after a
					// crossing to the one before the next.
					var runs [][]uint32
					start := -1
					for q := range n {
						if !ring[q].clear && ring[(q+1)%n].clear {
							start = (q + 1) % n
							break
						}
					}
					if start < 0 {
						// All clear (the outline passes between lattice points
						// at the tuck) or all covered: nothing, or a plain fan.
						if ring[0].clear {
							var poly []uint32
							for _, r := range ring {
								poly = append(poly, point(r.i, r.j))
							}
							for a := 1; a+1 < len(poly); a++ {
								tri(poly[0], poly[a], poly[a+1])
							}
						}
						continue
					}
					for q := start; ; {
						run := []uint32{edgePt((q + n - 1) % n)}
						for ring[q].clear {
							run = append(run, point(ring[q].i, ring[q].j))
							q = (q + 1) % n
						}
						run = append(run, edgePt((q+n-1)%n))
						runs = append(runs, run)
						for !ring[q].clear {
							q = (q + 1) % n
						}
						if q == start {
							break
						}
					}
					joined := len(runs) == 1
					if !joined {
						ce, cn := g.lattice(i0, j0)
						ce, cn = ce+float64(s)*g.fineM/2, cn+float64(s)*g.fineM/2
						joined = !g.covered(ce, cn)
					}
					if joined {
						var poly []uint32
						for _, run := range runs {
							poly = append(poly, run...)
						}
						for a := 1; a+1 < len(poly); a++ {
							tri(poly[0], poly[a], poly[a+1])
						}
						continue
					}
					for _, run := range runs {
						for a := 1; a+1 < len(run); a++ {
							tri(run[0], run[a], run[a+1])
						}
					}
				}
			}
		}
	}
	// Normals from the heights a little either side: the same at a
	// vertex whichever chunk has it, so chunks shade alike.
	const dn = 0.75
	for v := 0; v < len(p.Positions)/3; v++ {
		e, n := float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])
		dhde := (g.heightAt(e+dn, n) - g.heightAt(e-dn, n)) / (2 * dn)
		dhdn := (g.heightAt(e, n+dn) - g.heightAt(e, n-dn)) / (2 * dn)
		nx, ny, nz := -dhde, 1.0, dhdn
		ln := math.Sqrt(nx*nx + ny*ny + nz*nz)
		p.Normals[3*v], p.Normals[3*v+1], p.Normals[3*v+2] = float32(nx/ln), float32(ny/ln), float32(nz/ln)
	}
	// Water: a quad at its level over every terrain cell with a corner in
	// it (as the classic builder's), reaching under the banks.
	w := gltf.Primitive{Material: waterMaterial}
	size := t.o.ChunkM
	cells := max(1, int(math.Round(size/t.o.CellM)))
	cell := size / float64(cells)
	e0, n0 := float64(key[0])*size, float64(key[1])*size
	for j := range cells {
		for i := range cells {
			id := -1
			for _, c := range [4][2]float64{{0, 0}, {cell, 0}, {0, cell}, {cell, cell}} {
				e, n := e0+float64(i)*cell+c[0], n0+float64(j)*cell+c[1]
				if land, wid := t.cover.at(e, n); land == scenery.LandWater && wid >= 0 && !t.exact[wid] {
					id = wid
					break
				}
			}
			if id < 0 {
				continue
			}
			a := uint32(len(w.Positions) / 3)
			e, n := e0+float64(i)*cell, n0+float64(j)*cell
			for _, c := range [4][2]float64{{0, 0}, {cell, 0}, {0, cell}, {cell, cell}} {
				y := t.cover.level(t, id, e+c[0], n+c[1])
				w.Positions = append(w.Positions, float32(e+c[0]), float32(y), float32(-(n + c[1])))
				w.Normals = append(w.Normals, 0, 1, 0)
			}
			w.Indices = append(w.Indices, a, a+1, a+2, a+1, a+3, a+2)
		}
	}
	return p, w
}

// A ditch's banks by the road (owner, 2026-10-12: a ditch's water at
// 7.83 km of the polder ride lay 1.3 m below the road but read as level
// with it, its bank a wall within a metre of the asphalt, out of the
// rider's sight). Dutch ditches are mapped to the top of their bank (the
// BGT's insteek), and many reach the road's edge, so the water's edge is
// kept this far from any road surface: a verge before the bank, then the
// bank's run down to the water, which the land fills (it eases from the
// road's edge down to the water's).
const (
	gDitchVergeM = 0.8 // flat beside the road
	gDitchBankM  = 1.5 // the bank's run, about 1:1.2 for a metre and more's fall
	gDitchStepM  = 4.0 // the outline followed this finely
	gDitchWaterM = 0.8 // a narrow ditch keeps at least this much water (its bottom)
)

// ditchBanks is outline ring (east, north) with its points that come
// within gDitchVergeM+gDitchBankM of a road surface (dist: the distance to
// the nearest, up to its max) moved in, away from the road, as far as the
// water's width allows.
func ditchBanks(ring [][2]float64, dist func(e, n, max float64) float64) [][2]float64 {
	keep := gDitchVergeM + gDitchBankM
	var r [][2]float64
	var corner []bool // the outline's own points (the rest were put in)
	for i := range ring {
		a, b := ring[i], ring[(i+1)%len(ring)]
		k := max(1, int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1])/gDitchStepM)))
		for j := range k {
			f := float64(j) / float64(k)
			r = append(r, [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])})
			corner = append(corner, j == 0)
		}
	}
	var area float64
	for i := range r {
		a, b := r[i], r[(i+1)%len(r)]
		area += a[0]*b[1] - b[0]*a[1]
	}
	out := make([][2]float64, len(r))
	moved := make([]bool, len(r))
	any := false
	for i, p := range r {
		out[i] = p
		d := dist(p[0], p[1], keep)
		if d >= keep {
			continue
		}
		prev, next := r[(i-1+len(r))%len(r)], r[(i+1)%len(r)]
		tx, ty := next[0]-prev[0], next[1]-prev[1]
		l := math.Hypot(tx, ty)
		if l == 0 {
			continue
		}
		nx, ny := -ty/l, tx/l // left of the ring's way: inside an anticlockwise ring
		if area < 0 {
			nx, ny = -nx, -ny
		}
		// Only the bank on the road's side moves: the water may narrow to
		// gDitchWaterM, a ditch mapped from bank top to bank top having its
		// water at the bottom.
		move := math.Min(keep-d, math.Max(0, rayToRing(r, i, p, nx, ny)-gDitchWaterM))
		if move > 0 {
			out[i] = [2]float64{p[0] + nx*move, p[1] + ny*move}
			moved[i], any = true, true
		}
	}
	if !any {
		return ring
	}
	// The outline's own points, those moved and one either side of each
	// run moved (where the bank starts): the points put in on long straight
	// sides elsewhere would only make the water's triangles long and thin
	// across a canal (and every land height looked them all up: the polder
	// ride's build took three times as long).
	var kept [][2]float64
	for i := range out {
		near := moved[i] || moved[(i+1)%len(out)] || moved[(i-1+len(out))%len(out)]
		if corner[i] || near {
			kept = append(kept, out[i])
		}
	}
	return kept
}

// ditchBank is the land at e, n between a road and water (a ditch beside
// it): level with the road's edge for gDitchVergeM, then straight down to
// the water's edge, and the road's height as the edge it keeps under; false
// where no road and water are both near. The land's probes otherwise take
// the lowest surface near, the water, and stood the land at its level up
// to the asphalt: a wall the rider never saw, the water seeming level with
// the road (owner, 2026-10-12, the polder ride at 7.83 km).
func (g *gland) ditchBank(e, n float64) (h, edge float64, ok bool) {
	reach := gDitchVergeM + gDitchBankM + 0.7
	if g.waters == nil || g.dry == nil || !g.waters.anyWithin(e, n, reach) || !g.dry.anyWithin(e, n, reach) {
		return 0, 0, false
	}
	dw, hw, okw := g.nearestSurface(e, n, reach, true)
	if !okw || dw < 0.2 {
		return 0, 0, false // no water near, or within its own edge's tuck
	}
	dr, hr, okr := g.nearestSurface(e, n, reach, false)
	if !okr {
		return 0, 0, false
	}
	// Where the room is short, the verge gives way to the bank.
	verge := math.Min(gDitchVergeM, 0.35*(dr+dw))
	if dr <= verge {
		return hr - gSinkM, hr, true
	}
	f := (dr - verge) / (dr - verge + dw)
	return (hr-gSinkM)*(1-f) + (hw-gSinkM)*f, hr, true
}

// gBankRings are the rings ditchBank looks round a point on.
var gBankRings = [...]float64{0.15, 0.3, 0.5, 0.8, 1.2, 1.7, 2.3, 3.0}

// nearestSurface is how far the nearest water (water true) or other
// surface (a road, a junction, a car park) is from e, n within reach, in
// gBankRings, and the lowest surface's height there.
func (g *gland) nearestSurface(e, n, reach float64, water bool) (d, h float64, ok bool) {
	ix := g.dry
	if water {
		ix = g.waters
	}
	for _, r := range gBankRings {
		if r > reach {
			break
		}
		if !ix.anyWithin(e, n, r) {
			continue
		}
		low := math.Inf(1)
		for k := range 8 {
			if lh, found := ix.low(e+r*gProbeCos[k], n+r*gProbeSin[k]); found {
				low = math.Min(low, lh)
			}
		}
		if !math.IsInf(low, 1) {
			return r, low, true
		}
	}
	return 0, 0, false
}

// rayToRing is how far the ray from p (ring point i) along nx, ny goes
// before it meets the ring again (the water's width there); +Inf if never.
func rayToRing(r [][2]float64, i int, p [2]float64, nx, ny float64) float64 {
	best := math.Inf(1)
	for k := range r {
		if k == i || (k+1)%len(r) == i {
			continue
		}
		a, b := r[k], r[(k+1)%len(r)]
		ex, ey := b[0]-a[0], b[1]-a[1]
		den := nx*ey - ny*ex
		if math.Abs(den) < 1e-12 {
			continue
		}
		wx, wy := a[0]-p[0], a[1]-p[1]
		s := (wx*ey - wy*ex) / den // along the ray
		u := (wx*ny - wy*nx) / den // along the edge
		if s > 1e-6 && u >= 0 && u <= 1 {
			best = math.Min(best, s)
		}
	}
	return best
}

// gWaterSheet is water x's surface for the graph builder, from its own
// outline (as the classic builder's waterMeshes): its level from its
// banks, but never over the shore as the land stands there (dry, its
// heights eased to the roads beside it; shoreUnderM under it within
// shoreReachM), so a ditch beside a road lies below the verge between them
// (owner, 2026-10-11: the graph builder's 5 m water cells along the
// dike road and the dike road, had the land standing through
// the ditch in wedges). The land goes round it as round a road.
func gWaterSheet(t *terrain, x exactWater, dry func(e, n float64) float64, material int) gltf.Primitive {
	type shore struct{ e, n, h float64 }
	grid := map[[2]int][]shore{}
	cell := func(e, n float64) [2]int {
		return [2]int{int(math.Floor(e / shoreReachM)), int(math.Floor(n / shoreReachM))}
	}
	for i := range x.ring {
		a, b := x.ring[i], x.ring[(i+1)%len(x.ring)]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		k := max(1, int(math.Ceil(l/shoreStepM)))
		for j := range k {
			f := float64(j) / float64(k)
			e, n := a[0]+f*(b[0]-a[0]), a[1]+f*(b[1]-a[1])
			grid[cell(e, n)] = append(grid[cell(e, n)], shore{e, n, dry(e, n)})
		}
	}
	level := func(e, n float64) float64 {
		lv := t.cover.level(t, x.id, e, n)
		k := cell(e, n)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for _, s := range grid[[2]int{k[0] + dx, k[1] + dy}] {
					if math.Hypot(s.e-e, s.n-n) < shoreReachM {
						lv = math.Min(lv, s.h-shoreUnderM)
					}
				}
			}
		}
		return lv
	}
	return waterSheet(level, grownRing(x.ring, sheetGrowM), material)
}

// underRoads lowers the land's vertices so that no land triangle rises
// through a road surface: each triangle near a road is sampled along its
// edges and at its centre, and where the land there would stand over the
// lowest road surface found, its vertices go under it. A vertex's height is
// settled from every triangle it is in, over all chunks (vertices are
// shared by position), so chunks still meet without cracks. The land is
// built just under the roads at its vertices, but a road sloping across a
// triangle's span (the Diepesteeg's side road climbing 10 % beside its
// banks, 3.58 km on the Posbank Loop) stood up to 8 cm through it.
func (g *gland) underRoads(meshes []gltf.Primitive) {
	type vkey [2]float32
	low := map[vkey]float32{}
	near := func(e, n float64) bool {
		k := [2]int{int(math.Floor(e / landCellM)), int(math.Floor(-n / landCellM))}
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gz := k[1] - 1; gz <= k[1]+1; gz++ {
				if len(g.roads.cells[[2]int{gx, gz}]) > 0 {
					return true
				}
			}
		}
		return false
	}
	for _, p := range meshes {
		for t := 0; t+2 < len(p.Indices); t += 3 {
			var e, n, h [3]float64
			for k := range 3 {
				v := p.Indices[t+k]
				e[k], h[k], n[k] = float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), -float64(p.Positions[3*v+2])
			}
			ce, cn := (e[0]+e[1]+e[2])/3, (n[0]+n[1]+n[2])/3
			if !near(ce, cn) {
				continue
			}
			over := [3]bool{}
			probe := func(pe, pn, land float64, vs ...int) {
				if r, ok := g.roads.low(pe, pn); ok && land > r-gSinkM {
					for _, v := range vs {
						over[v] = true
						k := vkey{float32(e[v]), float32(n[v])}
						if cur, ok := low[k]; !ok || float32(r-gSinkM) < cur {
							low[k] = float32(r - gSinkM)
						}
					}
				}
			}
			for a := range 3 {
				b := (a + 1) % 3
				for _, f := range [3]float64{0.25, 0.5, 0.75} {
					probe(e[a]+f*(e[b]-e[a]), n[a]+f*(n[b]-n[a]), h[a]+f*(h[b]-h[a]), a, b)
				}
			}
			probe(ce, cn, (h[0]+h[1]+h[2])/3, 0, 1, 2)
			// At its own corners and at every road corner inside it: a long
			// thin road triangle (a junction's) can reach into a land
			// triangle between the samples along its edges (16.1 km of the
			// Route des Américains: 9 cm of land over a junction's
			// corner). These lower the land by as much as it stands over the
			// road there, not to the road's height: the land is linear, so
			// that puts it under, and keeps its shape (taken down whole, a
			// verge beside a road stood 14 cm under the road's edge).
			drop := func(pe, pn, land float64, w [3]float64) {
				if r, ok := g.roads.low(pe, pn); ok && land > r-gSinkM {
					d := land - (r - gSinkM)
					for v := range 3 {
						if w[v] <= 0 {
							continue
						}
						k := vkey{float32(e[v]), float32(n[v])}
						if cur, ok := low[k]; !ok || float32(h[v]-d) < cur {
							low[k] = float32(h[v] - d)
						}
					}
				}
			}
			for a := range 3 {
				var w [3]float64
				w[a] = 1
				drop(e[a], n[a], h[a], w)
			}
			g.roadCornersIn(e, n, func(pe, pn float64) {
				w0, w1, w2, ok := barycentric(e, n, pe, pn)
				if ok {
					drop(pe, pn, w0*h[0]+w1*h[1]+w2*h[2], [3]float64{w0, w1, w2})
				}
			})
		}
	}
	if len(low) == 0 {
		return
	}
	for _, p := range meshes {
		for v := 0; 3*v+2 < len(p.Positions); v++ {
			if y, ok := low[vkey{p.Positions[3*v], -p.Positions[3*v+2]}]; ok && y < p.Positions[3*v+1] {
				p.Positions[3*v+1] = y
			}
		}
	}
}

// roadCornersIn calls f with every corner of a road surface triangle that
// lies in the cells under the triangle e, n (east, north).
func (g *gland) roadCornersIn(e, n [3]float64, f func(e, n float64)) {
	x0, x1 := math.Min(e[0], math.Min(e[1], e[2])), math.Max(e[0], math.Max(e[1], e[2]))
	z0, z1 := math.Min(-n[0], math.Min(-n[1], -n[2])), math.Max(-n[0], math.Max(-n[1], -n[2]))
	seen := map[int32]bool{}
	for gx := int(math.Floor(x0 / landCellM)); gx <= int(math.Floor(x1/landCellM)); gx++ {
		for gz := int(math.Floor(z0 / landCellM)); gz <= int(math.Floor(z1/landCellM)); gz++ {
			for _, id := range g.roads.cells[[2]int{gx, gz}] {
				if seen[id] {
					continue
				}
				seen[id] = true
				t := g.roads.tri[9*int(id) : 9*int(id)+9]
				for c := range 3 {
					x, z := t[3*c], t[3*c+2]
					if x >= x0 && x <= x1 && z >= z0 && z <= z1 {
						f(x, -z)
					}
				}
			}
		}
	}
}

// barycentric is point pe, pn's weights in the triangle e, n, and whether
// it lies inside (edges included).
func barycentric(e, n [3]float64, pe, pn float64) (w0, w1, w2 float64, ok bool) {
	den := (n[1]-n[2])*(e[0]-e[2]) + (e[2]-e[1])*(n[0]-n[2])
	if math.Abs(den) < 1e-12 {
		return 0, 0, 0, false
	}
	w0 = ((n[1]-n[2])*(pe-e[2]) + (e[2]-e[1])*(pn-n[2])) / den
	w1 = ((n[2]-n[0])*(pe-e[2]) + (e[0]-e[2])*(pn-n[2])) / den
	w2 = 1 - w0 - w1
	const eps = -1e-9
	return w0, w1, w2, w0 >= eps && w1 >= eps && w2 >= eps
}
