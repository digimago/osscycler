package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// Side roads meet the main road square (owner, 2026-10-16, the lane off
// the Rue du Hohneck at 1.34 km of the Route des Américains: in the map
// its line runs into the main road at a shallow angle, so the strip
// between them ended in a slant, and no fitting of sidewalk or wall to
// that line made it the rectangle the aerial view shows; unridden side
// roads may be reshaped for the look of the place). An unridden side road
// that meets a road of a higher class at less than mouthSquareDeg has its
// last mouthReachM redrawn: straight out from the node, square to the main
// road, for mouthStubM, then on to where its own line was.
const (
	mouthSquareDeg = 60.0
	mouthStubM     = 4.0
	mouthReachM    = 10.0
)

// squareMouths returns ways with the unridden side roads' mouths squared
// (the slice and the changed ways are copies; the rest are shared).
func squareMouths(ways []scenery.Way, ridden map[int]bool) []scenery.Way {
	type at struct{ w, i int }
	byNode := map[int64][]at{}
	for wi, w := range ways {
		if len(w.Nodes) != len(w.Line) {
			continue
		}
		for i, n := range w.Nodes {
			byNode[n] = append(byNode[n], at{wi, i})
		}
	}
	out := append([]scenery.Way(nil), ways...)
	// Ids for the points put in: none the map has, and none joinNearEnds
	// makes (it counts down from -1_000_000_000; a shared id tied a bridge's
	// end to a squared stub elsewhere and dropped it 16 m into its gorge).
	next := int64(-2_000_000_000)
	cosMax := math.Cos(mouthSquareDeg * math.Pi / 180)
	for wi, w := range ways {
		if ridden[wi] || len(w.Line) < 3 || len(w.Nodes) != len(w.Line) {
			continue
		}
		for _, first := range []bool{true, false} {
			// The line as it stands (the first end's redrawing changed it).
			line := out[wi].Line
			nodes := out[wi].Nodes
			end := len(line) - 1
			if first {
				end = 0
			}
			node := nodes[end]
			main, mainAt, rank := -1, 0, classRank[w.Class]
			for _, a := range byNode[node] {
				if a.w == wi {
					continue
				}
				if r := classRank[ways[a.w].Class]; r > rank {
					main, mainAt, rank = a.w, a.i, r
				}
			}
			if main < 0 {
				continue
			}
			// The main road's way at the node, and the side road's out from it.
			ml := ways[main].Line
			a, b := ml[max(0, mainAt-1)], ml[min(len(ml)-1, mainAt+1)]
			me, mn := b[0]-a[0], b[1]-a[1]
			l := math.Hypot(me, mn)
			if l == 0 {
				continue
			}
			me, mn = me/l, mn/l
			dir := 1
			if end > 0 {
				dir = -1
			}
			p0 := line[end]
			// The side road's line resampled from the node: where it is
			// mouthReachM out, and its way to there.
			j, run := end, 0.0
			var q [2]float64
			for {
				k := j + dir
				if k < 0 || k >= len(line) {
					j = -1
					break
				}
				seg := math.Hypot(line[k][0]-line[j][0], line[k][1]-line[j][1])
				if run+seg >= mouthReachM {
					f := (mouthReachM - run) / seg
					q = [2]float64{line[j][0] + f*(line[k][0]-line[j][0]), line[j][1] + f*(line[k][1]-line[j][1])}
					j = k
					break
				}
				run += seg
				j = k
			}
			if j < 0 || math.Abs(mouthReachM-run) < 0.5 && (j == 0 || j == len(line)-1) {
				continue // too short to reshape
			}
			se, sn := q[0]-p0[0], q[1]-p0[1]
			if sl := math.Hypot(se, sn); sl == 0 || math.Abs(se*me+sn*mn)/sl < cosMax {
				continue // meets square enough already
			}
			// The points dropped mustn't carry another way.
			shared := false
			for k := end + dir; k != j; k += dir {
				if len(byNode[nodes[k]]) > 1 {
					shared = true
					break
				}
			}
			if shared {
				continue
			}
			// Square to the main road, on the side road's side of it.
			ne, nn := -mn, me
			if ne*se+nn*sn < 0 {
				ne, nn = -ne, -nn
			}
			p1 := [2]float64{p0[0] + ne*mouthStubM, p0[1] + nn*mouthStubM}
			var newLine [][2]float64
			var newNodes []int64
			if end == 0 {
				newLine = append(newLine, p0, p1, q)
				newNodes = append(newNodes, nodes[0], next, next-1)
				newLine = append(newLine, line[j:]...)
				newNodes = append(newNodes, nodes[j:]...)
			} else {
				newLine = append(newLine, line[:j+1]...)
				newNodes = append(newNodes, nodes[:j+1]...)
				newLine = append(newLine, q, p1, p0)
				newNodes = append(newNodes, next-1, next, nodes[end])
			}
			next -= 2
			cp := out[wi]
			cp.Line, cp.Nodes = newLine, newNodes
			out[wi] = cp
		}
	}
	return out
}
