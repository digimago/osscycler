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
	// Narrow and 10 m long: level, a sliver (stitched); falling a metre
	// from one road to another a step lower (16.1 km of the Route des
	// Américains, land in it), left alone.
	long := func(drop float32) []gltf.Primitive {
		ps := []gltf.Primitive{
			tri(1, [2]float32{0, 0}, [2]float32{10, 0}, [2]float32{10, -3}),
			tri(1, [2]float32{0, 0}, [2]float32{10, 3}, [2]float32{10, 0.4}),
		}
		for i := range ps {
			for v := 1; v < 3; v++ {
				ps[i].Positions[3*v+1] = drop // the far ends, the open end among them
			}
		}
		return ps
	}
	if n := stitchSlivers(long(0)); n != 1 {
		t.Errorf("a level sliver 10 m long: %d stitched, want 1", n)
	}
	if n := stitchSlivers(long(1)); n != 0 {
		t.Errorf("a wedge 10 m long falling a metre between two roads: %d stitched", n)
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

// A strip of nothing a hand wide between a sidewalk's end and the asphalt
// beside it (no shared vertex: the land's finest cell can't fill it) is
// zipped shut into the asphalt; a strip wider than seamMaxM, or one in
// the air (a bridge's edge over a road), is not.
func TestStitchSeams(t *testing.T) {
	quad := func(m int, y float32, pts ...[2]float32) gltf.Primitive {
		p := gltf.Primitive{Material: m}
		for _, q := range pts {
			p.Positions = append(p.Positions, q[0], y, q[1])
			p.Normals = append(p.Normals, 0, 1, 0)
		}
		p.Indices = []uint32{0, 1, 2, 0, 2, 3}
		return p
	}
	const road, walk = 1, 2
	joins := func(m int) bool { return m == road || m == walk }
	first := func(m int) bool { return m == road }
	// A sidewalk slab ending at x = 4, the asphalt beginning at x = 4.3.
	seam := func(gap, dy float32) []gltf.Primitive {
		return []gltf.Primitive{
			quad(road, 0, [2]float32{4 + gap, -2}, [2]float32{10, -2}, [2]float32{10, 2}, [2]float32{4 + gap, 2}),
			quad(walk, dy, [2]float32{0, -1}, [2]float32{4, -1}, [2]float32{4, 1}, [2]float32{0, 1}),
		}
	}
	ps := seam(0.3, 0.1)
	if n := stitchSeams(ps, joins, first); n != 1 || len(ps[0].Indices) != 12 {
		t.Errorf("a 0.3 m seam: %d zipped (asphalt %d indices), want 1 into the asphalt", n, len(ps[0].Indices))
	}
	if n := stitchSeams(seam(0.8, 0.1), joins, first); n != 0 {
		t.Errorf("a 0.8 m strip: %d zipped, want none (land)", n)
	}
	if n := stitchSeams(seam(0.3, 2), joins, first); n != 0 {
		t.Errorf("a seam 2 m up (a bridge's edge): %d zipped, want none", n)
	}
}
