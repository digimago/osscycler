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
