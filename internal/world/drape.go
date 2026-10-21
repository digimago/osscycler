package world

import (
	"math"
	"sort"
	"sync"

	"github.com/digimago/osscycler/internal/gltf"
)

// Flat things on the ground beside the route, laid on the terrain mesh: the
// roads branching off and the car parks. Each sits a little above the
// terrain, branches above car parks (an entrance runs into its car park),
// both below the route's road where they meet it.
const (
	parkingLiftM = 0.03
	drapeCellM   = 4.0 // a car park's triangles are at most this big
	branchStartM = 0.3 // beyond the route's edge, a branch lies on the ground
)

// surface is the terrain mesh's height anywhere: the chunk's vertices
// around the point, interpolated over the triangle the mesh has there.
type surface struct {
	t     *terrain
	mu    sync.Mutex
	lists map[[2]int]chunkLists
	// drawn is the land as drawn (terrain and verges, the roads cut out),
	// once there is one: what plants stand on (stand).
	drawn *landSurface
}

// stand is where a plant stands at e, n: on the land as drawn, which near
// a road is the verge rather than the terrain mesh cut away under it
// (owner, 2026-10-11: bushes floated and sank by up to 1.4 m beside the
// roads); the terrain mesh where no land is drawn.
func (s *surface) stand(e, n float64) float64 {
	if s.drawn != nil {
		if h, ok := s.drawn.at(e, n); ok {
			return h
		}
	}
	return s.at(e, n)
}

type chunkLists struct {
	segs []int
	near []sample
}

func (s *surface) at(e, n float64) float64 {
	t := s.t
	if t.landAt != nil {
		return t.landAt(e, n)
	}
	size := t.o.ChunkM
	cells := max(1, int(math.Round(size/t.o.CellM)))
	l := t.lod()
	return l.at(e, n, func(I, J int) float64 {
		key := [2]int{floorDiv(I, cells), floorDiv(J, cells)}
		l.mu.Lock()
		h, ok := l.raw[key]
		l.mu.Unlock()
		if ok {
			side := cells + 3
			return h[(J-key[1]*cells+1)*side+I-key[0]*cells+1]
		}
		s.mu.Lock()
		l, ok := s.lists[key]
		if !ok {
			l.segs, l.near = t.lists(key)
			s.lists[key] = l
		}
		s.mu.Unlock()
		h0, _, _ := t.vertex(float64(I)*size/float64(cells), float64(J)*size/float64(cells), l.segs, l.near)
		return h0
	})
}

// ground is the ground at e, n as the terrain's vertex there has it
// (with a cut terrain, the ground model; under water, its bed): what the
// verges stand on, between the terrain mesh's vertices too.
func (s *surface) ground(e, n float64) float64 {
	t := s.t
	if t.landAt != nil {
		return t.landAt(e, n)
	}
	size := t.o.ChunkM
	key := [2]int{int(math.Floor(e / size)), int(math.Floor(n / size))}
	s.mu.Lock()
	l, ok := s.lists[key]
	if !ok {
		l.segs, l.near = t.lists(key)
		s.lists[key] = l
	}
	s.mu.Unlock()
	h, _, _ := t.vertex(e, n, l.segs, l.near)
	return h
}

// parkingMesh is a car park's surface laid on the ground.
func parkingMesh(s *surface, outline [][2]float64, material int) gltf.Primitive {
	return draped(s.at, outline, material, parkingLiftM)
}

// draped is a flat area laid on a surface (height at east, north), lift
// above it: its outline triangulated, every triangle split alike into
// pieces at most drapeCellM across (so shared edges split the same and
// nothing cracks).
func draped(height func(e, n float64) float64, outline [][2]float64, material int, lift float64) gltf.Primitive {
	return drapedIn(height, outline, material, lift, drapeCellM, false)
}

// drapedIn is draped with triangles at most cell across; cell 0 leaves the
// outline's triangles whole (a junction: its outline has a point every
// 2 m along each road, at the road's height, and is near-flat between).
// fat flips the outline's triangles to Delaunay's (delaunayFlip).
func drapedIn(height func(e, n float64) float64, outline [][2]float64, material int, lift, cell float64, fat bool) gltf.Primitive {
	p := gltf.Primitive{Material: material}
	tris := earClip(outline)
	if fat {
		tris = delaunayFlip(outline, tris)
	}
	longest := 0.0
	for _, t := range tris {
		for k := range 3 {
			a, b := outline[t[k]], outline[t[(k+1)%3]]
			longest = math.Max(longest, math.Hypot(b[0]-a[0], b[1]-a[1]))
		}
	}
	n := 1
	if cell > 0 {
		n = min(40, max(1, int(math.Ceil(longest/cell))))
	}
	for _, t := range tris {
		a, b, c := outline[t[0]], outline[t[1]], outline[t[2]]
		idx := map[[2]int]uint32{}
		vtx := func(i, j int) uint32 {
			if v, ok := idx[[2]int{i, j}]; ok {
				return v
			}
			fi, fj := float64(i)/float64(n), float64(j)/float64(n)
			e := a[0] + fi*(b[0]-a[0]) + fj*(c[0]-a[0])
			no := a[1] + fi*(b[1]-a[1]) + fj*(c[1]-a[1])
			p.Positions = append(p.Positions, float32(e), float32(height(e, no)+lift), float32(-no))
			p.Normals = append(p.Normals, 0, 1, 0)
			p.UVs = append(p.UVs, float32(e/5), float32(no/5))
			v := uint32(len(p.Positions)/3 - 1)
			idx[[2]int{i, j}] = v
			return v
		}
		for i := 0; i < n; i++ {
			for j := 0; i+j < n; j++ {
				tri(&p, vtx(i, j), vtx(i+1, j), vtx(i, j+1), [3]float64{0, 1, 0})
				if i+j+2 <= n {
					tri(&p, vtx(i+1, j), vtx(i+1, j+1), vtx(i, j+1), [3]float64{0, 1, 0})
				}
			}
		}
	}
	smoothNormals(&p)
	return p
}

// smoothNormals sets each vertex's normal from the faces around it.
func smoothNormals(p *gltf.Primitive) {
	acc := make([]float64, len(p.Positions))
	pos := func(i uint32) [3]float64 {
		return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
	}
	for k := 0; k+2 < len(p.Indices); k += 3 {
		ia, ib, ic := p.Indices[k], p.Indices[k+1], p.Indices[k+2]
		a, b, c := pos(ia), pos(ib), pos(ic)
		u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
		v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
		n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
		for _, i := range []uint32{ia, ib, ic} {
			for d := range 3 {
				acc[3*i+uint32(d)] += n[d]
			}
		}
	}
	for i := 0; i+2 < len(acc); i += 3 {
		l := math.Sqrt(acc[i]*acc[i] + acc[i+1]*acc[i+1] + acc[i+2]*acc[i+2])
		if l == 0 {
			continue
		}
		p.Normals[i], p.Normals[i+1], p.Normals[i+2] = float32(acc[i]/l), float32(acc[i+1]/l), float32(acc[i+2]/l)
	}
}

// earClip triangulates a simple polygon (no holes, either winding): the
// triangles as indices into pts, counter-clockwise seen from above (east,
// north). Degenerate corners (repeated or collinear points) are dropped.
func earClip(pts [][2]float64) [][3]int {
	n := len(pts)
	if n < 3 {
		return nil
	}
	idx := make([]int, n)
	area := 0.0
	for i := range n {
		idx[i] = i
		j := (i + 1) % n
		area += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
	}
	if area < 0 { // clockwise: walk it the other way
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			idx[i], idx[j] = idx[j], idx[i]
		}
	}
	cross := func(a, b, c [2]float64) float64 {
		return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
	}
	inside := func(p, a, b, c [2]float64) bool {
		return cross(a, b, p) > 0 && cross(b, c, p) > 0 && cross(c, a, p) > 0
	}
	var out [][3]int
	for len(idx) > 3 {
		cut := false
		for k := range idx {
			ia, ib, ic := idx[(k+len(idx)-1)%len(idx)], idx[k], idx[(k+1)%len(idx)]
			a, b, c := pts[ia], pts[ib], pts[ic]
			cr := cross(a, b, c)
			if math.Abs(cr) < 1e-9 { // a repeated or straight corner
				idx = append(idx[:k], idx[k+1:]...)
				cut = true
				break
			}
			if cr < 0 {
				continue // reflex
			}
			ear := true
			for _, o := range idx {
				if o != ia && o != ib && o != ic && inside(pts[o], a, b, c) {
					ear = false
					break
				}
			}
			if ear {
				out = append(out, [3]int{ia, ib, ic})
				idx = append(idx[:k], idx[k+1:]...)
				cut = true
				break
			}
		}
		if !cut {
			break // not simple: leave the rest out rather than fold over
		}
	}
	if len(idx) == 3 {
		if a, b, c := pts[idx[0]], pts[idx[1]], pts[idx[2]]; cross(a, b, c) > 1e-9 {
			out = append(out, [3]int{idx[0], idx[1], idx[2]})
		}
	}
	return out
}

// delaunayFlip turns a polygon's triangles (earClip's, counter-clockwise,
// indices into pts) into the Delaunay triangulation of the polygon: every
// edge inside it is flipped while the other diagonal of its quadrilateral
// lies inside and makes the smaller angle larger (Lawson). Ear clipping
// fans out from one corner, and a junction's long fan triangles, each
// with its own normal at the outline, showed as streaks across it
// (owner, 2026-10-13, Dunes course 13.02 km); the outline stays as it is.
func delaunayFlip(pts [][2]float64, tris [][3]int) [][3]int {
	out := append([][3]int(nil), tris...)
	type edge [2]int
	key := func(a, b int) edge {
		if a > b {
			a, b = b, a
		}
		return edge{a, b}
	}
	// inCircle: whether d lies inside the circle through a, b, c
	// (counter-clockwise).
	inCircle := func(a, b, c, d [2]float64) bool {
		ax, ay := a[0]-d[0], a[1]-d[1]
		bx, by := b[0]-d[0], b[1]-d[1]
		cx, cy := c[0]-d[0], c[1]-d[1]
		det := (ax*ax+ay*ay)*(bx*cy-cx*by) - (bx*bx+by*by)*(ax*cy-cx*ay) + (cx*cx+cy*cy)*(ax*by-bx*ay)
		return det > 1e-9
	}
	ccw := func(a, b, c [2]float64) bool {
		return (b[0]-a[0])*(c[1]-a[1])-(b[1]-a[1])*(c[0]-a[0]) > 1e-9
	}
	for pass := 0; pass < 4*len(out)+8; pass++ {
		at := map[edge][]int{}
		for i, t := range out {
			for k := range 3 {
				e := key(t[k], t[(k+1)%3])
				at[e] = append(at[e], i)
			}
		}
		// Edges in a fixed order, so the result never depends on the map's.
		edges := make([]edge, 0, len(at))
		for e, ts := range at {
			if len(ts) == 2 {
				edges = append(edges, e)
			}
		}
		sort.Slice(edges, func(i, j int) bool {
			if edges[i][0] != edges[j][0] {
				return edges[i][0] < edges[j][0]
			}
			return edges[i][1] < edges[j][1]
		})
		flipped := false
		for _, e := range edges {
			ts := at[e]
			t0, t1 := out[ts[0]], out[ts[1]]
			// The corners opposite the edge.
			opp := func(t [3]int) int {
				for _, v := range t {
					if v != e[0] && v != e[1] {
						return v
					}
				}
				return -1
			}
			c, d := opp(t0), opp(t1)
			// a, b in t0's counter-clockwise order, so t0 is (a, b, c).
			a, b := e[0], e[1]
			for k := range 3 {
				if t0[k] == e[1] && t0[(k+1)%3] == e[0] {
					a, b = e[1], e[0]
				}
			}
			if !inCircle(pts[a], pts[b], pts[c], pts[d]) {
				continue
			}
			// The new diagonal c-d must lie inside (a convex quadrilateral).
			if !ccw(pts[c], pts[a], pts[d]) || !ccw(pts[d], pts[b], pts[c]) {
				continue
			}
			out[ts[0]] = [3]int{c, a, d}
			out[ts[1]] = [3]int{d, b, c}
			flipped = true
			break // the edge map is stale: start again
		}
		if !flipped {
			break
		}
	}
	return out
}

// gridDraped is a flat area laid on a surface (height at east, north),
// lift above it: its outline triangulated and each triangle cut along a
// fixed grid of cell, the pieces sharing their vertices by position
// (owner, 2026-10-18, the car parks along the Route des Crêtes at 19.7 km:
// split alike into n × n pieces, n set by the longest edge in the whole
// outline, a sliver along the road became a fan of slivers with none of
// its vertices shared, each sampling its own heights and normals, and the
// gravel rippled with light and dark bands on the slope).
func gridDraped(height func(e, n float64) float64, outline [][2]float64, material int, lift, cell float64) gltf.Primitive {
	p := gltf.Primitive{Material: material}
	idx := map[[2]int64]uint32{}
	vtx := func(q [2]float64) uint32 {
		k := [2]int64{int64(math.Round(q[0] * 1000)), int64(math.Round(q[1] * 1000))}
		if v, ok := idx[k]; ok {
			return v
		}
		e, no := float64(k[0])/1000, float64(k[1])/1000
		p.Positions = append(p.Positions, float32(e), float32(height(e, no)+lift), float32(-no))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.UVs = append(p.UVs, float32(e/5), float32(no/5))
		v := uint32(len(p.Positions)/3 - 1)
		idx[k] = v
		return v
	}
	// clip keeps the part of poly where s·(q - o) >= 0 along axis ax.
	clip := func(poly [][2]float64, ax int, at, s float64) [][2]float64 {
		var out [][2]float64
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			da, db := s*(a[ax]-at), s*(b[ax]-at)
			if da >= 0 {
				out = append(out, a)
			}
			if da*db < 0 {
				f := da / (da - db)
				q := [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])}
				q[ax] = at // exactly on the grid line: neighbours share it
				out = append(out, q)
			}
		}
		return out
	}
	up := [3]float64{0, 1, 0}
	fan := func(pc [][2]float64) {
		var vs []uint32
		for _, q := range pc {
			v := vtx(q)
			if len(vs) == 0 || vs[len(vs)-1] != v {
				vs = append(vs, v)
			}
		}
		if len(vs) > 1 && vs[0] == vs[len(vs)-1] {
			vs = vs[:len(vs)-1]
		}
		if len(vs) < 3 {
			return
		}
		ring := make([][2]float64, len(vs))
		for i, v := range vs {
			ring[i] = pc2(p, v)
		}
		for _, t := range earClip(ring) {
			a, b, c := ring[t[0]], ring[t[1]], ring[t[2]]
			if math.Abs((b[0]-a[0])*(c[1]-a[1])-(b[1]-a[1])*(c[0]-a[0])) < 1e-6 {
				continue
			}
			tri(&p, vs[t[0]], vs[t[1]], vs[t[2]], up)
		}
	}
	// The cells the outline's edges pass through: cut there; every other
	// cell within its box is whole, inside or out.
	edge := map[[2]int]bool{}
	lo := [2]float64{math.Inf(1), math.Inf(1)}
	hi := [2]float64{math.Inf(-1), math.Inf(-1)}
	for i, a := range outline {
		b := outline[(i+1)%len(outline)]
		lo = [2]float64{math.Min(lo[0], a[0]), math.Min(lo[1], a[1])}
		hi = [2]float64{math.Max(hi[0], a[0]), math.Max(hi[1], a[1])}
		steps := int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1])/(cell/4))) + 1
		for k := 0; k <= steps; k++ {
			f := float64(k) / float64(steps)
			e, n := a[0]+f*(b[0]-a[0]), a[1]+f*(b[1]-a[1])
			gx, gn := int(math.Floor(e/cell)), int(math.Floor(n/cell))
			for dx := -1; dx <= 1; dx++ { // a point on a grid line touches both
				for dn := -1; dn <= 1; dn++ {
					x0, n0 := float64(gx+dx)*cell, float64(gn+dn)*cell
					if e >= x0-1e-9 && e <= x0+cell+1e-9 && n >= n0-1e-9 && n <= n0+cell+1e-9 {
						edge[[2]int{gx + dx, gn + dn}] = true
					}
				}
			}
		}
	}
	for gx := int(math.Floor(lo[0] / cell)); float64(gx)*cell < hi[0]; gx++ {
		for gn := int(math.Floor(lo[1] / cell)); float64(gn)*cell < hi[1]; gn++ {
			x0, n0 := float64(gx)*cell, float64(gn)*cell
			sq := [][2]float64{{x0, n0}, {x0 + cell, n0}, {x0 + cell, n0 + cell}, {x0, n0 + cell}}
			if !edge[[2]int{gx, gn}] {
				if inPoly(outline, x0+cell/2, n0+cell/2) {
					fan(sq)
				}
				continue
			}
			// The outline clipped to the cell (the cell is convex).
			pc := clip(outline, 0, x0, 1)
			pc = clip(pc, 0, x0+cell, -1)
			pc = clip(pc, 1, n0, 1)
			pc = clip(pc, 1, n0+cell, -1)
			if len(pc) >= 3 {
				fan(pc)
			}
		}
	}
	smoothNormals(&p)
	return p
}

// pc2 is vertex v's east, north.
func pc2(p gltf.Primitive, v uint32) [2]float64 {
	return [2]float64{float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])}
}
