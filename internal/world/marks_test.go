package world

import (
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// Faults marked on a new course (owner, 2026-10-11), each kept fixed.

// flat is a square of material m at height y, x and z from a to b.
func flat(m int, a, b float64, y float32) gltf.Primitive {
	return gltf.Primitive{Material: m,
		Positions: []float32{float32(a), y, float32(a), float32(b), y, float32(a), float32(b), y, float32(b), float32(a), y, float32(b)},
		Normals:   []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0},
		Indices:   []uint32{0, 2, 1, 0, 3, 2}}
}

// Plants stand on the land as drawn: near a road that is the verge, not
// the terrain mesh cut away under it (bushes floated and sank by up to
// 1.4 m).
func TestPlantsStandOnTheLandAsDrawn(t *testing.T) {
	const land = 1
	s := &surface{drawn: newLandSurface([]gltf.Primitive{flat(land, -4, 4, 3)}, func(m int) bool { return m == land })}
	if h := s.stand(2, 2); h != 3 {
		t.Errorf("a plant on drawn land at 3 m stands at %.2f m", h)
	}
}

// A path across a junction's corner is on the junction, not on open
// ground: no road of the course's own is laid over the corner (it lay
// under the junction's sidewalk, onto the grass, and the rider rode the
// Settling looks both ways across the path for the road: at a junction's
// inside corner the nearest carriageway lies ahead, and a move towards it
// A small piece of land shut in by roads is paved; a larger one stays.
func TestSmallEnclosedLandIsPaved(t *testing.T) {
	const ground, asphalt = 0, 1
	// A road square 0..40 m with a 2 × 2 m hole in it at 10..12, and a
	// 10 × 10 m one at 25..35: rings of road round each.
	ring := func(x0, z0, x1, z1, w float64) []gltf.Primitive {
		r := func(a, b, c, d float64) gltf.Primitive {
			return gltf.Primitive{Material: asphalt,
				Positions: []float32{float32(a), 0, float32(b), float32(c), 0, float32(b), float32(c), 0, float32(d), float32(a), 0, float32(d)},
				Indices:   []uint32{0, 2, 1, 0, 3, 2}}
		}
		return []gltf.Primitive{r(x0-w, z0-w, x1+w, z0), r(x0-w, z1, x1+w, z1+w), r(x0-w, z0, x0, z1), r(x1, z0, x1+w, z1)}
	}
	// Chunk (0, -1): x 0..250 east, z -250..0 south (n 0..250).
	roads := map[[2]int][]gltf.Primitive{{0, 0}: append(ring(10, -12, 12, -10, 3), ring(25, -35, 35, -25, 3)...)}
	paved := pavedIslands(roads, ground, asphalt, 250)
	p, ok := paved[[2]int{0, 0}]
	if !ok {
		t.Fatal("nothing paved")
	}
	in := func(x, z float64) bool {
		for k := 0; k+2 < len(p.Indices); k += 3 {
			var tri [3][3]float64
			for c := range 3 {
				v := p.Indices[k+c]
				tri[c] = [3]float64{float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])}
			}
			if inTri2([6]float64{tri[0][0], tri[0][2], tri[1][0], tri[1][2], tri[2][0], tri[2][2]}, x, z) {
				return true
			}
		}
		return false
	}
	if !in(11, -11) {
		t.Error("the 2 × 2 m piece of land is not paved")
	}
	if in(30, -30) {
		t.Error("the 10 × 10 m piece of land is paved")
	}
}

// Roads joined side by side have no skirt along the edge they share: its
// face showed as a dark line along the seam.
func TestJoinedRoadsHaveNoSkirtBetween(t *testing.T) {
	strip := func(e float64) *roadLine {
		l := &roadLine{}
		for k := 0; k <= 50; k++ {
			l.samples = append(l.samples, sample{d: float64(k) * 2, e: e, n: float64(k) * 2, hw: 2})
		}
		return l
	}
	a, b := strip(0), strip(4.5) // edges 0.5 m apart
	joinAlongside([]*roadLine{a, b}, 5)
	mid := 25
	if !a.samples[mid].butt[1] || !b.samples[mid].butt[0] {
		t.Errorf("the facing edges: butt %v and %v, want no skirt on either", a.samples[mid].butt, b.samples[mid].butt)
	}
	if a.samples[mid].butt[0] || b.samples[mid].butt[1] {
		t.Error("an outer edge lost its skirt")
	}
}

// A sidewalk never runs into a building: houses at the street had it
// Water is drawn from its own outline, the land cut away under it: no
// ground stands up into it (green triangles stood in the ditches along
// A sidewalk or cycle lane that stops between two cross-sections ends in
// a taper to the road's edge: ended square, the wedge between it and the
// The rider's path never steps back against its way: a point past its
