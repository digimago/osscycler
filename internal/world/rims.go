package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// A car park lies at the road's height where it meets one and on the
// ground further in, so along its outline it can stand above or below the
// land beside it (cut away under it): from the road the eye looked under
// the land's edge (owner, 2026-10-11, Zandvoort 12.9 km: "looking under
// terrain when leaving the junction"). A rim closes that edge: a bank face
// round the outline from the higher of car park and land to under the
// lower, grass-coloured, seen from either side.
const (
	rimStepM = 1.0  // points along the outline
	rimOutM  = 0.3  // the land is taken this far outside the outline
	rimDownM = 0.3  // the face reaches this far under the lower side
	rimUpM   = 0.01 // and this far over the higher
)

// rimMesh is the rim round outline: height is the car park's surface,
// land the land as drawn before the cut, road the road surfaces.
func rimMesh(outline [][2]float64, height func(e, n float64) float64, land, road *landSurface, material int) gltf.Primitive {
	p := gltf.Primitive{Material: material}
	if len(outline) < 3 {
		return p
	}
	grass := landColors[scenery.LandNone]
	out := 1.0 // the outside is right of the ring's direction when it runs counter-clockwise
	if ringArea(outline) < 0 {
		out = -1
	}
	type col struct {
		e, n, top, bot float64
		ok             bool
	}
	var cols []col
	for i := range outline {
		a, b := outline[i], outline[(i+1)%len(outline)]
		de, dn := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		oe, on := dn/l*out, -de/l*out // outward
		k := max(1, int(math.Ceil(l/rimStepM)))
		for j := range k {
			f := float64(j) / float64(k)
			e, n := a[0]+f*de, a[1]+f*dn
			c := col{e: e, n: n}
			pe, pn := e+oe*rimOutM, n+on*rimOutM
			// Beside a road the rim closes to the road's surface (a car park
			// standing a few centimetres off it showed a dark gap).
			hl, ok := road.at(pe, pn)
			if !ok {
				hl, ok = land.at(pe, pn)
			}
			if ok {
				hc := height(e, n)
				c.top, c.bot, c.ok = math.Max(hc, hl)+rimUpM, math.Min(hc, hl)-rimDownM, true
			}
			cols = append(cols, c)
		}
	}
	vert := func(e, y, n float64, ne, nn float64) uint32 {
		p.Positions = append(p.Positions, float32(e), float32(y), float32(-n))
		p.Normals = append(p.Normals, float32(ne), 0, float32(-nn))
		p.Colors = append(p.Colors, grass[0], grass[1], grass[2])
		return uint32(len(p.Positions)/3 - 1)
	}
	for i := range cols {
		a, b := cols[i], cols[(i+1)%len(cols)]
		if !a.ok || !b.ok {
			continue
		}
		de, dn := b.e-a.e, b.n-a.n
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		oe, on := dn/l*out, -de/l*out
		for _, s := range []float64{1, -1} { // seen from outside, then from the car park
			ne, nn := oe*s, on*s
			at, ab := vert(a.e, a.top, a.n, ne, nn), vert(a.e, a.bot, a.n, ne, nn)
			bt, bb := vert(b.e, b.top, b.n, ne, nn), vert(b.e, b.bot, b.n, ne, nn)
			n3 := [3]float64{ne, 0, -nn}
			tri(&p, at, ab, bt, n3)
			tri(&p, bt, ab, bb, n3)
		}
	}
	return p
}
