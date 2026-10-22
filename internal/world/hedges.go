package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Hedges: clipped hedges as segments (templates "hedge ...", hedgeSegM
// long along their x), laid every hedgeStepM along a road and turned with
// it, so neighbours overlap and a hedge follows a bend unbroken. Dutch
// roadside hedges are mostly hornbeam (haagbeuk, Carpinus betulus)
// clipped to 0.8-1 m; round a garden up to 2 m, in hornbeam, a conifer
// (Thuja, Taxus) or ivy (hedera) grown on a fence (owner, 2026-10-09).
// The map rarely has them (Rheden and De Steeg: 2.6 km of barrier=hedge
// in 24 km², 2026-10-09), so they are guessed: between roads side by side,
// and along front gardens.
type hedgeKind struct {
	name         string
	drawM        float64
	h            float64 // height at scale 1
	base, top    float64 // half width at the foot and at the top
	shoulder     float64 // rounding of the top's edges
	bump         float64 // how irregular the clipped faces are
	colour       uint32
	mottle, dark float64 // colour variation; shade at the foot (light gets in less)
	// region: the style whose worlds have it ("" every world); wall: a
	// low garden wall with a railing rather than a clipped hedge.
	region string
	wall   bool
}

var hedgeKinds = []hedgeKind{
	hHornbeam:     {name: "hedge", drawM: 250, h: 0.9, base: 0.32, top: 0.26, shoulder: 0.08, bump: 0.025, colour: 0x587c34, mottle: 0.2, dark: 0.7},
	hHornbeamTall: {name: "hedge tall", drawM: 350, h: 1.8, base: 0.42, top: 0.32, shoulder: 0.1, bump: 0.03, colour: 0x587c34, mottle: 0.2, dark: 0.65},
	hConifer:      {name: "hedge conifer", drawM: 350, h: 1.9, base: 0.45, top: 0.3, shoulder: 0.2, bump: 0.02, colour: 0x37542c, mottle: 0.12, dark: 0.75},
	hIvy:          {name: "hedge ivy", drawM: 350, h: 1.7, base: 0.2, top: 0.18, shoulder: 0.04, bump: 0.045, colour: 0x2b4621, mottle: 0.3, dark: 0.8},
	// A French front garden's (owner's shot of the Rue Paul Claudel): a
	// rendered wall 0.6 m with a coping, a white railing to 1.2 m.
	hWall: {name: "garden wall", drawM: 250, h: 1.2, colour: 0xd6d0c2, region: RegionFR, wall: true},
	// Its pillars (at its ends, its steps and either side of a gate) and
	// its gate, a white steel one between two pillars (owner, 2026-10-16,
	// La Bresse: walls along the whole property, a column at either end,
	// some with a gate between two).
	hPillar: {name: "garden pillar", drawM: 250, h: 1.4, colour: 0xd6d0c2, region: RegionFR, wall: true},
	hGate:   {name: "garden gate", drawM: 250, h: 1.2, colour: 0xeceae4, region: RegionFR, wall: true},
}

const (
	hHornbeam = iota
	hHornbeamTall
	hConifer
	hIvy
	hWall
	hPillar
	hGate
	hedgeKindCount
)

const (
	hedgeSegM  = 1.25 // a segment's length
	hedgeStepM = 1.0  // segments this far apart
	// Between roads: a strip between two roads side by side (a main road
	// and its service road) this wide has a low hedge down its middle
	// (owner, 2026-10-09: so they do in Rheden).
	hedgeMinM, hedgeMaxM = 1.0, 8.0
	// Front gardens: plots this long along the road, each its own hedge
	// or none, where a building stands within gardenDepthM behind the
	// hedge line.
	gardenPlotM = 11.0
	// gardenWallDepthM: a garden wall only where the house stands within
	// this behind it.
	gardenWallDepthM = 10.0
	gardenDepthM     = 25.0
)

// gardenMix: the share of front-garden plots with each kind of hedge;
// the rest have none (an open lawn, a fence, a wall).
var gardenMix = [hedgeKindCount]float64{hHornbeam: 0.3, hHornbeamTall: 0.12, hConifer: 0.14, hIvy: 0.08}

// hedgeTemplate is a segment of clipped hedge along x, standing on y = 0
// (its foot 0.2 m into the ground), irregular as clipped hedges are, light
// on top and darker towards the foot.
func hedgeTemplate(p *gltf.Primitive, k hedgeKind) {
	switch {
	case k.wall && k.name == "garden pillar":
		gardenPillarTemplate(p, k)
		return
	case k.wall && k.name == "garden gate":
		gardenGateTemplate(p, k)
		return
	case k.wall:
		gardenWallTemplate(p, k)
		return
	}
	mid := (k.base + k.top) / 2
	// The cross-section, z across and y up, from one foot over the top to
	// the other.
	prof := [][2]float64{
		{-k.base, -0.2}, {-mid, k.h * 0.5}, {-k.top, k.h - k.shoulder}, {-(k.top - 0.6*k.shoulder), k.h},
		{0, k.h + 0.01},
		{k.top - 0.6*k.shoulder, k.h}, {k.top, k.h - k.shoulder}, {mid, k.h * 0.5}, {k.base, -0.2},
	}
	norm := make([][2]float64, len(prof))
	for i := range prof {
		var nz, ny float64
		for _, j := range []int{i - 1, i} {
			if j < 0 || j+1 >= len(prof) {
				continue
			}
			dz, dy := prof[j+1][0]-prof[j][0], prof[j+1][1]-prof[j][1]
			l := math.Hypot(dz, dy)
			nz, ny = nz-dy/l, ny+dz/l
		}
		l := math.Hypot(nz, ny)
		norm[i] = [2]float64{nz / l, ny / l}
	}
	base := splitmix(uint64(len(k.name)) ^ math.Float64bits(k.h))
	noise := func(st, i int, salt uint64) float64 {
		return unit(splitmix(base^uint64(st)<<16^uint64(i)<<4^salt)) - 0.5
	}
	const stations = 5
	pos := make([][][3]float64, stations)
	col := func(st, i int, y float64) [3]float32 {
		c := srgbLinear(k.colour)
		shade := k.dark + (1-k.dark)*math.Max(0, math.Min(1, y/k.h))
		if norm[i][1] > 0.7 {
			shade *= 1.12 // the clipped top catches the light
		}
		shade *= 1 + k.mottle*noise(st, i, 0xc0)
		return [3]float32{c[0] * float32(shade), c[1] * float32(shade), c[2] * float32(shade)}
	}
	for st := range stations {
		x := -hedgeSegM/2 + hedgeSegM*float64(st)/(stations-1)
		pos[st] = make([][3]float64, len(prof))
		for i, q := range prof {
			b := k.bump * 2 * noise(st, i, 0xb0)
			if q[1] < 0 {
				b = 0 // underground
			}
			pos[st][i] = [3]float64{x, q[1] + b*norm[i][1], q[0] + b*norm[i][0]}
		}
	}
	vert := func(q [3]float64, n [3]float64, c [3]float32) uint32 {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		p.Colors = append(p.Colors, c[0], c[1], c[2])
		return uint32(len(p.Positions)/3 - 1)
	}
	// The sides and top.
	first := uint32(len(p.Positions) / 3)
	for st := range stations {
		for i := range prof {
			vert(pos[st][i], [3]float64{0, norm[i][1], norm[i][0]}, col(st, i, prof[i][1]))
		}
	}
	idx := func(st, i int) uint32 { return first + uint32(st*len(prof)+i) }
	for st := range stations - 1 {
		for i := range len(prof) - 1 {
			n := [3]float64{0, norm[i][1] + norm[i+1][1], norm[i][0] + norm[i+1][0]}
			tri(p, idx(st, i), idx(st+1, i), idx(st+1, i+1), n)
			tri(p, idx(st, i), idx(st+1, i+1), idx(st, i+1), n)
		}
	}
	// The ends (seen where a hedge stops, at a gate or a corner).
	for _, st := range []int{0, stations - 1} {
		n := [3]float64{-1, 0, 0}
		if st > 0 {
			n[0] = 1
		}
		c := col(st, 0, k.h*0.4)
		centre := vert([3]float64{pos[st][0][0], k.h / 2, 0}, n, c)
		ring := make([]uint32, len(prof))
		for i := range prof {
			ring[i] = vert(pos[st][i], n, c)
		}
		for i := range len(prof) - 1 {
			tri(p, centre, ring[i], ring[i+1], n)
		}
		tri(p, centre, ring[len(prof)-1], ring[0], n)
	}
}

// hedges places the hedges along the roads: in the strip between two
// roads side by side, and along front gardens in built-up land.
func hedges(t *terrain, s *surface, sp *spots, wp *wayPlaces) []plant {
	sty := styleFor(t.o.Region)
	var out []plant
	heading := func(i int) (float64, float64) {
		a, b := t.fine[max(0, i-1)], t.fine[min(len(t.fine)-1, i+1)]
		if a.line != b.line {
			a, b = t.fine[i], t.fine[min(len(t.fine)-1, i+1)]
			if a.line != b.line {
				a, b = t.fine[max(0, i-1)], t.fine[i]
			}
		}
		de, dn := b.e-a.e, b.n-a.n
		l := math.Hypot(de, dn)
		if l == 0 {
			return 0, 0
		}
		return de / l, dn / l
	}
	put := func(kind int, e, n, de, dn, scale float64, h uint64) {
		// A centimetre or two between neighbours, so overlapping faces
		// never lie in one plane.
		scale *= 0.985 + 0.03*unit(h)
		out = append(out, plant{kind: kHedge + kind, x: float32(e), y: float32(s.stand(e, n)), z: float32(-n),
			s: float32(scale), yaw: float32(math.Atan2(dn, de))})
	}
	free := func(e, n, margin float64) bool {
		return sp.roadNear(e, n, margin, -1) < 0 && !sp.inArea(e, n) && !sp.byMotorway(e, n) &&
			(t.surfaces == nil || t.surfaces.dist(e, -n, margin) >= margin)
	}
	type plotKey struct {
		way  uint64
		side int
		plot int64
	}
	plots := map[plotKey]int{}
	houseOf := map[plotKey]int{}
	// Wall plots' points, laid out per property after (propertyWalls).
	type wallPt struct {
		key         uint64
		side, house int
		along, e, n float64
		de, dn      float64
	}
	var wallPts []wallPt

	for i := 0; i+1 < len(t.fine); i++ {
		a, b := t.fine[i], t.fine[i+1]
		if a.line != b.line || a.way != b.way || a.onPatch || b.onPatch || a.aloft {
			continue
		}
		de, dn := heading(i)
		key, sa, against, ok := wp.at(a, b.e-a.e, b.n-a.n)
		_, sb, _, okb := wp.at(b, b.e-a.e, b.n-a.n)
		if !ok || !okb || sa == sb {
			continue
		}
		// at is the point f of the way from a to b, off to the side.
		at := func(f, rx, rn, off float64) (float64, float64) {
			return a.e + f*(b.e-a.e) + rx*off, a.n + f*(b.n-a.n) + rn*off
		}
		for side, sign := range []float64{-1, 1} {
			if a.open[side] {
				continue // a junction's mouth
			}
			wside := side // the way's own left or right
			if against {
				wside = 1 - side
			}
			rx, rn := sign*dn, -sign*de
			gap, other := -1.0, -1
			for g := hedgeMinM; g <= hedgeMaxM; g += 0.5 {
				if k := sp.roadNear(a.e+rx*(a.edge+g), a.n+rn*(a.edge+g), 0, a.line); k >= 0 {
					gap, other = g, k
					break
				}
			}
			if gap >= 0 {
				if a.walk[side] || !sty.medianHedges {
					continue // the strip is the sidewalk's verge
				}
				// One of the two roads plants the strip: the one whose way
				// comes first (by place, so the same whichever lines draw them).
				o := t.fine[other]
				if okey, _, _, ok := wp.at(o, 0, 0); ok && okey < key {
					continue
				}
				if oe, on := heading(other); math.Abs(oe*de+on*dn) < 0.95 {
					continue // a road crossing or joining, not alongside
				}
				// The strip exactly: across to the other road's line.
				gap = math.Abs((o.e-a.e)*rx+(o.n-a.n)*rn) - a.edge - o.edge
				if gap < hedgeMinM {
					continue
				}
				off := a.edge + gap/2
				for k := math.Ceil(math.Min(sa, sb) / hedgeStepM); k*hedgeStepM < math.Max(sa, sb); k++ {
					e, n := at((k*hedgeStepM-sa)/(sb-sa), rx, rn, off)
					if sp.roadNear(e, n, 0.4, -1) >= 0 || sp.inArea(e, n) || sp.byMotorway(e, n) {
						continue // a motorway's central reservation is grass
					}
					// 0.8-1 m, its height easing from one 20 m stretch's to
					// the next's (a step showed where it jumped).
					st := k * hedgeStepM / 20
					hs := func(i float64) uint64 {
						return splitmix(splitmix(key) ^ uint64(int64(i))<<2 ^ uint64(wside) ^ 0x5eed)
					}
					f := st - math.Floor(st)
					f = f * f * (3 - 2*f)
					scale := (0.9 + 0.2*(unit(hs(math.Floor(st)))*(1-f)+unit(hs(math.Floor(st)+1))*f)) * math.Min(1, gap/1.3)
					put(hHornbeam, e, n, de, dn, scale, splitmix(hs(st)^uint64(int64(k))))
				}
				continue
			}

			// Front gardens: the hedge just behind the sidewalk, or a
			// verge's width off the road.
			off := a.hw + a.wide[side] + 1.2
			if a.walk[side] {
				off = a.hw + a.wide[side] + a.walkWidth(side) + 0.6
			}
			for k := math.Ceil(math.Min(sa, sb) / hedgeStepM); k*hedgeStepM < math.Max(sa, sb); k++ {
				along := k * hedgeStepM
				plot := int64(math.Floor(along / gardenPlotM))
				hp := splitmix(splitmix(key) ^ uint64(plot)<<3 ^ uint64(wside) ^ 0x9a4d)
				pk := plotKey{key, wside, plot}
				kind, seen := plots[pk]
				if !seen {
					kind = gardenKind(sty.gardens, hp)
					if kind >= 0 {
						// A house behind the plot's middle, on this side of
						// any other road.
						me, mn := at(((float64(plot)+0.5)*gardenPlotM-sa)/(sb-sa), rx, rn, off)
						house, depth := -1, 0.0
						for r := 1.5; r <= gardenDepthM && house < 0; r += 1.5 {
							pe, pn := me+rx*r, mn+rn*r
							if sp.roadNear(pe, pn, 0, -1) >= 0 {
								break
							}
							house, depth = sp.buildingAt(pe, pn), r
						}
						// A wall fronts the house just behind it, not a
						// meadow with a house far back (owner's mark at
						// 5.34 km, 2026-10-16).
						if house < 0 || kind == hWall && depth > gardenWallDepthM {
							kind = -1
						}
						houseOf[pk] = house
					}
					plots[pk] = kind
				}
				if kind < 0 {
					continue
				}
				if kind == hWall {
					// A wall runs along the whole property; its gate is its own.
					e, n := at((along-sa)/(sb-sa), rx, rn, off)
					if l, _ := sp.landAt(e, n); l == scenery.LandBuilt && free(e, n, 0.4) {
						wallPts = append(wallPts, wallPt{key, wside, houseOf[pk], along, e, n, de, dn})
					}
					continue
				}
				// Most plots have a gate or a drive at their start.
				if into := along - float64(plot)*gardenPlotM; unit(hp>>20) < 0.7 && into < 1+2.5*unit(hp>>36)+hedgeSegM/2 {
					continue
				}
				e, n := at((along-sa)/(sb-sa), rx, rn, off)
				if l, _ := sp.landAt(e, n); l != scenery.LandBuilt || !free(e, n, 0.4) {
					continue
				}
				scale := 0.9 + 0.2*unit(hp>>44)
				if kind == hHornbeam {
					scale = 0.88 + 0.22*unit(hp>>44) // 0.8-1 m
				}
				put(kind, e, n, de, dn, scale, splitmix(hp^uint64(int64(k))))
			}
		}
	}
	// The walls, property by property: a run of points along one way's
	// side with one house behind them.
	sort.SliceStable(wallPts, func(i, j int) bool {
		a, b := wallPts[i], wallPts[j]
		if a.key != b.key {
			return a.key < b.key
		}
		if a.side != b.side {
			return a.side < b.side
		}
		return a.along < b.along
	})
	for i := 0; i < len(wallPts); {
		j := i
		for j+1 < len(wallPts) {
			a, b := wallPts[j], wallPts[j+1]
			if a.key != b.key || a.side != b.side || a.house != b.house || b.along-a.along > 1.5*hedgeStepM {
				break
			}
			j++
		}
		run := make([]wallSpot, 0, j-i+1)
		for _, w := range wallPts[i : j+1] {
			run = append(run, wallSpot{e: w.e, n: w.n, de: w.de, dn: w.dn, ground: s.stand(w.e, w.n)})
		}
		h := splitmix(wallPts[i].key ^ uint64(wallPts[i].side)<<1 ^ uint64(int64(wallPts[i].along*10))<<2 ^ 0x3a11)
		for _, p := range propertyWall(run, h) {
			out = append(out, plant{kind: kHedge + p.kind, x: float32(p.e), y: float32(p.y), z: float32(-p.n), s: 1, yaw: float32(p.yaw)})
		}
		i = j + 1
	}
	return out
}

// wallSpot is a point of a property's frontage, a metre apart: where,
// which way the road runs, the ground there.
type wallSpot struct{ e, n, de, dn, ground float64 }

// wallPiece is a placed piece of a property wall.
type wallPiece struct {
	kind    int // hWall, hPillar, hGate
	e, n, y float64
	yaw     float64
}

const (
	wallMinM     = 3.0  // a property's frontage shorter than this has none
	wallGateM    = 3.0  // a gate's width between its pillars
	wallGateFrac = 0.55 // the share of properties with a gate
	// The wall's top stays level, its base sunk into the ground or
	// standing on a footing out of it, within these (m); past them it
	// steps, a pillar at the step.
	wallSinkM, wallRaiseM = 0.3, 0.6
)

// propertyWall lays a property's wall along run (a point a metre apart,
// hedgeStepM): pillars at both ends, a gate between two pillars in some
// (seeded h), the segments between at a level that steps where the ground
// climbs or falls too far (owner, 2026-10-16: the walls stood as facades
// at the ground's every slope, with no end).
func propertyWall(run []wallSpot, h uint64) []wallPiece {
	if float64(len(run)-1)*hedgeStepM < wallMinM {
		return nil
	}
	yaw := func(i int) float64 { return math.Atan2(run[i].dn, run[i].de) }
	var out []wallPiece
	pillar := func(i int, y float64) {
		out = append(out, wallPiece{hPillar, run[i].e, run[i].n, y, yaw(i)})
	}
	// The gate: its pillars at points g0 and g0+3, clear of the ends.
	g0, g1 := -1, -1
	gate := int(math.Round(wallGateM / hedgeStepM))
	if unit(h) < wallGateFrac && len(run) >= gate+5 {
		g0 = 2 + int(unit(h>>16)*float64(len(run)-gate-4))
		g1 = g0 + gate
	}
	level := run[0].ground + 0.1
	pillar(0, level)
	for i := 0; i < len(run); i++ {
		g := run[i].ground
		if i > 0 && (level-g > wallRaiseM || g-level > wallSinkM) {
			// A step: a pillar at the step, the next stretch at its own level.
			level = g + 0.1
			pillar(i, level)
		}
		switch {
		case i == g0:
			pillar(i, level)
			mid := (g0 + g1) / 2
			if g1 < len(run) {
				me, mn := (run[g0].e+run[g1].e)/2, (run[g0].n+run[g1].n)/2
				out = append(out, wallPiece{hGate, me, mn, level, yaw(mid)})
			}
		case i > g0 && i < g1:
			continue // the gateway
		case i == g1:
			pillar(i, level)
		}
		if i == g0 || i+1 >= len(run) {
			continue
		}
		// A segment from here to the next point (its 1.25 m overlaps the
		// next a little).
		out = append(out, wallPiece{hWall, (run[i].e + run[i+1].e) / 2, (run[i].n + run[i+1].n) / 2, level, yaw(i)})
	}
	pillar(len(run)-1, level)
	return out
}

// gardenKind is the hedge (if any: -1) a front-garden plot seeded h has.
func gardenKind(gardens [hedgeKindCount]float64, h uint64) int {
	r := unit(h)
	for k, share := range gardens {
		if r < share {
			return k
		}
		r -= share
	}
	return -1
}

// solidBox adds a closed box (its four sides and top) of colour c.
func solidBox(p *gltf.Primitive, xa, xb, ya, yb, za, zb float64, c [3]float32) {
	q := func(a, b, cc, d, n [3]float64) { quad(p, a, b, cc, d, n, c) }
	q([3]float64{xa, ya, zb}, [3]float64{xb, ya, zb}, [3]float64{xb, yb, zb}, [3]float64{xa, yb, zb}, [3]float64{0, 0, 1})
	q([3]float64{xb, ya, za}, [3]float64{xa, ya, za}, [3]float64{xa, yb, za}, [3]float64{xb, yb, za}, [3]float64{0, 0, -1})
	q([3]float64{xa, yb, zb}, [3]float64{xb, yb, zb}, [3]float64{xb, yb, za}, [3]float64{xa, yb, za}, [3]float64{0, 1, 0})
	q([3]float64{xa, ya, za}, [3]float64{xa, ya, zb}, [3]float64{xa, yb, zb}, [3]float64{xa, yb, za}, [3]float64{-1, 0, 0})
	q([3]float64{xb, ya, zb}, [3]float64{xb, ya, za}, [3]float64{xb, yb, za}, [3]float64{xb, yb, zb}, [3]float64{1, 0, 0})
}

// gardenWallTemplate is a segment of garden wall along x, hedgeSegM long
// (neighbours overlap a little): a rendered wall 0.6 m high and 0.25
// thick under a coping, a closed solid, its footing reaching 1 m down (it
// stands level, so the ground falls away under it in places), and a white
// railing on it, bars every 12 cm to a top rail at k.h.
func gardenWallTemplate(p *gltf.Primitive, k hedgeKind) {
	wall, cope, rail := srgbLinear(k.colour), srgbLinear(0xb8b2a6), srgbLinear(0xeceae4)
	x0, x1 := -hedgeSegM/2, hedgeSegM/2
	box := func(xa, xb, ya, yb, za, zb float64, c [3]float32) { solidBox(p, xa, xb, ya, yb, za, zb, c) }
	box(x0, x1, -1.0, 0.6, -0.125, 0.125, wall)
	box(x0, x1, 0.6, 0.66, -0.16, 0.16, cope)
	for x := x0 + 0.06; x < x1; x += 0.12 {
		box(x-0.01, x+0.01, 0.66, k.h, -0.01, 0.01, rail)
	}
	box(x0, x1, k.h-0.03, k.h, -0.025, 0.025, rail)
	box(x0, x1, 0.74, 0.77, -0.015, 0.015, rail)
}

// gardenPillarTemplate is a wall's pillar: 0.4 m square, rendered, its
// footing 1 m down, to 1.3 m under a cap 0.5 m across.
func gardenPillarTemplate(p *gltf.Primitive, k hedgeKind) {
	wall, cope := srgbLinear(k.colour), srgbLinear(0xb8b2a6)
	solidBox(p, -0.2, 0.2, -1.0, k.h-0.1, -0.2, 0.2, wall)
	solidBox(p, -0.25, 0.25, k.h-0.1, k.h, -0.25, 0.25, cope)
}

// gardenGateTemplate is a white steel gate between a wall's pillars, along
// x, wallGateM between their middles: two leaves of bars every 12 cm
// between rails, from just over the ground to k.h, its frame heavier.
func gardenGateTemplate(p *gltf.Primitive, k hedgeKind) {
	c := srgbLinear(k.colour)
	half := wallGateM/2 - 0.22 // to the pillars' faces
	for _, side := range []float64{-1, 1} {
		xa, xb := math.Min(side*0.01, side*half), math.Max(side*0.01, side*half)
		solidBox(p, xa, xb, 0.08, 0.13, -0.025, 0.025, c)
		solidBox(p, xa, xb, k.h-0.05, k.h, -0.025, 0.025, c)
		solidBox(p, xa, xb, 0.55, 0.59, -0.02, 0.02, c)
		for _, x := range []float64{xa, xb - 0.04} {
			solidBox(p, x, x+0.04, 0.08, k.h, -0.03, 0.03, c)
		}
		for x := xa + 0.12; x < xb-0.06; x += 0.12 {
			solidBox(p, x-0.01, x+0.01, 0.13, k.h-0.05, -0.01, 0.01, c)
		}
	}
}
