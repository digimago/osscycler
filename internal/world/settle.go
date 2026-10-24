package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// The road surfaces as drawn, indexed for lookups from above (the
// riding line kept on the carriageway, as World.Check sees it).
const settleCellM = 4.0 // the road triangles' index

// roadIndex is the road surfaces' triangles seen from above, in the
// builder's east and north.
type roadIndex struct {
	cells map[[2]int][][6]float64 // e0 n0 e1 n1 e2 n2
}

func newRoadIndex(prims []gltf.Primitive, isRoad func(mat int) bool) *roadIndex {
	ix := &roadIndex{cells: map[[2]int][][6]float64{}}
	for _, p := range prims {
		if !isRoad(p.Material) {
			continue
		}
		for ti := 0; ti+2 < len(p.Indices); ti += 3 {
			var t [6]float64
			for k, v := range p.Indices[ti : ti+3] {
				t[2*k], t[2*k+1] = float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])
			}
			// Seen edge on (walls, kerb faces, skirts) it covers nothing.
			if math.Abs((t[2]-t[0])*(t[5]-t[1])-(t[4]-t[0])*(t[3]-t[1])) < 1e-6 {
				continue
			}
			e0, e1 := math.Min(t[0], math.Min(t[2], t[4])), math.Max(t[0], math.Max(t[2], t[4]))
			n0, n1 := math.Min(t[1], math.Min(t[3], t[5])), math.Max(t[1], math.Max(t[3], t[5]))
			for ce := int(math.Floor(e0 / settleCellM)); ce <= int(math.Floor(e1/settleCellM)); ce++ {
				for cn := int(math.Floor(n0 / settleCellM)); cn <= int(math.Floor(n1/settleCellM)); cn++ {
					k := [2]int{ce, cn}
					ix.cells[k] = append(ix.cells[k], t)
				}
			}
		}
	}
	return ix
}

func (ix *roadIndex) key(e, n float64) [2]int {
	return [2]int{int(math.Floor(e / settleCellM)), int(math.Floor(n / settleCellM))}
}

// on tells whether e, n lies on a road surface.
func (ix *roadIndex) on(e, n float64) bool {
	for _, t := range ix.cells[ix.key(e, n)] {
		if inTri2(t, e, n) {
			return true
		}
	}
	return false
}

// inTri2 tells whether e, n lies in triangle t, edges included.
func inTri2(t [6]float64, e, n float64) bool {
	d := func(ax, ay, bx, by float64) float64 { return (bx-ax)*(n-ay) - (by-ay)*(e-ax) }
	d0, d1, d2 := d(t[0], t[1], t[2], t[3]), d(t[2], t[3], t[4], t[5]), d(t[4], t[5], t[0], t[1])
	neg := d0 < 0 || d1 < 0 || d2 < 0
	pos := d0 > 0 || d1 > 0 || d2 > 0
	return !(neg && pos)
}
