package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/scenery"
)

// Heights on the graph (graph.go): each road first takes the ground model
// along it, smoothed (heights, as the classic builder does), then every
// node one height, the mean of the roads meeting there, and each road is
// brought to its nodes' heights by a correction that runs straight from
// one node to the next along it. Roads meet at their node at one height by
// construction, rather than being blended towards each other over joinM
// and leaving whatever lies between nodes as it was.

// A minor road meeting a major one (owner, 2026-10-16, a lane off the
// Rue du Hohneck at 1.34 km of the Route des Américains: "the side road
// only starts its slope beyond the edge of the road and sidewalk; the
// first metre or so is flat, in line with the sidewalk"): the node takes
// the major road's height, and the minor road keeps it from the node out
// until its near edge is past the major road's edge and sidewalk
// (gMouthClearM more), and gMouthFlatM on along it, then eases into its
// own grade over gMouthEaseM.
const (
	gMouthClearM = 0.1
	// gBandOverlapM: a minor road's edge this far into a major road's edge
	// and sidewalk still lies beside it (map lines are that loose), not
	// across it; roadside.go's walls take the same.
	gBandOverlapM = 0.5
	gBandNearM    = 15.0 // a road's band is its widest edge this near
	gMouthFlatM   = 1.0
	gMouthEaseM   = 6.0
	gMouthMaxM    = 40.0 // a mouth this long at most
	gMouthLookM   = 60.0 // the major road's line this near its node
)

// anchorHeights sets the nodes' heights and brings the roads to them.
func (g *gnet) anchorHeights(ways []scenery.Way) {
	rankAt := func(l *roadLine, d float64) (int, sample) {
		s := sampleBetween(l, d)
		if s.way >= 0 && s.way < len(ways) {
			return classRank[ways[s.way].Class], s
		}
		return 0, s
	}
	// Per node the highest class meeting there, the mean height of its
	// roads of that class, and how far their edges (with sidewalks) reach.
	top := map[int64]int{}
	for k, st := range g.stops[:g.virtual] {
		for _, s := range st {
			r, _ := rankAt(g.lines[k], s.d)
			if t, ok := top[s.node]; !ok || r > t {
				top[s.node] = r
			}
		}
	}
	sum, n, reach := map[int64]float64{}, map[int64]int{}, map[int64]float64{}
	for k, st := range g.stops[:g.virtual] {
		for _, s := range st {
			r, a := rankAt(g.lines[k], s.d)
			if r != top[s.node] {
				continue
			}
			sum[s.node] += a.ele
			n[s.node]++
			// Its widest edge near the node: at the node itself the
			// sidewalk stops for the mouth.
			reach[s.node] = math.Max(reach[s.node], bandNear(g.lines[k].samples, s.d))
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
	// The major roads at each node, for the minor roads' mouths.
	majors := map[int64][]*roadLine{}
	for k, st := range g.stops[:g.virtual] {
		for _, s := range st {
			if r, _ := rankAt(g.lines[k], s.d); r == top[s.node] {
				majors[s.node] = append(majors[s.node], g.lines[k])
			}
		}
	}
	// How far a point is from a node's major roads (their centre lines
	// within gMouthLookM of the node).
	// and the height a mouth level with it has there: the road's own, or
	// its sidewalk's where it has one on that side and the point lies past
	// its carriageway (owner's photo: "flat, in line with the sidewalks").
	fromMajor := func(node int64, e, n float64) (dist, level float64) {
		dist = math.Inf(1)
		p := g.pos[node]
		for _, ml := range majors[node] {
			for i := 0; i+1 < len(ml.samples); i++ {
				a, b := ml.samples[i], ml.samples[i+1]
				if math.Hypot(a.e-p[0], a.n-p[1]) > gMouthLookM {
					continue
				}
				de, dn := b.e-a.e, b.n-a.n
				f := 0.0
				if l2 := de*de + dn*dn; l2 > 0 {
					f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
				}
				d := math.Hypot(e-a.e-f*de, n-a.n-f*dn)
				if d >= dist {
					continue
				}
				dist, level = d, a.ele+f*(b.ele-a.ele)
				side := 1 // right of the road's way
				if de*(n-a.n)-dn*(e-a.e) > 0 {
					side = 0
				}
				if a.walk[side] {
					// Ramped up over a metre past the carriageway's edge, as a
					// dropped kerb, not a step across the lane.
					level += kerbM * math.Max(0, math.Min(1, d-a.hw))
				}
			}
		}
		return dist, level
	}
	// Minor roads level with the major road at their mouths: from the node
	// out while they still cross the major road's edge and sidewalk (a lane
	// joining at a shallow angle hugs the road for metres: measured along
	// it, it had climbed while still across the sidewalk's line), then
	// easing into their grade.
	for k, l := range g.lines[:g.virtual] {
		for _, st := range g.stops[k] {
			r, _ := rankAt(l, st.d)
			if r >= top[st.node] {
				continue
			}
			clear := reach[st.node] + gMouthClearM
			for _, dir := range []int{-1, 1} {
				i := sort.Search(len(l.samples), func(i int) bool { return l.samples[i].d >= st.d })
				if dir < 0 {
					i--
				}
				// Flat while the lane's near edge lies more than
				// gBandOverlapM inside the road's edge and sidewalk (it is
				// crossing the sidewalk's line), and gMouthFlatM on. Map lines
				// put a lane beside a road at the sidewalk's edge, a few
				// decimetres over it: counted strictly, the whole lane lay
				// flat, under the sidewalk.
				end, out, z := st.d, math.Inf(1), g.ele[st.node]
				for ; i >= 0 && i < len(l.samples); i += dir {
					s := &l.samples[i]
					dist, level := fromMajor(st.node, s.e, s.n)
					if math.IsInf(out, 1) && (math.Abs(s.d-st.d) > gMouthMaxM || dist-s.hw >= clear-gBandOverlapM) {
						out = s.d
					}
					if !math.IsInf(out, 1) && math.Abs(s.d-out) > gMouthFlatM {
						break
					}
					if s.d == st.d {
						level = z // the node itself: the road's height
					}
					s.ele, end, z = level, s.d, level
				}
				// Then eased into its own grade over gMouthEaseM.
				for ; i >= 0 && i < len(l.samples); i += dir {
					s := &l.samples[i]
					dd := math.Abs(s.d - end)
					if dd >= gMouthEaseM {
						break
					}
					f := smoothstep(dd / gMouthEaseM)
					s.ele = z*(1-f) + s.ele*f
				}
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

// bandNear is a road's widest edge with its sidewalks within gBandNearM of
// d along it (a sidewalk stops at a mouth; the band it makes goes on).
func bandNear(ss []sample, d float64) float64 {
	w := 0.0
	for _, a := range ss {
		if math.Abs(a.d-d) <= gBandNearM {
			w = math.Max(w, math.Max(sideEdge(a, 0), sideEdge(a, 1)))
		}
	}
	return w
}
