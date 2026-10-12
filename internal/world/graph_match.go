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
	he, hn     float64 // the route's way at this step
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
	gSpurM      = 15.0                // a spur of the line through the network, out and back over the same way, this long goes; a longer one is the course's own turn-back
	gSpurTipCos = -0.7                // the line turns more than 135° at a spur's tip (the way back may start a metre aside)
	gSpurOffM   = 1.5                 // the way back keeps within this of the way out (a twin line runs a metre beside; a turn within it is a hairpin anyway)
	gAgainstCos = -0.3                // a net move more than 107° against the route's way is a step back (a hop up a crossing piece to the other carriageway was 116°)
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
		off := gstate{x: ox, y: oy, z: oz, w: ow, he: re, hn: rn, cost: gOffCost}
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
					c := gstate{on: true, line: li, t: t, dir: dir, x: qx, y: qy, he: re, hn: rn,
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
		// No step against the route's way: the net move from one state to
		// the next (over gJitterM) runs with the course, or across to a
		// road beside it, never back (the Dunes course at 17.35 km: from
		// the main road's node 13 m north up a crossing piece to the stubs
		// where the cycle path the map lacks touches, 0.7 m across onto
		// another, and back south, two right angles that no hairpin test
		// sees).
		if mx, my := c.x-p.x, c.y-p.y; math.Hypot(mx, my) > gJitterM && (mx*c.he+my*c.hn)/math.Hypot(mx, my) < gAgainstCos {
			return gBackCost
		}
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
		if h := g.hairpins(p.line, p.t, p.dir, c.line, c.t, c.dir, gp); h > 0 {
			return gNoWayCost
		}
		// Nor any leg of the way through the network against the route's
		// way (the Dunes course at 17.35 km: 13 m north up the crossing
		// piece between a dual road's carriageways, to where the cycle
		// path the map lacks touches, while the course heads south-east;
		// the net move was only 80° off, the state before being 10 m short
		// of the node).
		against := func(k int, a, b float64) bool {
			if math.Abs(b-a) <= gJitterM {
				return false
			}
			sa, sb := sampleBetween(g.lines[k], a), sampleBetween(g.lines[k], b)
			de, dn := sb.e-sa.e, sb.n-sa.n
			l := math.Hypot(de, dn)
			return l > gJitterM && (de*c.he+dn*c.hn)/l < gAgainstCos
		}
		if against(p.line, p.t, gp.out.d) || against(c.line, gp.in.d, c.t) {
			return gBackCost
		}
		for _, e := range gp.edges {
			if against(e.line, e.a, e.b) {
				return gBackCost
			}
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
	// A road change through a node the course overshot by a hair (ahead
	// allows gJitterM), or hasn't quite reached on the new road: the states
	// go to the node, so the line turns there rather than running past it
	// and back.
	for i := 0; i+1 < len(st); i++ {
		p, c := st[i], st[i+1]
		if !p.on || !c.on || p.line == c.line && p.dir == c.dir {
			continue
		}
		_, gp, ok := g.between(p.line, p.t, p.dir, c.line, c.t, c.dir)
		if !ok {
			continue
		}
		for j := i; j >= 0 && st[j].on && st[j].line == p.line && st[j].dir == p.dir && float64(p.dir)*(gp.out.d-st[j].t) < 0; j-- {
			st[j].t = gp.out.d
		}
		for j := i + 1; j < len(st) && st[j].on && st[j].line == c.line && st[j].dir == c.dir && float64(c.dir)*(st[j].t-gp.in.d) < 0; j++ {
			st[j].t = gp.in.d
		}
	}
	var out gpathOut
	var atIdx []int // the point each step lies at
	add := func(p gpoint) {
		if n := len(out.pts); n > 0 {
			q := out.pts[n-1]
			if math.Hypot(p.x-q.x, p.y-q.y) < gMergeM {
				out.pts[n-1].node = q.node || p.node
				out.pts[n-1].flat = q.flat || p.flat
				out.pts[n-1].free = q.free || p.free
				return
			}
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
			atIdx = append(atIdx, 0)
			out.on = append(out.on, c.on)
			continue
		}
		p := st[i-1]
		switch {
		case p.on && c.on && p.line == c.line && p.dir == c.dir && float64(p.dir)*(c.t-p.t) >= -gJitterM:
			along(c.line, p.t, c.t, false)
		case p.on && c.on:
			if _, gp, ok := g.between(p.line, p.t, p.dir, c.line, c.t, c.dir); ok {
				// A node the course overshot by a hair, or hasn't quite
				// reached on the new road: the line turns there, not back.
				from, to := p.t, c.t
				if float64(p.dir)*(gp.out.d-from) < 0 {
					from = gp.out.d
				}
				if float64(c.dir)*(to-gp.in.d) < 0 {
					to = gp.in.d
				}
				along(p.line, from, gp.out.d, false)
				out.pts[len(out.pts)-1].node = true
				for _, e := range gp.edges {
					along(e.line, e.a, e.b, true)
					out.pts[len(out.pts)-1].node = true
				}
				along(c.line, gp.in.d, to, true)
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
		atIdx = append(atIdx, len(out.pts)-1)
		out.on = append(out.on, c.on)
	}
	// A spur: the line out to a point and straight back over the same
	// points (a stub's end, a node reached past the line's own end, a
	// twin line over a road's last metres), up to gSpurM long; the course's
	// own turn-backs are longer. The spur goes, and the steps on it stand
	// at its foot.
	kept := make([]int, len(out.pts)) // new index of each point, -1 gone
	for i := range kept {
		kept[i] = i
	}
	// s along the line so far, for the spurs' lengths.
	sofar := make([]float64, len(out.pts))
	for i := 1; i < len(out.pts); i++ {
		sofar[i] = sofar[i-1] + math.Hypot(out.pts[i].x-out.pts[i-1].x, out.pts[i].y-out.pts[i-1].y)
	}
	// near says whether point q lies within gSpurOffM of the line through
	// points a..b (the way out, whose samples the way back needn't share:
	// a twin line has its own).
	near := func(q gpoint, a, b int) bool {
		for k := a; k < b; k++ {
			p, r := out.pts[k], out.pts[k+1]
			de, dn := r.x-p.x, r.y-p.y
			f := 0.0
			if l2 := de*de + dn*dn; l2 > 0 {
				f = math.Max(0, math.Min(1, ((q.x-p.x)*de+(q.y-p.y)*dn)/l2))
			}
			if math.Hypot(q.x-p.x-f*de, q.y-p.y-f*dn) <= gSpurOffM {
				return true
			}
		}
		return false
	}
	for i := 1; i+1 < len(out.pts); i++ {
		a, b, c := out.pts[i-1], out.pts[i], out.pts[i+1]
		ux, uy, vx, vy := b.x-a.x, b.y-a.y, c.x-b.x, c.y-b.y
		lu, lv := math.Hypot(ux, uy), math.Hypot(vx, vy)
		if lu == 0 || lv == 0 || (ux*vx+uy*vy)/(lu*lv) > gSpurTipCos {
			continue
		}
		// The way out: back from the tip as far as gSpurM.
		j := i
		for j-1 >= 0 && sofar[i]-sofar[j-1] <= gSpurM {
			j--
		}
		// The way back: on while it keeps to the way out.
		k := i
		for k+1 < len(out.pts) && sofar[k+1]-sofar[i] <= gSpurM && near(out.pts[k+1], j, i) {
			k++
		}
		if k == i {
			continue
		}
		// The foot: as far back along the way out as the way back comes.
		foot := i
		for foot-1 >= j && sofar[i]-sofar[foot-1] <= sofar[k]-sofar[i] {
			foot--
		}
		for q := foot + 1; q <= k; q++ {
			out.pts[foot].node = out.pts[foot].node || out.pts[q].node
			out.pts[foot].free = out.pts[foot].free || out.pts[q].free
			kept[q] = -1
		}
		i = k
	}
	var pts []gpoint
	for i, p := range out.pts {
		if kept[i] >= 0 {
			kept[i] = len(pts)
			pts = append(pts, p)
		}
	}
	for i := len(kept) - 1; i >= 0; i-- {
		if kept[i] < 0 {
			if i+1 < len(kept) && kept[i+1] >= 0 {
				kept[i] = kept[i+1]
			} else {
				kept[i] = len(pts) - 1
			}
		}
	}
	out.pts = pts
	out.s = make([]float64, len(pts))
	for i := 1; i < len(pts); i++ {
		out.s[i] = out.s[i-1] + math.Hypot(pts[i].x-pts[i-1].x, pts[i].y-pts[i-1].y)
	}
	for _, k := range atIdx {
		out.at = append(out.at, out.s[kept[k]])
	}
	return out
}
