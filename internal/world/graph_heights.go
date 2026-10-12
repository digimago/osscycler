package world

import "math"

// Heights on the graph (graph.go): each road first takes the ground model
// along it, smoothed (heights, as the classic builder does), then every
// node one height, the mean of the roads meeting there, and each road is
// brought to its nodes' heights by a correction that runs straight from
// one node to the next along it. Roads meet at their node at one height by
// construction, rather than being blended towards each other over joinM
// and leaving whatever lies between nodes as it was.

// anchorHeights sets the nodes' heights and brings the roads to them.
func (g *gnet) anchorHeights() {
	sum, n := map[int64]float64{}, map[int64]int{}
	for k, st := range g.stops[:g.virtual] {
		for _, s := range st {
			sum[s.node] += sampleBetween(g.lines[k], s.d).ele
			n[s.node]++
		}
	}
	for node, v := range sum {
		g.ele[node] = v / float64(n[node])
	}
	for k, l := range g.lines[:g.virtual] {
		st := g.stops[k]
		if len(st) == 0 {
			continue
		}
		corr := make([]float64, len(st))
		for i, s := range st {
			corr[i] = g.ele[s.node] - sampleBetween(l, s.d).ele
		}
		j := 0
		for i := range l.samples {
			s := &l.samples[i]
			for j+1 < len(st) && st[j+1].d <= s.d {
				j++
			}
			switch {
			case s.d <= st[0].d:
				s.ele += corr[0]
			case j+1 >= len(st):
				s.ele += corr[len(st)-1]
			default:
				a, b := st[j], st[j+1]
				f := 0.0
				if b.d > a.d {
					f = (s.d - a.d) / (b.d - a.d)
				}
				s.ele += corr[j] + f*(corr[j+1]-corr[j])
			}
		}
	}
	// The ways across roundabouts: straight from one road's end to the
	// other's (the roundabout's plane is fitted to its roads later).
	for k, l := range g.lines[g.virtual:] {
		st := g.stops[g.virtual+k]
		za, zb := g.ele[st[0].node], g.ele[st[1].node]
		L := math.Max(st[1].d, 1e-9)
		for i := range l.samples {
			l.samples[i].ele = za + l.samples[i].d/L*(zb-za)
		}
	}
}
