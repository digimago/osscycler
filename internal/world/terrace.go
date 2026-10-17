package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// A terrace (owner, 2026-10-16/17, three Street View photos and the
// aerial view of the lane off the Rue du Hohneck, 1.34-1.36 km of the
// Route des Américains; "the piece of sidewalk in front of the two roads
// joining should be a rectangle; on it, parallel to the road, a retaining
// wall: slope up, straight piece, slope down; restart everything for this
// mouth and make it look good"): where a higher road runs alongside a
// road just behind its sidewalk, between two mouths, the sidewalk and the
// wall are one designed piece rather than frames, junction outlines and
// stitches fitted together: a rectangle of sidewalk from the kerb to the
// wall, the wall on its outer edge with a trapezoid top, level fill
// behind it to the road above, and nothing else drawn in that span.
//
// Three stages: markTerraces finds them on the road samples before the
// meshes (turning the road's own sidewalk off there); terracePrims draws
// the slab, its kerb and the asphalt fillers at its ends with the road
// meshes, so the land is laid round them and tucked under them like any
// road surface (drawn later, with the roadside, the land stood over the
// slab's edge); terraceWall draws the wall and the fill behind it with
// the roadside.
const (
	terraceReachM = 3.0  // a road surface this near behind the sidewalk's edge
	terraceStepM  = 0.35 // higher by this somewhere along the run: a wall
	terraceLowM   = 0.03 // on while higher by this
	terraceMaxM   = 4.0  // not a hillside or a bridge
	terraceMinM   = 6.0  // a run shorter than this is none
	terraceEndM   = 0.3  // the wall's ends: this high over the sidewalk
	terraceSlope  = 0.4  // its wedges rise this much per metre
	terraceCopeM  = 0.25 // the wall's thickness
	terraceCrossC = 0.5  // a road within 60° of square at the probe: a mouth, not alongside
	terraceApronM = 2.5  // the asphalt filler ramp at each end, this long
)

// markTerraces marks, on each road's samples, the runs where a higher
// road lies within terraceReachM behind its edge and sidewalk, not across
// a mouth: terrace[side] the band's outer offset, terraceWalk the
// sidewalk's width there (0: the wall at the carriageway's edge),
// terraceBack the height of the surface behind, terraceBehind its offset.
// The road's own sidewalk is turned off there.
func markTerraces(lines []*roadLine) {
	var all []sample
	var owner []int // the line each sample is of (sample.line isn't set yet here)
	for li, l := range lines {
		all = append(all, l.samples...)
		for range l.samples {
			owner = append(owner, li)
		}
	}
	grid := newRouteGrid(all, 10)
	// nearest other road's sample within its half width + margin of e, n:
	// the sample and its neighbour along its line.
	near := func(e, n float64, skip int) (sample, sample, bool) {
		best, bi := math.Inf(1), -1
		grid.within(e, n, 10, func(i int) {
			a := all[i]
			if owner[i] == skip {
				return
			}
			if d := math.Hypot(a.e-e, a.n-n); d < best && d < a.hw+2 {
				best, bi = d, i
			}
		})
		if bi < 0 {
			return sample{}, sample{}, false
		}
		j := bi + 1
		if j >= len(all) || owner[j] != owner[bi] {
			j = bi - 1
		}
		if j < 0 || owner[j] != owner[bi] {
			return sample{}, sample{}, false
		}
		return all[bi], all[j], true
	}
	for li, l := range lines {
		ss := l.samples
		n := len(ss)
		for si, sign := range []float64{-1, 1} {
			on := make([]bool, n)
			band := make([]float64, n)
			step := make([]float64, n)
			back := make([]float64, n)
			behind := make([]float64, n)
			for i := range ss {
				s := ss[i]
				if s.again || s.aloft {
					continue
				}
				a, b := ss[max(0, i-1)], ss[min(n-1, i+1)]
				de, dn := b.e-a.e, b.n-a.n
				d := math.Hypot(de, dn)
				if d == 0 {
					continue
				}
				rx, rn := sign*dn/d, -sign*de/d // out to this side
				bw, base := s.hw, s.ele
				if s.walk[si] {
					bw, base = s.hw+s.walkWidth(si), s.ele+kerbM
				}
				at := func(off float64) (float64, float64) { return s.e + rx*off, s.n + rn*off }
				// Not where a road crosses the sidewalk's line (a mouth).
				fe, fn := at(bw)
				if o, _, ok := near(fe, fn, li); ok && o.ele < base+terraceLowM {
					continue
				}
				for g := bw + 0.3; g <= bw+terraceReachM; g += 0.3 {
					e, nn := at(g)
					o, o2, ok := near(e, nn, li)
					if !ok {
						continue
					}
					if math.Hypot(o.e-e, o.n-nn) > o.hw+0.3 {
						continue // its verge, not its surface
					}
					oe, on2 := o2.e-o.e, o2.n-o.n
					if ol := math.Hypot(oe, on2); ol > 0 && math.Abs(oe*de+on2*dn)/(ol*d) < terraceCrossC {
						break // joining or crossing, not alongside
					}
					dz := o.ele - base
					if dz <= terraceLowM || dz >= terraceMaxM {
						break
					}
					if !on[i] {
						on[i], band[i], step[i], behind[i] = true, bw, dz, g
					}
					back[i] = math.Max(back[i], o.ele)
				}
			}
			for _, r := range runs(on) {
				most := 0.0
				for k := r.from; k <= r.to; k++ {
					most = math.Max(most, step[k])
				}
				if most <= terraceStepM || ss[r.to].d-ss[r.from].d < terraceMinM {
					continue
				}
				// Where nothing was found behind (the road above bends away
				// for a few samples) the back runs straight between its
				// neighbours (a V-notch had cut the wall's top mid-run).
				for k := r.from; k <= r.to; k++ {
					if on[k] {
						continue
					}
					pk, nk := -1, -1
					for j := k - 1; j >= r.from; j-- {
						if on[j] {
							pk = j
							break
						}
					}
					for j := k + 1; j <= r.to; j++ {
						if on[j] {
							nk = j
							break
						}
					}
					switch {
					case pk >= 0 && nk >= 0:
						f := (ss[k].d - ss[pk].d) / math.Max(ss[nk].d-ss[pk].d, 1e-9)
						back[k], band[k] = back[pk]+f*(back[nk]-back[pk]), band[pk]
					case pk >= 0:
						back[k], band[k] = back[pk], band[pk]
					case nk >= 0:
						back[k], band[k] = back[nk], band[nk]
					}
				}
				for k := r.from; k <= r.to; k++ {
					ss[k].terrace[si], ss[k].terraceBack[si], ss[k].terraceBehind[si] = band[k], back[k], behind[k]
					if ss[k].walk[si] {
						ss[k].terraceWalk[si] = ss[k].walkWidth(si)
						ss[k].walk[si] = false
					}
				}
			}
		}
	}
}

// terraceRuns calls f for every terrace on the samples ss: its side, its
// run, and pos, a point off sample k's line on that side.
func terraceRuns(ss []sample, f func(si int, r sideRun, pos func(k int, d float64) (float64, float64))) {
	n := len(ss)
	if n < 2 {
		return
	}
	for si, sign := range []float64{-1, 1} {
		on := make([]bool, n)
		for k := range n {
			on[k] = ss[k].terrace[si] > 0
		}
		rs := runs(on)
		if len(rs) == 0 {
			continue
		}
		pos := func(k int, d float64) (float64, float64) {
			a, b := ss[max(0, k-1)], ss[min(n-1, k+1)]
			de, dn := b.e-a.e, b.n-a.n
			l := math.Hypot(de, dn)
			if l == 0 {
				return ss[k].e, ss[k].n
			}
			return ss[k].e + sign*dn/l*d, ss[k].n - sign*de/l*d
		}
		for _, r := range rs {
			f(si, r, pos)
		}
	}
}

// terraceBase is the terrace's slab top at sample s on side si (the
// carriageway's height where it has no sidewalk).
func terraceBase(s sample, si int) float64 {
	if s.terraceWalk[si] > 0 {
		return s.ele + kerbM
	}
	return s.ele
}

// terracePrims draws the terraces' slabs, kerbs and asphalt fillers,
// per chunk, in the roads' materials: with the road meshes, so the land
// is laid round them and tucked under them.
func terracePrims(lines []*roadLine, m roadMaterials, chunkM float64) map[[2]int][]gltf.Primitive {
	out := map[[2]int][]gltf.Primitive{}
	vert := func(p *gltf.Primitive, q [3]float64, n [3]float64) uint32 {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		p.UVs = append(p.UVs, float32(q[0]/5), float32(q[2]/5))
		return uint32(len(p.Positions)/3 - 1)
	}
	tquad := func(p *gltf.Primitive, a, b, c, d, n [3]float64) {
		i0, i1, i2, i3 := vert(p, a, n), vert(p, b, n), vert(p, c, n), vert(p, d, n)
		tri(p, i0, i1, i2, n)
		tri(p, i0, i2, i3, n)
	}
	for _, l := range lines {
		ss := l.samples
		terraceRuns(ss, func(si int, r sideRun, pos func(k int, d float64) (float64, float64)) {
			key := [2]int{int(math.Floor(ss[r.from].e / chunkM)), int(math.Floor(ss[r.from].n / chunkM))}
			slab := gltf.Primitive{Material: m.walk}
			kerb := gltf.Primitive{Material: m.kerb}
			apron := gltf.Primitive{Material: m.surface[ss[r.from].surface]}
			pt := func(k int, d, y float64) [3]float64 {
				e, n := pos(k, d)
				return [3]float64{e, y, -n}
			}
			up := [3]float64{0, 1, 0}
			for k := r.from; k < r.to; k++ {
				a, b := ss[k], ss[k+1]
				if a.terraceWalk[si] == 0 {
					continue // the wall at the carriageway's edge: no slab
				}
				ba, bb := terraceBase(a, si), terraceBase(b, si)
				ia, ib := a.terrace[si]-a.terraceWalk[si], b.terrace[si]-b.terraceWalk[si]
				s0, s1 := pt(k, ia, ba), pt(k+1, ib, bb)
				w0, w1 := pt(k, a.terrace[si], ba), pt(k+1, b.terrace[si], bb)
				tquad(&slab, s0, s1, w1, w0, up)
				k0, k1 := pt(k, ia, ba-kerbM), pt(k+1, ib, bb-kerbM)
				tquad(&kerb, k0, k1, s1, s0, wallNormal(k0, k1, s0, 1))
			}
			// The ends: square, with a face across the slab, and the asphalt
			// filler ramping from the mouth's surface up to the slab over the
			// next samples out (owner, 2026-10-17: asphalt filler slabs patch
			// the gap between road surfaces and a sidewalk; extend the road
			// with a few triangles over the land either side of the
			// in-between sidewalk); where the road's own sidewalk goes on,
			// the slab joins it flush instead.
			for _, k := range []int{r.from, r.to} {
				a := ss[k]
				base := terraceBase(a, si)
				band, inner := a.terrace[si], a.terrace[si]-a.terraceWalk[si]
				dir, out := -1, k-1
				if k == r.to {
					dir, out = 1, k+1
				}
				joins := out >= 0 && out < len(ss) && ss[out].walk[si]
				if joins && a.terraceWalk[si] > 0 {
					o := ss[out]
					ob := o.hw + o.walkWidth(si)
					s0, w0 := pt(k, inner, base), pt(k, band, base)
					s1, w1 := pt(out, o.hw, o.ele+kerbM), pt(out, ob, o.ele+kerbM)
					tquad(&slab, s0, s1, w1, w0, up)
					k0, k1 := pt(k, inner, base-kerbM), pt(out, o.hw, o.ele)
					tquad(&kerb, k0, k1, s1, s0, wallNormal(k0, k1, s0, 1))
					continue
				}
				if a.terraceWalk[si] > 0 {
					a0, b0 := pt(k, inner, base-walkFootM), pt(k, band+terraceCopeM, base-walkFootM)
					a1, b1 := pt(k, inner, base), pt(k, band+terraceCopeM, base)
					ne := wallNormal(a0, b0, a1, 1)
					tquad(&kerb, a0, b0, b1, a1, ne)
					tquad(&kerb, b0, a0, a1, b1, [3]float64{-ne[0], -ne[1], -ne[2]})
				}
				if joins {
					continue
				}
				far := math.Max(band+terraceCopeM, a.terraceBehind[si])
				reach := 0.0
				prev := [2][3]float64{pt(k, inner, base+0.01), pt(k, far, base+0.01)}
				for i := out; i >= 0 && i < len(ss) && reach < terraceApronM; i += dir {
					reach += math.Hypot(ss[i].e-ss[i-dir].e, ss[i].n-ss[i-dir].n)
					f := math.Min(1, reach/terraceApronM)
					y := (ss[i].ele+0.03)*f + (base+0.01)*(1-f)
					cur := [2][3]float64{pt(i, inner, y), pt(i, far, y)}
					tquad(&apron, prev[0], prev[1], cur[1], cur[0], up)
					prev = cur
				}
			}
			for _, p := range []gltf.Primitive{slab, kerb, apron} {
				if len(p.Indices) > 0 {
					out[key] = append(out[key], p)
				}
			}
		})
	}
	return out
}

// terraceWall draws a terrace's wall and the fill behind it: the samples
// from+r.from..from+r.to of the terrain's fine samples, side si.
func terraceWall(t *terrain, from int, r sideRun, si int, walls, fill *gltf.Primitive, landColour func(e, n float64) [3]float32, pos func(k int, d float64) (float64, float64)) {
	n := r.to - r.from + 1
	if n < 2 {
		return
	}
	concrete, cope := srgbLinear(0xb9b5ad), srgbLinear(0xa8a49c)
	band := make([]float64, n)
	base := make([]float64, n)
	back := make([]float64, n)
	behind := make([]float64, n)
	dist := make([]float64, n)
	for k := 0; k < n; k++ {
		a := t.fine[from+r.from+k]
		band[k], base[k], back[k], behind[k] = a.terrace[si], terraceBase(a, si), a.terraceBack[si], a.terraceBehind[si]
		if k > 0 {
			b := t.fine[from+r.from+k-1]
			dist[k] = dist[k-1] + math.Hypot(a.e-b.e, a.n-b.n)
		}
	}
	// The top: the surface behind, smoothed, with a parapet, within the
	// wedges rising from the ends (slope up, straight, slope down).
	top := make([]float64, n)
	for k := range top {
		sum, c := 0.0, 0
		for j := max(0, k-2); j <= min(n-1, k+2); j++ {
			sum, c = sum+back[j], c+1
		}
		plateau := sum/float64(c) + 0.15
		end := math.Min(dist[k], dist[n-1]-dist[k])
		top[k] = math.Max(base[k]+terraceEndM, math.Min(plateau, base[k]+terraceEndM+terraceSlope*end))
	}
	pt := func(k int, d, y float64) [3]float64 {
		e, nn := pos(r.from+k, d)
		return [3]float64{e, y, -nn}
	}
	flip := func(v [3]float64) [3]float64 { return [3]float64{-v[0], -v[1], -v[2]} }
	for k := 0; k+1 < n; k++ {
		f0, f1 := pt(k, band[k]-0.02, base[k]-walkFootM), pt(k+1, band[k+1]-0.02, base[k+1]-walkFootM)
		t0, t1 := pt(k, band[k]-0.02, top[k]), pt(k+1, band[k+1]-0.02, top[k+1])
		nf := wallNormal(f0, f1, t0, 1)
		quad(walls, f0, f1, t1, t0, nf, concrete)
		quad(walls, f1, f0, t0, t1, flip(nf), concrete)
		c0, c1 := pt(k, band[k]+terraceCopeM, top[k]), pt(k+1, band[k+1]+terraceCopeM, top[k+1])
		quad(walls, t0, t1, c1, c0, [3]float64{0, 1, 0}, cope)
		b0, b1 := pt(k, band[k]+terraceCopeM, base[k]-walkFootM), pt(k+1, band[k+1]+terraceCopeM, base[k+1]-walkFootM)
		nb := wallNormal(b0, b1, c0, 1)
		quad(walls, b0, b1, c1, c0, nb, concrete)
		quad(walls, b1, b0, c0, c1, flip(nb), concrete)
		// The land behind, level to the road above.
		if behind[k] > 0 && behind[k+1] > 0 {
			q := [4][3]float64{pt(k, band[k]+terraceCopeM, back[k]-0.02), pt(k+1, band[k+1]+terraceCopeM, back[k+1]-0.02), pt(k+1, behind[k+1], back[k+1]-0.02), pt(k, behind[k], back[k]-0.02)}
			if !overRoad(t, q) {
				col := landColour(q[0][0], -q[0][2])
				addUp(fill, q[0], q[1], q[2], col)
				addUp(fill, q[0], q[2], q[3], col)
			}
		}
	}
	// The wall's ends, square blocks.
	for _, k := range []int{0, n - 1} {
		w0, w1 := pt(k, band[k]-0.02, base[k]-walkFootM), pt(k, band[k]+terraceCopeM, base[k]-walkFootM)
		w2, w3 := pt(k, band[k]+terraceCopeM, top[k]), pt(k, band[k]-0.02, top[k])
		nw := wallNormal(w0, w1, w3, 1)
		quad(walls, w0, w1, w2, w3, nw, concrete)
		quad(walls, w1, w0, w3, w2, flip(nw), concrete)
	}
}
