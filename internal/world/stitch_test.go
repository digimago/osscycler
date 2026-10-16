package world

import (
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// A carriageway's end and its junction leave a thin wedge between them
// (owner, 2026-10-16, the start of the Route des Américains): stitched.
// A surface's own sharp corner isn't.
func TestStitchSlivers(t *testing.T) {
	tri := func(m int, pts ...[2]float32) gltf.Primitive {
		p := gltf.Primitive{Material: m}
		for _, q := range pts {
			p.Positions = append(p.Positions, q[0], 0, q[1])
			p.Normals = append(p.Normals, 0, 1, 0)
		}
		p.Indices = []uint32{0, 1, 2}
		return p
	}
	// Two pieces of road from one vertex, 0.3 m apart at their far end.
	ps := []gltf.Primitive{
		tri(1, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(1, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{4, 0.3}),
	}
	if n := stitchSlivers(ps); n != 1 {
		t.Errorf("a sliver between two pieces: %d stitched, want 1", n)
	}
	// Another material's piece is no sliver of this one's.
	ps = []gltf.Primitive{
		tri(1, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(2, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{4, 0.3}),
	}
	if n := stitchSlivers(ps); n != 0 {
		t.Errorf("pieces of two materials: %d stitched", n)
	}
	// A sharp corner of one surface: covered inside, left alone.
	ps = []gltf.Primitive{tri(1, [2]float32{0, 0}, [2]float32{4, 0.3}, [2]float32{4, 0})}
	if n := stitchSlivers(ps); n != 0 {
		t.Errorf("a surface's own sharp corner: %d stitched", n)
	}
	// Wider than stitchMaxM: a real gap (an island), left alone.
	ps = []gltf.Primitive{
		tri(1, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(1, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{4, 1.5}),
	}
	if n := stitchSlivers(ps); n != 0 {
		t.Errorf("a gap 1.5 m wide: %d stitched", n)
	}
}

// A sidewalk's end cut against a junction's asphalt leaves the same wedge
// across two materials (Rue du Hohneck, 1.34 km): stitchAcross closes it
// into the asphalt; a sidewalk lying along the road's edge (a kerb line,
// no area between them) gets nothing.
func TestStitchAcross(t *testing.T) {
	tri := func(m int, pts ...[2]float32) gltf.Primitive {
		p := gltf.Primitive{Material: m}
		for _, q := range pts {
			p.Positions = append(p.Positions, q[0], 0, q[1])
			p.Normals = append(p.Normals, 0, 1, 0)
		}
		p.Indices = []uint32{0, 1, 2}
		return p
	}
	const road, walk, grass = 1, 2, 3
	joins := func(m int) bool { return m == road || m == walk }
	first := func(m int) bool { return m == road }
	ps := []gltf.Primitive{
		tri(walk, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(road, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{4, 0.3}),
	}
	if n := stitchAcross(ps, joins, first); n != 1 || len(ps[1].Indices) != 6 {
		t.Errorf("a sliver between sidewalk and asphalt: %d stitched (asphalt %d indices), want 1 into the asphalt", n, len(ps[1].Indices))
	}
	ps = []gltf.Primitive{
		tri(grass, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(road, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{4, 0.3}),
	}
	if n := stitchAcross(ps, joins, first); n != 0 {
		t.Errorf("grass beside asphalt: %d stitched", n)
	}
	// Two edges along one line from a shared vertex: no area, no stitch.
	ps = []gltf.Primitive{
		tri(walk, [2]float32{0, 0}, [2]float32{4, 0}, [2]float32{4, -3}),
		tri(road, [2]float32{0, 0}, [2]float32{4, 3}, [2]float32{3.8, 0}),
	}
	if n := stitchAcross(ps, joins, first); n != 0 {
		t.Errorf("edges along one kerb line: %d stitched", n)
	}
}
