package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// Placement keyed by place (owner, 2026-10-09: worlds will grow to take in
// more routes, and adding one must move nothing else): what is placed
// along a road (avenue trees, hedges) is seeded and spaced by the map way
// it stands by and the distance along that way from its first node, never
// by the drawn line's number or its own distance (a line starts wherever
// the route and the clip to the corridor cut it).
type wayPlaces struct {
	ways []scenery.Way
	cum  [][]float64 // per way, the distance along it at each point
	// starts: the first sample of each drawn line (for the course's own
	// ways, which no map way places).
	starts map[int]sample
}

func newWayPlaces(ws []scenery.Way, fine []sample) *wayPlaces {
	wp := &wayPlaces{ways: ws, cum: make([][]float64, len(ws)), starts: map[int]sample{}}
	for i, w := range ws {
		c := make([]float64, len(w.Line))
		for k := 1; k < len(w.Line); k++ {
			c[k] = c[k-1] + math.Hypot(w.Line[k][0]-w.Line[k-1][0], w.Line[k][1]-w.Line[k-1][1])
		}
		wp.cum[i] = c
	}
	for _, s := range fine {
		if _, ok := wp.starts[s.line]; !ok {
			wp.starts[s.line] = s
		}
	}
	return wp
}

// at is where road sample s lies: a key for its way (the OSM ID; for the
// course's own ways, its line's start, to the metre), the distance along
// the way, and whether the drawn line (heading de, dn there) runs against
// the way's own direction. ok is false where s isn't by its way (a
// line's sample past a junction belongs to the next way).
func (wp *wayPlaces) at(s sample, de, dn float64) (key uint64, along float64, against, ok bool) {
	if s.way < 0 || s.way >= len(wp.ways) {
		st := wp.starts[s.line]
		key = splitmix(uint64(int64(math.Round(st.e)))<<32 ^ uint64(int64(math.Round(st.n))) ^ 0x0e1)
		return key, s.d - st.d, false, true
	}
	w := wp.ways[s.way]
	best, bestK, bestT := math.Inf(1), -1, 0.0
	for k := 0; k+1 < len(w.Line); k++ {
		a, b := w.Line[k], w.Line[k+1]
		ex, ny := b[0]-a[0], b[1]-a[1]
		t := 0.0
		if l2 := ex*ex + ny*ny; l2 > 0 {
			t = math.Max(0, math.Min(1, ((s.e-a[0])*ex+(s.n-a[1])*ny)/l2))
		}
		if d := math.Hypot(s.e-a[0]-t*ex, s.n-a[1]-t*ny); d < best {
			best, bestK, bestT = d, k, t
		}
	}
	if bestK < 0 || best > 3 {
		return 0, 0, false, false
	}
	a, b := w.Line[bestK], w.Line[bestK+1]
	c := wp.cum[s.way]
	along = c[bestK] + bestT*(c[bestK+1]-c[bestK])
	against = (b[0]-a[0])*de+(b[1]-a[1])*dn < 0
	return uint64(w.ID), along, against, true
}
