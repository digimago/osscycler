package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// strip is a road surface 4 m wide along the east axis from e0 to e1 (a
// primitive in glTF axes: z is south).
func strip(e0, e1 float64) gltf.Primitive {
	p := func(e, n float64) []float32 { return []float32{float32(e), 0, float32(-n)} }
	var pos []float32
	pos = append(pos, p(e0, -2)...)
	pos = append(pos, p(e1, -2)...)
	pos = append(pos, p(e1, 2)...)
	pos = append(pos, p(e0, 2)...)
	return gltf.Primitive{Positions: pos, Indices: []uint32{0, 1, 2, 0, 2, 3}}
}

// The riding line is settled on the road as drawn: a path that drifts 5 m
// off a road for a stretch (as the Posbank Loop's did in 12 places) comes
// back onto it, and a lane wider than the road narrows.
func TestSettleOnRoad(t *testing.T) {
	ix := newRoadIndex([]gltf.Primitive{strip(-50, 250)}, func(int) bool { return true })
	n := 41
	x, y, lane := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range n {
		x[i] = float64(i) * 5
		lane[i] = 1.5
		if i >= 15 && i <= 20 {
			y[i] = 5 // off the road, to its left
		}
	}
	lane[30] = 3.5 // the riding line beyond the road's right edge
	settleOnRoad(x, y, lane, false, ix)
	for i := range n {
		for _, f := range settleAt {
			if i == n-1 && f > 0 {
				continue
			}
			j := min(n-1, i+1)
			pe, pn := x[i]+f*(x[j]-x[i]), y[i]+f*(y[j]-y[i])
			off := lane[i] + f*(lane[j]-lane[i])
			// Heading east: right is south.
			if re, rn := pe, pn-off; !ix.on(re, rn) {
				t.Errorf("riding line off the road at point %d + %.1f: %.1f, %.1f", i, f, re, rn)
			}
		}
	}
	// Points that were on the road with room stay where they were.
	if x[5] != 25 || y[5] != 0 || math.Abs(lane[5]-1.5) > 1e-9 {
		t.Errorf("point 5 moved to %.2f, %.2f lane %.2f", x[5], y[5], lane[5])
	}
}

// A gap between two roads it can't bridge is left as it is rather than
// made worse: the path stays where it was beside it.
func TestSettleLeavesWhatItCantMend(t *testing.T) {
	ix := newRoadIndex([]gltf.Primitive{strip(-50, 95), strip(130, 250)}, func(int) bool { return true })
	n := 41
	x, y, lane := make([]float64, n), make([]float64, n), make([]float64, n)
	for i := range n {
		x[i] = float64(i) * 5
	}
	settleOnRoad(x, y, lane, false, ix)
	for i := range n {
		if y[i] != 0 || x[i] != float64(i)*5 {
			t.Fatalf("point %d moved to %.2f, %.2f", i, x[i], y[i])
		}
	}
}
