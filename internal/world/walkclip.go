package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
)

// Sidewalks lying in the carriageway (owner, 2026-10-16, marks at 0.06,
// 0.09, 0.51 and 0.54 km of the Route des Américains: sidewalk pieces in
// the road at the roundabouts; and the open items of 2026-10-13): a
// junction's sidewalk along its outline, or a road's sidewalk strip
// reaching into a junction or another road, overlaps a carriageway drawn
// by another piece. clearWalksOffRoads takes out every sidewalk and kerb
// triangle that lies over a carriageway at about its height, and closes
// the sidewalk where that opens it with a kerb face down to the road.
const walkOverM = 0.5 // a carriageway this far under a sidewalk, or over it, is the one it lies in

// clearWalksOffRoads clears the sidewalks (walk, kerb materials) of ps off
// the carriageways (carriageway materials) among them: sidewalk slabs are
// clipped along the carriageway's edge (seen from above, as the land is),
// a kerb face closing each new edge; kerb faces standing in the road go.
// It returns how many sidewalk triangles it changed or took out.
func clearWalksOffRoads(chunks map[[2]int][]gltf.Primitive, walkMat, kerbMat int, carriageway func(mat int) bool) int {
	const cell = 2.0
	type tri [3][3]float64
	c := newCutter()
	grid := map[[2]int][]tri{}
	at := func(p *gltf.Primitive, i uint32) [3]float64 {
		return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
	}
	// Every chunk's carriageways: a junction's sidewalks are filed under
	// its own chunk, the road they overlap perhaps under the next.
	keys := chunkOrder(chunks)
	for _, k := range keys {
		for pi := range chunks[k] {
			p := &chunks[k][pi]
			if !carriageway(p.Material) {
				continue
			}
			c.add(*p)
			for t := 0; t+2 < len(p.Indices); t += 3 {
				q := tri{at(p, p.Indices[t]), at(p, p.Indices[t+1]), at(p, p.Indices[t+2])}
				lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
				for _, v := range q {
					lo = [2]float64{math.Min(lo[0], v[0]), math.Min(lo[1], v[2])}
					hi = [2]float64{math.Max(hi[0], v[0]), math.Max(hi[1], v[2])}
				}
				for gx := int(math.Floor(lo[0] / cell)); gx <= int(math.Floor(hi[0]/cell)); gx++ {
					for gz := int(math.Floor(lo[1] / cell)); gz <= int(math.Floor(hi[1]/cell)); gz++ {
						grid[[2]int{gx, gz}] = append(grid[[2]int{gx, gz}], q)
					}
				}
			}
		}
	}
	if len(grid) == 0 {
		return 0
	}
	onRoad := func(x, y, z float64) bool {
		for _, q := range grid[[2]int{int(math.Floor(x / cell)), int(math.Floor(z / cell))}] {
			t6 := [6]float64{q[0][0], q[0][2], q[1][0], q[1][2], q[2][0], q[2][2]}
			if !inTri2(t6, x, z) {
				continue
			}
			if h, ok := triHeight(q, x, z); ok && math.Abs(h-y) < walkOverM {
				return true
			}
		}
		return false
	}
	type ekey [2][3]float32
	edgesOf := func(p *gltf.Primitive) map[ekey]int {
		es := map[ekey]int{}
		for t := 0; t+2 < len(p.Indices); t += 3 {
			for _, e := range [3][2]uint32{{p.Indices[t], p.Indices[t+1]}, {p.Indices[t+1], p.Indices[t+2]}, {p.Indices[t+2], p.Indices[t]}} {
				var a, b [3]float32
				copy(a[:], p.Positions[3*e[0]:3*e[0]+3])
				copy(b[:], p.Positions[3*e[1]:3*e[1]+3])
				if b[0] < a[0] || b[0] == a[0] && b[2] < a[2] {
					a, b = b, a
				}
				es[ekey{a, b}]++
			}
		}
		return es
	}
	changed := 0
	for _, k := range keys {
		for pi := range chunks[k] {
			p := &chunks[k][pi]
			switch p.Material {
			case walkMat:
				before := edgesOf(p)
				n0 := len(p.Indices)
				np := c.clipSeams(*p, false)
				if len(np.Indices) == n0 && len(np.Positions) == len(p.Positions) {
					continue // nothing over a carriageway
				}
				changed += n0 / 3
				// The slab's new edges (open, and not one of its own before):
				// closed down to the road.
				// In a fixed order (a map's isn't): the same world every build.
				es := edgesOf(&np)
				open := make([]ekey, 0, len(es))
				for e, n := range es {
					if n == 1 && before[e] == 0 {
						open = append(open, e)
					}
				}
				sort.Slice(open, func(i, j int) bool {
					a, b := open[i], open[j]
					for v := range 2 {
						for k := range 3 {
							if a[v][k] != b[v][k] {
								return a[v][k] < b[v][k]
							}
						}
					}
					return false
				})
				for _, e := range open {
					mx, my, mz := float64(e[0][0]+e[1][0])/2, float64(e[0][1]+e[1][1])/2, float64(e[0][2]+e[1][2])/2
					dx, dz := float64(e[1][0]-e[0][0]), float64(e[1][2]-e[0][2])
					l := math.Hypot(dx, dz)
					if l < 1e-3 {
						continue
					}
					nx, nz := -dz/l*0.1, dx/l*0.1
					if onRoad(mx+nx, my-kerbM, mz+nz) || onRoad(mx-nx, my-kerbM, mz-nz) {
						closeWalk(&np, e[0], e[1])
					}
				}
				*p = np
			case kerbMat:
				var keep []uint32
				for t := 0; t+2 < len(p.Indices); t += 3 {
					a, b, cc := at(p, p.Indices[t]), at(p, p.Indices[t+1]), at(p, p.Indices[t+2])
					ny := (b[2]-a[2])*(cc[0]-a[0]) - (b[0]-a[0])*(cc[2]-a[2])
					x, z := (a[0]+b[0]+cc[0])/3, (a[2]+b[2]+cc[2])/3
					y := math.Min(a[1], math.Min(b[1], cc[1]))
					in := false
					if math.Abs(ny) < 1e-6 {
						// A face: in the road when the road lies on both sides.
						nx, nz := 0.0, 0.0
						for _, e := range [3][2][3]float64{{a, b}, {b, cc}, {cc, a}} {
							if dx, dz := e[1][0]-e[0][0], e[1][2]-e[0][2]; math.Hypot(dx, dz) > math.Hypot(nz, nx) {
								nx, nz = -dz, dx
							}
						}
						if l := math.Hypot(nx, nz); l > 0 {
							nx, nz = nx/l*0.15, nz/l*0.15
							in = onRoad(x+nx, y, z+nz) && onRoad(x-nx, y, z-nz)
						}
					}
					if in {
						changed++
						continue
					}
					keep = append(keep, p.Indices[t], p.Indices[t+1], p.Indices[t+2])
				}
				p.Indices = keep
			}
		}
	}
	return changed
}

// closeWalk adds a face from a sidewalk's edge a-b down by its kerb's
// height, facing both ways (it is seen from the road side only, whichever
// that is).
func closeWalk(p *gltf.Primitive, a, b [3]float32) {
	drop := float32(kerbM + 0.05)
	base := uint32(len(p.Positions) / 3)
	for _, q := range [4][3]float32{a, b, {b[0], b[1] - drop, b[2]}, {a[0], a[1] - drop, a[2]}} {
		p.Positions = append(p.Positions, q[0], q[1], q[2])
		if len(p.Normals) > 0 {
			nx, nz := b[2]-a[2], a[0]-b[0]
			l := float32(math.Hypot(float64(nx), float64(nz)))
			if l > 0 {
				nx, nz = nx/l, nz/l
			}
			p.Normals = append(p.Normals, nx, 0, nz)
		}
		if len(p.Colors) > 0 {
			p.Colors = append(p.Colors, 1, 1, 1)
		}
		if len(p.UVs) > 0 {
			p.UVs = append(p.UVs, 0, 0)
		}
	}
	p.Indices = append(p.Indices, base, base+1, base+2, base, base+2, base+3, base, base+2, base+1, base, base+3, base+2)
}

// triHeight is the height of triangle q's plane at x, z.
func triHeight(q [3][3]float64, x, z float64) (float64, bool) {
	ax, az := q[1][0]-q[0][0], q[1][2]-q[0][2]
	bx, bz := q[2][0]-q[0][0], q[2][2]-q[0][2]
	det := ax*bz - az*bx
	if math.Abs(det) < 1e-12 {
		return 0, false
	}
	px, pz := x-q[0][0], z-q[0][2]
	u := (px*bz - pz*bx) / det
	v := (ax*pz - az*px) / det
	return q[0][1] + u*(q[1][1]-q[0][1]) + v*(q[2][1]-q[0][1]), true
}

// closeOpenWalks closes every sidewalk edge that nothing closes (owner,
// 2026-10-16, mark at 0.51 km: looking through a sidewalk where a
// junction's met a road's): an edge with sidewalk slab on one side only
// and no face hanging from it gets a face down by walkFootM + its kerb
// into the ground. Judged by geometry, probing either side of the edge's
// middle, not by matching vertices (clipped slabs' shared edges don't
// match to the bit, and faces hung there flickered against their
// neighbours, owner 2026-10-16). It returns how many edges it closed.
func closeOpenWalks(chunks map[[2]int][]gltf.Primitive, walkMat, kerbMat int) int {
	const cell, probe, near = 2.0, 0.03, 0.03
	type tri [3][3]float64
	type seg struct{ a, b [3]float64 }
	slabs := map[[2]int][]tri{}
	tops := map[[2]int][]seg{}
	key := func(x, z float64) [2]int { return [2]int{int(math.Floor(x / cell)), int(math.Floor(z / cell))} }
	at := func(p *gltf.Primitive, i uint32) [3]float64 {
		return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
	}
	type edge struct {
		k    [2]int
		pi   int
		a, b [3]float64
	}
	var cands []edge
	var faceTris []tri
	keys := chunkOrder(chunks)
	for _, k := range keys {
		for pi := range chunks[k] {
			p := &chunks[k][pi]
			if p.Material != walkMat && p.Material != kerbMat {
				continue
			}
			for t := 0; t+2 < len(p.Indices); t += 3 {
				q := tri{at(p, p.Indices[t]), at(p, p.Indices[t+1]), at(p, p.Indices[t+2])}
				ny := (q[1][2]-q[0][2])*(q[2][0]-q[0][0]) - (q[1][0]-q[0][0])*(q[2][2]-q[0][2])
				if math.Abs(ny) < 1e-6 {
					faceTris = append(faceTris, q)
					continue
				}
				if p.Material != walkMat {
					continue
				}
				lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
				for _, v := range q {
					lo = [2]float64{math.Min(lo[0], v[0]), math.Min(lo[1], v[2])}
					hi = [2]float64{math.Max(hi[0], v[0]), math.Max(hi[1], v[2])}
				}
				for gx := int(math.Floor(lo[0] / cell)); gx <= int(math.Floor(hi[0]/cell)); gx++ {
					for gz := int(math.Floor(lo[1] / cell)); gz <= int(math.Floor(hi[1]/cell)); gz++ {
						slabs[[2]int{gx, gz}] = append(slabs[[2]int{gx, gz}], q)
					}
				}
				for e := range 3 {
					cands = append(cands, edge{k, pi, q[e], q[(e+1)%3]})
				}
			}
		}
	}
	// A face's top edge: both its ends stand over another of the faces'
	// vertices at the same place (a face is a quad from foot to top, also
	// where it climbs with the road).
	type xz [2]int64
	lowest := map[xz]float64{}
	pk := func(v [3]float64) xz { return xz{int64(math.Round(v[0] * 200)), int64(math.Round(v[2] * 200))} }
	for _, q := range faceTris {
		for _, v := range q {
			if y, ok := lowest[pk(v)]; !ok || v[1] < y {
				lowest[pk(v)] = v[1]
			}
		}
	}
	upper := func(v [3]float64) bool { return v[1] > lowest[pk(v)]+0.02 }
	for _, q := range faceTris {
		for e := range 3 {
			a, b := q[e], q[(e+1)%3]
			if upper(a) && upper(b) {
				mk := key((a[0]+b[0])/2, (a[2]+b[2])/2)
				tops[mk] = append(tops[mk], seg{a, b})
			}
		}
	}
	onSlab := func(x, y, z float64) bool {
		for _, q := range slabs[key(x, z)] {
			if !inTri2([6]float64{q[0][0], q[0][2], q[1][0], q[1][2], q[2][0], q[2][2]}, x, z) {
				continue
			}
			if h, ok := triHeight(q, x, z); ok && math.Abs(h-y) < 0.1 {
				return true
			}
		}
		return false
	}
	hung := func(a, b [3]float64) bool {
		mx, my, mz := (a[0]+b[0])/2, (a[1]+b[1])/2, (a[2]+b[2])/2
		for gx := -1; gx <= 1; gx++ {
			for gz := -1; gz <= 1; gz++ {
				k := key(mx, mz)
				for _, sg := range tops[[2]int{k[0] + gx, k[1] + gz}] {
					dx, dz := sg.b[0]-sg.a[0], sg.b[2]-sg.a[2]
					l2 := dx*dx + dz*dz
					f := 0.0
					if l2 > 0 {
						f = math.Max(0, math.Min(1, ((mx-sg.a[0])*dx+(mz-sg.a[2])*dz)/l2))
					}
					if math.Hypot(mx-sg.a[0]-f*dx, mz-sg.a[2]-f*dz) < near && math.Abs(my-sg.a[1]-f*(sg.b[1]-sg.a[1])) < near {
						return true
					}
				}
			}
		}
		return false
	}
	closed := 0
	seen := map[[4]int64]bool{}
	for _, e := range cands {
		dx, dz := e.b[0]-e.a[0], e.b[2]-e.a[2]
		l := math.Hypot(dx, dz)
		if l < 0.02 {
			continue
		}
		// One closing per edge, whichever triangle lists it.
		q := func(v float64) int64 { return int64(math.Round(v * 100)) }
		k1 := [2]int64{q(e.a[0]), q(e.a[2])}
		k2 := [2]int64{q(e.b[0]), q(e.b[2])}
		if k2[0] < k1[0] || k2[0] == k1[0] && k2[1] < k1[1] {
			k1, k2 = k2, k1
		}
		sk := [4]int64{k1[0], k1[1], k2[0], k2[1]}
		if seen[sk] {
			continue
		}
		seen[sk] = true
		mx, my, mz := (e.a[0]+e.b[0])/2, (e.a[1]+e.b[1])/2, (e.a[2]+e.b[2])/2
		nx, nz := -dz/l*probe, dx/l*probe
		if onSlab(mx+nx, my, mz+nz) == onSlab(mx-nx, my, mz-nz) || hung(e.a, e.b) {
			continue
		}
		p := &chunks[e.k][e.pi]
		hangFace(p, [3]float32{float32(e.a[0]), float32(e.a[1]), float32(e.a[2])}, [3]float32{float32(e.b[0]), float32(e.b[1]), float32(e.b[2])}, float32(kerbM+walkFootM))
		closed++
	}
	return closed
}

// hangFace adds a face from edge a-b down by drop, seen from both sides.
func hangFace(p *gltf.Primitive, a, b [3]float32, drop float32) {
	base := uint32(len(p.Positions) / 3)
	nx, nz := b[2]-a[2], a[0]-b[0]
	if l := float32(math.Hypot(float64(nx), float64(nz))); l > 0 {
		nx, nz = nx/l, nz/l
	}
	for _, v := range [4][3]float32{a, b, {b[0], b[1] - drop, b[2]}, {a[0], a[1] - drop, a[2]}} {
		p.Positions = append(p.Positions, v[0], v[1], v[2])
		if len(p.Normals) > 0 {
			p.Normals = append(p.Normals, nx, 0, nz)
		}
		if len(p.Colors) > 0 {
			p.Colors = append(p.Colors, 1, 1, 1)
		}
		if len(p.UVs) > 0 {
			p.UVs = append(p.UVs, 0, 0)
		}
	}
	p.Indices = append(p.Indices, base, base+1, base+2, base, base+2, base+3, base, base+2, base+1, base, base+3, base+2)
}
