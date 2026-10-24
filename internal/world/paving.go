package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
)

// The made ground of an underpass the course's own way takes under a
// road's decks (owner, 2026-10-23: the slopes are artificial surfaces,
// single planes, not the land's creases; paved through the open strip
// between a dual carriageway's decks; no wing walls, those are a poured
// tunnel's): on each side of the way a level shoulder fillShoulderM wide,
// a paved slope at fillSlope, and a level berm back under the bank seats,
// at the highest seat's foot (a lower deck's seat stands deeper in it);
// along the way from the first deck's outer side to the last one's. The
// land lies under it, and beside it falls to its edge at fillSlope.

// pavedArea is one side of an underpass's made ground.
type pavedArea struct {
	quads          [][4][3]float64  // shoulder, slope, berm (glTF frame), each a plane
	sides          [2][4][3]float64 // its two ends along the way: edge, toe, crest, back
	e0, n0, e1, n1 float64          // its bounds (east, north), reach included
}

// pavedReachM: the land falls to an area's sides within this.
const pavedReachM = 12.0

// pavings are the underpasses' paved areas, from the decks over the
// course's own way in one (lines as drawn: samples aloft, own ways marked
// underpass).
func pavings(lines []*roadLine) []*pavedArea {
	below := roadsBelow(lines)
	type group struct {
		o0         sample
		way, nrm   [2]float64
		s          float64
		path       [][2]float64 // along the way, the way's height there
		aMin, aMax float64
		edge, berm float64
		back       float64
	}
	var groups []*group
	for _, l := range lines {
		ss := l.samples
		for i := 0; i+1 < len(ss); {
			if !onDeck(ss, i) {
				i++
				continue
			}
			j := i + 1
			for j+1 < len(ss) && onDeck(ss, j) {
				j++
			}
			u := below(l, i, j)
			if !u.o.underpass {
				i = j
				continue
			}
			o := u.o
			for _, k := range []int{i, j} {
				f := frameOf(l, ss[k])
				var g *group
				for _, h := range groups {
					side := (f.c[0]-h.o0.e)*h.nrm[0] + (-f.c[2]-h.o0.n)*h.nrm[1]
					if math.Hypot(h.o0.e-o.e, h.o0.n-o.n) < gUnderJoinM && math.Copysign(1, side) == h.s {
						g = h
						break
					}
				}
				if g == nil {
					nrm := [2]float64{-u.way[1], u.way[0]}
					mid := (f.c[0]-o.e)*nrm[0] + (-f.c[2]-o.n)*nrm[1]
					g = &group{o0: o, way: u.way, nrm: nrm, s: math.Copysign(1, mid), aMin: math.Inf(1), aMax: math.Inf(-1)}
					groups = append(groups, g)
				}
				// In g's frame: along its way, and which side.
				along := func(e, n float64) float64 { return (e-g.o0.e)*g.way[0] + (n-g.o0.n)*g.way[1] }
				g.path = append(g.path, [2]float64{along(o.e, o.n), o.ele})
				g.edge = math.Max(g.edge, o.edge)
				g.berm = math.Max(g.berm, f.c[1]-deckDepthM-bankSeatM)
				dist := math.Abs((f.c[0]-g.o0.e)*g.nrm[0] + (-f.c[2]-g.o0.n)*g.nrm[1])
				g.back = math.Max(g.back, dist+abutmentT)
				for _, x := range []float64{-f.left - edgeBeamW - underDeckPadM, f.right + edgeBeamW + underDeckPadM} {
					a := along(f.c[0]+f.r[0]*x, -(f.c[2] + f.r[2]*x))
					g.aMin, g.aMax = math.Min(g.aMin, a), math.Max(g.aMax, a)
				}
			}
			i = j
		}
	}
	var out []*pavedArea
	for _, g := range groups {
		sort.Slice(g.path, func(a, b int) bool { return g.path[a][0] < g.path[b][0] })
		ele := func(a float64) float64 {
			p := g.path
			if len(p) == 1 || a <= p[0][0] {
				return p[0][1]
			}
			for k := 1; k < len(p); k++ {
				if a <= p[k][0] || k == len(p)-1 {
					f := (a - p[k-1][0]) / math.Max(p[k][0]-p[k-1][0], 1e-9)
					return p[k-1][1] + f*(p[k][1]-p[k-1][1])
				}
			}
			return p[len(p)-1][1]
		}
		toeD := g.edge + fillShoulderM
		pt := func(a, d, y float64) [3]float64 {
			e := g.o0.e + g.way[0]*a + g.nrm[0]*g.s*d
			n := g.o0.n + g.way[1]*a + g.nrm[1]*g.s*d
			return [3]float64{e, y, -n}
		}
		ar := &pavedArea{e0: math.Inf(1), n0: math.Inf(1), e1: math.Inf(-1), n1: math.Inf(-1)}
		ok := true
		for si, a := range []float64{g.aMin, g.aMax} {
			ty := ele(a) - gSinkM
			crest := toeD + math.Max(0, g.berm-ty)/fillSlope
			if crest >= g.back-0.1 {
				ok = false // no room for a berm: the land as it is
			}
			ar.sides[si] = [4][3]float64{pt(a, g.edge, ty), pt(a, toeD, ty), pt(a, crest, g.berm), pt(a, g.back, g.berm)}
		}
		if !ok {
			continue
		}
		a, b := ar.sides[0], ar.sides[1]
		for q := 0; q < 3; q++ {
			ar.quads = append(ar.quads, [4][3]float64{a[q], b[q], b[q+1], a[q+1]})
		}
		for _, side := range ar.sides {
			for _, c := range side {
				ar.e0, ar.e1 = math.Min(ar.e0, c[0]-pavedReachM), math.Max(ar.e1, c[0]+pavedReachM)
				ar.n0, ar.n1 = math.Min(ar.n0, -c[2]-pavedReachM), math.Max(ar.n1, -c[2]+pavedReachM)
			}
		}
		out = append(out, ar)
	}
	return out
}

// landCap is the most the land may stand at e, n beside or under the
// paved areas: paveSinkM under the paving, and falling at fillSlope to
// an area's sides along the way (the fill between and beside the decks).
func landCap(areas []*pavedArea, e, n float64) (float64, bool) {
	cap, any := math.Inf(1), false
	for _, a := range areas {
		if e < a.e0 || e > a.e1 || n < a.n0 || n > a.n1 {
			continue
		}
		for _, q := range a.quads {
			for _, t := range [2][3][3]float64{{q[0], q[1], q[2]}, {q[0], q[2], q[3]}} {
				if h, in := planHeight(t, e, n); in {
					cap, any = math.Min(cap, h-paveSinkM), true
				}
			}
		}
		for _, side := range a.sides {
			for k := 1; k < 4; k++ { // the toe onwards: the shoulder lies by the way
				p, q := side[k-1], side[k]
				pe, pn, qe, qn := p[0], -p[2], q[0], -q[2]
				de, dn := qe-pe, qn-pn
				f := 0.0
				if l2 := de*de + dn*dn; l2 > 0 {
					f = math.Max(0, math.Min(1, ((e-pe)*de+(n-pn)*dn)/l2))
				}
				d := math.Hypot(e-pe-f*de, n-pn-f*dn)
				if d < pavedReachM {
					cap, any = math.Min(cap, p[1]+f*(q[1]-p[1])+fillSlope*d), true
				}
			}
		}
	}
	return cap, any
}

// planHeight is triangle t's height (glTF frame) at e, n, and whether
// e, n lies in it, seen from above.
func planHeight(t [3][3]float64, e, n float64) (float64, bool) {
	ax, az, bx, bz, cx, cz := t[0][0], t[0][2], t[1][0], t[1][2], t[2][0], t[2][2]
	px, pz := e, -n
	den := (bz-cz)*(ax-cx) + (cx-bx)*(az-cz)
	if math.Abs(den) < 1e-12 {
		return 0, false
	}
	u := ((bz-cz)*(px-cx) + (cx-bx)*(pz-cz)) / den
	v := ((cz-az)*(px-cx) + (ax-cx)*(pz-cz)) / den
	w := 1 - u - v
	if u < -1e-9 || v < -1e-9 || w < -1e-9 {
		return 0, false
	}
	return u*t[0][1] + v*t[1][1] + w*t[2][1], true
}

// drawPavings draws the paved areas in chunk-keyed primitives of material
// mat, each plane skirted down a metre along the area's ends.
func drawPavings(areas []*pavedArea, chunkM float64, mat int) map[[2]int]*gltf.Primitive {
	out := map[[2]int]*gltf.Primitive{}
	for _, a := range areas {
		c := a.quads[0][0]
		k := [2]int{int(math.Floor(c[0] / chunkM)), int(math.Floor(-c[2] / chunkM))}
		p := out[k]
		if p == nil {
			p = &gltf.Primitive{Material: mat}
			out[k] = p
		}
		for _, q := range a.quads {
			n := cross([3]float64{q[1][0] - q[0][0], q[1][1] - q[0][1], q[1][2] - q[0][2]}, [3]float64{q[3][0] - q[0][0], q[3][1] - q[0][1], q[3][2] - q[0][2]})
			if n[1] < 0 {
				n = scale3(n, -1)
			}
			if l := math.Sqrt(dot3(n, n)); l > 0 {
				n = scale3(n, 1/l)
			}
			quad(p, q[0], q[1], q[2], q[3], n, slopePaving)
		}
		mid := [3]float64{(a.sides[0][0][0] + a.sides[1][3][0]) / 2, 0, (a.sides[0][0][2] + a.sides[1][3][2]) / 2}
		for _, side := range a.sides {
			for k := 0; k < 3; k++ {
				p0, p1 := side[k], side[k+1]
				d0, d1 := p0, p1
				d0[1], d1[1] = p0[1]-1, p1[1]-1
				quad(p, p0, p1, d1, d0, outward(p0, p1, [3]float64{p0[0] - mid[0], 0, p0[2] - mid[2]}), slopePaving)
			}
		}
	}
	return out
}

// pavedOutlines are the paved areas' outlines (east, north), for the
// ground map: no grass on them.
func pavedOutlines(areas []*pavedArea) [][][2]float64 {
	var out [][][2]float64
	for _, a := range areas {
		var poly [][2]float64
		for _, c := range a.sides[0] {
			poly = append(poly, [2]float64{c[0], -c[2]})
		}
		for k := 3; k >= 0; k-- {
			c := a.sides[1][k]
			poly = append(poly, [2]float64{c[0], -c[2]})
		}
		out = append(out, poly)
	}
	return out
}
