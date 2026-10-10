package world

import (
	"encoding/binary"
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Vegetation: trees, bushes and heather as instances of a few simple
// models (templates in the glTF, hidden), their places in a binary file
// beside it (InstancesFile: per instance x, y, z, scale, yaw as float32,
// in groups by kind and terrain chunk the manifest lists), so renderers
// draw thousands of them as one (Godot: a MultiMesh per group). Where
// they stand is seeded by the spot itself: the same every build. Never on
// a road, a building, a car park or water.
const (
	InstancesFile  = "instances.bin"
	instanceFloats = 5
	plantCellM     = 3.0   // candidate spots on a grid this fine, jittered
	plantNearM     = 100.0 // full density this close to the route, thinner beyond
	farDensity     = 0.35
	clearRoadM     = 1.5 // plants keep this far from a road's edge
	clearHouseM    = 1.5 // and from a building's walls
	avenueStepM    = 9.0 // trees along open roads, this far apart
	avenueStretchM = 150 // and of one species for this far
)

// Categories of plant: broadleaved and coniferous trees (their species
// chosen in trees.go), bushes, heather, marram grass.
const (
	catBroad = iota
	catConifer
	catBush
	catHeather
	catMarram
)

// plantRule is what grows on a land use: per category, the ground area
// each plant takes (m², 0: none) near the route; a candidate spot is one
// per plantCellM², so no category can take less.
type plantRule [5]float64

// A grown wood has 150-250 trees a hectare (owner, 2026-10-10: thin them
// at least a bit; they had about 370 in a mixed wood of average stand):
// forestTreeM2 each of a mixed wood's two kinds, a pure wood's own kind
// pureTreeM2 and the other pureOtherM2 (× standDensity: about 250/ha).
const (
	forestTreeM2 = 66.0
	pureTreeM2   = 35.0
	pureOtherM2  = 600.0
)

var plantRules = map[scenery.Land]plantRule{
	scenery.LandForest:  {catBroad: forestTreeM2, catConifer: forestTreeM2, catBush: 120},
	scenery.LandHeath:   {catConifer: 2500, catBroad: 4000, catBush: 220, catHeather: 9},
	scenery.LandBuilt:   {catBroad: 450, catConifer: 900, catBush: 160},
	scenery.LandMeadow:  {catBroad: 2500, catBush: 1200},
	scenery.LandNone:    {catBroad: 3000, catBush: 1500},
	scenery.LandOrchard: {catBroad: 30},
	// Dunes (scenery/dunes.go): marram on the sand, sea buckthorn and
	// brambles in thickets, low turf between; trees few and low (the
	// sea keeps them so: seaFactor).
	scenery.LandDuneSand:  {catConifer: 8000, catBush: 400, catMarram: 12},
	scenery.LandDuneGrass: {catConifer: 5000, catBroad: 5000, catBush: 160, catMarram: 45},
	scenery.LandDuneScrub: {catConifer: 3000, catBroad: 900, catBush: 13, catMarram: 90},
}

// poorGround: on these lands trees stay mostly small (heathSolitary).
var poorGround = map[scenery.Land]bool{scenery.LandHeath: true, scenery.LandDuneSand: true, scenery.LandDuneGrass: true, scenery.LandDuneScrub: true}

// heathSolitary: on heath (poor ground, owner 2026-10-09) this share of
// the trees stay small, bush-like; the rest grow full, solitary.
const heathSolitary = 0.9

// borderMixM: a tree this near another kind of land is, by a borderMix
// chance, one of that land's species (seeds blow across).
const (
	borderMixM = 12.0
	borderMix  = 0.35
)

// Manifest part: where the instances are.
type instanceGroup struct {
	Kind     string  `json:"kind"`
	Template string  `json:"template"` // the glTF node whose mesh to draw
	Chunk    [2]int  `json:"chunk"`
	Offset   int     `json:"offset"` // in instances
	Count    int     `json:"count"`
	DrawM    float64 `json:"draw_m"`
}

type plant struct {
	kind            int
	x, y, z, s, yaw float32
}

// plants places the vegetation: candidates on a jittered grid over each
// terrain chunk, kept by the land's rule there (thinner far from the
// route), off roads, buildings, car parks and water; and trees along open
// roads.
func plants(t *terrain, s *surface, land *scenery.LandMap, keys [][2]int, fps []scenery.Footprint, parks []scenery.Parking, ways []scenery.Way, route *routeGrid) []plant {
	sp := newSpots(t, land, fps, parks)
	roadNear, inArea := sp.roadNear, sp.inArea
	clearOf := func(e, n, m float64) bool {
		return roadNear(e, n, m, -1) < 0 && (t.surfaces == nil || t.surfaces.dist(e, -n, m) >= m)
	}
	onRoad := func(e, n float64) bool { return !clearOf(e, n, clearRoadM) }
	// The distance to the route, once per 30 m block (density only needs
	// to know near from far).
	blockDist := map[[2]int]float64{}
	routeDist := func(e, n float64) float64 {
		k := [2]int{int(math.Floor(e / 30)), int(math.Floor(n / 30))}
		if d, ok := blockDist[k]; ok {
			return d
		}
		ce, cn := (float64(k[0])+0.5)*30, (float64(k[1])+0.5)*30
		best := math.Inf(1)
		route.within(ce, cn, route.cell, func(i int) {
			best = math.Min(best, math.Hypot(route.fine[i].e-ce, route.fine[i].n-cn))
		})
		blockDist[k] = best
		return best
	}
	landAt := sp.landAt

	// Trees by the sea grow lower (seaFactor), looked up once per 30 m
	// block.
	blockSea := map[[2]int]float64{}
	seaAt := func(e, n float64) float64 {
		if t.o.SeaDistance == nil {
			return 1
		}
		k := [2]int{int(math.Floor(e / 30)), int(math.Floor(n / 30))}
		if f, ok := blockSea[k]; ok {
			return f
		}
		lat, lon := t.c.Unproject((float64(k[0])+0.5)*30, (float64(k[1])+0.5)*30)
		f := seaFactor(t.o.SeaDistance(lat, lon))
		blockSea[k] = f
		return f
	}
	var out []plant
	add := func(kind int, e, n float64, h uint64, scale float64) {
		size := plantSize(h, scale)
		if kind < speciesKinds {
			size *= seaAt(e, n)
		}
		out = append(out, plant{kind: kind, x: float32(e), y: float32(s.at(e, n)), z: float32(-n),
			s: float32(size), yaw: float32(2 * math.Pi * unit(h>>24))})
	}
	size := t.o.ChunkM
	cells := int(size / plantCellM)
	for _, key := range keys {
		e0, n0 := float64(key[0])*size, float64(key[1])*size
		for i := range cells {
			for j := range cells {
				h := splitmix(uint64(int64(key[0]*cells+i))<<32 ^ uint64(int64(key[1]*cells+j)))
				e := e0 + (float64(i)+unit(h))*plantCellM
				n := n0 + (float64(j)+unit(h>>16))*plantCellM
				l, leaf := landAt(e, n)
				rule, ok := plantRules[l]
				if !ok {
					continue
				}
				if l == scenery.LandForest {
					switch leaf {
					case "needleleaved":
						rule[catConifer], rule[catBroad] = pureTreeM2, pureOtherM2
					case "broadleaved":
						rule[catBroad], rule[catConifer] = pureTreeM2, pureOtherM2
					}
				}
				dist := routeDist(e, n)
				density := 1.0
				if dist > plantNearM {
					density = farDensity
				}
				stand := 1.0
				if l == scenery.LandForest {
					stand = standField(e, n) // stands.go
					density *= standDensity(stand)
				}
				// One draw decides the kind: each takes its share of the cell.
				r := unit(splitmix(h))
				kind := -1
				for k, per := range rule {
					if per <= 0 {
						continue
					}
					p := plantCellM * plantCellM / per * density
					if r < p {
						kind = k
						break
					}
					r -= p
				}
				if kind < 0 && unit(splitmix(h+11)) < regenChance(stand) {
					// A young tree where the stand lets light through,
					// its crown clear of the road (owner, 2026-10-10: young
					// firs, whose crown had counted as nothing, spread
					// their lowest tier over a forest path at head
					// height).
					sp, scale := youngTree(splitmix(h+13), leaf)
					if clearOf(e, n, math.Max(clearRoadM, allSpecies[sp].crownReach()*plantSize(splitmix(h+1), scale))) && !inArea(e, n) {
						add(treeKind(sp, formOpen, dist > treeNearM), e, n, splitmix(h+1), scale)
					}
					continue
				}
				if kind < 0 || onRoad(e, n) || inArea(e, n) {
					continue
				}
				scale := 1.0
				switch kind {
				case catBush:
					// A species of this land, its own shape and size (bushes.go).
					kb, sb, ok := pickBush(l, splitmix(h+5))
					if !ok {
						continue
					}
					kind, scale = kb, sb
					// Its whole spread off the road (owner, 2026-10-10: a
					// rhododendron 1.5× its size, its centre 1.85 m from a
					// cycle path's edge, spread over the path at 1.96 km of
					// the Amsterdam Water Supply Dunes preview).
					if !clearOf(e, n, math.Max(clearRoadM, bushReach(kb-kBush)*plantSize(splitmix(h+1), sb))) {
						continue
					}
				case catHeather:
					kind = kHeather
				case catMarram:
					kind = kMarram
				default:
					// A species: of this land, or now and then near a border
					// of the land next to it; an orchard's are small.
					mixLand := l
					hb := splitmix(h + 7)
					if unit(hb) < borderMix {
						for _, d := range [4][2]float64{{borderMixM, 0}, {-borderMixM, 0}, {0, borderMixM}, {0, -borderMixM}} {
							if nl, _ := landAt(e+d[0], n+d[1]); nl != l {
								if _, ok := speciesByLand[nl]; ok {
									mixLand = nl
									break
								}
							}
						}
					}
					sp := -1
					if l == scenery.LandOrchard && mixLand == l {
						sp, scale = 0, 0.4 // fruit trees: small and round, as a young oak
					} else {
						mix := speciesByLand[mixLand]
						table := mix.broad
						if kind == catConifer && mix.conifer != nil || table == nil {
							table = mix.conifer
						}
						sp = pickSpecies(table, unit(hb>>16))
					}
					if sp < 0 {
						continue
					}
					form := formOpen
					if l == scenery.LandForest {
						form = formForest
						scale *= standAge(e, n)
					}
					if poorGround[l] && unit(hb>>48) < heathSolitary {
						// Heath and dunes are poor ground: trees stay small, bush-like;
						// only now and then a solitary one grows full.
						scale *= 0.35 + 0.25*unit(hb>>32)
					}
					kind = treeKind(sp, form, dist > treeNearM)
				}
				add(kind, e, n, splitmix(h+1), scale)
			}
		}
	}

	// Avenues: trees along roads through open land, both sides, every
	// avenueStepM along their map way (placement keyed by place: place.go).
	wp := newWayPlaces(ways, t.fine)
	for i := 0; i+1 < len(t.fine); i++ {
		a, b := t.fine[i], t.fine[i+1]
		if a.line != b.line || a.way != b.way {
			continue
		}
		de, dn := b.e-a.e, b.n-a.n
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		key, sa, against, ok := wp.at(a, de, dn)
		_, sb, _, okb := wp.at(b, de, dn)
		if !ok || !okb || sa == sb {
			continue
		}
		for k := math.Ceil(math.Min(sa, sb) / avenueStepM); k*avenueStepM < math.Max(sa, sb); k++ {
			f := (k*avenueStepM - sa) / (sb - sa)
			pe, pn, edge := a.e+f*de, a.n+f*dn, a.edge+f*(b.edge-a.edge)
			for side, sign := range []float64{-1, 1} {
				wside := side // the way's own left or right
				if against {
					wside = 1 - side
				}
				h := splitmix(splitmix(key) ^ uint64(int64(k))<<1 ^ uint64(wside))
				off := edge + 2 + 1.5*unit(h)
				e, n := pe+sign*dn/l*off, pn-sign*de/l*off
				if lnd, _ := landAt(e, n); lnd != scenery.LandNone && lnd != scenery.LandMeadow && lnd != scenery.LandFarmland {
					continue
				}
				if unit(h>>32) > 0.45 || onRoad(e, n) || inArea(e, n) {
					continue
				}
				// One species along a stretch of road, as avenues are planted;
				// poplars too along minor country roads, more by water.
				hs := splitmix(splitmix(key) ^ uint64(int64(k*avenueStepM/avenueStretchM)))
				mix := avenueMix
				if a.way >= 0 && a.way < len(ways) && countryRoad[ways[a.way].Class] {
					mix = countryAvenueMix
					for _, s := range []float64{-1, 1} {
						for _, d := range []float64{edge + 6, edge + 12} {
							if w, _ := landAt(pe+s*dn/l*d, pn-s*de/l*d); w == scenery.LandWater {
								mix = canalAvenueMix
							}
						}
					}
				}
				sp := pickSpecies(mix, unit(hs))
				add(treeKind(sp, formOpen, routeDist(e, n) > treeNearM), e, n, splitmix(h+2), 1)
			}
		}
	}

	// addTurned is add facing a given way (yaw).
	addTurned := func(kind int, e, n float64, h uint64, scale, yaw float64) {
		add(kind, e, n, h, scale)
		out[len(out)-1].yaw = float32(yaw)
	}
	willows(land, keys, t.o.ChunkM, onRoad, inArea, routeDist, landAt, add, addTurned)
	out = append(out, waterPlants(t, land, keys, onRoad, routeDist)...)

	out = append(out, hedges(t, s, sp, wp)...)
	return clearOverhead(out, t.fine)
}

// Pollard willows (owner, 2026-10-10): in rows along some farmland ditches
// between plots: on one bank of narrow water (a ditch: 2·area/outline
// under reedWideM) between fields or meadows, willowStepM apart with a
// gap now and then, on willowShare of ditch stretches (chosen per 100 m by
// place), within willowReachM of the route.
const (
	willowLeanDeg = 7.0  // the leaning willow template's lean, towards +x
	willowLeaning = 0.65 // the share of willows that lean towards their ditch
	willowStepM   = 7.0
	willowBankM   = 1.3 // from the water's edge
	willowShare   = 0.35
	willowReachM  = 150.0
)

func willows(land *scenery.LandMap, keys [][2]int, chunkM float64, onRoad func(e, n float64) bool, inArea func(e, n float64) bool,
	routeDist func(e, n float64) float64, landAt func(e, n float64) (scenery.Land, string), add func(kind int, e, n float64, h uint64, scale float64),
	addTurned func(kind int, e, n float64, h uint64, scale, yaw float64)) {
	sp := speciesIndex("willow")
	if land == nil || sp < 0 {
		return
	}
	inKeys := map[[2]int]bool{}
	for _, k := range keys {
		inKeys[k] = true
	}
	for _, id := range land.Waters() {
		area, outline := land.Size(id)
		if outline == 0 || 2*area/outline >= reedWideM {
			continue // not a ditch
		}
		for _, ed := range land.Edges(id) {
			l := math.Hypot(ed[2]-ed[0], ed[3]-ed[1])
			if l < willowStepM {
				continue
			}
			ux, uy := (ed[2]-ed[0])/l, (ed[3]-ed[1])/l
			for a := willowStepM / 2; a < l; a += willowStepM {
				e, n := ed[0]+ux*a, ed[1]+uy*a
				if unit(splitmix(uint64(int64(math.Floor(e/100)))<<32^uint64(int64(math.Floor(n/100)))^0x3111)) > willowShare {
					continue
				}
				// The bank's land side; one bank only (facing north or east).
				nx, ny := uy, -ux
				if w, _ := landAt(e+nx*willowBankM, n+ny*willowBankM); w == scenery.LandWater {
					nx, ny = -nx, -ny
				}
				if nx+ny <= 0 {
					continue
				}
				pe, pn := e+nx*willowBankM, n+ny*willowBankM
				if lnd, _ := landAt(pe, pn); lnd != scenery.LandFarmland && lnd != scenery.LandMeadow {
					continue
				}
				if !inKeys[[2]int{int(math.Floor(pe / chunkM)), int(math.Floor(pn / chunkM))}] || routeDist(pe, pn) > willowReachM || onRoad(pe, pn) || inArea(pe, pn) {
					continue
				}
				h := splitmix(uint64(int64(pe*10))<<32 ^ uint64(int64(pn*10)) ^ 0x5a11)
				if unit(h>>32) > 0.85 {
					continue // a gap in the row
				}
				far := routeDist(pe, pn) > treeNearM
				if unit(h>>48) < willowLeaning && !far {
					// Leaning towards the water, give or take 25°: the
					// template leans to +x, which yaw turns to (cos, sin).
					yaw := math.Atan2(-ny, -nx) + (unit(h>>8)-0.5)*50*math.Pi/180
					addTurned(treeKind(sp, formForest, false), pe, pn, h, 0.85+0.3*unit(h>>40), yaw)
					continue
				}
				add(treeKind(sp, formOpen, far), pe, pn, h, 0.85+0.3*unit(h>>40))
			}
		}
	}
}

// By the sea woods grow low (owner, 2026-10-10: the forest in the dunes
// of the Amsterdam Water Supply Dunes course stood far too tall): wind and
// salt keep trees within seaLowM of the shore at seaLow of their height,
// growing to full height by seaFullM inland.
const (
	seaLowM  = 500.0
	seaFullM = 6000.0
	seaLow   = 0.4
)

// seaFactor is how tall trees grow at d metres from the sea.
func seaFactor(d float64) float64 {
	if d >= seaFullM {
		return 1
	}
	f := math.Max(0, (d-seaLowM)/(seaFullM-seaLowM))
	f = f * f * (3 - 2*f) // eased in and out
	return seaLow + (1-seaLow)*f
}

// plantSize is a plant's size: scale, and the spread of add (0.75-1.25)
// seeded by h.
func plantSize(h uint64, scale float64) float64 { return (0.75 + 0.5*unit(h>>8)) * scale }

// clearOverhead drops trees whose crown would reach through a road above
// them: a tree beside the lower road where two cross on a bridge.
func clearOverhead(ps []plant, fine []sample) []plant {
	g := newRouteGrid(fine, 20)
	kept := ps[:0]
	for _, p := range ps {
		if p.kind < speciesKinds {
			sp := allSpecies[p.kind/(treeVariants+1)]
			s := float64(p.s)
			reach, base, top := sp.crownReach()*s, float64(p.y)+2, float64(p.y)+sp.h*s+1
			e, n := float64(p.x), -float64(p.z)
			hit := false
			g.within(e, n, 20, func(i int) {
				a := fine[i]
				if !hit && a.ele > base && a.ele < top && math.Hypot(a.e-e, a.n-n) < reach+a.edge {
					hit = true
				}
			})
			if hit {
				continue
			}
		}
		kept = append(kept, p)
	}
	return kept
}

// unit is a hash's low bits as a number from 0 to 1.
func unit(h uint64) float64 { return float64(h&0xffff) / 65536 }

// polyEdgeDist is the distance from e, n to the polygon's outline.
func polyEdgeDist(poly [][2]float64, e, n float64) float64 {
	best := math.Inf(1)
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		de, dn := b[0]-a[0], b[1]-a[1]
		f := 0.0
		if l2 := de*de + dn*dn; l2 > 0 {
			f = math.Max(0, math.Min(1, ((e-a[0])*de+(n-a[1])*dn)/l2))
		}
		best = math.Min(best, math.Hypot(e-a[0]-f*de, n-a[1]-f*dn))
	}
	return best
}

// packPlants groups the plants by kind and chunk and packs them for
// InstancesFile.
func packPlants(ps []plant, chunkM float64) ([]byte, []instanceGroup) {
	type gk struct {
		kind int
		c    [2]int
	}
	groups := map[gk][]plant{}
	for _, p := range ps {
		k := gk{p.kind, [2]int{int(math.Floor(float64(p.x) / chunkM)), int(math.Floor(-float64(p.z) / chunkM))}}
		groups[k] = append(groups[k], p)
	}
	keys := make([]gk, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.c[1] != b.c[1] {
			return a.c[1] < b.c[1]
		}
		return a.c[0] < b.c[0]
	})
	var buf []byte
	var out []instanceGroup
	n := 0
	for _, k := range keys {
		g := groups[k]
		out = append(out, instanceGroup{Kind: plantKinds[k.kind].name, Template: "template " + plantKinds[k.kind].name,
			Chunk: k.c, Offset: n, Count: len(g), DrawM: plantKinds[k.kind].drawM})
		for _, p := range g {
			for _, f := range []float32{p.x, p.y, p.z, p.s, p.yaw} {
				buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(f))
			}
		}
		n += len(g)
	}
	return buf, out
}

// addTemplates adds the plant models, one hidden node each (extras kind
// "template"), coloured in their vertices: low-poly, as many are drawn.
func (w *World) addTemplates() error {
	mat := w.doc.AddMaterial(gltf.Material{Name: "vegetation", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.95})
	for k, kind := range plantKinds {
		p := gltf.Primitive{Material: mat}
		switch {
		case k < speciesKinds:
			v := k % (treeVariants + 1)
			addTreeTemplate(&p, allSpecies[k/(treeVariants+1)], v, v == treeVariants)
		case k < kHeather:
			bushTemplate(&p, allBushes[k-kBush])
		case k == kHeather:
			blob(&p, 0, 0.15, 0.6, 0.3, srgbLinear(0x7a5a72), 0)
		case k == kReed:
			reedTemplate(&p)
		case k == kLily:
			lilyTemplate(&p)
		case k == kMarram:
			marramTemplate(&p)
		case k >= kHedge:
			hedgeTemplate(&p, hedgeKinds[k-kHedge])
		}
		mesh, err := w.doc.AddMesh("template "+kind.name, p)
		if err != nil {
			return err
		}
		w.doc.AddNode("template "+kind.name, mesh, map[string]any{"kind": "template", "plant": kind.name})
	}
	return nil
}

// cone adds a cone (or a cylinder's frustum) of sides faces from y0 up h,
// radius r0 at its foot and r1 at its top, around x, z = 0 (plus offset x).
func cone(p *gltf.Primitive, x, y0, r0, r1, h float64, sides int, c [3]float32) {
	base := uint32(len(p.Positions) / 3)
	for i := range sides + 1 {
		a := 2 * math.Pi * float64(i) / float64(sides)
		ca, sa := math.Cos(a), math.Sin(a)
		slope := (r0 - r1) / h
		nl := math.Hypot(1, slope)
		for _, ring := range []struct{ r, y float64 }{{r0, y0}, {r1, y0 + h}} {
			p.Positions = append(p.Positions, float32(x+ring.r*ca), float32(ring.y), float32(ring.r*sa))
			p.Normals = append(p.Normals, float32(ca/nl), float32(slope/nl), float32(sa/nl))
			p.Colors = append(p.Colors, c[0], c[1], c[2])
		}
	}
	for i := range sides {
		a := base + uint32(2*i)
		tri(p, a, a+2, a+1, [3]float64{math.Cos(2 * math.Pi * (float64(i) + 0.5) / float64(sides)), 0, math.Sin(2 * math.Pi * (float64(i) + 0.5) / float64(sides))})
		tri(p, a+1, a+2, a+3, [3]float64{math.Cos(2 * math.Pi * (float64(i) + 0.5) / float64(sides)), 0, math.Sin(2 * math.Pi * (float64(i) + 0.5) / float64(sides))})
	}
}

// blob adds a low-poly ellipsoid around (0, y, 0): radius r across, half
// height hh; lift moves its lower half up (a crown, flatter below).
func blob(p *gltf.Primitive, x, y, r, hh float64, c [3]float32, lift float64) {
	const rings, sides = 4, 8
	base := uint32(len(p.Positions) / 3)
	for i := 0; i <= rings; i++ {
		v := math.Pi * float64(i) / rings // from the top
		for j := 0; j <= sides; j++ {
			u := 2 * math.Pi * float64(j) / sides
			nx, ny, nz := math.Sin(v)*math.Cos(u), math.Cos(v), math.Sin(v)*math.Sin(u)
			yy := y + hh*ny
			if ny < 0 {
				yy = y + hh*ny*(1-0.4*lift)
			}
			// A little irregular, as crowns are.
			k := 1 + 0.12*math.Sin(3*u+float64(i))
			p.Positions = append(p.Positions, float32(x+r*k*nx), float32(yy), float32(r*k*nz))
			p.Normals = append(p.Normals, float32(nx), float32(ny), float32(nz))
			shade := float32(0.85 + 0.3*(ny+1)/2) // lighter on top
			p.Colors = append(p.Colors, c[0]*shade, c[1]*shade, c[2]*shade)
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

// spots tells what is at a point near the route: a road (and which), a
// building or car park, the land use. Plants and the ground map share it,
// so they keep off the same things.
type spots struct {
	roads *routeGrid
	// roadNear is a road (other than line skip) whose edge is within
	// margin of e, n, measured to its line between samples (they are 2 m
	// apart): the sample hit, or -1.
	roadNear func(e, n, margin float64, skip int) int
	// inArea tells whether e, n is in or within clearHouseM of a building,
	// or in or by a car park.
	inArea func(e, n float64) bool
	// inBuilding tells whether e, n is inside a building's footprint.
	inBuilding func(e, n float64) bool
	// clearance is how far e, n is from the nearest road's edge (with its
	// sidewalks), building wall or car park edge: 0 on or in one, at most
	// max.
	clearance func(e, n, max float64) float64
	land      *scenery.LandMap
}

func newSpots(t *terrain, land *scenery.LandMap, fps []scenery.Footprint, parks []scenery.Parking) *spots {
	sp := &spots{roads: newRouteGrid(t.fine, 10), land: land}
	sp.roadNear = func(e, n, margin float64, skip int) int {
		hit := -1
		sp.roads.within(e, n, 10, func(i int) {
			if hit >= 0 {
				return
			}
			a := t.fine[i]
			if a.line == skip {
				return
			}
			for _, j := range []int{i - 1, i + 1} {
				if j < 0 || j >= len(t.fine) || t.fine[j].line != a.line {
					continue
				}
				b := t.fine[j]
				de, dn := b.e-a.e, b.n-a.n
				f := 0.0
				if l2 := de*de + dn*dn; l2 > 0 {
					f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
				}
				if math.Hypot(e-a.e-f*de, n-a.n-f*dn) < math.Max(a.edge, b.edge)+margin {
					hit = i
					return
				}
			}
		})
		return hit
	}
	// Buildings and car parks by 20 m cell.
	type area struct {
		poly     [][2]float64
		building bool
	}
	blocked := map[[2]int][]area{}
	addArea := func(poly [][2]float64, pad float64, building bool) {
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, q := range poly {
			lo[0], lo[1], hi[0], hi[1] = math.Min(lo[0], q[0]), math.Min(lo[1], q[1]), math.Max(hi[0], q[0]), math.Max(hi[1], q[1])
		}
		for gx := int(math.Floor((lo[0] - pad) / 20)); gx <= int(math.Floor((hi[0]+pad)/20)); gx++ {
			for gy := int(math.Floor((lo[1] - pad) / 20)); gy <= int(math.Floor((hi[1]+pad)/20)); gy++ {
				blocked[[2]int{gx, gy}] = append(blocked[[2]int{gx, gy}], area{poly, building})
			}
		}
	}
	for _, f := range fps {
		addArea(f.Outline, clearHouseM, true)
	}
	for _, p := range parks {
		addArea(p.Outline, 0, false)
	}
	sp.inBuilding = func(e, n float64) bool {
		for _, a := range blocked[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}] {
			if a.building && inPoly(a.poly, e, n) {
				return true
			}
		}
		return false
	}
	sp.inArea = func(e, n float64) bool {
		for _, a := range blocked[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}] {
			if inPoly(a.poly, e, n) || polyEdgeDist(a.poly, e, n) < clearHouseM {
				return true
			}
		}
		return false
	}
	sp.clearance = func(e, n, max float64) float64 {
		best := max
		if t.surfaces != nil {
			// Any road surface, junctions and roundabouts too; near ones
			// only (what grows needs a metre or two).
			best = math.Min(best, t.surfaces.dist(e, -n, math.Min(max, 3)))
			if best == 0 {
				return 0
			}
		}
		sp.roads.within(e, n, 10, func(i int) {
			a := t.fine[i]
			for _, j := range []int{i - 1, i + 1} {
				if j < 0 || j >= len(t.fine) || t.fine[j].line != a.line {
					continue
				}
				b := t.fine[j]
				de, dn := b.e-a.e, b.n-a.n
				f := 0.0
				if l2 := de*de + dn*dn; l2 > 0 {
					f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
				}
				edge := a.edge + f*(b.edge-a.edge)
				best = math.Min(best, math.Max(0, math.Hypot(e-a.e-f*de, n-a.n-f*dn)-edge))
			}
		})
		for _, a := range blocked[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}] {
			if inPoly(a.poly, e, n) {
				return 0
			}
			best = math.Min(best, polyEdgeDist(a.poly, e, n))
		}
		return best
	}
	return sp
}

// landAt is the land use at e, n, and its leaf type where the map says.
func (sp *spots) landAt(e, n float64) (scenery.Land, string) {
	if sp.land == nil {
		return scenery.LandNone, ""
	}
	l, id := sp.land.Area(e, n)
	return l, sp.land.LeafType(id)
}
