package world

import (
	"math"
	"sort"
)

// The course on the graph (graph.go): every path step (pathStep along the
// course) is either on a road of the network (a line, how far along it,
// which way) or off it (the course's own point). The way through costs
// least that keeps near the route, moves on about a step each step, and
// changes roads only where the network joins them (gnet.between); a road
// it can't reach from the last costs as much as leaving the roads. So the
// rider can't hop between roads side by side, onto a road below, or back
// and forth: what the classic builder mends afterwards (keepToConnected,
// unfold, keepOnRoad, settleOnRoad, openRuns) has nothing to mend.

// gstate is one step's place: on a road, or off.
type gstate struct {
	on         bool
	line       int
	t          float64 // along the line
	dir        int8    // +1 along the line, -1 against it
	x, y, z, w float64 // where, its height, the carriageway's width
	cost       float64
}

const (
	gSnapM      = 25.0 // roads this far from the route are candidates
	gPieceM     = 40.0 // a line's candidates: its nearest point per this much of it
	gOffCost    = 12.0 // a step off the roads
	gOffMapCost = 8.0  // more where the map's own match has the route on a road
	gLeaveCost  = 60.0 // leaving the roads, or coming back onto them
	gChangeCost = 4.0  // a change of road (at a node)
	gNoWayCost  = 300.0
	gBackCost   = 1000.0 // going back along a road
	gUTurnCost  = 200.0
	gRankCost   = 4.0 // per class below the main road alongside
	gMapBonus   = 3.0 // the map's own match agreeing (it flips between roads side by side by a hair)
	gRushCost   = 0.4 // per metre covered past a step
	gJitterM    = 3.0 // a step back along a road this short is the route's wobble, not a turn
	gMergeM     = 0.1 // points of the line through the network closer than this are one (a node reached from two roads)
	gStepReachM = 20.0
	gHairpinRad = 110 * math.Pi / 180 // a way turning back more than this at a node is a hairpin
	gCloseM     = 8.0                 // a road this near the route is likely the one it rides
	gFarCost    = 0.05                // per square metre further out
	gAcrossCost = 16.0                // a road across the route's way (it turns there, through a node, not along it)
)

// alongOf is how much line l runs the way (re, rn) at t: 1 along it
// either way, 0 across.
func alongOf(l *roadLine, t, re, rn float64) float64 {
	de, dn, _ := l.smooth.heading(t)
	return math.Abs(de*re + dn*rn)
}

// gpoint is a point of the course's line through the network.
type gpoint struct {
	x, y, z, w float64
	node       bool // at a node where the course may turn
	flat       bool // on a flat roundabout's crossing (no riding line there)
	free       bool // a turn off the roads (leaving them, or coming back): swept as wide as any, there being no carriageway to keep to
	on         bool
}

// gpathOut is the course placed on the network: the line through it
// (pts, with s their distances along it) and where each path step lies on
// it (at), and which steps are off the roads.
type gpathOut struct {
	pts []gpoint
	s   []float64
	at  []float64
	on  []bool
}

// matchCourse places the course on the network: steps+1 path points every
// step along the course (own: the course's own point, height and width at
// each), mapOn the map's own match (the way at a distance along the
// course, -1 none) and the ways' lines (which ways each line is made of).
func (g *gnet) matchCourse(steps int, step float64, own [][4]float64, heading [][2]float64, mapOn func(i int) int, oneway func(k int, t float64) int8, rank func(k int, t float64) int, sidepath func(k int, t float64) bool) gpathOut {
	type cell [2]int
	const cellM = sidepathM
	grid := map[cell][]sampleRef{}
	key := func(e, n float64) cell { return cell{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for _, l := range g.lines {
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	n0 := steps + 1
	cands := make([][]gstate, n0)
	for i := range n0 {
		ox, oy, oz, ow := own[i][0], own[i][1], own[i][2], own[i][3]
		re, rn := heading[i][0], heading[i][1]
		off := gstate{x: ox, y: oy, z: oz, w: ow, cost: gOffCost}
		if mapOn(i) >= 0 {
			off.cost += gOffMapCost
		}
		cands[i] = append(cands[i], off)
		type pk struct {
			l     int
			piece int
		}
		best := map[pk]int{}
		k := key(ox, oy)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[cell{gx, gy}] {
					ss := r.l.samples
					if r.i+1 >= len(ss) {
						continue
					}
					a, b := ss[r.i], ss[r.i+1]
					de, dn := b.e-a.e, b.n-a.n
					l2 := de*de + dn*dn
					if l2 == 0 {
						continue
					}
					f := math.Max(0, math.Min(1, ((ox-a.e)*de+(oy-a.n)*dn)/l2))
					qx, qy := a.e+f*de, a.n+f*dn
					dd := math.Hypot(qx-ox, qy-oy)
					if dd > sidepathM {
						continue
					}
					li := g.lineOf[r.l]
					t := a.d + f*(b.d-a.d)
					// A road that sends cyclists onto a path beside it is
					// the course further out (the route on that path, which
					// the map data lacks), as in the classic builder.
					side := sidepath(li, t)
					if dd > gSnapM && !side {
						continue
					}
					// Past a road's end only close by: the rider would stall there.
					if (r.i == 0 && f == 0 || r.i+2 == len(ss) && f == 1) && dd > 3 {
						continue
					}
					cos := (de*re + dn*rn) / math.Sqrt(l2)
					dir := int8(1)
					if cos < 0 {
						dir = -1
					}
					along := math.Abs(cos)
					// Near, the distance counts little; further out, more and
					// more (a road 20 m off is another road, not this one),
					// except one that sends cyclists to a path beside it.
					cost := dd*0.5 + gFarCost*math.Pow(math.Max(0, dd-gCloseM), 2) + gAcrossCost*(1-along)
					if side {
						cost = dd*0.25 + gAcrossCost*(1-along)
					}
					if ow := oneway(li, t); ow != 0 && ow != dir {
						cost += wrongWayCost
					}
					if mk := mapOn(i); mk >= 0 && r.l.has(mk) && along >= parallelCosine {
						cost -= gMapBonus // the map's own match agrees
					}
					c := gstate{on: true, line: li, t: t, dir: dir, x: qx, y: qy,
						z: a.ele + f*(b.ele-a.ele), w: 2 * (a.hw + f*(b.hw-a.hw)), cost: cost}
					p := pk{li, int(t / gPieceM)}
					if j, ok := best[p]; !ok {
						best[p] = len(cands[i])
						cands[i] = append(cands[i], c)
					} else if cost < cands[i][j].cost {
						cands[i][j] = c
					}
				}
			}
		}
		// A closed course keeps to the main road: where a road of a higher
		// class runs alongside within ventM, an access road beside it
		// (ventweg) or a road the route took for its cycle path costs more
		// (as the classic builder's courseRoads).
		top := -1
		for _, c := range cands[i][1:] {
			if r := rank(c.line, c.t); r > top && math.Hypot(c.x-ox, c.y-oy) <= ventM && alongOf(g.lines[c.line], c.t, re, rn) >= parallelCosine {
				top = r
			}
		}
		for j := 1; j < len(cands[i]); j++ {
			if r := rank(cands[i][j].line, cands[i][j].t); r < top {
				cands[i][j].cost += gRankCost * float64(min(top-r, 3))
			}
		}
		// The same order every build.
		cs := cands[i][1:]
		sort.Slice(cs, func(a, b int) bool {
			if cs[a].line != cs[b].line {
				return cs[a].line < cs[b].line
			}
			return cs[a].t < cs[b].t
		})
	}
	// Viterbi. The pace: covering less ground than the step costs (the
	// rider would stall), more costs little, so the course can go round a
	// roundabout with the traffic or through a junction the long way when
	// the network leaves nothing else.
	pace := func(adv float64) float64 {
		if adv < step {
			return 2 * (step - adv)
		}
		return gRushCost * (adv - step)
	}
	trans := func(p, c gstate) float64 {
		switch {
		case !p.on && !c.on:
			return 2 * math.Abs(math.Hypot(c.x-p.x, c.y-p.y)-step)
		case p.on != c.on:
			return gLeaveCost + 2*math.Abs(math.Hypot(c.x-p.x, c.y-p.y)-step)
		}
		if p.line == c.line && p.dir == c.dir {
			adv := float64(p.dir) * (c.t - p.t)
			if adv < -gJitterM {
				return gBackCost
			}
			return pace(adv)
		}
		d, gp, ok := g.between(p.line, p.t, p.dir, c.line, c.t, c.dir)
		if !ok || d > step+gStepReachM {
			return gNoWayCost
		}
		if h := g.hairpins(p.line, p.t, p.dir, c.line, c.t, gp); h > 0 {
			return gNoWayCost
		}
		cost := pace(d) + gChangeCost
		if p.line == c.line {
			cost += gUTurnCost
		}
		return cost
	}
	acc := make([][]float64, n0)
	from := make([][]int, n0)
	acc[0] = make([]float64, len(cands[0]))
	for j, c := range cands[0] {
		acc[0][j] = c.cost
	}
	for i := 1; i < n0; i++ {
		acc[i] = make([]float64, len(cands[i]))
		from[i] = make([]int, len(cands[i]))
		for j, c := range cands[i] {
			bc, bk := math.Inf(1), 0
			for k, p := range cands[i-1] {
				if v := acc[i-1][k] + trans(p, c); v < bc {
					bc, bk = v, k
				}
			}
			acc[i][j], from[i][j] = bc+c.cost, bk
		}
	}
	pick := make([]int, n0)
	for j := range acc[n0-1] {
		if acc[n0-1][j] < acc[n0-1][pick[n0-1]] {
			pick[n0-1] = j
		}
	}
	for i := n0 - 1; i > 0; i-- {
		pick[i-1] = from[i][pick[i]]
	}
	st := make([]gstate, n0)
	for i := range n0 {
		st[i] = cands[i][pick[i]]
	}
	return g.trace(st)
}

// trace is the line through the network the states make: along the roads
// between states on them (through the nodes and across roundabouts),
// straight to and from the course's own points.
func (g *gnet) trace(st []gstate) gpathOut {
	// A step back along the same road (the route's wobble) holds still.
	st = append([]gstate(nil), st...)
	for i := 1; i < len(st); i++ {
		p, c := st[i-1], &st[i]
		if p.on && c.on && p.line == c.line && p.dir == c.dir && float64(p.dir)*(c.t-p.t) < 0 {
			c.t = p.t
		}
	}
	var out gpathOut
	add := func(p gpoint) {
		if n := len(out.pts); n > 0 {
			q := out.pts[n-1]
			l := math.Hypot(p.x-q.x, p.y-q.y)
			if l < gMergeM {
				out.pts[n-1].node = q.node || p.node
				out.pts[n-1].flat = q.flat || p.flat
				out.pts[n-1].free = q.free || p.free
				return
			}
			out.s = append(out.s, out.s[n-1]+l)
		} else {
			out.s = append(out.s, 0)
		}
		out.pts = append(out.pts, p)
	}
	along := func(k int, a, b float64, node bool) {
		l := g.lines[k]
		pt := func(d float64, nd bool) gpoint {
			s := sampleBetween(l, d)
			return gpoint{x: s.e, y: s.n, z: s.ele, w: 2 * s.hw, node: nd, on: true}
		}
		add(pt(a, node))
		ss := l.samples
		if b > a {
			for _, s := range ss {
				if s.d > a && s.d < b {
					add(gpoint{x: s.e, y: s.n, z: s.ele, w: 2 * s.hw, on: true})
				}
			}
		} else {
			for i := len(ss) - 1; i >= 0; i-- {
				if s := ss[i]; s.d < a && s.d > b {
					add(gpoint{x: s.e, y: s.n, z: s.ele, w: 2 * s.hw, on: true})
				}
			}
		}
		add(pt(b, false))
	}
	for i, c := range st {
		if i == 0 {
			add(gpoint{x: c.x, y: c.y, z: c.z, w: c.w, on: c.on})
			out.at = append(out.at, 0)
			out.on = append(out.on, c.on)
			continue
		}
		p := st[i-1]
		switch {
		case p.on && c.on && p.line == c.line && p.dir == c.dir && float64(p.dir)*(c.t-p.t) >= -gJitterM:
			along(c.line, p.t, c.t, false)
		case p.on && c.on:
			if _, gp, ok := g.between(p.line, p.t, p.dir, c.line, c.t, c.dir); ok {
				along(p.line, p.t, gp.out.d, false)
				out.pts[len(out.pts)-1].node = true
				for _, e := range gp.edges {
					along(e.line, e.a, e.b, true)
					out.pts[len(out.pts)-1].node = true
				}
				along(c.line, gp.in.d, c.t, true)
			} else {
				add(gpoint{x: c.x, y: c.y, z: c.z, w: c.w, on: true})
			}
		case p.on != c.on:
			// Leaving the roads, or coming back: the course's own line may
			// run beside the road it was on (a cycle path the map lacks), so
			// the step across is a turn at both ends, rounded as one at a
			// node is (owner's A/B, 2026-10-12: 56° and 58° turns 15 m apart
			// where the Posbank Loop leaves the Ellecomsedijk for the cycle
			// tunnel under the N348 at 26.74 km).
			out.pts[len(out.pts)-1].node, out.pts[len(out.pts)-1].free = true, true
			add(gpoint{x: c.x, y: c.y, z: c.z, w: c.w, on: c.on, node: true, free: true})
		default:
			add(gpoint{x: c.x, y: c.y, z: c.z, w: c.w, on: c.on})
		}
		out.at = append(out.at, out.s[len(out.s)-1])
		out.on = append(out.on, c.on)
	}
	return out
}
