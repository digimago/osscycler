package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// A sidewalk never runs into a building (owner, 2026-10-11: on a new
// course, houses that stand at the street had the sidewalk drawn through
// their fronts, 180 m² of it at 3.3 km). Where a building stands within a
// sidewalk's reach of the road's edge, the sidewalk narrows to the wall
// (owner, 2026-10-11: at 3.12 km the house stands right at a narrow
// sidewalk, and dropping it there was wrong); with less than walkMinM
// left, that side has none, the land running from the kerb to the wall.
const (
	wallClearM = 0.02 // a sidewalk stops this far short of a wall
	walkMinM   = 0.5  // a narrower sidewalk is left out (narrow Dutch stoepen are 0.6 m)
	wallScanM  = 0.1  // the room is measured in these steps
)

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
		// room[side][i]: how wide a sidewalk fits before a wall (walkM: no
		// wall within reach).
		room := [2][]float64{make([]float64, len(ss)), make([]float64, len(ss))}
		for i := range ss {
			s := ss[i]
			room[0][i], room[1][i] = walkM, walkM
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
				for w := wallScanM; w <= walkM+wallClearM+1e-9; w += wallScanM {
					off := s.hw + w
					if inBuilding(s.e+sg*re*off, s.n+sg*rn*off) {
						room[side][i] = math.Max(0, w-wallScanM-wallClearM)
						break
					}
				}
			}
		}
		// The least room over a sample either way, so a sidewalk doesn't
		// widen again at the wall's corner exactly (tidyStrips mends short
		// pieces later).
		for side := range 2 {
			for i := range ss {
				if !ss[i].walk[side] {
					continue
				}
				r := room[side][i]
				if i > 0 {
					r = math.Min(r, room[side][i-1])
				}
				if i+1 < len(ss) {
					r = math.Min(r, room[side][i+1])
				}
				switch {
				case r < walkMinM:
					ss[i].walk[side] = false
				case r < walkM:
					ss[i].walkW[side] = r
				}
			}
		}
	}
}
