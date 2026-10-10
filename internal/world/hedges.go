package world

import (
	"math"

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
}

var hedgeKinds = []hedgeKind{
	hHornbeam:     {name: "hedge", drawM: 250, h: 0.9, base: 0.32, top: 0.26, shoulder: 0.08, bump: 0.025, colour: 0x587c34, mottle: 0.2, dark: 0.7},
	hHornbeamTall: {name: "hedge tall", drawM: 350, h: 1.8, base: 0.42, top: 0.32, shoulder: 0.1, bump: 0.03, colour: 0x587c34, mottle: 0.2, dark: 0.65},
	hConifer:      {name: "hedge conifer", drawM: 350, h: 1.9, base: 0.45, top: 0.3, shoulder: 0.2, bump: 0.02, colour: 0x37542c, mottle: 0.12, dark: 0.75},
	hIvy:          {name: "hedge ivy", drawM: 350, h: 1.7, base: 0.2, top: 0.18, shoulder: 0.04, bump: 0.045, colour: 0x2b4621, mottle: 0.3, dark: 0.8},
}

const (
	hHornbeam = iota
	hHornbeamTall
	hConifer
	hIvy
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
	gardenPlotM  = 11.0
	gardenDepthM = 25.0
)

// gardenMix: the share of front-garden plots with each kind of hedge;
// the rest have none (an open lawn, a fence, a wall).
var gardenMix = [...]float64{hHornbeam: 0.3, hHornbeamTall: 0.12, hConifer: 0.14, hIvy: 0.08}

// hedgeTemplate is a segment of clipped hedge along x, standing on y = 0
// (its foot 0.2 m into the ground), irregular as clipped hedges are, light
// on top and darker towards the foot.
func hedgeTemplate(p *gltf.Primitive, k hedgeKind) {
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
		out = append(out, plant{kind: kHedge + kind, x: float32(e), y: float32(s.at(e, n)), z: float32(-n),
			s: float32(scale), yaw: float32(math.Atan2(dn, de))})
	}
	free := func(e, n, margin float64) bool {
		return sp.roadNear(e, n, margin, -1) < 0 && !sp.inArea(e, n) &&
			(t.surfaces == nil || t.surfaces.dist(e, -n, margin) >= margin)
	}
	type plotKey struct {
		way  uint64
		side int
		plot int64
	}
	plots := map[plotKey]int{}

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
				if a.walk[side] {
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
					if sp.roadNear(e, n, 0.4, -1) >= 0 || sp.inArea(e, n) {
						continue
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
				off = a.hw + a.wide[side] + walkM + 0.6
			}
			for k := math.Ceil(math.Min(sa, sb) / hedgeStepM); k*hedgeStepM < math.Max(sa, sb); k++ {
				along := k * hedgeStepM
				plot := int64(math.Floor(along / gardenPlotM))
				hp := splitmix(splitmix(key) ^ uint64(plot)<<3 ^ uint64(wside) ^ 0x9a4d)
				pk := plotKey{key, wside, plot}
				kind, seen := plots[pk]
				if !seen {
					kind = gardenKind(hp)
					if kind >= 0 {
						// A house behind the plot's middle, on this side of
						// any other road.
						me, mn := at(((float64(plot)+0.5)*gardenPlotM-sa)/(sb-sa), rx, rn, off)
						house := false
						for r := 1.5; r <= gardenDepthM && !house; r += 1.5 {
							pe, pn := me+rx*r, mn+rn*r
							if sp.roadNear(pe, pn, 0, -1) >= 0 {
								break
							}
							house = sp.inBuilding(pe, pn)
						}
						if !house {
							kind = -1
						}
					}
					plots[pk] = kind
				}
				if kind < 0 {
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
	return out
}

// gardenKind is the hedge (if any: -1) a front-garden plot seeded h has.
func gardenKind(h uint64) int {
	r := unit(h)
	for k, share := range gardenMix {
		if r < share {
			return k
		}
		r -= share
	}
	return -1
}
