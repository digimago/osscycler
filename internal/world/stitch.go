package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
)

// Slivers between road pieces (owner, 2026-10-16, the start of the Route
// des Américains: a dark slit between the lanes where a road split round
// an island): a carriageway's end and the junction it meets leave a thin
// wedge between two open edges from one vertex, centimetres to decimetres
// wide; the skirts under them show through it, dark. stitchSlivers fills
// such a wedge with a triangle where nothing covers it.
const (
	stitchMaxDeg = 35.0  // the wedge's angle at its shared vertex (its width, stitchMaxM, keeps real gaps out)
	stitchMaxM   = 0.4   // its open end at most this wide
	stitchSnapM  = 0.02  // vertices this close are one
	stitchMinM2  = 0.002 // a wedge smaller than this seen from above is none
	// A wedge longer than stitchMaxLenM is a sliver only if its height
	// changes by stitchLongDyM at most from its apex to its open end.
	stitchMaxLenM = 6.0
	seamMaxM      = 0.4 // stitchSeams: a seam at most this wide
	seamMaxDyM    = 0.3 // and its two sides within this in height
	seamCos       = 0.8 // and nearly parallel
	stitchLongDyM = 0.3
)

// stitchSlivers closes the slivers between the open edges of primitives
// sharing a material (a carriageway's end and its junction are two),
// adding each closing triangle to the first of them; it returns how many
// it added.
func stitchSlivers(ps []gltf.Primitive) int {
	byMat := map[int][]int{}
	var mats []int
	for i := range ps {
		if _, ok := byMat[ps[i].Material]; !ok {
			mats = append(mats, ps[i].Material)
		}
		byMat[ps[i].Material] = append(byMat[ps[i].Material], i)
	}
	added := 0
	for _, m := range mats {
		group := make([]*gltf.Primitive, 0, len(byMat[m]))
		for _, i := range byMat[m] {
			group = append(group, &ps[i])
		}
		// A sliver along a road edge of several short pieces closes a
		// triangle at a time, each leaving the next: again until none.
		for range 12 {
			n := stitchGroup(group)
			added += n
			if n == 0 {
				break
			}
		}
	}
	return added
}

// stitchAcross closes the slivers left between surfaces of different
// materials that a rider sees as one, a sidewalk's end cut against a
// junction's asphalt (owner, 2026-10-16, Rue du Hohneck at 1.34 km: the
// main road's sidewalk between a lane's two mouths, the void under the
// roads showing at its ends): the same wedges as stitchSlivers, over the
// primitives whose material joins (one), each closing triangle added to
// the first primitive whose material first is true for (the asphalt).
func stitchAcross(ps []gltf.Primitive, joins, first func(mat int) bool) int {
	var group []*gltf.Primitive
	for i := range ps {
		if joins(ps[i].Material) && first(ps[i].Material) {
			group = append(group, &ps[i])
		}
	}
	if len(group) == 0 {
		return 0
	}
	for i := range ps {
		if joins(ps[i].Material) && !first(ps[i].Material) {
			group = append(group, &ps[i])
		}
	}
	added := 0
	for range 12 {
		n := stitchGroup(group)
		added += n
		if n == 0 {
			break
		}
	}
	return added
}

// stitchSeams zips shut the seams between the open edges of the surfaces
// in ps whose material joins (one), narrower than seamMaxM and nearly
// parallel: a sidewalk's end against a junction's outline, two road
// pieces meeting without a shared vertex. The land's finest cell can't
// fill a strip that thin (1.365 km of the Route des Américains: a 0.25 m
// strip of nothing between the sidewalk's square end and the junction's
// rounded corner), and no real street has a hand's width of ground there.
// Each closing quad goes into the first primitive first is true for (the
// asphalt); seams in the air (one surface over the other by more than
// seamMaxDyM) are left: a bridge over a road.
func stitchSeams(ps []gltf.Primitive, joins, first func(mat int) bool) int {
	var group []*gltf.Primitive
	for i := range ps {
		if joins(ps[i].Material) && first(ps[i].Material) {
			group = append(group, &ps[i])
		}
	}
	if len(group) == 0 {
		return 0
	}
	for i := range ps {
		if joins(ps[i].Material) && !first(ps[i].Material) {
			group = append(group, &ps[i])
		}
	}
	type seg struct {
		a, b   [3]float64
		ra, rb vref
	}
	var all []seg
	posOf := func(p *gltf.Primitive, i uint32) [3]float64 {
		q := p.Positions[3*i : 3*i+3]
		return [3]float64{float64(q[0]), float64(q[1]), float64(q[2])}
	}
	key := func(q [3]float64) [2]int64 {
		return [2]int64{int64(math.Round(q[0] / stitchSnapM)), int64(math.Round(q[2] / stitchSnapM))}
	}
	// Open edges of upward triangles, per primitive (an edge shared with
	// another primitive's triangle is a seam of width 0: zipped by nothing).
	var tris [][3][3]float64
	for gi, p := range group {
		type ek [2][2]int64
		count := map[ek]int{}
		edges := map[ek][2]uint32{}
		for t := 0; t+2 < len(p.Indices); t += 3 {
			a, b, c := p.Indices[t], p.Indices[t+1], p.Indices[t+2]
			pa, pb, pc := posOf(p, a), posOf(p, b), posOf(p, c)
			ny := (pb[2]-pa[2])*(pc[0]-pa[0]) - (pb[0]-pa[0])*(pc[2]-pa[2])
			if math.Abs(ny) < 1e-6 {
				continue
			}
			tris = append(tris, [3][3]float64{pa, pb, pc})
			for _, e := range [3][2]uint32{{a, b}, {b, c}, {c, a}} {
				x, y := key(posOf(p, e[0])), key(posOf(p, e[1]))
				if x == y {
					continue
				}
				k := ek{x, y}
				if x[0] > y[0] || x[0] == y[0] && x[1] > y[1] {
					k = ek{y, x}
				}
				count[k]++
				edges[k] = e
			}
		}
		for k, n := range count {
			if n == 1 {
				e := edges[k]
				all = append(all, seg{posOf(p, e[0]), posOf(p, e[1]), vref{gi, e[0]}, vref{gi, e[1]}})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { // a fixed order: the same world every build
		x, y := all[i], all[j]
		for c := range 3 {
			if x.a[c] != y.a[c] {
				return x.a[c] < y.a[c]
			}
		}
		for c := range 3 {
			if x.b[c] != y.b[c] {
				return x.b[c] < y.b[c]
			}
		}
		return false
	})
	const cell = 2.0
	grid := map[[2]int][]int{}
	for ti, t := range tris {
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, v := range t {
			lo = [2]float64{math.Min(lo[0], v[0]), math.Min(lo[1], v[2])}
			hi = [2]float64{math.Max(hi[0], v[0]), math.Max(hi[1], v[2])}
		}
		for gx := int(math.Floor(lo[0] / cell)); gx <= int(math.Floor(hi[0]/cell)); gx++ {
			for gz := int(math.Floor(lo[1] / cell)); gz <= int(math.Floor(hi[1]/cell)); gz++ {
				grid[[2]int{gx, gz}] = append(grid[[2]int{gx, gz}], ti)
			}
		}
	}
	covered := func(x, z float64) bool {
		for _, ti := range grid[[2]int{int(math.Floor(x / cell)), int(math.Floor(z / cell))}] {
			t := tris[ti]
			if inTri2([6]float64{t[0][0], t[0][2], t[1][0], t[1][2], t[2][0], t[2][2]}, x, z) {
				return true
			}
		}
		return false
	}
	segGrid := map[[2]int][]int{}
	for i, s := range all {
		for gx := int(math.Floor(math.Min(s.a[0], s.b[0]) / cell)); gx <= int(math.Floor(math.Max(s.a[0], s.b[0])/cell)); gx++ {
			for gz := int(math.Floor(math.Min(s.a[2], s.b[2]) / cell)); gz <= int(math.Floor(math.Max(s.a[2], s.b[2])/cell)); gz++ {
				segGrid[[2]int{gx, gz}] = append(segGrid[[2]int{gx, gz}], i)
			}
		}
	}
	// The point of segment s nearest x, z, by its parameter.
	along := func(s seg, x, z float64) (float64, [3]float64) {
		dx, dz := s.b[0]-s.a[0], s.b[2]-s.a[2]
		l2 := dx*dx + dz*dz
		f := 0.0
		if l2 > 0 {
			f = math.Max(0, math.Min(1, ((x-s.a[0])*dx+(z-s.a[2])*dz)/l2))
		}
		return f, [3]float64{s.a[0] + f*dx, s.a[1] + f*(s.b[1]-s.a[1]), s.a[2] + f*dz}
	}
	added := 0
	used := make([]bool, len(all))
	for i, e := range all {
		if used[i] || all[i].ra.p >= len(group) {
			continue
		}
		ex, ez := e.b[0]-e.a[0], e.b[2]-e.a[2]
		el := math.Hypot(ex, ez)
		if el < stitchSnapM {
			continue
		}
		mx, mz := (e.a[0]+e.b[0])/2, (e.a[2]+e.b[2])/2
		best, bestD := -1, seamMaxM
		for gx := int(math.Floor((mx - seamMaxM) / cell)); gx <= int(math.Floor((mx+seamMaxM)/cell)); gx++ {
			for gz := int(math.Floor((mz - seamMaxM) / cell)); gz <= int(math.Floor((mz+seamMaxM)/cell)); gz++ {
				for _, j := range segGrid[[2]int{gx, gz}] {
					if j == i || used[j] || all[j].ra.p == e.ra.p {
						continue // the same primitive: its own edges
					}
					f := all[j]
					fx, fz := f.b[0]-f.a[0], f.b[2]-f.a[2]
					fl := math.Hypot(fx, fz)
					if fl < stitchSnapM || math.Abs(ex*fx+ez*fz)/(el*fl) < seamCos {
						continue
					}
					// Both ends of e near f, and across a gap, not along it.
					_, pa := along(f, e.a[0], e.a[2])
					_, pb := along(f, e.b[0], e.b[2])
					da, db := math.Hypot(pa[0]-e.a[0], pa[2]-e.a[2]), math.Hypot(pb[0]-e.b[0], pb[2]-e.b[2])
					d := math.Max(da, db)
					if d >= bestD || d < stitchSnapM || math.Abs(pa[1]-e.a[1]) > seamMaxDyM || math.Abs(pb[1]-e.b[1]) > seamMaxDyM {
						continue
					}
					// The gap between: uncovered at its middle (a seam, not an
					// overlap or a surface's own notch), with some area.
					gx2, gz2 := (e.a[0]+e.b[0]+pa[0]+pb[0])/4, (e.a[2]+e.b[2]+pa[2]+pb[2])/4
					if covered(gx2, gz2) {
						continue
					}
					best, bestD = j, d
				}
			}
		}
		if best < 0 {
			continue
		}
		f := all[best]
		_, pa := along(f, e.a[0], e.a[2])
		_, pb := along(f, e.b[0], e.b[2])
		q := group[0]
		addQuadUp(q, e.a, e.b, pb, pa)
		used[i], used[best] = true, true
		added++
	}
	return added
}

// addQuadUp adds quad a b c d (x y z) to p facing up, with up normals and
// white colour / zero UVs where p has those attributes.
func addQuadUp(p *gltf.Primitive, a, b, c, d [3]float64) {
	base := uint32(len(p.Positions) / 3)
	for _, q := range [4][3]float64{a, b, c, d} {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		if len(p.Normals) > 0 {
			p.Normals = append(p.Normals, 0, 1, 0)
		}
		if len(p.Colors) > 0 {
			p.Colors = append(p.Colors, 1, 1, 1)
		}
		if len(p.UVs) > 0 {
			p.UVs = append(p.UVs, float32(q[0]/5), float32(q[2]/5))
		}
	}
	for _, t := range [2][3]uint32{{0, 1, 2}, {0, 2, 3}} {
		pa, pb, pc := [3]float64{a[0], a[1], a[2]}, [3]float64{}, [3]float64{}
		pts := [4][3]float64{a, b, c, d}
		pa, pb, pc = pts[t[0]], pts[t[1]], pts[t[2]]
		ny := (pb[2]-pa[2])*(pc[0]-pa[0]) - (pb[0]-pa[0])*(pc[2]-pa[2])
		if ny > 0 {
			p.Indices = append(p.Indices, base+t[0], base+t[1], base+t[2])
		} else {
			p.Indices = append(p.Indices, base+t[0], base+t[2], base+t[1])
		}
	}
}

// vref is a vertex of one of a group's primitives.
type vref struct {
	p int
	i uint32
}

func stitchGroup(group []*gltf.Primitive) int {
	var all []vref
	first := make([]int, len(group)) // each primitive's first vertex in all
	for gi, p := range group {
		first[gi] = len(all)
		for i := range len(p.Positions) / 3 {
			all = append(all, vref{gi, uint32(i)})
		}
	}
	nv := len(all)
	if nv == 0 {
		return 0
	}
	posOf := func(r vref) [3]float64 {
		q := group[r.p].Positions[3*r.i : 3*r.i+3]
		return [3]float64{float64(q[0]), float64(q[1]), float64(q[2])}
	}
	pos := func(i uint32) [3]float64 { return posOf(all[i]) }
	// Vertices by place (x, z within stitchSnapM): one id per place.
	key := func(q [3]float64) [2]int64 {
		return [2]int64{int64(math.Round(q[0] / stitchSnapM)), int64(math.Round(q[2] / stitchSnapM))}
	}
	place := map[[2]int64]int{}
	rep := make([]uint32, nv) // a vertex (in all) standing for each place
	id := make([]int, nv)
	for v := range nv {
		k := key(pos(uint32(v)))
		if j, ok := place[k]; ok {
			id[v] = j
			continue
		}
		place[k] = len(place)
		id[v] = place[k]
		rep[place[k]] = uint32(v)
	}
	// Upward triangles and their edges by place: an edge of one triangle
	// only is open.
	type edge struct{ a, b int }
	count := map[edge]int{}
	var tris [][3]uint32
	var idx []uint32
	for gi, p := range group {
		for _, v := range p.Indices {
			idx = append(idx, uint32(first[gi])+v)
		}
	}
	for t := 0; t+2 < len(idx); t += 3 {
		a, b, c := idx[t], idx[t+1], idx[t+2]
		pa, pb, pc := pos(a), pos(b), pos(c)
		ny := (pb[2]-pa[2])*(pc[0]-pa[0]) - (pb[0]-pa[0])*(pc[2]-pa[2])
		if math.Abs(ny) < 1e-6 {
			continue // a wall or skirt: nothing seen from above
		}
		tris = append(tris, [3]uint32{a, b, c})
		for _, e := range [3][2]uint32{{a, b}, {b, c}, {c, a}} {
			x, y := id[e[0]], id[e[1]]
			if x == y {
				continue
			}
			if x > y {
				x, y = y, x
			}
			count[edge{x, y}]++
		}
	}
	open := map[int][]int{} // place → the places its open edges lead to
	for e, n := range count {
		if n == 1 {
			open[e.a] = append(open[e.a], e.b)
			open[e.b] = append(open[e.b], e.a)
		}
	}
	// covered tells whether a point (x, z) lies in one of the upward
	// triangles (by a coarse grid).
	const cell = 2.0
	grid := map[[2]int][]int{}
	for ti, t := range tris {
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, v := range t {
			q := pos(v)
			lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[2])}
			hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[2])}
		}
		for gx := int(math.Floor(lo[0] / cell)); gx <= int(math.Floor(hi[0]/cell)); gx++ {
			for gz := int(math.Floor(lo[1] / cell)); gz <= int(math.Floor(hi[1]/cell)); gz++ {
				grid[[2]int{gx, gz}] = append(grid[[2]int{gx, gz}], ti)
			}
		}
	}
	covered := func(x, z float64) bool {
		for _, ti := range grid[[2]int{int(math.Floor(x / cell)), int(math.Floor(z / cell))}] {
			t := tris[ti]
			a, b, c := pos(t[0]), pos(t[1]), pos(t[2])
			if inTri2([6]float64{a[0], a[2], b[0], b[2], c[0], c[2]}, x, z) {
				return true
			}
		}
		return false
	}
	places := make([]int, 0, len(open))
	for k := range open {
		places = append(places, k)
	}
	sort.Ints(places) // a fixed order: the same world every build
	added := 0
	done := map[[3]int]bool{}
	cosMax := math.Cos(stitchMaxDeg * math.Pi / 180)
	for _, a := range places {
		nb := append([]int(nil), open[a]...)
		sort.Ints(nb)
		pa := pos(rep[a])
		for i := range nb {
			for j := i + 1; j < len(nb); j++ {
				b, c := nb[i], nb[j]
				pb, pc := pos(rep[b]), pos(rep[c])
				ub := [2]float64{pb[0] - pa[0], pb[2] - pa[2]}
				uc := [2]float64{pc[0] - pa[0], pc[2] - pa[2]}
				lb, lc := math.Hypot(ub[0], ub[1]), math.Hypot(uc[0], uc[1])
				if lb < stitchSnapM || lc < stitchSnapM || (ub[0]*uc[0]+ub[1]*uc[1])/(lb*lc) < cosMax {
					continue
				}
				// The open end: from the nearer end across to the other edge.
				near, far := pb, pc
				ln, lf := lb, lc
				if lc < lb {
					near, far, ln, lf = pc, pb, lc, lb
				}
				fe := [2]float64{pa[0] + (far[0]-pa[0])*ln/lf, pa[2] + (far[2]-pa[2])*ln/lf}
				if math.Hypot(near[0]-fe[0], near[2]-fe[1]) > stitchMaxM {
					continue
				}
				tk := [3]int{a, b, c}
				if done[tk] {
					continue
				}
				// A long wedge only where it lies level along its length: a
				// sliver between two pieces of one road (the Posbank Loop has
				// some over 6 m on the flat), not the gap between a road and
				// another beside it a step lower (16.1 km of the Route des
				// Américains: 10 m, a metre down, land in it).
				if math.Max(lb, lc) > stitchMaxLenM && math.Abs((pb[1]+pc[1])/2-pa[1]) > stitchLongDyM {
					continue
				}
				// A wedge with some area seen from above (two edges along
				// one kerb line, a sidewalk's over the road's, have none).
				if math.Abs(ub[0]*uc[1]-ub[1]*uc[0]) < 2*stitchMinM2 {
					continue
				}
				// Uncovered inside: a sliver, not a surface's own sharp corner.
				mx := (pa[0] + pb[0] + pc[0]) / 3
				mz := (pa[2] + pb[2] + pc[2]) / 3
				if covered(mx, mz) {
					continue
				}
				done[tk] = true
				addStitch(group[0], all[rep[a]], all[rep[b]], all[rep[c]], group)
				added++
			}
		}
	}
	return added
}

// addStitch adds to p a triangle over vertices a, b, c of the group
// (copied with their attributes where both have them), facing up.
func addStitch(p *gltf.Primitive, a, b, c vref, group []*gltf.Primitive) {
	base := uint32(len(p.Positions) / 3)
	for _, r := range [3]vref{a, b, c} {
		src, v := group[r.p], r.i
		p.Positions = append(p.Positions, src.Positions[3*v:3*v+3]...)
		if len(p.Normals) > 0 {
			p.Normals = append(p.Normals, 0, 1, 0)
		}
		if len(p.Colors) > 0 {
			if len(src.Colors) > 0 {
				p.Colors = append(p.Colors, src.Colors[3*v:3*v+3]...)
			} else {
				p.Colors = append(p.Colors, 1, 1, 1)
			}
		}
		if len(p.UVs) > 0 {
			if len(src.UVs) > 0 {
				p.UVs = append(p.UVs, src.UVs[2*v:2*v+2]...)
			} else {
				p.UVs = append(p.UVs, 0, 0)
			}
		}
	}
	pa := [3]float32{p.Positions[3*base], p.Positions[3*base+1], p.Positions[3*base+2]}
	pb := [3]float32{p.Positions[3*base+3], p.Positions[3*base+4], p.Positions[3*base+5]}
	pc := [3]float32{p.Positions[3*base+6], p.Positions[3*base+7], p.Positions[3*base+8]}
	// glTF is counter-clockwise seen from the front: up means the cross
	// product's y positive.
	ny := (pb[2]-pa[2])*(pc[0]-pa[0]) - (pb[0]-pa[0])*(pc[2]-pa[2])
	if ny > 0 {
		p.Indices = append(p.Indices, base, base+1, base+2)
	} else {
		p.Indices = append(p.Indices, base, base+2, base+1)
	}
}
