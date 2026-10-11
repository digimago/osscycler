package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// A sidewalk never runs into a building (owner, 2026-10-11: on a new
// course, houses that stand at the street had the sidewalk drawn through
// their fronts, 180 m² of it at 3.3 km): where a building stands within a
// sidewalk's reach of the road's edge, that side has no sidewalk; the land
// runs from the kerb up to the wall. The map's widths and our guessed
// sidewalks don't always leave the room the houses do.
const wallClearM = 0.2 // a sidewalk stays this far off a wall

func clearWalksOfBuildings(lines []*roadLine, fps []scenery.Footprint) {
	if len(fps) == 0 {
		return
	}
	const cell = 20.0
	grid := map[[2]int][]int{}
	for i, f := range fps {
		e0, n0, e1, n1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range f.Outline {
			e0, n0, e1, n1 = math.Min(e0, p[0]), math.Min(n0, p[1]), math.Max(e1, p[0]), math.Max(n1, p[1])
		}
		for ce := int(math.Floor(e0 / cell)); ce <= int(math.Floor(e1/cell)); ce++ {
			for cn := int(math.Floor(n0 / cell)); cn <= int(math.Floor(n1/cell)); cn++ {
				grid[[2]int{ce, cn}] = append(grid[[2]int{ce, cn}], i)
			}
		}
	}
	inBuilding := func(e, n float64) bool {
		for _, i := range grid[[2]int{int(math.Floor(e / cell)), int(math.Floor(n / cell))}] {
			if inPoly(fps[i].Outline, e, n) {
				return true
			}
		}
		return false
	}
	for _, l := range lines {
		ss := l.samples
		drop := [2][]bool{make([]bool, len(ss)), make([]bool, len(ss))}
		for i := range ss {
			s := ss[i]
			a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
			de, dn := b.e-a.e, b.n-a.n
			ln := math.Hypot(de, dn)
			if ln == 0 {
				continue
			}
			re, rn := dn/ln, -de/ln // right of the line's direction
			for side := range 2 {
				if !s.walk[side] {
					continue
				}
				sg := 1.0
				if side == 0 {
					sg = -1
				}
				for _, off := range []float64{s.hw + 0.3, s.hw + walkM/2, s.hw + walkM + wallClearM} {
					if inBuilding(s.e+sg*re*off, s.n+sg*rn*off) {
						drop[side][i] = true
						break
					}
				}
			}
		}
		// Over a sample either way too, so a sidewalk doesn't end at the
		// wall's corner exactly (tidyStrips mends short pieces later).
		for side := range 2 {
			for i := range ss {
				if drop[side][i] || i > 0 && drop[side][i-1] || i+1 < len(ss) && drop[side][i+1] {
					ss[i].walk[side] = false
				}
			}
		}
	}
}
