package world

import (
	"math"
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
}

type chunkLists struct {
	segs []int
	near []sample
}

func (s *surface) at(e, n float64) float64 {
	t := s.t
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
	return drapedIn(height, outline, material, lift, drapeCellM)
}

// drapedIn is draped with triangles at most cell across; cell 0 leaves the
// outline's triangles whole (a junction: its outline has a point every
// 2 m along each road, at the road's height, and is near-flat between).
func drapedIn(height func(e, n float64) float64, outline [][2]float64, material int, lift, cell float64) gltf.Primitive {
	p := gltf.Primitive{Material: material}
	tris := earClip(outline)
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
