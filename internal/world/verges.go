package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
)

// Verges: with a ground model the roads are cut out of the terrain (owner,
// 2026-10-09: two meshes that only roughly agreed kept showing through
// each other). Every road on the ground, and every junction's free edges,
// carry a verge: a strip of terrain from their edge (or their sidewalk's)
// outward to vergeM, a vertex at each of vergeSteps across, on the ground
// model and in its land cover, its first vertices the road's own edge
// vertices (shared: nothing can overlap or gap). It stops where another
// road begins. The 5 m terrain leaves out every cell the roads and their
// verges cover (terrain.chunk), and isn't flattened under or beside the
// roads any more: it never meets a road. Sunken lanes keep their banks at
// the kerb, embankments their slopes; bridges (aloft) cut nothing and the
// land shows under them.
const vergeM = 9.0

// vergeSteps are a verge's vertices across, from its road's edge: 1 m
// detail near the road, coarser out to vergeM.
var vergeSteps = []float64{0, 0.8, 1.8, 3, 4.5, 6.5, vergeM}

// verger builds verges on the terrain t (its chunks' lists cached in s),
// in the terrain's material.
type verger struct {
	t   *terrain
	s   *surface
	mat int
	// over: surfaces laid over the roads (roundabouts, overlay
	// junctions): verges stop at them as at a road.
	over []patch
}

// onOver tells whether e, n lies on a surface laid over the roads.
func (v *verger) onOver(e, n float64) bool {
	for _, p := range v.over {
		if inPoly(p.poly, e, n) {
			return true
		}
	}
	return false
}

// vpt is a verge vertex: glTF position and colour.
type vpt struct {
	x, y, z float64
	c       [3]float32
}

// lists are the chunk lists for a point (cached).
func (v *verger) lists(e, n float64) chunkLists {
	size := v.t.o.ChunkM
	key := [2]int{int(math.Floor(e / size)), int(math.Floor(n / size))}
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	l, ok := v.s.lists[key]
	if !ok {
		l.segs, l.near = v.t.lists(key)
		v.s.lists[key] = l
	}
	return l
}

// across is a verge across from a road's or junction's edge at glTF x, z
// (height y0) outward along (dx, dz), its offsets from base (the edge's own
// offset along that line, so the first vertex is the edge's) up to limit;
// line is the road it belongs to (-1: a junction), whose own carriageway
// doesn't stop it.
func (v *verger) across(x, z, dx, dz, base, limit, y0 float64, line int) []vpt {
	pts := make([]vpt, len(vergeSteps))
	stopped := false
	for k, st := range vergeSteps {
		off := math.Min(base+st, limit)
		px, pz := x+dx*off, z+dz*off
		e, n := px, -pz
		l := v.lists(e, n)
		// The first step always: it hides the kerb's outer face (a verge
		// stopped at once left the land below the sidewalk showing, as a
		// wall and a pit, at 2.28 km); over a road it is clipped away.
		if k > 1 && (stopped || v.t.roadGap(e, n, l.segs, line) < 0.2 || v.onOver(e, n)) {
			stopped = true
			pts[k] = pts[k-1]
			continue
		}
		h, land, _ := v.t.vertex(e, n, l.segs, l.near)
		if k == 0 {
			h = y0
		}
		c := landColors[land]
		pts[k] = vpt{px, h, pz, [3]float32{c[0], c[1], c[2]}}
	}
	return pts
}

// vergeStrip adds the quads between two verges across (a verge's
// vertices at two cross-sections) to p; ia are a's vertices if already in
// p (nil: added). It returns b's, for the next strip to share.
func vergeStrip(p *gltf.Primitive, a, b []vpt, ia []uint32) []uint32 {
	vert := func(q vpt) uint32 {
		p.Positions = append(p.Positions, float32(q.x), float32(q.y), float32(q.z))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, q.c[0], q.c[1], q.c[2])
		return uint32(len(p.Positions)/3 - 1)
	}
	up := [3]float64{0, 1, 0}
	if ia == nil {
		ia = make([]uint32, len(a))
		for k := range a {
			ia[k] = vert(a[k])
		}
	}
	ib := make([]uint32, len(b))
	for k := range b {
		ib[k] = vert(b[k])
	}
	for k := 0; k+1 < len(a); k++ {
		if a[k+1] == a[k] && b[k+1] == b[k] {
			continue // stopped: nothing there
		}
		tri(p, ia[k], ia[k+1], ib[k], up)
		tri(p, ia[k+1], ib[k+1], ib[k], up)
	}
	return ib
}

// A verge's outer rows (from vergeSteps[vergeDense]) needn't follow
// every cross-section of the road: the ground model is a 5 m grid. They
// skip cross-sections (up to vergeSkip in a row) wherever the line
// between the ones kept passes within vergeTolM of the skipped vertices
// with the same land cover; bends, stops at another road and the ground's
// own shape keep them.
const (
	vergeDense = 3 // rows to 1.8 m: at every cross-section (banks, ditches at the kerb)
	vergeSkip  = 4
	vergeTolM  = 0.15
)

// vergeRun adds a run of a road side's verges (one per cross-section, in
// order) to p: the inner rows as strips, the outer rows over the cross-
// sections kept, fanned to the last inner row so no edge has a vertex in
// its middle (no cracks).
func vergeRun(p *gltf.Primitive, run [][]vpt) {
	if len(run) < 2 {
		return
	}
	rows := len(run[0])
	if rows <= vergeDense {
		var prev []uint32
		for k := 0; k+1 < len(run); k++ {
			prev = vergeStrip(p, run[k], run[k+1], prev)
		}
		return
	}
	// The cross-sections the outer rows keep.
	near := func(a, b, c vpt, t float64) bool {
		x, y, z := a.x+t*(b.x-a.x), a.y+t*(b.y-a.y), a.z+t*(b.z-a.z)
		return math.Sqrt((c.x-x)*(c.x-x)+(c.y-y)*(c.y-y)+(c.z-z)*(c.z-z)) <= vergeTolM && c.c == a.c && c.c == b.c
	}
	keep := []int{0}
	for g0 := 0; g0 < len(run)-1; {
		best := g0 + 1
		for g1 := g0 + 2; g1 <= min(len(run)-1, g0+vergeSkip); g1++ {
			ok := true
			for f := g0 + 1; f < g1 && ok; f++ {
				t := float64(f-g0) / float64(g1-g0)
				for k := vergeDense; k < rows && ok; k++ {
					ok = near(run[g0][k], run[g1][k], run[f][k], t)
				}
			}
			if !ok {
				break
			}
			best = g1
		}
		keep = append(keep, best)
		g0 = best
	}
	vert := func(q vpt) uint32 {
		p.Positions = append(p.Positions, float32(q.x), float32(q.y), float32(q.z))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, q.c[0], q.c[1], q.c[2])
		return uint32(len(p.Positions)/3 - 1)
	}
	up := [3]float64{0, 1, 0}
	// Inner rows at every cross-section.
	R := vergeDense - 1
	inner := make([][]uint32, len(run))
	for f, a := range run {
		inner[f] = make([]uint32, vergeDense)
		for k := range vergeDense {
			inner[f][k] = vert(a[k])
		}
	}
	for f := 0; f+1 < len(run); f++ {
		a, b := run[f], run[f+1]
		for k := 0; k < R; k++ {
			if a[k+1] == a[k] && b[k+1] == b[k] {
				continue // stopped: nothing there
			}
			tri(p, inner[f][k], inner[f][k+1], inner[f+1][k], up)
			tri(p, inner[f][k+1], inner[f+1][k+1], inner[f+1][k], up)
		}
	}
	// Outer rows at the cross-sections kept; between the last inner row
	// and the first outer one, a fan from each kept end to the inner
	// vertices up to the middle.
	outer := map[int][]uint32{}
	for _, g := range keep {
		o := make([]uint32, rows)
		o[R] = inner[g][R]
		for k := vergeDense; k < rows; k++ {
			o[k] = vert(run[g][k])
		}
		outer[g] = o
	}
	for q := 0; q+1 < len(keep); q++ {
		g0, g1 := keep[q], keep[q+1]
		a, b := run[g0], run[g1]
		o0, o1 := outer[g0], outer[g1]
		if !(a[R+1] == a[R] && b[R+1] == b[R]) {
			mid := (g0 + g1 + 1) / 2
			for f := g0; f < g1; f++ {
				anchor := o0[R+1]
				if f >= mid {
					anchor = o1[R+1]
				}
				tri(p, inner[f][R], inner[f+1][R], anchor, up)
			}
			tri(p, inner[mid][R], o1[R+1], o0[R+1], up)
		}
		for k := R + 1; k+1 < rows; k++ {
			if a[k+1] == a[k] && b[k+1] == b[k] {
				continue
			}
			tri(p, o0[k], o0[k+1], o1[k], up)
			tri(p, o0[k+1], o1[k+1], o1[k], up)
		}
	}
}

// footprintM: the footprint's points, this far apart.
const footprintM = 0.5

// rasterize marks, per terrain chunk (chunkM square), the points every
// footprintM (from its south-west corner, its edges included) that the
// primitives' triangles cover seen from above (an edge counts): the
// terrain leaves out a cell only where every point of it is covered, so
// it can't leave a hole, whatever the roads' and verges' shapes.
func rasterize(byChunk map[[2]int][]gltf.Primitive, chunkM float64) map[[2]int][]uint64 {
	side := int(math.Round(chunkM/footprintM)) + 1
	out := map[[2]int][]uint64{}
	mark := func(x, n float64) {
		k := [2]int{int(math.Floor(x / chunkM)), int(math.Floor(n / chunkM))}
		// A point on a chunk's edge is the neighbour's too.
		for _, dk := range [][2]int{{0, 0}, {-1, 0}, {0, -1}, {-1, -1}} {
			c := [2]int{k[0] + dk[0], k[1] + dk[1]}
			pi := int(math.Round((x - float64(c[0])*chunkM) / footprintM))
			pj := int(math.Round((n - float64(c[1])*chunkM) / footprintM))
			if pi < 0 || pi >= side || pj < 0 || pj >= side {
				continue
			}
			bits := out[c]
			if bits == nil {
				bits = make([]uint64, (side*side+63)/64)
				out[c] = bits
			}
			idx := pj*side + pi
			bits[idx/64] |= 1 << (idx % 64)
		}
	}
	for _, ps := range byChunk {
		for _, p := range ps {
			pos := p.Positions
			for t := 0; t+2 < len(p.Indices); t += 3 {
				var xs, ns [3]float64
				for c := range 3 {
					v := p.Indices[t+c]
					xs[c], ns[c] = float64(pos[3*v]), -float64(pos[3*v+2])
				}
				den := (ns[1]-ns[2])*(xs[0]-xs[2]) + (xs[2]-xs[1])*(ns[0]-ns[2])
				if math.Abs(den) < 1e-9 {
					continue // upright (a wall, a skirt): no footprint
				}
				i0 := int(math.Ceil(min(xs[0], xs[1], xs[2]) / footprintM))
				i1 := int(math.Floor(max(xs[0], xs[1], xs[2]) / footprintM))
				j0 := int(math.Ceil(min(ns[0], ns[1], ns[2]) / footprintM))
				j1 := int(math.Floor(max(ns[0], ns[1], ns[2]) / footprintM))
				const eps = 1e-6
				for j := j0; j <= j1; j++ {
					n := float64(j) * footprintM
					for i := i0; i <= i1; i++ {
						x := float64(i) * footprintM
						a := ((ns[1]-ns[2])*(x-xs[2]) + (xs[2]-xs[1])*(n-ns[2])) / den
						b := ((ns[2]-ns[0])*(x-xs[2]) + (xs[0]-xs[2])*(n-ns[2])) / den
						if a >= -eps && b >= -eps && 1-a-b >= -eps {
							mark(x, n)
						}
					}
				}
			}
		}
	}
	return out
}

// Terrain minus roads (owner, 2026-10-09: (terrain + verges) − roads):
// every terrain or verge triangle that a road surface (carriageway,
// sidewalk, junction, roundabout) covers seen from above is clipped to
// what lies outside it, its new vertices taking the triangle's own height,
// normal and colour; so no terrain can lie over a road, wherever roads
// cross, meet or run close. Road triangles are convex, so taking one out
// of a convex piece is a few half-plane clips.

// cutter holds the road surface triangles (glTF x, z), by cell.
type cutter struct {
	tris  [][3][2]float64
	cells map[[2]int][]int
}

const cutterCellM = 4.0

func newCutter() *cutter { return &cutter{cells: map[[2]int][]int{}} }

// add takes p's triangles that aren't upright as road surface.
func (c *cutter) add(p gltf.Primitive) {
	pos := p.Positions
	for t := 0; t+2 < len(p.Indices); t += 3 {
		var tr [3][2]float64
		for k := range 3 {
			v := p.Indices[t+k]
			tr[k] = [2]float64{float64(pos[3*v]), float64(pos[3*v+2])}
		}
		if math.Abs(cross2(tr[0], tr[1], tr[2])) < 1e-6 {
			continue // upright: a wall, a skirt
		}
		if cross2(tr[0], tr[1], tr[2]) < 0 {
			tr[1], tr[2] = tr[2], tr[1] // anticlockwise in x, z
		}
		id := len(c.tris)
		c.tris = append(c.tris, tr)
		lo, hi := bbox(tr[:])
		for gx := int(math.Floor(lo[0] / cutterCellM)); gx <= int(math.Floor(hi[0]/cutterCellM)); gx++ {
			for gz := int(math.Floor(lo[1] / cutterCellM)); gz <= int(math.Floor(hi[1]/cutterCellM)); gz++ {
				c.cells[[2]int{gx, gz}] = append(c.cells[[2]int{gx, gz}], id)
			}
		}
	}
}

// lessTri orders triangles by their corners' coordinates.
func lessTri(a, b [3][2]float64) bool {
	for k := range 3 {
		for i := range 2 {
			if a[k][i] != b[k][i] {
				return a[k][i] < b[k][i]
			}
		}
	}
	return false
}

func cross2(a, b, c [2]float64) float64 {
	return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
}

func bbox(ps [][2]float64) (lo, hi [2]float64) {
	lo, hi = [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, p := range ps {
		lo[0], lo[1], hi[0], hi[1] = math.Min(lo[0], p[0]), math.Min(lo[1], p[1]), math.Max(hi[0], p[0]), math.Max(hi[1], p[1])
	}
	return lo, hi
}

// clipHalf is the convex polygon ps on the left of a→b (keepLeft) or on
// its right.
func clipHalf(ps [][2]float64, a, b [2]float64, keepLeft bool) [][2]float64 {
	side := func(p [2]float64) float64 {
		s := cross2(a, b, p)
		if !keepLeft {
			s = -s
		}
		return s
	}
	var out [][2]float64
	for i := range ps {
		p, q := ps[i], ps[(i+1)%len(ps)]
		sp, sq := side(p), side(q)
		if sp >= 0 {
			out = append(out, p)
		}
		if (sp >= 0) != (sq >= 0) {
			f := sp / (sp - sq)
			out = append(out, [2]float64{p[0] + f*(q[0]-p[0]), p[1] + f*(q[1]-p[1])})
		}
	}
	return out
}

func area2(ps [][2]float64) float64 {
	a := 0.0
	for i := range ps {
		a += cross2([2]float64{}, ps[i], ps[(i+1)%len(ps)])
	}
	return a / 2
}

// subtract is convex polygon p (anticlockwise) without triangle t
// (anticlockwise): up to three convex pieces.
func subtract(p [][2]float64, t [3][2]float64) [][][2]float64 {
	var out [][][2]float64
	rem := p
	for k := range 3 {
		a, b := t[k], t[(k+1)%3]
		if outside := clipHalf(rem, a, b, false); len(outside) >= 3 && area2(outside) > 1e-6 {
			out = append(out, outside)
		}
		rem = clipHalf(rem, a, b, true)
		if len(rem) < 3 || area2(rem) <= 1e-9 {
			return out // no overlap: all of it is in the pieces outside
		}
	}
	return out // rem (the overlap) is dropped
}

// clip is p without what the road surfaces cover.
func (c *cutter) clip(p gltf.Primitive) gltf.Primitive {
	out := gltf.Primitive{Material: p.Material}
	pos := p.Positions
	hasN, hasC, hasUV := len(p.Normals) > 0, len(p.Colors) > 0, len(p.UVs) > 0
	reuse := map[uint32]uint32{}
	keep := func(v uint32) uint32 {
		if w, ok := reuse[v]; ok {
			return w
		}
		w := uint32(len(out.Positions) / 3)
		out.Positions = append(out.Positions, pos[3*v:3*v+3]...)
		if hasN {
			out.Normals = append(out.Normals, p.Normals[3*v:3*v+3]...)
		}
		if hasC {
			out.Colors = append(out.Colors, p.Colors[3*v:3*v+3]...)
		}
		if hasUV {
			out.UVs = append(out.UVs, p.UVs[2*v:2*v+2]...)
		}
		reuse[v] = w
		return w
	}
	for t := 0; t+2 < len(p.Indices); t += 3 {
		idx := [3]uint32{p.Indices[t], p.Indices[t+1], p.Indices[t+2]}
		var tr [3][2]float64
		for k := range 3 {
			tr[k] = [2]float64{float64(pos[3*idx[k]]), float64(pos[3*idx[k]+2])}
		}
		lo, hi := bbox(tr[:])
		seen := map[int]bool{}
		var cands []int
		for gx := int(math.Floor(lo[0] / cutterCellM)); gx <= int(math.Floor(hi[0]/cutterCellM)); gx++ {
			for gz := int(math.Floor(lo[1] / cutterCellM)); gz <= int(math.Floor(hi[1]/cutterCellM)); gz++ {
				for _, id := range c.cells[[2]int{gx, gz}] {
					if !seen[id] {
						seen[id] = true
						clo, chi := bbox(c.tris[id][:])
						if clo[0] < hi[0] && chi[0] > lo[0] && clo[1] < hi[1] && chi[1] > lo[1] {
							cands = append(cands, id)
						}
					}
				}
			}
		}
		// By the cutting triangles' own coordinates, not by when they were
		// added: the order of the clips is the pieces', and the roads come
		// in map order (the Posbank Loop's terrain had differed from build
		// to build).
		sort.Slice(cands, func(i, j int) bool { return lessTri(c.tris[cands[i]], c.tris[cands[j]]) })
		den := cross2(tr[0], tr[1], tr[2])
		if len(cands) == 0 || math.Abs(den) < 1e-9 {
			out.Indices = append(out.Indices, keep(idx[0]), keep(idx[1]), keep(idx[2]))
			continue
		}
		flip := den < 0
		poly := [][2]float64{tr[0], tr[1], tr[2]}
		if flip {
			poly[1], poly[2] = poly[2], poly[1]
		}
		pieces := [][][2]float64{poly}
		for _, id := range cands {
			var next [][][2]float64
			for _, pc := range pieces {
				next = append(next, subtract(pc, c.tris[id])...)
			}
			pieces = next
			if len(pieces) == 0 {
				break
			}
		}
		if len(pieces) == 1 && len(pieces[0]) == 3 && pieces[0][0] == poly[0] && pieces[0][1] == poly[1] && pieces[0][2] == poly[2] {
			out.Indices = append(out.Indices, keep(idx[0]), keep(idx[1]), keep(idx[2]))
			continue
		}
		// New vertices: the original triangle's attributes, interpolated.
		vert := func(q [2]float64) uint32 {
			a := cross2(q, tr[1], tr[2]) / den
			b := cross2(tr[0], q, tr[2]) / den
			w := [3]float64{a, b, 1 - a - b}
			mix := func(attr []float32, n int, k int) float32 {
				s := 0.0
				for i := range 3 {
					s += w[i] * float64(attr[n*int(idx[i])+k])
				}
				return float32(s)
			}
			out.Positions = append(out.Positions, float32(q[0]), mix(pos, 3, 1), float32(q[1]))
			if hasN {
				nx, ny, nz := mix(p.Normals, 3, 0), mix(p.Normals, 3, 1), mix(p.Normals, 3, 2)
				l := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz)))
				if l == 0 {
					l = 1
				}
				out.Normals = append(out.Normals, nx/l, ny/l, nz/l)
			}
			if hasC {
				out.Colors = append(out.Colors, mix(p.Colors, 3, 0), mix(p.Colors, 3, 1), mix(p.Colors, 3, 2))
			}
			if hasUV {
				out.UVs = append(out.UVs, mix(p.UVs, 2, 0), mix(p.UVs, 2, 1))
			}
			return uint32(len(out.Positions)/3 - 1)
		}
		for _, pc := range pieces {
			vs := make([]uint32, len(pc))
			for i, q := range pc {
				vs[i] = vert(q)
			}
			for i := 1; i+1 < len(vs); i++ {
				if flip {
					out.Indices = append(out.Indices, vs[0], vs[i+1], vs[i])
				} else {
					out.Indices = append(out.Indices, vs[0], vs[i], vs[i+1])
				}
			}
		}
	}
	return out
}

// dist is how far glTF x, z is from the road surfaces (0 on one), up to
// max (max when none is nearer).
func (c *cutter) dist(x, z, max float64) float64 {
	best := max
	p := [2]float64{x, z}
	for gx := int(math.Floor((x - max) / cutterCellM)); gx <= int(math.Floor((x+max)/cutterCellM)); gx++ {
		for gz := int(math.Floor((z - max) / cutterCellM)); gz <= int(math.Floor((z+max)/cutterCellM)); gz++ {
			for _, id := range c.cells[[2]int{gx, gz}] {
				t := c.tris[id]
				if cross2(t[0], t[1], p) >= 0 && cross2(t[1], t[2], p) >= 0 && cross2(t[2], t[0], p) >= 0 {
					return 0
				}
				for k := range 3 {
					a, b := t[k], t[(k+1)%3]
					best = math.Min(best, segDist(a, b, x, z))
				}
			}
		}
	}
	return best
}
