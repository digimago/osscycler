package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// Land shut in between a road and a car park 5 cm lower is paved, under
// both, out to a cell under them (owner, 2026-10-18, 19.51 km of the
// Hohneck loop: a green triangle between the road's edge and the car
// park's outline).
func TestLandBetweenRoadAndCarParkIsPaved(t *testing.T) {
	const road, lot, ground, paving = 1, 2, 3, 4
	quad := func(p *gltf.Primitive, x0, z0, x1, z1, y float64) {
		b := uint32(len(p.Positions) / 3)
		p.Positions = append(p.Positions, float32(x0), float32(y), float32(z0), float32(x1), float32(y), float32(z0), float32(x1), float32(y), float32(z1), float32(x0), float32(y), float32(z1))
		p.Indices = append(p.Indices, b, b+2, b+1, b, b+3, b+2)
	}
	// A road round a 3 m square of land (x 100-103, z -150..-147).
	r := gltf.Primitive{Material: road}
	quad(&r, 95, -155, 108, -150, 10)
	quad(&r, 95, -147, 108, -142, 10)
	quad(&r, 95, -150, 100, -147, 10)
	quad(&r, 103, -150, 108, -147, 10)
	// A car park over half the square, 5 cm lower.
	l := gltf.Primitive{Material: lot, Positions: []float32{100, 9.95, -150, 103, 9.95, -150, 100, 9.95, -147}, Indices: []uint32{0, 2, 1}}
	roads := map[[2]int][]gltf.Primitive{{0, 0}: {r}}
	if got := pavedIslandsGrown(roads, ground, paving, 250, true); len(got) != 0 {
		t.Fatalf("9 m² of land between roads paved: larger than an island")
	}
	roads[[2]int{0, 0}] = []gltf.Primitive{r, l}
	got := pavedIslandsGrown(roads, ground, paving, 250, true)
	p, ok := got[[2]int{0, 0}]
	if !ok {
		t.Fatal("the land between the road and the car park isn't paved")
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	x0, x1 := math.Inf(1), math.Inf(-1)
	for v := 0; v+2 < len(p.Positions); v += 3 {
		lo, hi = math.Min(lo, float64(p.Positions[v+1])), math.Max(hi, float64(p.Positions[v+1]))
		x0, x1 = math.Min(x0, float64(p.Positions[v])), math.Max(x1, float64(p.Positions[v]))
	}
	if hi >= 9.95 {
		t.Errorf("the paving stands at %.3f m, want it under the car park at 9.95", hi)
	}
	if x1 < 103+islandCellM-0.1 {
		t.Errorf("the paving runs from x %.2f to %.2f, want it a cell under the road beyond the land's edge at 103", x0, x1)
	}
}
