package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Trees by species (owner, 2026-10-09: a trunk and major branches with
// clumps of leaves, the crown on the trunk, not floating above it; the
// main species where they grow, sizes varying; mixed near borders between
// kinds of land and in gardens, by seeding and by choice). Each species
// has two detailed forms, for trees within treeNearM of the route, where
// riders see them close: formForest (in a forest trees compete for light:
// bare trunks taller, crowns narrower, firs not wide at the base) and
// formOpen (solitary, wide); and a simple one in the
// same shape and colours for the rest, only ever seen from afar: the
// detailed ones have ten times the triangles.
const (
	treeVariants = 2 // the forms
	formForest   = 0
	formOpen     = 1
	treeNearM    = 60.0
)

// species describes a kind of tree: its size (height H m), how far up its
// trunk goes bare in the open and how thick it is at the foot (radius;
// old trees' girth, as riders expect of grown woods and avenues (owner,
// 2026-10-10: older broadleaves looked too skinny, a thicket of poles
// rather than grown trees): oak 1.0 m across at 14 m, beech 0.8 at 20 m,
// ash 0.7 at 18 m, birch 0.35 at 12 m, pine 0.45 at 18 m), its branches
// and leaf clumps, its colours; conifers are built of tiers instead (fir)
// or carry a flat crown on a tall bare trunk (pine). A fir (in Dutch
// woods mostly Norway spruce and Douglas fir) is narrow: its crown about
// a third of its height across in the open, under a quarter in a stand
// (owner, 2026-10-10: firs were comically wide, 0.64 of their height).
type species struct {
	name                 string
	h, bare, trunkR      float64
	branches             int
	spread, rise         float64 // branch length (× crown radius) and angle above level (degrees)
	crownR, clumpR       float64
	clumps               int // along each branch, besides the one at its end
	bark, leaf           uint32
	fir, pine            bool
	tierCount, tierShape float64
	// pollard: cut back to its head every few years (knotwilg): a short,
	// thick trunk, a knobbly head and a crown of thin straight shoots.
	pollard bool
}

// crownReach is how far the species' open form reaches out from its
// trunk at scale 1: tiers for a fir (h × tierShape, with its jitter),
// branches and their end clumps for the rest (crownR + clumpR, as far as
// a branch with its clump can reach). A forest form reaches less.
func (sp species) crownReach() float64 {
	if sp.fir {
		return sp.h * sp.tierShape * 1.1
	}
	return sp.crownR + sp.clumpR
}

var allSpecies = []species{
	{name: "oak", h: 14, bare: 3, trunkR: 0.5, branches: 6, spread: 0.9, rise: 30, crownR: 5.5, clumpR: 2.9, bark: 0x4e3d2e, leaf: 0x3e5f2a},
	{name: "beech", h: 20, bare: 7, trunkR: 0.4, branches: 6, spread: 0.75, rise: 45, crownR: 5, clumpR: 2.9, clumps: 1, bark: 0x8f8a7e, leaf: 0x4f7a2e},
	{name: "ash", h: 18, bare: 6, trunkR: 0.35, branches: 5, spread: 0.85, rise: 50, crownR: 4.5, clumpR: 2.4, bark: 0x6e6658, leaf: 0x5f8a36},
	{name: "birch", h: 12, bare: 3, trunkR: 0.175, branches: 5, spread: 0.6, rise: 65, crownR: 2.4, clumpR: 1.5, clumps: 1, bark: 0xd6d2c6, leaf: 0x7a9a3a},
	{name: "pine", h: 18, bare: 8, trunkR: 0.22, branches: 4, spread: 0.85, rise: 15, crownR: 3.2, clumpR: 1.9, bark: 0x9a5a36, leaf: 0x3a5a3a, pine: true},
	{name: "fir", h: 20, bare: 2, trunkR: 0.25, bark: 0x5a4030, leaf: 0x2a442c, fir: true, tierCount: 5, tierShape: 0.16},
	// Canada poplar (Populus × canadensis), the Dutch polder road's tree:
	// tall (25-30 m), the trunk bare to about 5 m (owner, 2026-10-10: 8 m
	// started the branches too high), branches rising steeply into
	// a crown about a third of its height across (owner, 2026-10-10: an
	// alternative along country, field and canal-side roads).
	{name: "poplar", h: 26, bare: 5, trunkR: 0.45, branches: 7, spread: 0.8, rise: 60, crownR: 6, clumpR: 2.4, clumps: 1, bark: 0x8c897e, leaf: 0x5e8c3a},
	// Pollard willow (knotwilg, Salix alba pollarded): a trunk 2-2.5 m
	// tall and about 0.7 m across, its head of shoots 3 m long (owner,
	// 2026-10-10: along farmland ditches between plots).
	{name: "willow", h: 5.5, bare: 2.2, trunkR: 0.35, crownR: 2.2, clumpR: 0.9, bark: 0x6b6152, leaf: 0x8aa06a, pollard: true},
}

// Kinds of plant (templates): each species' detailed variants and simple
// one, then the bushes (one per allBushes), heather, reed, lily, hedges.
var (
	speciesKinds = len(allSpecies) * (treeVariants + 1)
	kBush        = speciesKinds // then one per allBushes
	kHeather     = kBush + len(allBushes)
	kReed        = kHeather + 1
	kLily        = kHeather + 2
	kHedge       = kHeather + 3 // then one per hedgeKinds
)

// treeKind is the kind for species sp: its variant v (0..treeVariants-1),
// or its simple one (far).
func treeKind(sp, v int, far bool) int {
	if far {
		return sp*(treeVariants+1) + treeVariants
	}
	return sp*(treeVariants+1) + v
}

// plantKinds are the kinds' names and how far renderers draw them.
var plantKinds = func() []plantKind {
	var out []plantKind
	for _, s := range allSpecies {
		for v := range treeVariants {
			out = append(out, plantKind{s.name + " " + string(rune('1'+v)), 700})
		}
		out = append(out, plantKind{s.name + " far", 700})
	}
	for _, b := range allBushes {
		out = append(out, plantKind{"bush " + b.name, b.drawM})
	}
	out = append(out, plantKind{"heather", 150}, plantKind{"reed", 150}, plantKind{"lily", 100})
	for _, h := range hedgeKinds {
		out = append(out, plantKind{h.name, h.drawM})
	}
	return out
}()

type plantKind struct {
	name  string
	drawM float64
}

// Species by where they grow (weights): broadleaved and coniferous kinds
// of each land use, as plantRules' categories pick them.
type speciesMix struct{ broad, conifer map[string]float64 }

var speciesByLand = map[scenery.Land]speciesMix{
	scenery.LandForest: {
		broad:   map[string]float64{"oak": 45, "beech": 35, "birch": 10, "ash": 10},
		conifer: map[string]float64{"pine": 70, "fir": 30},
	},
	scenery.LandHeath: {
		broad:   map[string]float64{"birch": 85, "oak": 15},
		conifer: map[string]float64{"pine": 100},
	},
	// Gardens: whatever people planted.
	scenery.LandBuilt: {
		broad:   map[string]float64{"birch": 30, "oak": 15, "ash": 15, "beech": 15},
		conifer: map[string]float64{"fir": 60, "pine": 40},
	},
	scenery.LandMeadow: {broad: map[string]float64{"oak": 55, "ash": 35, "birch": 10}},
	scenery.LandNone:   {broad: map[string]float64{"oak": 50, "ash": 35, "birch": 15}},
}

// avenueMix: the trees along roads through open land, one species per
// stretch.
var avenueMix = map[string]float64{"oak": 45, "ash": 40, "beech": 15}

// countryAvenueMix along minor country roads (countryRoad), canalAvenueMix
// where water runs beside them: poplars, the Dutch polder road's and
// canal side's tree (owner, 2026-10-10).
var (
	countryAvenueMix = map[string]float64{"oak": 35, "ash": 30, "poplar": 25, "beech": 10}
	canalAvenueMix   = map[string]float64{"poplar": 55, "ash": 25, "oak": 20}
	countryRoad      = map[string]bool{"unclassified": true, "tertiary": true, "service": true, "track": true}
)

// speciesIndex is the species named name's index, -1 if none.
func speciesIndex(name string) int {
	for i, s := range allSpecies {
		if s.name == name {
			return i
		}
	}
	return -1
}

// pickSpecies draws a species from mix with r in [0, 1); -1 for none.
func pickSpecies(mix map[string]float64, r float64) int {
	total := 0.0
	for _, s := range allSpecies { // in a fixed order
		total += mix[s.name]
	}
	if total == 0 {
		return -1
	}
	r *= total
	for i, s := range allSpecies {
		if r < mix[s.name] {
			return i
		}
		r -= mix[s.name]
	}
	return -1
}

// addTreeTemplate adds species sp's template: variant v, or its simple
// one (far).
func addTreeTemplate(p *gltf.Primitive, sp species, v int, far bool) {
	bark, leaf := srgbLinear(sp.bark), srgbLinear(sp.leaf)
	if v == formForest && !far && !sp.pollard { // a pollard's forest slot is its leaning form
		// Drawn up by its neighbours: bare higher, crown narrower.
		// At most two thirds bare: a stand's crowns are a third of
		// their height or more (Scots pine, oak, beech in Dutch stands).
		sp.bare = math.Max(sp.bare, math.Min(sp.bare+(sp.h-sp.bare)*0.3, sp.h*0.65))
		sp.crownR *= 0.7
		sp.clumpR *= 0.85
		sp.tierShape *= 0.7
	}
	rng := splitmix(uint64(len(sp.name))<<8 ^ uint64(sp.name[0])<<16 ^ uint64(v+1))
	r := func() float64 { rng = splitmix(rng); return unit(rng) }
	sides := 7
	if far {
		sides = 5
	}
	switch {
	case sp.fir:
		// A trunk, and tiers of cones from low down to the top, each
		// narrower: the crown is the tiers, on the trunk.
		cone(p, 0, 0, sp.trunkR, sp.trunkR*0.3, sp.h*0.9, sides, bark)
		tiers := int(sp.tierCount)
		if far {
			tiers = 2
		}
		span := sp.h - sp.bare
		for k := range tiers {
			f := float64(k) / float64(tiers)
			y := sp.bare + span*f*0.85
			rad := sp.h * sp.tierShape * (1 - f*0.8) * (0.9 + 0.2*r())
			cone(p, 0, y, rad, 0, span*(1-f*0.85)*0.5+1, sides+1, shade(leaf, 0.9+0.2*f))
			// Closed below, darker (in its own shade): an open cone showed
			// the sky through it from underneath.
			disc(p, 0, y, rad, sides+1, shade(leaf, 0.6))
		}
		return
	case sp.pollard:
		// The forest form, which a willow never takes, is the leaning
		// one: pollards by water often sag towards it (owner, 2026-10-10:
		// 0-8°); willows turns it to face the ditch.
		if v == formForest && !far {
			from := len(p.Positions)
			defer func() {
				k := math.Tan(willowLeanDeg * math.Pi / 180)
				for i := from; i+2 < len(p.Positions); i += 3 {
					p.Positions[i] += float32(k * float64(p.Positions[i+1]))
				}
			}()
		}
		// The trunk thickening into its head, and the shoots: straight,
		// thin, rising steeply all round, leafy along their upper part.
		cone(p, 0, 0, sp.trunkR*1.1, sp.trunkR*1.2, sp.bare, sides, bark)
		blobAt(p, 0, sp.bare, 0, sp.trunkR*1.5, sp.trunkR*0.9, bark, 0.4, 3, 6)
		if far {
			// Resting on the head (owner, 2026-10-10: flattened below, it
			// floated above it).
			blobAt(p, 0, sp.bare+1.4, 0, sp.crownR*0.8, 1.4, leaf, 0, 3, 6)
			return
		}
		const shoots = 14
		for k := range shoots {
			az := 2*math.Pi*float64(k)/shoots + 0.5*(r()-0.5)
			rise := (58 + 25*r()) * math.Pi / 180
			length := 2.6 + 0.9*r()
			dx, dy, dz := math.Cos(az)*math.Cos(rise), math.Sin(rise), math.Sin(az)*math.Cos(rise)
			x0, z0 := math.Cos(az)*sp.trunkR, math.Sin(az)*sp.trunkR
			limb(p, x0, sp.bare+0.2, z0, x0+dx*length, sp.bare+0.2+dy*length, z0+dz*length, 0.05, 0.015, 4, bark)
			for _, f := range []float64{0.55, 0.85} {
				blobAt(p, x0+dx*length*f, sp.bare+0.2+dy*length*f, z0+dz*length*f, sp.clumpR*0.45, sp.clumpR*0.7, shade(leaf, 0.85+0.3*r()), 0.3, 3, 5)
			}
		}
		return
	case far:
		// The simple one: the trunk into the crown, a crown of one or two
		// clumps in the species' shape.
		top := sp.h - sp.crownR*0.9
		cone(p, 0, 0, sp.trunkR, sp.trunkR*0.7, top+sp.crownR*0.5, sides, bark)
		if sp.pine {
			blobAt(p, 0, sp.h-sp.crownR*0.35, 0, sp.crownR, sp.crownR*0.4, leaf, 1, 3, 6)
		} else {
			blobAt(p, 0, top+sp.crownR*0.2, 0, sp.crownR, sp.crownR*0.85, leaf, 1, 3, 6)
		}
		return
	}
	// A trunk up into the crown, its major branches from where it goes
	// bare, each ending in a clump of leaves (and some along it), and a
	// clump at the top of the trunk: the crown is on its branches.
	trunkTop := sp.h - sp.clumpR*1.2
	blobAt(p, 0, trunkTop, 0, sp.clumpR*1.2, sp.clumpR, leaf, 0.3, 3, 6)
	// Where the branches leave the trunk, lowest first.
	type branch struct{ az, y0, rise, length float64 }
	bs := make([]branch, sp.branches)
	for b := range bs {
		az := 2*math.Pi*float64(b)/float64(sp.branches) + 0.8*(r()-0.5)
		y0 := sp.bare + (trunkTop-sp.bare)*(0.15+0.7*float64(b)/float64(sp.branches))*(0.85+0.3*r())
		if sp.pine {
			y0 = sp.bare + (sp.h-sp.bare)*0.15*r() + (sp.h-sp.bare)*0.5*float64(b)/float64(sp.branches)
		}
		y0 = math.Min(y0, trunkTop-0.5)
		bs[b] = branch{az, y0, (sp.rise + 15*(r()-0.5)) * math.Pi / 180, sp.crownR * sp.spread * (0.8 + 0.4*r())}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].y0 < bs[j].y0 })
	// The trunk by Leonardo's rule (owner, 2026-10-09: trunks looked too
	// slim): its cross-section at any height is the sum of what it carries
	// above, so it keeps its girth up to the first branch and thins only
	// where a branch leaves it; each branch takes a share of the area, the
	// leader into the top clump a larger one. A little flare at the foot
	// and taper with height, as wood grows.
	const leader = 1.5
	total := leader + float64(len(bs))
	girth := func(share, y float64) float64 { return sp.trunkR * math.Sqrt(share/total) * (1 - 0.12*y/sp.h) }
	flare := math.Min(1, sp.bare*0.5)
	cone(p, 0, 0, sp.trunkR*1.15, girth(total, flare), flare, sides, bark)
	y, carried := flare, total
	for _, b := range bs {
		if b.y0 > y {
			cone(p, 0, y, girth(carried, y), girth(carried, b.y0), b.y0-y, sides, bark)
			y = b.y0
		}
		carried--
	}
	cone(p, 0, y, girth(carried, y), girth(carried, trunkTop)*0.6, trunkTop-y, sides, bark)
	branchR := sp.trunkR * math.Sqrt(1/total)
	for _, b := range bs {
		az, y0, rise, length := b.az, b.y0, b.rise, b.length
		dx, dy, dz := math.Cos(az)*math.Cos(rise), math.Sin(rise), math.Sin(az)*math.Cos(rise)
		x1, y1, z1 := dx*length, y0+dy*length, dz*length
		limb(p, 0, y0, 0, x1, y1, z1, branchR, branchR*0.3, 5, bark)
		// Big, round clumps that overlap their neighbours: one crown.
		cr := sp.clumpR * (0.85 + 0.3*r())
		hh := cr * 0.85
		if sp.pine {
			hh = cr * 0.5 // flatter, as a pine's crown is
		}
		blobAt(p, x1, y1+hh*0.2, z1, cr, hh, shade(leaf, 0.9+0.2*r()), 0.3, 3, 6)
		for c := range sp.clumps {
			f := 0.55 + 0.2*float64(c)
			blobAt(p, dx*length*f, y0+dy*length*f+cr*0.3, dz*length*f, cr*0.8, cr*0.7, shade(leaf, 0.85+0.2*r()), 0.3, 3, 6)
		}
	}
}

// disc adds a flat round face looking down at height y, radius r, of
// sides corners: a cone's open foot closed.
func disc(p *gltf.Primitive, x, y, r float64, sides int, c [3]float32) {
	base := uint32(len(p.Positions) / 3)
	p.Positions = append(p.Positions, float32(x), float32(y), 0)
	p.Normals = append(p.Normals, 0, -1, 0)
	p.Colors = append(p.Colors, c[0], c[1], c[2])
	for i := range sides + 1 {
		a := 2 * math.Pi * float64(i) / float64(sides)
		p.Positions = append(p.Positions, float32(x+r*math.Cos(a)), float32(y), float32(r*math.Sin(a)))
		p.Normals = append(p.Normals, 0, -1, 0)
		p.Colors = append(p.Colors, c[0], c[1], c[2])
	}
	for i := range sides {
		tri(p, base, base+1+uint32(i), base+2+uint32(i), [3]float64{0, -1, 0})
	}
}

// shade is a colour lighter or darker by k.
func shade(c [3]float32, k float64) [3]float32 {
	return [3]float32{c[0] * float32(k), c[1] * float32(k), c[2] * float32(k)}
}

// limb adds a tapering branch from (x0, y0, z0) to (x1, y1, z1), radius r0
// to r1, of sides faces.
func limb(p *gltf.Primitive, x0, y0, z0, x1, y1, z1, r0, r1 float64, sides int, c [3]float32) {
	ax, ay, az := x1-x0, y1-y0, z1-z0
	l := math.Sqrt(ax*ax + ay*ay + az*az)
	if l == 0 {
		return
	}
	ax, ay, az = ax/l, ay/l, az/l
	// Two directions across the branch.
	ux, uy, uz := -az, 0.0, ax
	if ul := math.Hypot(ux, uz); ul < 1e-6 {
		ux, uy, uz = 1, 0, 0
	} else {
		ux, uz = ux/ul, uz/ul
	}
	vx, vy, vz := ay*uz-az*uy, az*ux-ax*uz, ax*uy-ay*ux
	base := uint32(len(p.Positions) / 3)
	for i := range sides + 1 {
		a := 2 * math.Pi * float64(i) / float64(sides)
		nx, ny, nz := math.Cos(a)*ux+math.Sin(a)*vx, math.Cos(a)*uy+math.Sin(a)*vy, math.Cos(a)*uz+math.Sin(a)*vz
		for _, end := range []struct{ x, y, z, r float64 }{{x0, y0, z0, r0}, {x1, y1, z1, r1}} {
			p.Positions = append(p.Positions, float32(end.x+nx*end.r), float32(end.y+ny*end.r), float32(end.z+nz*end.r))
			p.Normals = append(p.Normals, float32(nx), float32(ny), float32(nz))
			p.Colors = append(p.Colors, c[0], c[1], c[2])
		}
	}
	for i := range sides {
		a := base + uint32(2*i)
		n := [3]float64{float64(p.Normals[3*a]), float64(p.Normals[3*a+1]), float64(p.Normals[3*a+2])}
		tri(p, a, a+2, a+1, n)
		tri(p, a+1, a+2, a+3, n)
	}
}

// blobAt adds a low-poly ellipsoid around (x, y, z), radius r across, half
// height hh, rings by sides; lift flattens its underside (a clump of
// leaves is flatter below).
func blobAt(p *gltf.Primitive, x, y, z, r, hh float64, c [3]float32, lift float64, rings, sides int) {
	base := uint32(len(p.Positions) / 3)
	for i := 0; i <= rings; i++ {
		v := math.Pi * float64(i) / float64(rings) // from the top
		for j := 0; j <= sides; j++ {
			u := 2 * math.Pi * float64(j) / float64(sides)
			nx, ny, nz := math.Sin(v)*math.Cos(u), math.Cos(v), math.Sin(v)*math.Sin(u)
			yy := y + hh*ny
			if ny < 0 {
				yy = y + hh*ny*(1-0.4*lift)
			}
			k := 1 + 0.15*math.Sin(3*u+float64(i)*1.7+x)
			p.Positions = append(p.Positions, float32(x+r*k*nx), float32(yy), float32(z+r*k*nz))
			p.Normals = append(p.Normals, float32(nx), float32(ny), float32(nz))
			s := float32(0.8 + 0.35*(ny+1)/2) // lighter on top
			p.Colors = append(p.Colors, c[0]*s, c[1]*s, c[2]*s)
		}
	}
	for i := range rings {
		for j := range sides {
			a := base + uint32(i*(sides+1)+j)
			b := a + uint32(sides+1)
			n := [3]float64{float64(p.Normals[3*a]), float64(p.Normals[3*a+1]), float64(p.Normals[3*a+2])}
			tri(p, a, b, a+1, n)
			tri(p, a+1, b, b+1, n)
		}
	}
}
