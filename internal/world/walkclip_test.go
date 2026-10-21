package world

import (
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// A junction's sidewalk reaching over another road's carriageway (owner,
// 2026-10-16, the roundabouts of the Route des Américains): the slab is
// clipped along the carriageway's edge and closed with a face; the part
// off the road stays, nothing is left over the road.
func TestSidewalksAreClippedOffCarriageways(t *testing.T) {
	quadPrim := func(m int, x0, z0, x1, z1, y float32) gltf.Primitive {
		p := gltf.Primitive{Material: m}
		p.Positions = []float32{x0, y, z0, x1, y, z0, x1, y, z1, x0, y, z1}
		p.Normals = []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0}
		p.Indices = []uint32{0, 2, 1, 0, 3, 2}
		return p
	}
	const road, walk, kerb = 1, 2, 3
	chunks := map[[2]int][]gltf.Primitive{{0, 0}: {
		quadPrim(road, 0, 0, 10, 6, 0),     // a carriageway 6 m wide
		quadPrim(walk, 4, -2, 6, 3, kerbM), // a sidewalk from beside it to halfway in
	}}
	n := clearWalksOffRoads(chunks, walk, kerb, func(m int) bool { return m == road })
	if n == 0 {
		t.Fatal("nothing cleared")
	}
	w := chunks[[2]int{0, 0}][1]
	maxZ, faces := float32(-1e9), 0
	for i := 0; i+2 < len(w.Indices); i += 3 {
		a, b, c := w.Indices[i], w.Indices[i+1], w.Indices[i+2]
		ys := []float32{w.Positions[3*a+1], w.Positions[3*b+1], w.Positions[3*c+1]}
		if ys[0] < kerbM-0.01 || ys[1] < kerbM-0.01 || ys[2] < kerbM-0.01 {
			faces++
			continue
		}
		for _, v := range []uint32{a, b, c} {
			maxZ = max(maxZ, w.Positions[3*v+2])
		}
	}
	if maxZ > 0.01 {
		t.Errorf("the sidewalk reaches %.2f m over the carriageway (its edge at z 0)", maxZ)
	}
	if maxZ < -0.01 {
		t.Errorf("the sidewalk off the road is gone too (reaches only z %.2f)", maxZ)
	}
	if faces == 0 {
		t.Error("the clipped edge is open: no face closes it")
	}
}

// A sidewalk slab's edge nothing closes gets a face down into the ground
// (owner, 2026-10-16, 0.51 km: looking through a sidewalk); an edge a
// face already hangs from, or another slab continues, doesn't.
func TestOpenSidewalkEdgesAreClosed(t *testing.T) {
	slab := func(x0, x1 float32) gltf.Primitive {
		p := gltf.Primitive{Material: 2}
		p.Positions = []float32{x0, kerbM, 0, x1, kerbM, 0, x1, kerbM, 2, x0, kerbM, 2}
		p.Normals = []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0}
		p.Indices = []uint32{0, 2, 1, 0, 3, 2}
		return p
	}
	// Two slabs end to end: their shared edge is no opening; the other
	// six edges are.
	chunks := map[[2]int][]gltf.Primitive{{0, 0}: {slab(0, 3), slab(3, 6)}}
	if n := closeOpenWalks(chunks, 2, 3); n != 6 {
		t.Errorf("%d edges closed, want 6 (the shared one stays open)", n)
	}
	// With a kerb face hanging from the long edge at z 0 of the first.
	kerb := gltf.Primitive{Material: 3}
	kerb.Positions = []float32{0, kerbM, 0, 3, kerbM, 0, 3, 0, 0, 0, 0, 0}
	kerb.Indices = []uint32{0, 1, 2, 0, 2, 3}
	chunks = map[[2]int][]gltf.Primitive{{0, 0}: {slab(0, 3), slab(3, 6), kerb}}
	if n := closeOpenWalks(chunks, 2, 3); n != 5 {
		t.Errorf("%d edges closed, want 5 (one has its kerb)", n)
	}
}

// Slabs whose shared edge doesn't match to the bit (clipped pieces) are
// one sidewalk there: no face hangs inside it (owner, 2026-10-16: the
// sidewalk's edge flickered where such faces stood).
func TestSidewalkSeamsAreNotClosed(t *testing.T) {
	slab := func(x0, x1 float32) gltf.Primitive {
		p := gltf.Primitive{Material: 2}
		p.Positions = []float32{x0, kerbM, 0, x1, kerbM, 0, x1, kerbM, 2, x0, kerbM, 2}
		p.Normals = []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0}
		p.Indices = []uint32{0, 2, 1, 0, 3, 2}
		return p
	}
	// The second slab starts a millimetre past where the first ends.
	chunks := map[[2]int][]gltf.Primitive{{0, 0}: {slab(0, 3), slab(3.001, 6)}}
	if n := closeOpenWalks(chunks, 2, 3); n != 6 {
		t.Errorf("%d edges closed, want the outline's 6, not the seam", n)
	}
}

// On a climbing road a sidewalk's kerb face rises along it: its top edge
// still closes the slab's edge (owner, 2026-10-16: duplicate faces on
// sloped sidewalks flickered).
func TestSlopedSidewalkKeepsItsKerb(t *testing.T) {
	// A slab 6 m long climbing 4 %, its kerb face along z = 0.
	y := func(x float32) float32 { return kerbM + 0.04*x }
	s := gltf.Primitive{Material: 2}
	s.Positions = []float32{0, y(0), 0, 6, y(6), 0, 6, y(6), 2, 0, y(0), 2}
	s.Indices = []uint32{0, 2, 1, 0, 3, 2}
	k := gltf.Primitive{Material: 3}
	k.Positions = []float32{0, y(0), 0, 6, y(6), 0, 6, y(6) - kerbM, 0, 0, y(0) - kerbM, 0}
	k.Indices = []uint32{0, 1, 2, 0, 2, 3}
	chunks := map[[2]int][]gltf.Primitive{{0, 0}: {s, k}}
	if n := closeOpenWalks(chunks, 2, 3); n != 3 {
		t.Errorf("%d edges closed, want 3 (the kerb's edge has its face)", n)
	}
}
