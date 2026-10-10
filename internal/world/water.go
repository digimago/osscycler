package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Plants of the water (owner, 2026-10-09): reeds along the banks of water
// wider than reedWideM, in clumps with gaps; lily pads on water larger
// than lilyAreaM2, in patches, standing on its surface.
const (
	reedWideM   = 4.0
	reedStepM   = 0.8
	lilyAreaM2  = 500.0
	lilyStepM   = 2.5
	lilyEdgeM   = 1.5 // pads keep this far off the bank
	waterReachM = 150.0
)

// waterPlants places the reeds and lily pads of the water areas near the
// route, in the chunks keys.
func waterPlants(t *terrain, land *scenery.LandMap, keys [][2]int, onRoad func(e, n float64) bool, routeDist func(e, n float64) float64) []plant {
	if land == nil || t.cover == nil {
		return nil
	}
	inKeys := map[[2]int]bool{}
	for _, k := range keys {
		inKeys[k] = true
	}
	chunkM := t.o.ChunkM
	here := func(e, n float64) bool {
		return inKeys[[2]int{int(math.Floor(e / chunkM)), int(math.Floor(n / chunkM))}] && routeDist(e, n) < waterReachM
	}
	var out []plant
	for _, id := range land.Waters() {
		area, outline := land.Size(id)
		if outline == 0 || 2*area/outline < reedWideM {
			continue // a ditch: nothing grows in it
		}
		is := func(e, n float64) bool { _, k := land.Area(e, n); return k == id }
		// Reeds just inside the bank, in clumps where an 8 m noise allows.
		for _, ed := range land.Edges(id) {
			l := math.Hypot(ed[2]-ed[0], ed[3]-ed[1])
			if l == 0 {
				continue
			}
			ux, uy := (ed[2]-ed[0])/l, (ed[3]-ed[1])/l
			for a := 0.0; a < l; a += reedStepM {
				e, n := ed[0]+ux*a, ed[1]+uy*a
				if !here(e, n) {
					continue
				}
				h := splitmix(uint64(int64(e*10))<<32 ^ uint64(int64(n*10)) ^ 0x4eed)
				if unit(splitmix(uint64(int64(math.Floor(e/8)))<<32^uint64(int64(math.Floor(n/8)))^0x7ee)) > 0.55 {
					continue
				}
				in := 0.4 + 1.2*unit(h)
				pe, pn := e-uy*in, n+ux*in
				if !is(pe, pn) {
					pe, pn = e+uy*in, n-ux*in
					if !is(pe, pn) {
						continue
					}
				}
				if onRoad(pe, pn) {
					continue
				}
				out = append(out, plant{kind: kReed, x: float32(pe), y: float32(t.cover.level(t, id, pe, pn) - 0.3), z: float32(-pn),
					s: float32(0.8 + 0.4*unit(h>>16)), yaw: float32(2 * math.Pi * unit(h>>24))})
			}
		}
		if area < lilyAreaM2 {
			continue
		}
		// Lily pads on a grid over the water, in patches, off the banks.
		minE, minN, maxE, maxN := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, ed := range land.Edges(id) {
			minE, maxE = math.Min(minE, math.Min(ed[0], ed[2])), math.Max(maxE, math.Max(ed[0], ed[2]))
			minN, maxN = math.Min(minN, math.Min(ed[1], ed[3])), math.Max(maxN, math.Max(ed[1], ed[3]))
		}
		if (maxE-minE)*(maxN-minN) > 4e6 {
			continue // a lake or a river's length: too much to cover
		}
		for e := minE; e < maxE; e += lilyStepM {
			for n := minN; n < maxN; n += lilyStepM {
				h := splitmix(uint64(int64(e*10))<<32 ^ uint64(int64(n*10)) ^ 0x1111)
				pe, pn := e+lilyStepM*unit(h), n+lilyStepM*unit(h>>16)
				if unit(splitmix(uint64(int64(math.Floor(pe/10)))<<32^uint64(int64(math.Floor(pn/10)))^0x1a1)) > 0.35 || unit(h>>32) > 0.6 {
					continue
				}
				if !here(pe, pn) || !is(pe, pn) {
					continue
				}
				off := false
				for _, d := range [][2]float64{{lilyEdgeM, 0}, {-lilyEdgeM, 0}, {0, lilyEdgeM}, {0, -lilyEdgeM}} {
					if !is(pe+d[0], pn+d[1]) {
						off = true
						break
					}
				}
				if off {
					continue
				}
				out = append(out, plant{kind: kLily, x: float32(pe), y: float32(t.cover.level(t, id, pe, pn) + 0.02), z: float32(-pn),
					s: float32(0.8 + 0.5*unit(h>>40)), yaw: float32(2 * math.Pi * unit(h>>48))})
			}
		}
	}
	return out
}

// reedTemplate is a clump of reeds about 2 m tall from its base (set
// 0.3 m under the water): thin three-sided blades, a few with a brown
// head.
func reedTemplate(p *gltf.Primitive) {
	green, tan, head := srgbLinear(0x5f7a3a), srgbLinear(0x9a9a5a), srgbLinear(0x5a3a22)
	rng := splitmix(0xbeed)
	r := func() float64 { rng = splitmix(rng); return unit(rng) }
	for k := range 28 {
		a := 2 * math.Pi * r()
		d := 0.6 * math.Sqrt(r())
		x, z := d*math.Cos(a), d*math.Sin(a)
		h := 1.6 + 0.8*r()
		lean := 0.15 * r()
		la := 2 * math.Pi * r()
		tx, tz := x+lean*math.Cos(la), z+lean*math.Sin(la)
		c := green
		if k%3 == 0 {
			c = tan
		}
		const w = 0.045
		base := uint32(len(p.Positions) / 3)
		for s := range 3 {
			b := 2 * math.Pi * float64(s) / 3
			p.Positions = append(p.Positions, float32(x+w*math.Cos(b)), 0, float32(z+w*math.Sin(b)))
			p.Normals = append(p.Normals, float32(math.Cos(b)), 0, float32(math.Sin(b)))
			p.Colors = append(p.Colors, shade(c, 0.8)[0], shade(c, 0.8)[1], shade(c, 0.8)[2])
		}
		p.Positions = append(p.Positions, float32(tx), float32(h), float32(tz))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, c[0], c[1], c[2])
		for s := range 3 {
			p.Indices = append(p.Indices, base+uint32(s), base+uint32((s+1)%3), base+3)
		}
		if k%4 == 0 {
			blobAt(p, tx*0.95, h*0.85, tz*0.95, 0.05, 0.18, head, 0, 2, 5)
		}
	}
}

// lilyTemplate is a patch of lily pads on the water (y 0), one flowering.
func lilyTemplate(p *gltf.Primitive) {
	pad, flower := srgbLinear(0x3d6b2a), srgbLinear(0xf2efe6)
	rng := splitmix(0x1117)
	r := func() float64 { rng = splitmix(rng); return unit(rng) }
	for k := range 5 {
		a := 2 * math.Pi * r()
		d := 0.6 * math.Sqrt(r())
		cx, cz := d*math.Cos(a), d*math.Sin(a)
		rad := 0.18 + 0.12*r()
		notch := 2 * math.Pi * r()
		base := uint32(len(p.Positions) / 3)
		p.Positions = append(p.Positions, float32(cx), 0, float32(cz))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, pad[0], pad[1], pad[2])
		const sides = 10
		for s := 0; s <= sides; s++ {
			b := notch + 0.35 + (2*math.Pi-0.7)*float64(s)/sides
			p.Positions = append(p.Positions, float32(cx+rad*math.Cos(b)), 0, float32(cz+rad*math.Sin(b)))
			p.Normals = append(p.Normals, 0, 1, 0)
			c := shade(pad, 0.85+0.3*r())
			p.Colors = append(p.Colors, c[0], c[1], c[2])
		}
		for s := range sides {
			tri(p, base, base+1+uint32(s), base+2+uint32(s), [3]float64{0, 1, 0})
		}
		if k == 0 {
			blobAt(p, cx, 0.05, cz, 0.08, 0.06, flower, 0, 2, 6)
		}
	}
}
