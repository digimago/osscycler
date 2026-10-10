package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// Cycle lanes across junctions (owner, 2026-10-09): a road's cycle lane
// goes on across a junction where the road goes on beyond it with a lane
// on the same side, as Dutch fietsstroken do past side streets.
const (
	laneAcrossDeg = 35.0 // two arms this near opposite are one road going on
	laneLiftM     = 0.01 // above the junction's surface
	laneStepM     = 2.0  // pieces along it, to follow the surface
)

// laneBands are the lanes across a junction: for each pair of arms
// opposite each other, a band from one arm's end to the other's on the
// side traffic keeps to, where both have a lane there: its outer corner
// and the point laneM in at each end.
func laneBands(arms []armOut) [][4][2]float64 {
	inner := func(c, other [2]float64) [2]float64 {
		dx, dy := other[0]-c[0], other[1]-c[1]
		l := math.Hypot(dx, dy)
		if l < 2*laneM {
			return c
		}
		return [2]float64{c[0] + dx/l*laneM, c[1] + dy/l*laneM}
	}
	var out [][4][2]float64
	for i := range arms {
		for j := i + 1; j < len(arms); j++ {
			a, b := arms[i], arms[j]
			d := math.Abs(math.Remainder(a.angle-b.angle, 2*math.Pi))
			if math.Abs(d-math.Pi) > laneAcrossDeg*math.Pi/180 {
				continue
			}
			// Into the junction from a and out along b: a's left (seen
			// from the node out) is the rider's right coming in, b's right
			// going out; and the other way round.
			for _, k := range [][2]int{{1, 0}, {0, 1}} {
				if !a.lane[k[0]] || !b.lane[k[1]] {
					continue
				}
				ca, cb := a.end[k[0]], b.end[k[1]]
				ia, ib := inner(ca, a.end[1-k[0]]), inner(cb, b.end[1-k[1]])
				if ia == ca || ib == cb {
					continue
				}
				out = append(out, [4][2]float64{ca, ia, ib, cb})
			}
		}
	}
	return out
}

// laneMesh is the junction's lanes across it, on its surface.
func laneMesh(p patch, mat int) gltf.Primitive {
	q := gltf.Primitive{Material: mat}
	up := [3]float64{0, 1, 0}
	vert := func(x, y float64) uint32 {
		q.Positions = append(q.Positions, float32(x), float32(p.height(x, y)+laneLiftM), float32(-y))
		q.Normals = append(q.Normals, 0, 1, 0)
		q.UVs = append(q.UVs, float32(x/5), float32(y/5))
		return uint32(len(q.Positions)/3 - 1)
	}
	for _, b := range p.lanes {
		// From a's end (corner, inner) to b's (inner, corner).
		l := math.Hypot((b[0][0]+b[1][0]-b[2][0]-b[3][0])/2, (b[0][1]+b[1][1]-b[2][1]-b[3][1])/2)
		n := max(1, int(math.Ceil(l/laneStepM)))
		var pa, pb uint32
		for k := 0; k <= n; k++ {
			f := float64(k) / float64(n)
			ox, oy := b[0][0]+f*(b[3][0]-b[0][0]), b[0][1]+f*(b[3][1]-b[0][1])
			ix, iy := b[1][0]+f*(b[2][0]-b[1][0]), b[1][1]+f*(b[2][1]-b[1][1])
			va, vb := vert(ox, oy), vert(ix, iy)
			if k > 0 {
				tri(&q, pa, pb, va, up)
				tri(&q, pb, vb, va, up)
			}
			pa, pb = va, vb
		}
	}
	return q
}
