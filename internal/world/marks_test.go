package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
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
// drawn through their fronts.
func TestSidewalkNeverRunsIntoABuilding(t *testing.T) {
	l := &roadLine{}
	for k := 0; k <= 50; k++ {
		s := sample{d: float64(k) * 2, e: 0, n: float64(k) * 2, hw: 3}
		s.walk = [2]bool{true, true}
		l.samples = append(l.samples, s)
	}
	// Heading north, right is east: a house from 4 m east of the centre
	// (inside the sidewalk, which runs 3 to 4.8 m) between n 40 and 50.
	house := scenery.Footprint{Outline: [][2]float64{{4, 40}, {14, 40}, {14, 50}, {4, 50}}}
	clearWalksOfBuildings([]*roadLine{l}, []scenery.Footprint{house})
	if l.samples[22].walk[1] {
		t.Error("a sidewalk through the house's front at n 44")
	}
	if !l.samples[22].walk[0] || !l.samples[5].walk[1] {
		t.Error("sidewalks away from the house were dropped")
	}
}

// Water is drawn from its own outline, the land cut away under it: no
// ground stands up into it (green triangles stood in the ditches along
// A sidewalk or cycle lane that stops between two cross-sections ends in
// a taper to the road's edge: ended square, the wedge between it and the
// road widening over the step was a hole (or land showing in the road).
func TestSidewalksAndLanesEndInATaper(t *testing.T) {
	c := northCourse(t)
	for _, which := range []string{"sidewalk", "cycle lane"} {
		tr := testTerrain(c, Options{})
		for i := range tr.fine[:100] { // to 198 m, then none
			if which == "sidewalk" {
				tr.fine[i].walk[1] = true
			} else {
				tr.fine[i].lane[1] = true
			}
		}
		m := addRoadMaterials(gltf.New("test"))
		ps := roadMesh(tr.fine[:200], tr.fine, 0, courseLine(c, nil), false, m, nil)
		covered := func(e, n float64) bool {
			for _, p := range ps {
				for k := 0; k+2 < len(p.Indices); k += 3 {
					var tri [6]float64
					for j := range 3 {
						v := p.Indices[k+j]
						tri[2*j], tri[2*j+1] = float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])
					}
					if math.Abs((tri[2]-tri[0])*(tri[5]-tri[1])-(tri[4]-tri[0])*(tri[3]-tri[1])) > 1e-9 && inTri2(tri, e, n) {
						return true
					}
				}
			}
			return false
		}
		// Heading north, right is east: just inside the road's edge (the
		// lane) or just outside it (the sidewalk), a quarter into the step.
		hw := tr.fine[99].hw
		e := hw - 0.3
		if which == "sidewalk" {
			e = hw + 0.3
		}
		if !covered(e, 198.5) {
			t.Errorf("the %s's end leaves a hole at %.2f m east, 198.5 m north", which, e)
		}
	}
}

// The rider's path never steps back against its way: a point past its
