package world

import (
	"fmt"
	"math"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Quays (owner, 2026-10-16, mark at 0.06 km of the Route des Américains:
// the Moselotte through La Bresse lay in a deep grass channel, where its
// quays are masonry, dressed stone, on both sides): along a river in town
// whose bank stands well over the water, a stone wall from under the water
// to the bank's top, the land filled in behind it as behind a road's
// retaining wall. The style says where (quays).
const (
	quayStepM     = 2.0  // along the water's edge
	quayRiseM     = 1.2  // the bank this much over the water within quayBankM: a quay
	quayBankM     = 4.0  // where the bank's top is measured, from the water's edge
	quayTownM     = 25.0 // built-up land this near: in town
	quayMinWidthM = 4.0  // rivers and canals: the water's mean width within these
	quayMaxWidthM = 40.0
	quayMinAreaM2 = 1500.0
	quayFillM     = 10.0 // the land filled in behind it at most this far
)

// addQuays adds the quays along waters (the exact ones, from their
// outlines), a mesh per chunk: stone walls and the land behind them.
func (w *World) addQuays(t *terrain, waters []exactWater, landAt func(e, n float64) float64, ground int) error {
	st := styleFor(t.o.Region)
	if !st.quays || t.cover == nil || t.cover.m == nil || landAt == nil {
		return nil
	}
	stone := w.doc.AddMaterial(gltf.Material{Name: "stone wall", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.95})
	walls, fills := map[[2]int]*gltf.Primitive{}, map[[2]int]*gltf.Primitive{}
	prim := func(m map[[2]int]*gltf.Primitive, e, n float64, material int) *gltf.Primitive {
		k := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
		if m[k] == nil {
			m[k] = &gltf.Primitive{Material: material}
		}
		return m[k]
	}
	town := func(e, n float64) bool {
		for _, d := range [5][2]float64{{0, 0}, {quayTownM, 0}, {-quayTownM, 0}, {0, quayTownM}, {0, -quayTownM}} {
			if l, _ := t.cover.at(e+d[0], n+d[1]); l == scenery.LandBuilt {
				return true
			}
		}
		return false
	}
	onRoad := func(e, n float64) bool { return t.surfaces != nil && t.surfaces.dist(e, -n, 0.5) < 0.5 }
	for _, x := range waters {
		area, outline := t.cover.m.Size(x.id)
		if outline == 0 || area < quayMinAreaM2 {
			continue
		}
		if width := 2 * area / outline; width < quayMinWidthM || width > quayMaxWidthM {
			continue
		}
		ring := resampleRing(x.ring, quayStepM)
		n := len(ring)
		if n < 3 {
			continue
		}
		// The land side: right of the ring's way when it runs
		// counter-clockwise round the water.
		signed := 0.0
		for i := range ring {
			a, b := ring[i], ring[(i+1)%n]
			signed += a[0]*b[1] - b[0]*a[1]
		}
		side := 1.0
		if signed < 0 {
			side = -1
		}
		out := make([][2]float64, n) // the land side's unit normal
		level := make([]float64, n)
		top := make([]float64, n)
		on := make([]bool, n)
		for i := range ring {
			a, b := ring[(i+n-1)%n], ring[(i+1)%n]
			de, dn := b[0]-a[0], b[1]-a[1]
			l := math.Hypot(de, dn)
			if l == 0 {
				continue
			}
			out[i] = [2]float64{side * dn / l, -side * de / l}
			p := ring[i]
			level[i] = t.cover.level(t, x.id, p[0], p[1])
			be, bn := p[0]+out[i][0]*quayBankM, p[1]+out[i][1]*quayBankM
			top[i] = landAt(be, bn)
			// A road along the bank's top is a quai's own road: the quay holds
			// it up, its fill stops short of it (fillTo); only the water's
			// edge under a road (a bridge) has none.
			if onRoad(be, bn) {
				top[i] = landAt(p[0]+out[i][0]*1.5, p[1]+out[i][1]*1.5)
				for d := 0.5; d < quayBankM; d += 0.5 {
					if onRoad(p[0]+out[i][0]*d, p[1]+out[i][1]*d) {
						break
					}
					top[i] = math.Max(top[i], landAt(p[0]+out[i][0]*d, p[1]+out[i][1]*d))
				}
			}
			on[i] = top[i]-level[i] > quayRiseM && town(p[0], p[1]) && !onRoad(p[0], p[1])
		}
		for _, r := range runs(on) {
			// The top smoothed along the run, at the bank's height (never
			// over a road beside it: those cut the fill).
			k0 := r.from
			hs := make([]float64, r.to-r.from+1)
			for k := r.from; k <= r.to; k++ {
				sum, c := 0.0, 0
				for j := max(r.from, k-3); j <= min(r.to, k+3); j++ {
					sum, c = sum+top[j], c+1
				}
				hs[k-k0] = sum / float64(c)
			}
			p := prim(walls, ring[k0][0], ring[k0][1], stone)
			f := prim(fills, ring[k0][0], ring[k0][1], ground)
			c := srgbLinear(0x8a8478)
			for k := r.from; k < r.to; k++ {
				a, b := ring[k], ring[k+1]
				pt := func(q [2]float64, o [2]float64, off, y float64) [3]float64 {
					return [3]float64{q[0] + o[0]*off, y, -(q[1] + o[1]*off)}
				}
				// The face, leaning back a little, from under the water to the top.
				const batter = 0.08
				ya, yb := hs[k-k0], hs[k+1-k0]
				fa, fb := pt(a, out[k], 0, level[k]-0.4), pt(b, out[k+1], 0, level[k+1]-0.4)
				ta, tb := pt(a, out[k], batter*(ya-level[k]), ya), pt(b, out[k+1], batter*(yb-level[k+1]), yb)
				nrm := [3]float64{-(out[k][0] + out[k+1][0]) / 2, 0.1, (out[k][1] + out[k+1][1]) / 2}
				quad(p, fa, fb, tb, ta, nrm, c)
				// The coping, 0.4 m deep, back over the land.
				ca := pt(a, out[k], batter*(ya-level[k])+0.4, ya)
				cb := pt(b, out[k+1], batter*(yb-level[k+1])+0.4, yb)
				quad(p, ta, tb, cb, ca, [3]float64{0, 1, 0}, srgbLinear(0x76726a))
				// The land behind: from the coping on until it meets the bank.
				backA := fillTo(a, out[k], batter*(ya-level[k])+0.4, ya, landAt, onRoad)
				backB := fillTo(b, out[k+1], batter*(yb-level[k+1])+0.4, yb, landAt, onRoad)
				q0, q1 := ca, cb
				q2 := [3]float64{b[0] + out[k+1][0]*backB[0], backB[1], -(b[1] + out[k+1][1]*backB[0])}
				q3 := [3]float64{a[0] + out[k][0]*backA[0], backA[1], -(a[1] + out[k][1]*backA[0])}
				colA, _ := t.cover.at(a[0]+out[k][0]*2, a[1]+out[k][1]*2)
				lc := st.landColour(colA)
				// Two triangles, facing up.
				addUp(f, q0, q1, q2, lc)
				addUp(f, q0, q2, q3, lc)
			}
		}
	}
	add := func(m map[[2]int]*gltf.Primitive, kind string) error {
		keys := make(map[[2]int][]gltf.Primitive, len(m))
		for k, p := range m {
			keys[k] = []gltf.Primitive{*p}
		}
		for _, k := range chunkOrder(keys) {
			if p := m[k]; len(p.Indices) > 0 {
				if err := w.add(fmt.Sprintf("%s %d %d", kind, k[0], k[1]), map[string]any{"kind": kind, "chunk": k}, *p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := add(fills, "terrain"); err != nil {
		return err
	}
	return add(walls, "quays")
}

// fillTo is where the land filled in behind a quay's top at height top,
// from off along o past q, meets the bank (its offset and height): at most
// quayFillM on, or short of a road.
func fillTo(q, o [2]float64, off, top float64, landAt func(e, n float64) float64, onRoad func(e, n float64) bool) [2]float64 {
	for d := off + 0.5; d <= off+quayFillM; d += 0.5 {
		e, n := q[0]+o[0]*d, q[1]+o[1]*d
		if onRoad(e, n) {
			return [2]float64{d - 0.5, landAt(q[0]+o[0]*(d-0.5), q[1]+o[1]*(d-0.5)) + 0.03}
		}
		if z := landAt(e, n); z >= top-0.05 {
			return [2]float64{d, z + 0.03}
		}
	}
	d := off + quayFillM
	return [2]float64{d, landAt(q[0]+o[0]*d, q[1]+o[1]*d) + 0.03}
}

// addUp adds triangle a b c to p facing up, in colour col.
func addUp(p *gltf.Primitive, a, b, c [3]float64, col [3]float32) {
	ny := (b[2]-a[2])*(c[0]-a[0]) - (b[0]-a[0])*(c[2]-a[2])
	if ny < 0 {
		b, c = c, b
	}
	base := uint32(len(p.Positions) / 3)
	for _, q := range [3][3]float64{a, b, c} {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, col[0], col[1], col[2])
	}
	p.Indices = append(p.Indices, base, base+1, base+2)
}

// resampleRing is a closed ring's points every step metres along it.
func resampleRing(ring [][2]float64, step float64) [][2]float64 {
	n := len(ring)
	if n < 3 {
		return nil
	}
	var out [][2]float64
	next := 0.0 // the next point's distance along the ring
	run := 0.0
	for i := range ring {
		a, b := ring[i], ring[(i+1)%n]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		for ; l > 0 && next < run+l; next += step {
			f := (next - run) / l
			out = append(out, [2]float64{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f})
		}
		run += l
	}
	return out
}
