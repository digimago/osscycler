package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/scenery"
)

// Layers (owner, 2026-10-22: a motorway crossing the figure 8 over and
// under the course): where two drawn roads cross with no node in common,
// the map says which lies over the other (its layer; a bridge with none
// is 1, a tunnel -1). A ground model usually has them apart already
// (embankments and cuttings are ground, bridges span straight from bank
// to bank, tunnels lie straight low under what they pass); where it
// doesn't (no ground model, where every road takes the course's profile;
// an embankment flattened by the smoothing) one of the two is moved until
// the upper clears the lower by layerM: the one the route doesn't ride,
// a motorway before another road, else the upper one up (the lower keeps
// to the ground), eased in and out along its line.
const (
	// layerClearM is the headroom under a structure: what European
	// motorways keep under their bridges (4.5-4.7 m).
	layerClearM = 4.6
	// layerM: the upper road's surface over the lower's, a structure's
	// depth (a deck's slab, a tunnel's roof) over the headroom.
	layerM = layerClearM + deckDepthM
	// A moved road eases from its own height to the one it needs at most
	// this steeply: a motorway's grade, another road's.
	layerMotorwayGrade = 0.04
	layerRoadGrade     = 0.06
	// layerPlateauMaxM: a skew crossing holds its height along the moved
	// road at most this far either side of the other road's middle.
	layerPlateauMaxM = 60.0
	// layerJoinM: two crossings on one road whose holds are this close
	// (their ramps meeting) are joined by a straight grade, not eased
	// apart and back (the motorway rising from over the course's southern
	// arc to under its northern one, 0.5 km on).
	layerJoinM = 800.0
	// layerPullMinM: a road joining one that moves eases the change out
	// over at least this, or neither moves.
	layerPullMinM = 10.0
)

// layerHold is a height a road must take near d along it: c more than it
// has, over the plateau p either side.
type layerHold struct {
	d, c, p float64
}

// separateLayers moves roads crossing others on another level apart
// (layers above). ridden: the map's ways the route rides on (riddenWays).
// Before the course is matched onto the roads, after their heights.
func (g *gnet) separateLayers(ways []scenery.Way, ridden map[int]bool) {
	lines := g.lines[:g.virtual]
	wayOf := func(l *roadLine, d float64) (scenery.Way, int, bool) {
		k := sampleBetween(l, d).way
		if k < 0 || k >= len(ways) {
			return scenery.Way{}, k, false
		}
		return ways[k], k, true
	}
	// rides tells whether the route rides line j.
	rides := func(j int) bool {
		if lines[j].route {
			return true
		}
		for _, k := range lines[j].ways {
			if ridden[k] {
				return true
			}
		}
		return false
	}
	holds := make([][]layerHold, len(lines))
	for _, x := range crossings(lines) {
		a, b := lines[x.a], lines[x.b]
		wa, ka, oka := wayOf(a, x.da)
		wb, kb, okb := wayOf(b, x.db)
		if !oka || !okb || wa.Layer == wb.Layer {
			continue // on the level (or the map doesn't say)
		}
		u, l, du, dl := x.a, x.b, x.da, x.db
		if wa.Layer < wb.Layer {
			u, l, du, dl = l, u, dl, du
		}
		short := layerM - (sampleBetween(lines[u], du).ele - sampleBetween(lines[l], dl).ele)
		if short <= 0 {
			continue
		}
		pinned := func(i int, k int) bool { return ridden[k] || lines[i].route }
		ku, kl := ka, kb
		if u != x.a {
			ku, kl = kb, ka
		}
		// Which moves: not one the route rides; a motorway before another
		// road; else the upper one, up.
		type move struct {
			i    int
			d, c float64
		}
		var moves []move
		switch pu, pl := pinned(u, ku), pinned(l, kl); {
		case pu && pl:
			moves = []move{{u, du, short / 2}, {l, dl, -short / 2}}
		case pu:
			moves = []move{{l, dl, -short}}
		case pl:
			moves = []move{{u, du, short}}
		case lines[l].motorway && !lines[u].motorway:
			moves = []move{{l, dl, -short}}
		default:
			moves = []move{{u, du, short}}
		}
		for _, m := range moves {
			other := x.b
			if m.i == x.b {
				other = x.a
			}
			od := x.db
			if other == x.a {
				od = x.da
			}
			// Held across the other road's width and a deck's reach past it,
			// longer the more askew the two cross.
			p := math.Min(layerPlateauMaxM, (sampleBetween(lines[other], od).edge+deckClearM)/math.Max(x.sin, 0.1))
			holds[m.i] = append(holds[m.i], layerHold{m.d, m.c, p})
		}
	}
	for i, hs := range holds {
		if len(hs) == 0 {
			continue
		}
		grade := layerRoadGrade
		if lines[i].motorway {
			grade = layerMotorwayGrade
		}
		corr := layerCorrection(hs, grade)
		// A road's nodes shared with others move with it, and the roads
		// joining there (a motorway's slip roads) take the change at the
		// node and ease it out along them before their own next node, so
		// their far ends stay (owner, 2026-10-22: the A13 by Rodange ran
		// through the bridge the course crosses it on; held still at the
		// interchange's nodes, it hadn't moved). Not where a road joining
		// is one the route rides, or has no room to ease out.
		type pull struct {
			j     int
			d, c  float64
			reach float64
		}
		var pulls []pull
		ok := true
		for _, st := range g.stops[i] {
			c := corr(st.d)
			if math.Abs(c) <= 0.01 {
				continue
			}
			for j := range lines {
				if j == i {
					continue
				}
				for _, sj := range g.stops[j] {
					if sj.node != st.node {
						continue
					}
					if rides(j) {
						ok = false
						continue
					}
					gj := layerRoadGrade
					if lines[j].motorway {
						gj = layerMotorwayGrade
					}
					reach := math.Pi / 2 * math.Abs(c) / gj
					for _, o := range g.stops[j] {
						if o.node != st.node {
							reach = math.Min(reach, math.Abs(o.d-sj.d)-1)
						}
					}
					ss := lines[j].samples
					reach = math.Min(reach, math.Max(sj.d-ss[0].d, ss[len(ss)-1].d-sj.d))
					if reach < layerPullMinM {
						ok = false
						continue
					}
					pulls = append(pulls, pull{j, sj.d, c, reach})
				}
			}
		}
		if !ok {
			continue
		}
		for k := range lines[i].samples {
			lines[i].samples[k].ele += corr(lines[i].samples[k].d)
		}
		for _, st := range g.stops[i] {
			g.ele[st.node] += corr(st.d)
		}
		for _, p := range pulls {
			for k := range lines[p.j].samples {
				s := &lines[p.j].samples[k]
				if x := math.Abs(s.d-p.d) / p.reach; x < 1 {
					s.ele += p.c * (1 + math.Cos(math.Pi*x)) / 2
				}
			}
		}
	}
}

// layerCorrection is how much a road moves along it to take holds, easing
// in and out at grade at most (a half cosine: steepest midway, level at
// both ends), and between two holds near each other along a straight
// grade from one to the other.
func layerCorrection(hs []layerHold, grade float64) func(d float64) float64 {
	hs = append([]layerHold(nil), hs...)
	sort.Slice(hs, func(i, j int) bool { return hs[i].d < hs[j].d })
	ramp := func(h layerHold) float64 { return math.Pi / 2 * math.Abs(h.c) / grade }
	ease := func(h layerHold, x float64) float64 { // x past the hold's plateau
		r := ramp(h)
		if x >= r {
			return 0
		}
		return h.c * (1 + math.Cos(math.Pi*x/r)) / 2
	}
	return func(d float64) float64 {
		// Within a hold: its height (the larger where two overlap).
		var c float64
		in := false
		for _, h := range hs {
			if math.Abs(d-h.d) <= h.p {
				if !in || math.Abs(h.c) > math.Abs(c) {
					c = h.c
				}
				in = true
			}
		}
		if in {
			return c
		}
		// Between two holds near enough: straight from one to the other.
		k := sort.Search(len(hs), func(i int) bool { return hs[i].d > d })
		if k > 0 && k < len(hs) {
			a, b := hs[k-1], hs[k]
			a0, b0 := a.d+a.p, b.d-b.p
			if b0-a0 <= layerJoinM {
				f := (d - a0) / math.Max(b0-a0, 1e-9)
				return a.c + f*(b.c-a.c)
			}
			return ease(a, d-a0) + ease(b, b0-d)
		}
		if k == 0 {
			return ease(hs[0], hs[0].d-hs[0].p-d)
		}
		last := hs[len(hs)-1]
		return ease(last, d-last.d-last.p)
	}
}

// layerCross is where line a crosses line b: how far along each, and the
// sine of the angle between them.
type layerCross struct {
	a, b   int
	da, db float64
	sin    float64
}

// crossings are where the lines cross each other, away from a node they
// share (a junction on the level).
func crossings(lines []*roadLine) []layerCross {
	type seg struct{ l, i int }
	const cellM = 20.0
	grid := map[[2]int][]seg{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range lines {
		for i := 0; i+1 < len(l.samples); i++ {
			a, b := l.samples[i], l.samples[i+1]
			k0, k1 := key(math.Min(a.e, b.e), math.Min(a.n, b.n)), key(math.Max(a.e, b.e), math.Max(a.n, b.n))
			for x := k0[0]; x <= k1[0]; x++ {
				for y := k0[1]; y <= k1[1]; y++ {
					grid[[2]int{x, y}] = append(grid[[2]int{x, y}], seg{li, i})
				}
			}
		}
	}
	shared := func(a, b *roadLine, da, db float64) bool {
		for node, ds := range a.nodes {
			others, ok := b.nodes[node]
			if !ok {
				continue
			}
			for _, x := range ds {
				for _, y := range others {
					if math.Abs(x-da) < 2 && math.Abs(y-db) < 2 {
						return true
					}
				}
			}
		}
		return false
	}
	seen := map[[4]int]bool{}
	var out []layerCross
	keys := make([][2]int, 0, len(grid))
	for k := range grid {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		ss := grid[k]
		for x, s0 := range ss {
			for _, s1 := range ss[x+1:] {
				p, q := s0, s1
				if p.l == q.l {
					continue
				}
				if p.l > q.l {
					p, q = q, p
				}
				id := [4]int{p.l, p.i, q.l, q.i}
				if seen[id] {
					continue
				}
				seen[id] = true
				a0, a1 := lines[p.l].samples[p.i], lines[p.l].samples[p.i+1]
				b0, b1 := lines[q.l].samples[q.i], lines[q.l].samples[q.i+1]
				re, rn := a1.e-a0.e, a1.n-a0.n
				se, sn := b1.e-b0.e, b1.n-b0.n
				den := re*sn - rn*se
				if math.Abs(den) < 1e-9 {
					continue // parallel
				}
				qe, qn := b0.e-a0.e, b0.n-a0.n
				t := (qe*sn - qn*se) / den
				u := (qe*rn - qn*re) / den
				if t < 0 || t >= 1 || u < 0 || u >= 1 {
					continue
				}
				da, db := a0.d+t*(a1.d-a0.d), b0.d+u*(b1.d-b0.d)
				if shared(lines[p.l], lines[q.l], da, db) {
					continue
				}
				out = append(out, layerCross{p.l, q.l, da, db, math.Abs(den) / (math.Hypot(re, rn) * math.Hypot(se, sn))})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].a != out[j].a {
			return out[i].a < out[j].a
		}
		if out[i].b != out[j].b {
			return out[i].b < out[j].b
		}
		return out[i].da < out[j].da
	})
	return out
}
