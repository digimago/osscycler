package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// Dune plants (owner, 2026-10-10: dunes by the sea are sand with marram
// grass, sea buckthorn, brambles and other dune plants), on the dune land
// classes (scenery/dunes.go). As they grow in the Dutch coastal dunes:
//   - marram grass (helm, Ammophila arenaria): tufts of stiff rolled
//     leaves, 0.6-1 m, grey-green with straw-coloured dead ones, on the
//     sand, which it holds.
//   - sea buckthorn (duindoorn, Hippophae rhamnoides): the dunes' thicket,
//     a dense thorny shrub of silvery grey-green narrow leaves, 1-3 m,
//     shorn flat-topped by the wind.
//   - bramble (duinbraam, Rubus): a low sprawl of arching canes, 0.5-1 m
//     high and wider than tall, dark green.
//   - wild privet (wilde liguster, Ligustrum vulgare): upright, dense,
//     dark green, 2-3 m, in the older thickets.
//   - creeping willow (kruipwilg, Salix repens): a low grey-green mat,
//     0.3-0.6 m, in the dune valleys and on the grey dune.
//   - elder (vlier, Sambucus nigra): a large shrub, 3-5 m, of arching
//     stems with a broad flat crown, where the dunes are richer (near
//     rabbits' burrows and paths).
// Their places and mixes: vegetation.go and bushes.go.

func seaBuckthornTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 1.8 m: stiff stems fanning out from the foot, leafy from knee
	// height up in round clumps that overlap, the top uneven; wider than
	// tall. Bare stems under clumps at the top read as small trees, flat
	// clumps as bonsai, one big mound as a boulder.
	clump(p, 0, 0.75, 0, 0.6, 0.5, shade(leaf, 0.8), 0.2)
	for i := range 6 {
		a := 2*math.Pi*float64(i)/6 + 0.3*float64(i%3)
		x, z := around(a, 0.7+0.2*float64(i%3))
		y := 1.25 + 0.2*float64(i%4)
		limb(p, 0, 0, 0, x, y, z, 0.035, 0.015, 3, wood)
		clump(p, x*0.55, y*0.5, z*0.55, 0.45, 0.4, shade(leaf, 0.82+0.04*float64(i%3)), 0.2)
		clump(p, x, y, z, 0.42+0.06*float64(i%2), 0.38, shade(leaf, 0.92+0.05*float64(i%3)), 0.2)
	}
}

func brambleTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 0.8 m: a low mound, canes arching out of it to the ground, leaves
	// along them.
	clump(p, 0, 0.4, 0, 0.9, 0.4, leaf, 0.7)
	for i := range 6 {
		a := 2*math.Pi*float64(i)/6 + 0.35
		mx, mz := around(a, 0.8)
		ex, ez := around(a, 1.5+0.2*float64(i%2))
		limb(p, 0, 0.2, 0, mx, 0.85, mz, 0.015, 0.012, 3, wood)
		limb(p, mx, 0.85, mz, ex, 0.05, ez, 0.012, 0.008, 3, wood)
		clump(p, mx*0.95, 0.7, mz*0.95, 0.4, 0.25, shade(leaf, 0.9+0.07*float64(i%3)), 0.4)
		clump(p, (mx+ex)/2, 0.45, (mz+ez)/2, 0.3, 0.2, shade(leaf, 0.95), 0.4)
	}
}

func privetTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 2.5 m: several upright stems, leafy from knee height to the top,
	// narrower than tall.
	for i := range 4 {
		a := 2*math.Pi*float64(i)/4 + 0.3
		x, z := around(a, 0.35)
		limb(p, 0, 0, 0, x, 2.2, z, 0.04, 0.02, 3, wood)
	}
	for i, t := range [][2]float64{{0.75, 0.7}, {1.25, 0.8}, {1.75, 0.7}, {2.2, 0.5}} {
		x, z := around(float64(i)*1.9, 0.15)
		clump(p, x, t[0], z, t[1], 0.6*t[1]+0.15, shade(leaf, 0.9+0.05*float64(i)), 0.2)
	}
}

func creepingWillowTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 0.45 m: a low mat of small rounded clumps, spreading unevenly (one
	// flat clump read as a slab of stone).
	clump(p, 0, 0.25, 0, 0.45, 0.22, leaf, 0.5)
	for i := range 6 {
		x, z := around(2*math.Pi*float64(i)/6+0.6*float64(i%2), 0.5+0.2*float64(i%3))
		clump(p, x, 0.15+0.06*float64(i%3), z, 0.32+0.05*float64(i%2), 0.2, shade(leaf, 0.85+0.07*float64(i%3)), 0.5)
	}
}

func elderTemplate(p *gltf.Primitive, leaf, wood [3]float32) {
	// 4 m: stems arching out from one foot under a rounded crown that
	// comes down to about a metre at its edge.
	for i := range 5 {
		a := 2*math.Pi*float64(i)/5 + 0.25
		mx, mz := around(a, 0.45)
		x, z := around(a, 1.2+0.2*float64(i%2))
		y := 2.7 + 0.3*float64(i%3)
		limb(p, 0, 0, 0, mx, 1.6, mz, 0.07, 0.05, 4, wood)
		limb(p, mx, 1.6, mz, x, y, z, 0.05, 0.025, 3, wood)
		clump(p, x, y, z, 0.85, 0.65, shade(leaf, 0.9+0.06*float64(i%3)), 0.3)
		lx, lz := around(a+0.6, 1.3)
		clump(p, lx, 1.7, lz, 0.75, 0.55, shade(leaf, 0.86), 0.3)
	}
	clump(p, 0, 3.3, 0, 1.0, 0.6, shade(leaf, 1.05), 0.3)
}

// marramTemplate is a tuft of marram grass, 0.8 m: stiff leaves rising
// from a narrow foot and arching out, grey-green, about a third of them
// straw-coloured (dead); a few flower spikes standing over them.
func marramTemplate(p *gltf.Primitive) {
	green, straw, spike := srgbLinear(0x98a470), srgbLinear(0xc4b68a), srgbLinear(0xb8a878)
	rng := splitmix(0x4e1a)
	r := func() float64 { rng = splitmix(rng); return unit(rng) }
	for k := range 26 {
		a := 2 * math.Pi * r()
		d := 0.12 * math.Sqrt(r())
		x, z := d*math.Cos(a), d*math.Sin(a)
		// Leaning out from the middle, more the further out they start.
		la := a + 0.4*(r()-0.5)
		lean := 0.15 + 0.35*r()
		h := 0.55 + 0.35*r()
		c := green
		if k%3 == 0 {
			c = straw
		}
		mx, my, mz := x+0.45*lean*math.Cos(la), 0.6*h, z+0.45*lean*math.Sin(la)
		tx, ty, tz := x+lean*math.Cos(la), h*(0.9-0.4*lean), z+lean*math.Sin(la)
		blade(p, x, 0, z, mx, my, mz, tx, ty, tz, 0.018, 0.011, c)
	}
	for k := range 3 {
		a := 2*math.Pi*float64(k)/3 + 0.7
		x, z := 0.08*math.Cos(a), 0.08*math.Sin(a)
		limb(p, x, 0, z, x*2, 0.95, z*2, 0.008, 0.006, 3, spike)
		blobAt(p, x*2.05, 1.03, z*2.05, 0.025, 0.11, spike, 0, 2, 4)
	}
}

// blade is a leaf of three faces from a foot through a middle point to a
// tip: radius r0 at the foot, r1 in the middle, a point at the tip.
func blade(p *gltf.Primitive, x0, y0, z0, x1, y1, z1, x2, y2, z2, r0, r1 float64, c [3]float32) {
	base := uint32(len(p.Positions) / 3)
	ring := func(x, y, z, r float64, k float64) {
		for s := range 3 {
			b := 2*math.Pi*float64(s)/3 + 0.3
			p.Positions = append(p.Positions, float32(x+r*math.Cos(b)), float32(y), float32(z+r*math.Sin(b)))
			p.Normals = append(p.Normals, float32(math.Cos(b)), 0.3, float32(math.Sin(b)))
			p.Colors = append(p.Colors, c[0]*float32(k), c[1]*float32(k), c[2]*float32(k))
		}
	}
	ring(x0, y0, z0, r0, 0.75)
	ring(x1, y1, z1, r1, 0.95)
	p.Positions = append(p.Positions, float32(x2), float32(y2), float32(z2))
	p.Normals = append(p.Normals, 0, 1, 0)
	p.Colors = append(p.Colors, c[0]*1.05, c[1]*1.05, c[2]*1.05)
	for s := range uint32(3) {
		a, b := base+s, base+(s+1)%3
		p.Indices = append(p.Indices, a, b, a+3, b, b+3, a+3)
		p.Indices = append(p.Indices, a+3, b+3, base+6)
	}
}
