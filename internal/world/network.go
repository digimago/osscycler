package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

// The roads are the map's, not the route's (owner, 2026-10-08: roads are
// a given of the landscape; the route only maps the rider onto it). Every
// road of the map near the route is drawn once from its own line, a road
// the map split where its tags change chained back into one; where roads
// meet, a junction. The route is matched to the road it rides on, and the
// rider's path is the route's distance placed on that road's centre line.
// Where the route rides on no road of the map (a cycle path the map data
// leaves out, or the test tracks) the road is built along the route.
const (
	clipM       = 150.0 // roads are drawn this far from the route
	minLineM    = 10.0  // pieces of road shorter than this are left out, unless they join roads at both ends
	heightSpanM = 15.0  // a road's height from the ground model is smoothed over this far each way (roads are graded)
	overlapM    = 10.0  // a stretch built along the route reaches this far onto the roads at its ends
	routeSinkM  = 0.01  // and lies this much below them, so they show on top
)

// roadLine is one road drawn in the world.
type roadLine struct {
	samples []sample // every fineStep or less along it (d: along the line)
	smooth  *smoothLine
	loop    bool  // the course's own line on a loop (passes compare round the loop)
	route   bool  // built along the route
	ways    []int // the map's ways it is made of
	// nodes are where the map's nodes lie along it.
	nodes map[int64][]float64
}

// chainPoint is a point of a chained road: where it is, its node, and the
// way it belongs to (reversed when the chain runs against the way).
type chainPoint struct {
	x, y     float64
	node     int64
	way      int
	reversed bool
}

// drawable says whether way k is drawn: motorways (viaducts and tunnels
// come later), driveways, and tunnels the route doesn't ride are left
// out. Bridges are drawn, on decks (bridges.go).
func drawable(w scenery.Way, ridden bool) bool {
	if w.Motorway || w.Driveway || w.Roundabout {
		return false // a roundabout is drawn as one (roundabout.go)
	}
	return ridden || !w.Tunnel
}

func sameRoad(a, b scenery.Way) bool {
	if a.Name != "" || b.Name != "" {
		return a.Name == b.Name
	}
	return a.Class == b.Class
}

// chains joins the drawn ways into roads: two ways end to end at a node
// no other way touches, one road by name (or class), are one line.
func chains(ws []scenery.Way, ridden map[int]bool) [][]chainPoint {
	use := map[int]bool{}
	incid := map[int64]int{} // node → ways touching it (an end 1, a middle 2)
	ends := map[int64][]int{}
	for k, w := range ws {
		if len(w.Line) < 2 || len(w.Nodes) != len(w.Line) || !drawable(w, ridden[k]) {
			continue
		}
		use[k] = true
		for i, n := range w.Nodes {
			if i == 0 || i == len(w.Nodes)-1 {
				incid[n]++
				ends[n] = append(ends[n], k)
			} else {
				incid[n] += 2
			}
		}
	}
	// next is the way going on from way k's end at node n, if one does.
	next := func(k int, n int64) int {
		if incid[n] != 2 || len(ends[n]) != 2 {
			return -1
		}
		for _, o := range ends[n] {
			if o != k && sameRoad(ws[o], ws[k]) {
				return o
			}
		}
		return -1
	}
	done := map[int]bool{}
	var keys []int
	for k := range use {
		keys = append(keys, k)
	}
	sort.Ints(keys) // the same chains every build
	var out [][]chainPoint
	for _, k := range keys {
		if done[k] {
			continue
		}
		// Back to the chain's first way.
		first, rev := k, false
		seen := map[int]bool{k: true}
		for {
			w := ws[first]
			start := w.Nodes[0]
			if rev {
				start = w.Nodes[len(w.Nodes)-1]
			}
			o := next(first, start)
			if o < 0 || seen[o] {
				break
			}
			seen[o] = true
			// o continues backwards from start: in the chain it runs to start.
			first, rev = o, ws[o].Nodes[0] == start
		}
		// Forward from there.
		var pts []chainPoint
		cur, r := first, rev
		for cur >= 0 && !done[cur] {
			done[cur] = true
			w := ws[cur]
			idx := make([]int, len(w.Line))
			for i := range idx {
				idx[i] = i
				if r {
					idx[i] = len(w.Line) - 1 - i
				}
			}
			for j, i := range idx {
				if j == 0 && len(pts) > 0 {
					// The shared node: the next way's segments start there.
					pts[len(pts)-1].way, pts[len(pts)-1].reversed = cur, r
					continue
				}
				pts = append(pts, chainPoint{w.Line[i][0], w.Line[i][1], w.Nodes[i], cur, r})
			}
			end := w.Nodes[idx[len(idx)-1]]
			o := next(cur, end)
			if o < 0 || done[o] {
				break
			}
			cur, r = o, ws[o].Nodes[len(ws[o].Nodes)-1] == end
		}
		out = append(out, pts)
	}
	return out
}

// networkLines are the map's roads near the route as lines, sampled,
// with their surface, sidewalks and lanes, clipped to clipM from it.
func networkLines(ws []scenery.Way, ridden map[int]bool, near func(e, n float64) bool) []*roadLine {
	// How many drawn ways pass each node: a short road with others at both
	// ends links two junctions and is drawn (2026-10-09: an 8.7 m piece of
	// Rhenen's Herenstraat between two junctions was left out, and the
	// rider crossed the grass under it, 1.62 km into a course).
	touch := map[int64]int{}
	for k, w := range ws {
		if len(w.Line) >= 2 && len(w.Nodes) == len(w.Line) && drawable(w, ridden[k]) {
			for _, n := range w.Nodes {
				touch[n]++
			}
		}
	}
	var out []*roadLine
	for _, ch := range chains(ws, ridden) {
		link := touch[ch[0].node] >= 2 && touch[ch[len(ch)-1].node] >= 2
		d := make([]float64, len(ch))
		x, y := make([]float64, len(ch)), make([]float64, len(ch))
		for i, p := range ch {
			x[i], y[i] = p.x, p.y
			if i > 0 {
				d[i] = d[i-1] + math.Hypot(p.x-ch[i-1].x, p.y-ch[i-1].y)
			}
		}
		if d[len(d)-1] < minLineM && !link {
			continue
		}
		sl := newSmoothLine(d, x, y, false, nil)
		nodes := map[int64][]float64{}
		var wayIDs []int
		for i, p := range ch {
			nodes[p.node] = append(nodes[p.node], d[i])
			if len(wayIDs) == 0 || wayIDs[len(wayIDs)-1] != p.way {
				wayIDs = append(wayIDs, p.way)
			}
		}
		// The way at a distance along the chain: that of the segment it lies
		// on (each point carries the way its next segment belongs to).
		wayAt := func(at float64) chainPoint {
			i := sort.SearchFloat64s(d, at) // the first point at or past at
			if i > 0 && (i == len(d) || d[i] > at) {
				i--
			}
			return ch[min(i, len(ch)-1)]
		}
		var run []sample
		flush := func() {
			if len(run) >= 2 && (run[len(run)-1].d-run[0].d >= minLineM || link) {
				out = append(out, &roadLine{samples: run, smooth: sl, ways: wayIDs, nodes: nodes})
			}
			run = nil
		}
		for _, at := range sl.stations(fineStep) {
			e, n := sl.at(at)
			if !near(e, n) {
				flush()
				continue
			}
			cp := wayAt(at)
			w := ws[cp.way]
			s := sample{d: at, e: e, n: n, hw: w.WidthM / 2, surface: w.Surface, walk: w.Sidewalk, lane: w.CycleLane, way: cp.way}
			s.oneway = int8(w.Oneway)
			if cp.reversed { // the way's left is the chain's right
				s.walk[0], s.walk[1] = s.walk[1], s.walk[0]
				s.lane[0], s.lane[1] = s.lane[1], s.lane[0]
				s.oneway = -s.oneway
			}
			run = append(run, s)
		}
		flush()
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// finishRoad sets a line's widths (tapered where they change) and where
// the ground lies flat beside it.
func finishRoad(ss []sample, cell float64) {
	raw := make([]float64, len(ss))
	for i := range ss {
		raw[i] = ss[i].hw
		for side := range 2 {
			if 2*ss[i].hw < 2*laneM+2.5 { // a lane needs room for traffic beside it
				ss[i].lane[side] = false
			}
		}
	}
	for i := range ss {
		var sum float64
		n := 0
		for k := max(0, i-taperSteps); k <= min(len(ss)-1, i+taperSteps); k++ {
			sum += raw[k]
			n++
		}
		ss[i].hw = sum / float64(n)
		ss[i].edge = ss[i].hw
		if ss[i].walk[0] || ss[i].walk[1] {
			ss[i].edge += walkM
		}
		ss[i].flat = ss[i].edge + flatPast(cell)
	}
}

// routeGrid finds route samples near a point quickly.
type routeGrid struct {
	fine  []sample
	cells map[[2]int][]int
	cell  float64
}

func newRouteGrid(fine []sample, cell float64) *routeGrid {
	g := &routeGrid{fine: fine, cells: map[[2]int][]int{}, cell: cell}
	for i, s := range fine {
		k := [2]int{int(math.Floor(s.e / cell)), int(math.Floor(s.n / cell))}
		g.cells[k] = append(g.cells[k], i)
	}
	return g
}

// within calls f for every route sample within r of e, n (r ≤ cell).
func (g *routeGrid) within(e, n, r float64, f func(i int)) {
	cx, cy := int(math.Floor(e/g.cell)), int(math.Floor(n/g.cell))
	for gx := cx - 1; gx <= cx+1; gx++ {
		for gy := cy - 1; gy <= cy+1; gy++ {
			for _, i := range g.cells[[2]int{gx, gy}] {
				if math.Hypot(g.fine[i].e-e, g.fine[i].n-n) <= r {
					f(i)
				}
			}
		}
	}
}

// heights sets a network line's surface: the ground model along it,
// smoothed (roads are graded), every road alike, the ridden ones too, so
// they sit in the landscape and meet at junctions; the rider's path takes
// its height from the road (the sim keeps the ride's own profile). A
// bridge spans straight between its ends (the ground model has the river
// under it). Without a ground model the ground is the route's profile.
func heights(l *roadLine, t *terrain, ws []scenery.Way) {
	ss := l.samples
	base := make([]float64, len(ss))
	lists := map[[2]int]chunkLists{}
	for i, s := range ss {
		key := [2]int{int(math.Floor(s.e / t.o.ChunkM)), int(math.Floor(s.n / t.o.ChunkM))}
		cl, ok := lists[key]
		if !ok {
			_, cl.near = t.lists(key)
			lists[key] = cl
		}
		base[i] = t.base(s.e, s.n, cl.near)
	}
	// Bridges first, on the raw ground: straight from end to end (the
	// smoothing would pull their ends down into what they cross).
	bridge := func(i int) bool { return ss[i].way >= 0 && ss[i].way < len(ws) && ws[ss[i].way].Bridge }
	for i := 0; i < len(ss); {
		if !bridge(i) {
			i++
			continue
		}
		j := i
		for j < len(ss) && bridge(j) {
			j++
		}
		a, b := max(0, i-1), min(len(ss)-1, j)
		for k := i; k < j; k++ {
			f := (ss[k].d - ss[a].d) / math.Max(ss[b].d-ss[a].d, 1e-9)
			base[k] = base[a] + f*(base[b]-base[a])
		}
		i = j
	}
	span := max(1, int(math.Round(heightSpanM/fineStep)))
	for i := range ss {
		var b, bw float64
		for k := max(0, i-span); k <= min(len(ss)-1, i+span); k++ {
			wt := float64(span + 1 - abs(k-i))
			b += wt * base[k]
			bw += wt
		}
		ss[i].ele = b / bw
	}
}

// routeLines are the stretches of the route on no drawn road, built along
// the route (its smooth line), reaching overlapM onto the roads at either
// end; on -1 means no road. all: the whole route (no map data).
func routeLines(c *course.Course, line *smoothLine, roads []scenery.RoadStretch, on func(d float64) int, drawn func(way int) bool, all bool, o Options, lift func(float64) float64) []*roadLine {
	whole := sampleRoad(c, line, fineStep)
	for i := range whole {
		whole[i].ele += lift(whole[i].d)
	}
	applyRoads(whole, roads, o.RoadWidthM, o.CellM)
	if all {
		return []*roadLine{{samples: whole, smooth: line, loop: c.Loop, route: true}}
	}
	off := make([]bool, len(whole))
	for i, s := range whole {
		k := on(s.d)
		off[i] = k < 0 || !drawn(k)
	}
	var out []*roadLine
	reach := int(overlapM / fineStep)
	for i := 0; i < len(whole); {
		if !off[i] {
			i++
			continue
		}
		j := i
		for j < len(whole) && off[j] {
			j++
		}
		a, b := max(0, i-reach), min(len(whole), j+reach)
		ss := append([]sample(nil), whole[a:b]...)
		for k := range ss {
			ss[k].ele -= routeSinkM
			ss[k].way = -1
		}
		if len(ss) >= 2 {
			out = append(out, &roadLine{samples: ss, smooth: line, route: true})
		}
		i = j
	}
	return out
}

// networkPatches are the junctions of the drawn roads: wherever lines meet
// at a node other than where a road simply goes on. A patch takes the
// surface of its main road: the highest class (ws: the map's ways), then
// the widest.
func networkPatches(ls []*roadLine, ws []scenery.Way) []patch {
	type inc = patchInc
	at := map[int64][]inc{}
	for _, l := range ls {
		if l.route {
			continue
		}
		lo, hi := l.samples[0].d, l.samples[len(l.samples)-1].d
		for node, ds := range l.nodes {
			for _, d := range ds {
				if d < lo-0.5 || d > hi+0.5 {
					continue // clipped away
				}
				at[node] = append(at[node], inc{l, d, d > lo+1 && d < hi-1})
			}
		}
	}
	var out []patch
	var nodes []int64
	for n := range at {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	for _, node := range nodes {
		is := at[node]
		count := 0
		for _, i := range is {
			count += 1 + boolInt(i.middle)
		}
		if count < 2 || len(is) < 2 {
			continue // a road's own bend, or its end
		}
		widest := 0.0
		surface, mainRank, mainHW := scenery.SurfaceAsphalt, -1, 0.0
		var arms []patchArm
		for _, i := range is {
			s := sampleAt(i.l, i.at)
			widest = math.Max(widest, s.hw)
			r := 0
			if s.way >= 0 && s.way < len(ws) {
				r = classRank[ws[s.way].Class]
			}
			if r > mainRank || r == mainRank && s.hw > mainHW {
				surface, mainRank, mainHW = s.surface, r, s.hw
			}
			arms = append(arms, patchArm{i.l, i.at})
		}
		p := patch{arms: arms, surface: surface, rank: mainRank}
		for k, i := range is {
			lo, hi := overlapReach(is[k].l, i.at, -1, widest+apronAlongM, is, k), overlapReach(is[k].l, i.at, 1, widest+apronAlongM, is, k)
			first, last := i.l.samples[0].d, i.l.samples[len(i.l.samples)-1].d
			p.spans = append(p.spans, patchSpan{i.l, math.Max(first, i.at-lo), math.Min(last, i.at+hi), i.at, i.at, i.at})
		}
		s0 := sampleAt(is[0].l, is[0].at)
		p.at, p.node = s0.d, [2]float64{s0.e, s0.n}
		p.nodes = [][2]float64{p.node}
		out = append(out, p)
	}
	return shapePatches(mergePatches(out))
}

// patchInc is a road at a junction's node: its line, where along it the
// node lies, and whether the road goes on past it.
type patchInc struct {
	l      *roadLine
	at     float64
	middle bool
}

const (
	mergeReachM = 80.0 // a patch reaches at most this far along overlapping roads
	mergeGapM   = 1.5  // roads closer than this at their edges are one surface (a narrower gap is a dark slit at eye level)
)

// overlapReach is how far from the junction, one way (dir) along a
// road, the patch reaches: at least base, and on while the road still
// overlaps another road meeting there (two carriageways joining, a
// slip road), so where they overlap they are one surface.
func overlapReach(l *roadLine, at, dir, base float64, is []patchInc, self int) float64 {
	reach := base
	for d := base; d <= mergeReachM; d += fineStep {
		s := sampleAt(l, at+dir*d)
		if math.Abs(s.d-(at+dir*d)) > fineStep {
			break // the road ends
		}
		over := false
		for k, o := range is {
			if k == self || o.l == l {
				continue
			}
			for _, t := range o.l.samples {
				if math.Abs(t.d-o.at) <= mergeReachM+20 && math.Hypot(t.e-s.e, t.n-s.n) < s.hw+t.hw+mergeGapM {
					over = true
					break
				}
			}
			if over {
				break
			}
		}
		if !over {
			break
		}
		reach = d
	}
	return reach
}

func sampleAt(l *roadLine, d float64) sample {
	i := sort.Search(len(l.samples), func(i int) bool { return l.samples[i].d >= d })
	return l.samples[max(0, min(i, len(l.samples)-1))]
}

// network is the world's roads, and how the route lies on them.
type network struct {
	lines []*roadLine
	on    func(d float64) int // the way the course runs on at d, -1 its own
	byWay map[int][]sampleRef

	c         *course.Course
	o         Options
	t         *terrain
	route     []sample
	line      *smoothLine
	hasWays   bool
	stretches []scenery.RoadStretch
}

type sampleRef struct {
	l *roadLine
	i int
}

// newNetwork builds the roads: the map's near the route (when the map data
// has them), and the route's own where it leaves them, or all of it
// without map data. The course runs on the roads: where the route follows
// a cycle path or footway beside a drawn road (a router's bike profile
// likes those, the map data leaves them out), the course is that road, as
// on a closed course; only where the route leaves the roads altogether is
// its own way built.
func newNetwork(c *course.Course, o Options, line *smoothLine, route []sample, t *terrain) *network {
	sc := o.Scenery
	hasWays := sc != nil && len(sc.Ways) > 0 && len(sc.RouteOn) > 0 && sc.RouteStepM > 0
	var stretches []scenery.RoadStretch
	if sc != nil {
		stretches = sc.Roads
	}
	n := &network{on: func(float64) int { return -1 }, byWay: map[int][]sampleRef{},
		c: c, o: o, t: t, route: route, line: line, hasWays: hasWays, stretches: stretches}
	drawn := map[int]bool{}
	if hasWays {
		mapOn := func(d float64) int {
			i := int(math.Round(d / sc.RouteStepM))
			return sc.RouteOn[max(0, min(i, len(sc.RouteOn)-1))]
		}
		ridden := map[int]bool{}
		for _, k := range sc.RouteOn {
			if k >= 0 {
				ridden[k] = true
			}
		}
		// Bridges and tunnels the route runs along are ridden too, whatever
		// the match says (it may hold on to the road onto a bridge): left
		// out, they leave a gap the rider would hop around.
		along := newRouteGrid(route, 10)
		for k, w := range sc.Ways {
			if (w.Bridge || w.Tunnel) && !ridden[k] && runsAlong(w.Line, route, along) {
				ridden[k] = true
			}
		}
		clip := newRouteGrid(route, clipM)
		near := func(e, no float64) bool {
			found := false
			clip.within(e, no, clipM, func(int) { found = true })
			return found
		}
		n.lines = networkLines(sc.Ways, ridden, near)
		for _, l := range n.lines {
			finishRoad(l.samples, o.CellM)
			for i, s := range l.samples {
				drawn[s.way] = true
				n.byWay[s.way] = append(n.byWay[s.way], sampleRef{l, i})
			}
		}
		course := courseRoads(route, mapOn, drawn, n.lines, sc.Ways)
		fineD := c.Distance / float64(len(route)-1)
		n.on = func(d float64) int { return course[max(0, min(int(math.Round(d/fineD)), len(course)-1))] }
		for _, l := range n.lines {
			heights(l, t, sc.Ways)
		}
		joinHeights(n.lines)
	}
	if !hasWays {
		// No map roads: the road is the route's, as it always was.
		n.lines = append(n.lines, routeLines(c, line, stretches, n.on, func(int) bool { return false }, true, o, t.liftAt)...)
	}
	return n
}

// finish places the rider (the path, as path does) and, with map roads,
// builds the course's own ways along it where it leaves them: built
// along the rider's line, they branch off the road where the rider does,
// so the rider is always on something drawn.
//
// The path's sharp turns are swept (smoothTurns) before the course's own
// roads are laid along it, so a road the course builds itself takes the
// turn as the rider does (owner, 2026-10-10: on a preview of the Amsterdam
// Water Supply Dunes course the road kept a 9 m dogleg at 2.36 km and a
// right angle at 9.92 km while the rider swept across the grass, through
// an oak). Map roads keep their own shape: their junctions carry the
// sweep. Connectors look for jumps on the path as matched: sweeping
// spreads a jump over several steps.
func (n *network) finish(rbs []roundabout) (d, x, y, z, w []float64) {
	d, x, y, z, w, f := n.path()
	sx, sy := append([]float64(nil), x...), append([]float64(nil), y...)
	smoothTurns(sx, sy, pathStep, n.c.Loop)
	if n.hasWays {
		own := append(n.ownWays(d, sx, sy, z, f), n.connectors(d, x, y, z, rbs)...)
		n.lines = continueDeadEnds(n.lines, own)
		// Wherever the rider's path still has no road under it, the course
		// builds its own there too.
		if gaps, any := n.openRuns(d, sx, sy, rbs); any {
			n.lines = continueDeadEnds(n.lines, n.ownWays(d, sx, sy, z, gaps))
		}
	} else {
		sweepRouteLines(n.lines, d, x, y, sx, sy)
	}
	return d, sx, sy, z, w
}

// Gaps under the rider's path (owner, 2026-10-10: the riding line off road,
// a rule for every course): ownWays covers where the matching put the path
// off the map roads, connectors the long jumps between them over open
// ground; the rest, short stretches where the path has no drawn road under
// it at all (the Grebbeberg course: 171 m in 31 places, up to 25 m, beside
// a drawn road whose end or bend leaves the path on the grass), get the
// course's own way as well.
const (
	gapMinM  = 3.0 // a gap this long or longer gets a way
	gapCheck = 1.0 // the path is looked at every metre
	gapRbM   = 5.0 // not within a roundabout's ring and this much beyond (its disc is drawn later)
)

// openRuns marks, per path point (d, x, y), where the path has no drawn
// road under it for gapMinM or more: 0 there (and the points either side),
// 1 elsewhere, as ownWays takes; and whether there is any.
func (n *network) openRuns(d, x, y []float64, rbs []roundabout) ([]float64, bool) {
	const cell = 10.0
	grid := map[[2]int][]sampleRef{}
	key := func(e, no float64) [2]int { return [2]int{int(math.Floor(e / cell)), int(math.Floor(no / cell))} }
	for _, l := range n.lines {
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	on := func(e, no float64) bool {
		for _, rb := range rbs {
			if math.Hypot(e-rb.c[0], no-rb.c[1]) < rb.outer()+gapRbM {
				return true
			}
		}
		k := key(e, no)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					ss := r.l.samples
					if r.i+1 >= len(ss) {
						continue
					}
					a, b := ss[r.i], ss[r.i+1]
					de, dn := b.e-a.e, b.n-a.n
					t := 0.0
					if l2 := de*de + dn*dn; l2 > 0 {
						t = math.Max(0, math.Min(1, ((e-a.e)*de+(no-a.n)*dn)/l2))
					}
					if math.Hypot(e-a.e-t*de, no-a.n-t*dn) < a.hw+t*(b.hw-a.hw) {
						return true
					}
				}
			}
		}
		return false
	}
	f := make([]float64, len(d))
	for i := range f {
		f[i] = 1
	}
	any := false
	runFrom, run := -1, 0.0
	mark := func(to int) {
		if run >= gapMinM {
			for k := max(0, runFrom-1); k <= min(len(d)-1, to+1); k++ {
				f[k] = 0
			}
			any = true
		}
		runFrom, run = -1, 0
	}
	for i := 0; i+1 < len(d); i++ {
		l := math.Hypot(x[i+1]-x[i], y[i+1]-y[i])
		for t := 0.0; t < l; t += gapCheck {
			e, no := x[i]+(x[i+1]-x[i])*t/l, y[i]+(y[i+1]-y[i])*t/l
			if on(e, no) {
				mark(i)
				continue
			}
			if runFrom < 0 {
				runFrom = i
			}
			run += gapCheck
		}
	}
	mark(len(d) - 1)
	return f, any
}

// sweepRouteLines moves the route's own lines onto the swept path (sx, sy)
// where it differs from the path as placed (x, y), both at distances d.
func sweepRouteLines(ls []*roadLine, d, x, y, sx, sy []float64) {
	moved := make([]bool, len(d))
	some := false
	for i := range d {
		moved[i] = math.Hypot(sx[i]-x[i], sy[i]-y[i]) > 1e-3
		some = some || moved[i]
	}
	if !some {
		return
	}
	swept := newSmoothLine(d, sx, sy, false, nil)
	for _, l := range ls {
		if !l.route {
			continue
		}
		changed := false
		for k := range l.samples {
			s := &l.samples[k]
			i := sort.SearchFloat64s(d, s.d)
			if (i < len(d) && moved[i]) || (i > 0 && moved[i-1]) {
				s.e, s.n = swept.at(s.d)
				changed = true
			}
		}
		if changed {
			ds, es, ns := make([]float64, len(l.samples)), make([]float64, len(l.samples)), make([]float64, len(l.samples))
			for k, s := range l.samples {
				ds[k], es[k], ns[k] = s.d, s.e, s.n
			}
			l.smooth = newSmoothLine(ds, es, ns, l.loop, nil)
		}
	}
}

// Where the course jumps between roads.
const (
	jumpM     = 8.0 // a step this long between two roads is a jump
	openRunM  = 18  // over this much of nothing drawn: more than keepOnRoad mends from either side
	openCheck = 1.0 // checked every metre
	jumpRbM   = 25  // a jump this near a roundabout's ring is the way through it (passThrough)
)

// connectors are the course's own ways where its path jumps from one road
// to another over open ground: the route took a path the map lacks, and
// the roads it was matched to are further apart than matching reaches round
// the corner (Ellecomsedijk to Zutphensestraatweg at 26.8 km on the
// Posbank Loop: 51 m in one step, a straight line over the grass). Shorter
// open runs the rider is kept off later (keepOnRoad), and by a roundabout
// the path goes through it (passThrough).
func (n *network) connectors(d, x, y, z []float64, rbs []roundabout) []*roadLine {
	const cell = 10.0
	grid := map[[2]int][]sampleRef{}
	key := func(e, no float64) [2]int { return [2]int{int(math.Floor(e / cell)), int(math.Floor(no / cell))} }
	for _, l := range n.lines {
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	// on tells whether e, no lies on a road (within a metre of its edge)
	// or a roundabout; inside, well inside a carriageway.
	near := func(e, no, past float64) bool {
		k := key(e, no)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					ss := r.l.samples
					if r.i+1 >= len(ss) {
						continue
					}
					a, b := ss[r.i], ss[r.i+1]
					de, dn := b.e-a.e, b.n-a.n
					t := 0.0
					if l2 := de*de + dn*dn; l2 > 0 {
						t = math.Max(0, math.Min(1, ((e-a.e)*de+(no-a.n)*dn)/l2))
					}
					if math.Hypot(e-a.e-t*de, no-a.n-t*dn) < math.Max(a.edge, b.edge)+past {
						return true
					}
				}
			}
		}
		return false
	}
	// roadHeight is the height of the road whose carriageway e, no lies on.
	roadHeight := func(e, no float64) (float64, bool) {
		best, z := math.Inf(1), 0.0
		k := key(e, no)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					a := r.l.samples[r.i]
					if d := math.Hypot(e-a.e, no-a.n); d < best && d < a.hw+fineStep {
						best, z = d, a.ele
					}
				}
			}
		}
		return z, !math.IsInf(best, 1)
	}
	byRoundabout := func(e, no, reach float64) bool {
		for _, rb := range rbs {
			if math.Hypot(e-rb.c[0], no-rb.c[1]) < rb.r+rb.hw+reach {
				return true
			}
		}
		return false
	}
	var out []*roadLine
	for i := 1; i < len(x); i++ {
		l := math.Hypot(x[i]-x[i-1], y[i]-y[i-1])
		if l < jumpM || byRoundabout(x[i-1], y[i-1], jumpRbM) || byRoundabout(x[i], y[i], jumpRbM) {
			continue
		}
		run, longest := 0.0, 0.0
		for t := 0.0; t <= l; t += openCheck {
			e, no := x[i-1]+(x[i]-x[i-1])*t/l, y[i-1]+(y[i]-y[i-1])*t/l
			if near(e, no, 1) || byRoundabout(e, no, 0) {
				run = 0
				continue
			}
			run += openCheck
			longest = math.Max(longest, run)
		}
		if longest < openRunM {
			continue
		}
		// Straight across, sampled every fineStep along it; its line is
		// measured in course distance, as the rider's path is.
		sl := newSmoothLine([]float64{d[i-1], d[i]}, []float64{x[i-1], x[i]}, []float64{y[i-1], y[i]}, false, nil)
		steps := max(2, int(math.Ceil(l/fineStep)))
		var ss []sample
		for k := 0; k <= steps; k++ {
			f := float64(k) / float64(steps)
			at := d[i-1] + f*(d[i]-d[i-1])
			e, no := sl.at(at)
			ss = append(ss, sample{d: at, e: e, n: no, ele: z[i-1] + f*(z[i]-z[i-1]) - routeSinkM, way: -1})
		}
		// Only over the open ground: a piece between each two roads it
		// crosses, reaching one sample inside them (the roads draw the rest).
		in := make([]bool, len(ss))
		for k := range ss {
			in[k] = near(ss[k].e, ss[k].n, -0.5)
		}
		for k := 0; k < len(ss); {
			if in[k] {
				k++
				continue
			}
			j := k
			for j < len(ss) && !in[j] {
				j++
			}
			piece := append([]sample{}, ss[max(0, k-1):min(len(ss), j+1)]...)
			k = j
			if len(piece) < 2 {
				continue
			}
			applyRoads(piece, n.stretches, n.o.RoadWidthM, n.o.CellM)
			for q := range piece {
				// A path the map lacks: no sidewalks or lanes (as ownWays).
				piece[q].way, piece[q].walk, piece[q].lane = -1, [2]bool{}, [2]bool{}
			}
			// Its ends at the height of the roads they reach into, just
			// under them, and straight between (no step where it meets them).
			h0, h1 := piece[0].ele, piece[len(piece)-1].ele
			if z, ok := roadHeight(piece[0].e, piece[0].n); ok {
				h0 = z - 0.01
			}
			if z, ok := roadHeight(piece[len(piece)-1].e, piece[len(piece)-1].n); ok {
				h1 = z - 0.01
			}
			for q := range piece {
				f := float64(q) / float64(len(piece)-1)
				piece[q].ele = h0 + f*(h1-h0)
			}
			out = append(out, &roadLine{samples: piece, smooth: sl, route: true})
		}
	}
	return out
}

// Joining an own way to a dead end.
const (
	deadEndM = 4.0  // an own way's end this near a map road's dead end continues it
	easeM    = 10.0 // and takes on its own width and height over this far
)

// continueDeadEnds joins each own way that carries on from a map road's
// dead end to that road (owner, 2026-10-09: where a cycle path went on
// from a residential road's end, the two overlapped, with verges and
// sidewalk ends crossing between them): one line, so one surface, eased
// from the road's width and height to the way's own and on into the next
// road where it ends at another dead end. It returns the lines: the map's,
// joined where they were, and the own ways left on their own.
func continueDeadEnds(maps, own []*roadLine) []*roadLine {
	lines := append([]*roadLine{}, maps...)
	// deadEnd is the map line whose end (start: false, end: true) lies near
	// p with nothing else meeting it there.
	deadEnd := func(p sample) (*roadLine, bool, bool) {
		for _, l := range lines {
			if l.route || len(l.samples) < 2 {
				continue
			}
			for _, last := range []bool{false, true} {
				s := l.samples[0]
				if last {
					s = l.samples[len(l.samples)-1]
				}
				if math.Hypot(p.e-s.e, p.n-s.n) >= deadEndM {
					continue
				}
				alone := true
				for _, m := range lines {
					for _, t := range m.samples {
						if m == l && math.Abs(t.d-s.d) < 2*deadEndM {
							continue
						}
						if math.Hypot(t.e-s.e, t.n-s.n) < 2*deadEndM {
							alone = false
						}
					}
				}
				if alone {
					return l, last, true
				}
			}
		}
		return nil, false, false
	}
	var out []*roadLine
	for _, o := range own {
		if len(o.samples) < 2 {
			continue
		}
		a, aLast, okA := deadEnd(o.samples[0])
		b, bLast, okB := deadEnd(o.samples[len(o.samples)-1])
		if okB && b == a {
			okB = false // a road's two ends: leave it a loop of its own
		}
		if !okA && !okB {
			out = append(out, o)
			continue
		}
		// The road before (its dead end last), the way, the road after
		// (its dead end first); each map part's nodes follow it.
		var ss []sample
		joined := &roadLine{}
		take := func(l *roadLine, rev bool) {
			part := l.samples
			if rev {
				part = reversed(part)
			}
			off := 0.0
			if len(ss) > 0 {
				ss = appendEased(ss, part)
				off = ss[len(ss)-len(part)].d - part[0].d
			} else {
				ss = append([]sample{}, part...)
			}
			length := l.samples[len(l.samples)-1].d
			if joined.nodes == nil {
				joined.nodes = map[int64][]float64{}
			}
			for node, ds := range l.nodes {
				for _, d := range ds {
					if rev {
						d = length - d
					}
					joined.nodes[node] = append(joined.nodes[node], off+d)
				}
			}
			joined.ways = append(joined.ways, l.ways...)
		}
		if okA {
			take(a, !aLast)
		}
		way := o.samples
		if len(ss) > 0 {
			ss = appendEased(ss, way)
		} else {
			ss = append([]sample{}, way...)
		}
		if okB {
			take(b, bLast)
		}
		joined.samples, joined.smooth = ss, smoothOf(ss)
		var keep []*roadLine
		for _, l := range lines {
			if !(okA && l == a) && !(okB && l == b) {
				keep = append(keep, l)
			}
		}
		lines = append(keep, joined)
	}
	return append(lines, out...)
}

// reversed is ss back to front, distances counted from its new start.
func reversed(ss []sample) []sample {
	out := make([]sample, len(ss))
	end := ss[len(ss)-1].d
	for i := range ss {
		s := ss[len(ss)-1-i]
		s.d = end - s.d
		// Left and right swap, and a one-way turns round.
		s.walk[0], s.walk[1] = s.walk[1], s.walk[0]
		s.lane[0], s.lane[1] = s.lane[1], s.lane[0]
		s.wide[0], s.wide[1] = s.wide[1], s.wide[0]
		s.open[0], s.open[1] = s.open[1], s.open[0]
		s.oneway = -s.oneway
		out[i] = s
	}
	return out
}

// appendEased appends the own way's samples to a road's, from the road's
// end on, its width and height eased from the road's over easeM.
func appendEased(road, own []sample) []sample {
	end := road[len(road)-1]
	out := road
	at := end.d
	prev := end
	for k, s := range own {
		step := math.Hypot(s.e-prev.e, s.n-prev.n)
		if k == 0 && step < fineStep/2 {
			continue // the way's first sample lies on the road's end
		}
		at += step
		prev = s
		f := math.Min(1, (at-end.d)/easeM)
		f = f * f * (3 - 2*f)
		s.d = at
		s.hw = end.hw + f*(s.hw-end.hw)
		s.edge = end.edge + f*(s.edge-end.edge)
		s.flat = end.flat + f*(s.flat-end.flat)
		s.ele = end.ele + f*(s.ele-end.ele)
		out = append(out, s)
	}
	return out
}

// smoothOf is a smooth line through samples, by their distances.
func smoothOf(ss []sample) *smoothLine {
	d, x, y := make([]float64, len(ss)), make([]float64, len(ss)), make([]float64, len(ss))
	for i, s := range ss {
		d[i], x[i], y[i] = s.d, s.e, s.n
	}
	return newSmoothLine(d, x, y, false, nil)
}

// ownWays are the course's own ways: along the path wherever it isn't
// wholly on a road (f < 1), a few steps more either side so they meet the
// roads, just under them.
func (n *network) ownWays(d, x, y, z, f []float64) []*roadLine {
	var out []*roadLine
	const reach = 3 // path steps either side
	// inside tells whether e, no lies well inside a map road's carriageway:
	// an own way's ends are trimmed back to just inside it, so it meets the
	// road rather than lying under it (owner, 2026-10-09: under a dead end's
	// last 15 m it showed through in dark lines, its verges in slivers).
	const cell = 10.0
	grid := map[[2]int][]sampleRef{}
	key := func(e, no float64) [2]int { return [2]int{int(math.Floor(e / cell)), int(math.Floor(no / cell))} }
	for _, l := range n.lines {
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	inside := func(e, no float64) bool {
		k := key(e, no)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
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
					t := ((e-a.e)*de + (no-a.n)*dn) / l2
					if t < 0 || t > 1 {
						continue
					}
					if math.Hypot(e-a.e-t*de, no-a.n-t*dn) < a.hw+t*(b.hw-a.hw)-0.5 {
						return true
					}
				}
			}
		}
		return false
	}
	for i := 0; i < len(d); {
		if f[i] > 0.999 {
			i++
			continue
		}
		j := i
		for j < len(d) && f[j] <= 0.999 {
			j++
		}
		a, b := max(0, i-reach), min(len(d)-1, j-1+reach)
		if b-a < 1 {
			i = j
			continue
		}
		sl := newSmoothLine(d[a:b+1], x[a:b+1], y[a:b+1], false, nil)
		var ss []sample
		for at := d[a]; ; at += fineStep {
			at = math.Min(at, d[b])
			e, no := sl.at(at)
			k := min(b-1, a+int((at-d[a])/(d[a+1]-d[a])))
			t := (at - d[k]) / math.Max(d[k+1]-d[k], 1e-9)
			ss = append(ss, sample{d: at, e: e, n: no, ele: z[k] + t*(z[k+1]-z[k]) - routeSinkM, way: -1})
			if at >= d[b] {
				break
			}
		}
		// Trimmed to one sample inside the road at either end.
		lo, hi := 0, len(ss)
		for lo+1 < hi && inside(ss[lo].e, ss[lo].n) && inside(ss[lo+1].e, ss[lo+1].n) {
			lo++
		}
		for hi-1 > lo+1 && inside(ss[hi-1].e, ss[hi-1].n) && inside(ss[hi-2].e, ss[hi-2].n) {
			hi--
		}
		ss = ss[lo:hi]
		if len(ss) < 2 {
			i = j
			continue
		}
		applyRoads(ss, n.stretches, n.o.RoadWidthM, n.o.CellM)
		for k := range ss {
			// A path the map data lacks (a cycle path, a track): no
			// sidewalks or cycle lanes of a road it meets (owner,
			// 2026-10-09: they crossed the road where a path went on from
			// a dead end).
			ss[k].way, ss[k].walk, ss[k].lane = -1, [2]bool{}, [2]bool{}
		}
		out = append(out, &roadLine{samples: ss, smooth: sl, route: true})
		i = j
	}
	return out
}

// The course on the roads.
const (
	parallelM      = 25.0  // a drawn road this close alongside the route, and parallel, is the course
	sidepathM      = 40.0  // or this close, when it sends cyclists onto a path beside it
	minCourseRunM  = 30.0  // the course doesn't change roads for less than this
	joinM          = 15.0  // roads meeting at a junction are brought to one height over this far
	ventM          = 20.0  // a higher-class road this close alongside the route's is the course
	parallelCosine = 0.866 // within 30°
)

// courseRoads is the road the course runs on at each route sample: the
// map's road under the route where it is drawn (but the main road where
// the route takes a service road alongside it: a closed course keeps to
// the road), else a drawn road alongside (the route on a cycle path
// beside it), else -1 (the route's own way). Runs shorter than
// minCourseRunM take their neighbours' road, so the rider doesn't hop
// from road to path and back.
func courseRoads(route []sample, mapOn func(float64) int, drawn map[int]bool, lines []*roadLine, ws []scenery.Way) []int {
	service := func(k int) bool { return k >= 0 && k < len(ws) && ws[k].Class == "service" }
	rank := func(k int) int {
		if k < 0 || k >= len(ws) {
			return -1
		}
		return classRank[ws[k].Class]
	}
	sidepath := func(k int) bool { return k >= 0 && k < len(ws) && ws[k].Sidepath }
	grid := map[[2]int][]sampleRef{}
	key := func(e, n float64) [2]int {
		return [2]int{int(math.Floor(e / sidepathM)), int(math.Floor(n / sidepathM))}
	}
	for _, l := range lines {
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	dir := func(ss []sample, i int) (float64, float64) {
		a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
		l := math.Hypot(b.e-a.e, b.n-a.n)
		if l == 0 {
			return 0, 0
		}
		return (b.e - a.e) / l, (b.n - a.n) / l
	}
	// alongside is the nearest drawn road within reach of r that runs its
	// way and that want accepts, -1 if none.
	alongside := func(r sample, re, rn float64, self int, reach float64, want func(w int) bool) int {
		best, out := reach, -1
		cell := key(r.e, r.n)
		for gx := cell[0] - 1; gx <= cell[0]+1; gx++ {
			for gy := cell[1] - 1; gy <= cell[1]+1; gy++ {
				for _, ref := range grid[[2]int{gx, gy}] {
					s := ref.l.samples[ref.i]
					if s.way == self || !want(s.way) {
						continue
					}
					d := math.Hypot(s.e-r.e, s.n-r.n)
					if d >= best {
						continue
					}
					se, sn := dir(ref.l.samples, ref.i)
					if math.Abs(se*re+sn*rn) < parallelCosine || against(s, se*re+sn*rn) {
						continue
					}
					best, out = d, s.way
				}
			}
		}
		return out
	}
	// wrongWay tells whether the route runs against way k's one-way at r:
	// it then lies on the other carriageway of a dual road (the map's
	// matching goes by distance only, and the route on a cycle path beside
	// the road lies nearer the wrong one as often as not).
	wrongWay := func(r sample, re, rn float64, k int) bool {
		if k < 0 || k >= len(ws) || ws[k].Oneway == 0 {
			return false
		}
		best, wrong := sidepathM, false
		cell := key(r.e, r.n)
		for gx := cell[0] - 1; gx <= cell[0]+1; gx++ {
			for gy := cell[1] - 1; gy <= cell[1]+1; gy++ {
				for _, ref := range grid[[2]int{gx, gy}] {
					s := ref.l.samples[ref.i]
					if s.way != k {
						continue
					}
					if d := math.Hypot(s.e-r.e, s.n-r.n); d < best {
						se, sn := dir(ref.l.samples, ref.i)
						best, wrong = d, against(s, se*re+sn*rn)
					}
				}
			}
		}
		return wrong
	}
	out := make([]int, len(route))
	chosen := make([]bool, len(route)) // by a rule below, not the map's matching
	for i, r := range route {
		k := mapOn(r.d)
		if k >= 0 && drawn[k] {
			re, rn := dir(route, i)
			if wrongWay(r, re, rn, k) {
				// The carriageway the route's traffic takes, alongside.
				if m := alongside(r, re, rn, k, parallelM, func(w int) bool { return rank(w) >= rank(k) }); m >= 0 {
					out[i], chosen[i] = m, true
					continue
				}
			}
		}
		// A higher-class road alongside, within ventM: the route takes the
		// access road beside it (a ventweg); a closed course keeps to it.
		if k >= 0 && drawn[k] {
			re, rn := dir(route, i)
			if m := alongside(r, re, rn, k, ventM, func(w int) bool { return rank(w) > rank(k) }); m >= 0 {
				out[i], chosen[i] = m, true
				continue
			}
		}
		if k >= 0 && drawn[k] && !service(k) {
			out[i] = k
			continue
		}
		out[i] = -1
		if k >= 0 && drawn[k] {
			out[i] = k // a service road, unless a road runs alongside
		}
		// The route on a cycle path: the road beside it is the course (a
		// closed course keeps to the road), from further out when the road
		// itself sends cyclists onto that path (Dieren's Harderwijkerweg,
		// 22-29 m off).
		re, rn := dir(route, i)
		best := sidepathM
		cell := key(r.e, r.n)
		for gx := cell[0] - 1; gx <= cell[0]+1; gx++ {
			for gy := cell[1] - 1; gy <= cell[1]+1; gy++ {
				for _, ref := range grid[[2]int{gx, gy}] {
					s := ref.l.samples[ref.i]
					d := math.Hypot(s.e-r.e, s.n-r.n)
					if d >= best || (d >= parallelM && !sidepath(s.way)) {
						continue
					}
					se, sn := dir(ref.l.samples, ref.i)
					if math.Abs(se*re+sn*rn) < parallelCosine || service(s.way) || against(s, se*re+sn*rn) {
						continue
					}
					best, out[i] = d, s.way
				}
			}
		}
	}
	if len(route) > 1 {
		step := route[len(route)-1].d / float64(len(route)-1)
		smoothRuns(out, max(1, int(minCourseRunM/step)))
	}
	keepToConnected(out, chosen, route, lines, ws)
	return out
}

// keepToConnected keeps the course on its road where the next road it is
// matched to doesn't meet it (owner, 2026-10-10: on the Grebbeberg course
// at 55-62 km the route's line runs down the middle of a canal with a
// road on either bank, 10 m each side; the match took one bank, then the
// other, every few hundred metres, and the rider crossed the water with no
// bridge). A rider changes roads only where they meet: so a change of road
// (one way to another, out per route sample) is kept where the two roads'
// surfaces touch near there, and otherwise the course stays on its road for
// as long as that runs alongside the route, as on a closed course (a change
// by way of a bridge between the banks came anywhere near the bridge, over
// the water). Changes the rules above chose on purpose (the carriageway
// the traffic takes, the main road beside an access road) are kept.
func keepToConnected(out []int, chosen []bool, route []sample, lines []*roadLine, ws []scenery.Way) {
	const cell, reach, ringM = 10.0, 30.0, 40.0
	// Roundabouts' rings (not drawn as roads: the roads meeting there don't
	// touch): a change of road near one is the route's.
	rings := map[[2]int]bool{}
	for _, w := range ws {
		if !w.Roundabout {
			continue
		}
		for _, p := range w.Line {
			for gx := -1; gx <= 1; gx++ {
				for gy := -1; gy <= 1; gy++ {
					rings[[2]int{int(math.Floor(p[0]/ringM)) + gx, int(math.Floor(p[1]/ringM)) + gy}] = true
				}
			}
		}
	}
	grid := map[[2]int][]sampleRef{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cell)), int(math.Floor(n / cell))} }
	for _, l := range lines {
		if l.route {
			continue
		}
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	dir := func(ref sampleRef) (float64, float64) {
		ss := ref.l.samples
		a, b := ss[max(0, ref.i-1)], ss[min(len(ss)-1, ref.i+1)]
		l := math.Hypot(b.e-a.e, b.n-a.n)
		if l == 0 {
			return 0, 0
		}
		return (b.e - a.e) / l, (b.n - a.n) / l
	}
	r := int(math.Ceil(reach / cell))
	cur := -1
	for i, k := range out {
		if i == 0 || k == cur || k < 0 || cur < 0 || chosen[i] {
			cur = k
			continue
		}
		at := route[i]
		if rings[[2]int{int(math.Floor(at.e / ringM)), int(math.Floor(at.n / ringM))}] {
			cur = k
			continue
		}
		byWay := map[int][]sample{}
		nearest := map[int]sampleRef{}
		dist := map[int]float64{}
		c := key(at.e, at.n)
		for gx := c[0] - r; gx <= c[0]+r; gx++ {
			for gy := c[1] - r; gy <= c[1]+r; gy++ {
				for _, ref := range grid[[2]int{gx, gy}] {
					s := ref.l.samples[ref.i]
					d := math.Hypot(s.e-at.e, s.n-at.n)
					if d > reach {
						continue
					}
					byWay[s.way] = append(byWay[s.way], s)
					if old, ok := dist[s.way]; !ok || d < old {
						dist[s.way], nearest[s.way] = d, ref
					}
				}
			}
		}
		// Only between roads side by side (a canal's banks, a dual road's
		// carriageways): a turn onto a crossing road, or a change through a
		// roundabout or over open ground (the course's connectors), is the
		// route's to make.
		dc, okc := dist[cur]
		_, okk := dist[k]
		if !okc || !okk || dc >= parallelM {
			cur = k
			continue
		}
		ae, an := dir(nearest[cur])
		be, bn := dir(nearest[k])
		if math.Abs(ae*be+an*bn) < parallelCosine {
			cur = k
			continue
		}
		touch := false
		for _, a := range byWay[cur] {
			for _, b := range byWay[k] {
				if math.Hypot(a.e-b.e, a.n-b.n) < a.hw+b.hw+1 {
					touch = true
					break
				}
			}
			if touch {
				break
			}
		}
		if touch {
			cur = k
			continue
		}
		out[i] = cur
	}
}

// against tells whether travel along a road's sample, at dot (the travel
// direction's dot product with the road line's direction), runs against
// its one-way.
func against(s sample, dot float64) bool {
	return s.oneway != 0 && dot*float64(s.oneway) < 0
}

// smoothRuns takes out hops between road and own way shorter than
// minRun: a brief own way between roads, or a brief road amid own way,
// gets its neighbours' value. Changes from one road to another stay, short
// as they may be (a junction's few metres on a cross road).
func smoothRuns(v []int, minRun int) {
	for range 8 {
		changed := false
		for i := 0; i < len(v); {
			j := i
			for j < len(v) && v[j] == v[i] {
				j++
			}
			before, after := -2, -2
			if i > 0 {
				before = v[i-1]
			}
			if j < len(v) {
				after = v[j]
			}
			hop := v[i] < 0 || (before < 0 && after < 0) // own way between roads, or road amid own way
			if j-i < minRun && (i > 0 || j < len(v)) && hop {
				fill := -1
				switch {
				case i > 0 && j < len(v) && v[i-1] == v[j]:
					fill = v[j]
				case i > 0:
					fill = v[i-1]
				default:
					fill = v[j]
				}
				if fill != v[i] {
					for k := i; k < j; k++ {
						v[k] = fill
					}
					changed = true
				}
			}
			i = j
		}
		if !changed {
			return
		}
	}
}

// joinHeights gives the roads meeting at a node one height there (their
// mean), each brought to it over joinM: no steps, nothing floating.
func joinHeights(ls []*roadLine) {
	type inc struct {
		l  *roadLine
		at float64
	}
	at := map[int64][]inc{}
	for _, l := range ls {
		lo, hi := l.samples[0].d, l.samples[len(l.samples)-1].d
		for node, ds := range l.nodes {
			for _, d := range ds {
				if d >= lo-0.5 && d <= hi+0.5 {
					at[node] = append(at[node], inc{l, d})
				}
			}
		}
	}
	// Node by node in a fixed order: a sample near two nodes moves for
	// both, the second from where the first left it, so map order had made
	// the heights differ from build to build.
	nodes := make([]int64, 0, len(at))
	for n := range at {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	for _, n := range nodes {
		is := at[n]
		if len(is) < 2 {
			continue
		}
		sum := 0.0
		for _, i := range is {
			sum += sampleAt(i.l, i.at).ele
		}
		target := sum / float64(len(is))
		for _, i := range is {
			here := sampleAt(i.l, i.at).ele
			for k := range i.l.samples {
				s := &i.l.samples[k]
				if d := math.Abs(s.d - i.at); d < joinM {
					s.ele += (target - here) * (1 - d/joinM)
				}
			}
		}
	}
}

// path is where the rider is every pathStep along the course: on the
// centre line of the drawn road under it, the nearest within snapM that
// runs the way the route does there (the road the course is matched to
// first), else on the course's own way; its height the road's. The points
// are smoothed over a few steps, so a change of road doesn't jump.
func (n *network) path() (d, x, y, z, w, onRoadF []float64) {
	c, line, route := n.c, n.line, n.route
	steps := max(1, int(math.Ceil(c.Distance/pathStep)))
	fineD := c.Distance / float64(len(route)-1)
	grid := map[[2]int][]sampleRef{}
	key := func(e, no float64) [2]int { return [2]int{int(math.Floor(e / snapM)), int(math.Floor(no / snapM))} }
	for _, l := range n.lines {
		if l.route {
			continue
		}
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	// Map matching: at each point the candidates are the projections onto
	// the drawn roads running the route's way within snapM, and the route's
	// own point. The rider's line is the sequence of candidates that costs
	// least (Viterbi): near the route, on the road the course is matched to,
	// and moving on steadily (each step about pathStep long, few changes of
	// road), so the rider neither hops between parallel roads nor stalls.
	type cand struct {
		x, y, ele, width float64
		line             *roadLine
		road             bool
		cost             float64
	}
	n0 := steps + 1
	own := make([][4]float64, n0)
	cands := make([][]cand, n0)
	for i := 0; i <= steps; i++ {
		at := c.Distance * float64(i) / float64(steps)
		ox, oy := line.at(at)
		re, rn, _ := line.heading(at)
		own[i] = [4]float64{ox, oy, n.ownHeight(at, ox, oy), 2 * route[max(0, min(int(math.Round(at/fineD)), len(route)-1))].hw}
		course := n.on(at)
		ownCost := 0.0
		if course >= 0 {
			ownCost = 20 // the course is on the roads here
		}
		cands[i] = append(cands[i], cand{x: ox, y: oy, ele: own[i][2], width: own[i][3], cost: ownCost})
		best := map[*roadLine]int{} // a line's best candidate so far
		k := key(ox, oy)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
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
					along := math.Abs(de*re+dn*rn) / math.Sqrt(l2)
					f := math.Max(0, math.Min(1, ((ox-a.e)*de+(oy-a.n)*dn)/l2))
					qx, qy := a.e+f*de, a.n+f*dn
					dd := math.Hypot(qx-ox, qy-oy)
					if dd > snapM {
						continue
					}
					// Past a road's end the projection sticks to the end: only
					// close by does it count (the rider would stall there).
					if (r.i == 0 && f == 0 || r.i+2 == len(ss) && f == 1) && dd > 3 {
						continue
					}
					// Roads at an angle count too, for less: the course turns
					// onto them at junctions, and roads jog.
					cost := dd*0.5 + 8*(1-along)
					if against(a, de*re+dn*rn) {
						cost += wrongWayCost // the other carriageway, for oncoming traffic
					}
					if course >= 0 && r.l.has(course) && along >= parallelCosine {
						// The road the course is matched to (any piece of it),
						// where it runs the route's way: more than any distance
						// within sidepathM weighs, so a road nearer the route's
						// line (an access road beside it) doesn't take the rider.
						cost -= sidepathM * 0.5
					}
					if course < 0 {
						cost += 20 // the course is on its own way here
					}
					c := cand{x: qx, y: qy, ele: a.ele + f*(b.ele-a.ele), width: 2 * (a.hw + f*(b.hw-a.hw)), line: r.l, road: true, cost: cost}
					if j, ok := best[r.l]; !ok {
						best[r.l] = len(cands[i])
						cands[i] = append(cands[i], c)
					} else if cost < cands[i][j].cost {
						cands[i][j] = c
					}
				}
			}
		}
	}
	// Viterbi.
	step := c.Distance / float64(steps)
	acc := make([][]float64, n0)
	from := make([][]int, n0)
	acc[0] = make([]float64, len(cands[0]))
	for j, cd := range cands[0] {
		acc[0][j] = cd.cost
	}
	for i := 1; i < n0; i++ {
		acc[i] = make([]float64, len(cands[i]))
		from[i] = make([]int, len(cands[i]))
		for j, cd := range cands[i] {
			bestCost, bestK := math.Inf(1), 0
			for k, pc := range cands[i-1] {
				gap := math.Hypot(cd.x-pc.x, cd.y-pc.y)
				t := math.Abs(gap-step) * 2
				if cd.line != pc.line || cd.road != pc.road {
					t += 4 // a change of road
				}
				if v := acc[i-1][k] + t; v < bestCost {
					bestCost, bestK = v, k
				}
			}
			acc[i][j], from[i][j] = bestCost+cd.cost, bestK
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
	road := make([][4]float64, n0)
	onRoad := make([]float64, n0)
	for i := range n0 {
		cd := cands[i][pick[i]]
		road[i] = [4]float64{cd.x, cd.y, cd.ele, cd.width}
		if cd.road {
			onRoad[i] = 1
		}
	}
	const blend = 6 // steps either way: 30 m
	for i := 0; i <= steps; i++ {
		var sum, wsum float64
		for j := max(0, i-blend); j <= min(steps, i+blend); j++ {
			wt := float64(blend + 1 - abs(j-i))
			sum += wt * onRoad[j]
			wsum += wt
		}
		f := sum / wsum
		at := c.Distance * float64(i) / float64(steps)
		mix := func(k int) float64 { return own[i][k] + f*(road[i][k]-own[i][k]) }
		d, x, y, z, w = append(d, at), append(x, mix(0)), append(y, mix(1)), append(z, mix(2)), append(w, mix(3))
		onRoadF = append(onRoadF, f)
	}
	// Not smoothed: the points lie on road centre lines (turns between
	// roads meet at a junction, on its patch), and smoothing cut the
	// corners onto the grass; renderers draw a spline through them.
	sx, sy := x, y
	return d, sx, sy, z, w, onRoadF
}

// ownHeight is the height of the course's own way at d (at e, n): the
// ground model there with map roads (the own ways meet them), else the
// road built along the route (its profile).
func (n *network) ownHeight(d, e, no float64) float64 {
	if n.hasWays && n.t.o.Elevation != nil {
		key := [2]int{int(math.Floor(e / n.t.o.ChunkM)), int(math.Floor(no / n.t.o.ChunkM))}
		_, near := n.t.lists(key)
		return n.t.base(e, no, near)
	}
	for _, l := range n.lines {
		if !l.route || len(l.samples) < 2 || d < l.samples[0].d || d > l.samples[len(l.samples)-1].d {
			continue
		}
		i := sort.Search(len(l.samples), func(i int) bool { return l.samples[i].d >= d })
		a, b := l.samples[max(0, i-1)], l.samples[min(i, len(l.samples)-1)]
		if b.d <= a.d {
			return a.ele
		}
		return a.ele + (d-a.d)/(b.d-a.d)*(b.ele-a.ele)
	}
	ele, _ := n.c.At(d)
	return ele + n.t.liftAt(d)
}

// snapM: the rider is put on the road the course runs on when the route
// lies this close to it (a cycle path beside it is further than the road's
// own wobble).
const snapM = sidepathM + 5

// wrongWayCost: a one-way road against the route's direction costs more
// than the bonus for the course's road and any offset within parallelM.
const wrongWayCost = sidepathM*0.5 + parallelM*0.5 + 5

// keepOnRoad makes sure the rider is on something drawn at every path
// point (x, y and the riding line's offset, lane): where the riding line
// puts them off it (a junction's corner, where the course leaves the
// roads) it narrows, and where the path itself is off, it moves to the
// nearest road within keepM. The changes are smoothed along the path.
func keepOnRoad(x, y, lane []float64, ls []*roadLine, ps []patch) {
	const cell, keepM = 10.0, 10.0
	grid := map[[2]int][]sampleRef{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cell)), int(math.Floor(n / cell))} }
	for _, l := range ls {
		for i := range l.samples {
			s := l.samples[i]
			k := key(s.e, s.n)
			grid[k] = append(grid[k], sampleRef{l, i})
		}
	}
	// nearest is the nearest point on a drawn road's centre line, and
	// whether e, n lies on that road.
	nearest := func(e, n float64) (qx, qy, dist float64, on bool) {
		dist = math.Inf(1)
		k := key(e, n)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					ss := r.l.samples
					if r.i+1 >= len(ss) {
						continue
					}
					a, b := ss[r.i], ss[r.i+1]
					de, dn := b.e-a.e, b.n-a.n
					f := 0.0
					if l2 := de*de + dn*dn; l2 > 0 {
						f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
					}
					px, py := a.e+f*de, a.n+f*dn
					if d := math.Hypot(e-px, n-py); d < dist {
						qx, qy, dist = px, py, d
						on = d < a.hw+f*(b.hw-a.hw)-0.3
					}
				}
			}
		}
		return qx, qy, dist, on
	}
	onSurface := func(e, n float64) bool {
		if _, _, _, on := nearest(e, n); on {
			return true
		}
		for _, p := range ps {
			if inPoly(p.poly, e, n) {
				return true
			}
		}
		return false
	}
	right := func(i int) (float64, float64) {
		a, b := max(0, i-1), min(len(x)-1, i+1)
		de, dn := x[b]-x[a], y[b]-y[a]
		l := math.Hypot(de, dn)
		if l == 0 {
			return 0, 0
		}
		return dn / l, -de / l
	}
	want := append([]float64(nil), lane...)
	moved := make([][2]float64, len(x))
	for i := range x {
		rx, ry := right(i)
		if onSurface(x[i]+rx*lane[i], y[i]+ry*lane[i]) {
			continue
		}
		if onSurface(x[i]+rx*lane[i]/2, y[i]+ry*lane[i]/2) {
			want[i] = lane[i] / 2
			continue
		}
		want[i] = 0
		if !onSurface(x[i], y[i]) {
			if qx, qy, d, _ := nearest(x[i], y[i]); d < keepM {
				moved[i] = [2]float64{qx - x[i], qy - y[i]}
			}
		}
	}
	// Smoothed: the rider eases over rather than jumps; but never wider
	// than what fits.
	const span = 3
	for i := range x {
		var sl, sx, sy, w float64
		for j := max(0, i-span); j <= min(len(x)-1, i+span); j++ {
			wt := float64(span + 1 - abs(j-i))
			sl += wt * want[j]
			sx += wt * moved[j][0]
			sy += wt * moved[j][1]
			w += wt
		}
		if math.Abs(sl/w) < math.Abs(lane[i]) || want[i] != lane[i] {
			lane[i] = math.Copysign(math.Min(math.Abs(sl/w), math.Abs(want[i])+math.Abs(sl/w-want[i])), lane[i])
		}
		x[i], y[i] = x[i]+sx/w, y[i]+sy/w
	}
}

// has tells whether the line is made of (among others) the map's way k.
func (l *roadLine) has(k int) bool {
	for _, w := range l.ways {
		if w == k {
			return true
		}
	}
	return false
}

// runsAlong tells whether most of a line lies within 10 m of the route,
// running its way (either direction).
func runsAlong(line [][2]float64, route []sample, g *routeGrid) bool {
	if len(line) < 2 {
		return false
	}
	var on, all float64
	for j := 0; j+1 < len(line); j++ {
		a, b := line[j], line[j+1]
		de, dn := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		mx, my := (a[0]+b[0])/2, (a[1]+b[1])/2
		hit := false
		g.within(mx, my, 10, func(i int) {
			if hit || i+1 >= len(route) {
				return
			}
			re, rn := route[i+1].e-route[i].e, route[i+1].n-route[i].n
			if rl := math.Hypot(re, rn); rl > 0 && math.Abs(de*re+dn*rn)/(l*rl) > parallelCosine {
				hit = true
			}
		})
		all += l
		if hit {
			on += l
		}
	}
	return all > 0 && on/all > 0.6
}

// classRank orders the road classes, the main roads highest.
var classRank = map[string]int{
	"service": 1, "living_street": 2, "residential": 3, "road": 3, "unclassified": 4,
	"tertiary": 5, "secondary": 6, "primary": 7, "trunk": 8,
}

// evenPace spreads the path's points evenly along it: each keeps its place
// on the line the points make, but its distance along it is averaged over
// a few steps, so where the matching took a longer or shorter way (onto a
// road, round a junction) the rider's pace doesn't jump. Heights, widths
// and the riding line go along; the start and finish stay put.
func evenPace(x, y, z, w, lane []float64, loop bool) {
	n := len(x)
	if n < 3 || loop {
		return // a loop's line closes on itself; it is matched alike all round
	}
	cum := make([]float64, n)
	for i := 1; i < n; i++ {
		cum[i] = cum[i-1] + math.Hypot(x[i]-x[i-1], y[i]-y[i-1])
	}
	const span = 4
	want := make([]float64, n)
	for i := range n {
		k := min(span, i, n-1-i)
		var s float64
		for j := i - k; j <= i+k; j++ {
			s += cum[j]
		}
		want[i] = s / float64(2*k+1)
	}
	at := func(v []float64, s float64) float64 {
		j := sort.SearchFloat64s(cum, s)
		if j <= 0 {
			return v[0]
		}
		if j >= n {
			return v[n-1]
		}
		f := (s - cum[j-1]) / math.Max(cum[j]-cum[j-1], 1e-9)
		return v[j-1] + f*(v[j]-v[j-1])
	}
	ox, oy, oz, ow, ol := append([]float64(nil), x...), append([]float64(nil), y...), append([]float64(nil), z...), append([]float64(nil), w...), append([]float64(nil), lane...)
	for i := range n {
		x[i], y[i], z[i], w[i], lane[i] = at(ox, want[i]), at(oy, want[i]), at(oz, want[i]), at(ow, want[i]), at(ol, want[i])
	}
}

// dropCovered takes out of the route's own ways (and out of roundabouts:
// rbs) (built where the route
// rides on no map road: cycle paths the map query leaves out) the
// stretches that would lie on a map road's carriageway. Drawn there as
// well, at their own heights and a little offset, the two surfaces showed
// through each other and the ground through both (the Posbank Loop at
// 5.4-5.8 km, a cycle path beside the Beekhuizenseweg); the rider rides
// the map road there. Runs shorter than coveredMinSamples either way are
// smoothed out, so a way isn't cut into slivers.
func dropCovered(ls []*roadLine, rbs []roundabout) []*roadLine {
	const cellM = 10.0
	type ref struct{ l, i int }
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range ls {
		if l.route {
			continue
		}
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], ref{li, i})
		}
	}
	var out []*roadLine
	for _, l := range ls {
		if !l.route {
			out = append(out, l)
			continue
		}
		ss := l.samples
		covered := make([]bool, len(ss))
		for i, s := range ss {
			for _, rb := range rbs {
				// Well inside: the roundabout's; near its edge the route's
				// way stays, to be cut there as any road reaching it.
				if math.Hypot(s.e-rb.c[0], s.n-rb.c[1]) < rb.outer()-1 {
					covered[i] = true
				}
			}
			k := key(s.e, s.n)
			for gx := k[0] - 1; gx <= k[0]+1 && !covered[i]; gx++ {
				for gy := k[1] - 1; gy <= k[1]+1 && !covered[i]; gy++ {
					for _, r := range grid[[2]int{gx, gy}] {
						ms := ls[r.l].samples
						a := ms[r.i]
						if r.i+1 >= len(ms) {
							continue
						}
						b := ms[r.i+1]
						de, dn := b.e-a.e, b.n-a.n
						f := 0.0
						if l2 := de*de + dn*dn; l2 > 0 {
							f = math.Max(0, math.Min(1, ((s.e-a.e)*de+(s.n-a.n)*dn)/l2))
						}
						if math.Hypot(s.e-a.e-f*de, s.n-a.n-f*dn) < a.hw+0.5*s.hw {
							covered[i] = true
							break
						}
					}
				}
			}
		}
		flipShortRuns(covered, coveredMinSamples)
		any := false
		for _, c := range covered {
			any = any || c
		}
		if !any {
			out = append(out, l)
			continue
		}
		for i := 0; i < len(ss); {
			j := i
			for j < len(ss) && covered[j] == covered[i] {
				j++
			}
			// A short stub between covered stretches (a few metres of cycle
			// path from a roundabout to the road beside it) would only make
			// slivers: the rider keeps to the roads there.
			stub := i > 0 && j < len(ss) && ss[j-1].d-ss[i].d < stubM
			if !covered[i] && j-i >= 2 && !stub {
				out = append(out, &roadLine{samples: append([]sample(nil), ss[i:j]...), smooth: l.smooth, route: true, ways: l.ways})
			}
			i = j
		}
	}
	return out
}

// stubM: a stretch of the route's own way shorter than this between
// stretches lying on roads isn't drawn.
const stubM = 30.0

// coveredMinSamples: runs of covered or uncovered route samples shorter
// than this (8 m) take their neighbours' side.
const coveredMinSamples = 4

// flipShortRuns flips runs of b shorter than min samples that lie between
// runs of the other value.
func flipShortRuns(b []bool, min int) {
	for i := 0; i < len(b); {
		j := i
		for j < len(b) && b[j] == b[i] {
			j++
		}
		if i > 0 && j < len(b) && j-i < min {
			for k := i; k < j; k++ {
				b[k] = !b[k]
			}
		}
		i = j
	}
}

// joinAlongside makes one surface of two map roads running alongside with
// less than mergeGapM between their edges (owner, 2026-10-09: a dual road
// whose carriageways the map draws apart, as the Beekhuizenseweg up to the
// Posbank, came out as two flat strips at their own heights, overlapping
// here and leaving a sliver of ground between them there). The later road
// takes the earlier one's height there, 1 cm under it (so the earlier one
// shows where they overlap), easing back to its own over joinEaseSamples;
// and where they don't touch it reaches over to the other's edge on that
// side, unless it has a sidewalk there. Further apart, the strip between
// them is verge. Junctions merge roads near their nodes the same way.
func joinAlongside(ls []*roadLine, cell float64) {
	const cellM = 10.0
	type ref struct{ l, i int }
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range ls {
		if l.route {
			continue
		}
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			grid[k] = append(grid[k], ref{li, i})
		}
	}
	dir := func(ss []sample, i int) (float64, float64) {
		a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
		l := math.Hypot(b.e-a.e, b.n-a.n)
		if l == 0 {
			return 0, 0
		}
		return (b.e - a.e) / l, (b.n - a.n) / l
	}
	for bi, l := range ls {
		if l.route {
			continue
		}
		ss := l.samples
		target := make([]float64, len(ss))
		joined := make([]bool, len(ss))
		for i := range ss {
			s := &ss[i]
			de, dn := dir(ss, i)
			k := key(s.e, s.n)
			bestGap, bestEle, bestSide, bestLine := math.Inf(1), 0.0, 0, -1
			for gx := k[0] - 1; gx <= k[0]+1; gx++ {
				for gy := k[1] - 1; gy <= k[1]+1; gy++ {
					for _, r := range grid[[2]int{gx, gy}] {
						if r.l == bi || r.i+1 >= len(ls[r.l].samples) {
							continue
						}
						os := ls[r.l].samples
						a, b := os[r.i], os[r.i+1]
						ae, an := b.e-a.e, b.n-a.n
						al := math.Hypot(ae, an)
						if al == 0 || math.Abs(ae/al*de+an/al*dn) < math.Cos(30*math.Pi/180) {
							continue // not alongside
						}
						f := math.Max(0, math.Min(1, ((s.e-a.e)*ae+(s.n-a.n)*an)/(al*al)))
						pe, pn := a.e+f*ae, a.n+f*an
						gap := math.Hypot(s.e-pe, s.n-pn) - s.hw - (a.hw + f*(b.hw-a.hw))
						if gap < bestGap {
							side := 1 // right of the way this road runs
							if de*(pn-s.n)-dn*(pe-s.e) > 0 {
								side = 0
							}
							bestGap, bestEle, bestSide, bestLine = gap, a.ele+f*(b.ele-a.ele), side, r.l
						}
					}
				}
			}
			if bestLine < 0 || bestGap >= mergeGapM {
				continue
			}
			if bestGap > 0 && !s.walk[bestSide] {
				s.wide[bestSide] = bestGap + 0.1 // just under the other's edge
				s.lane[bestSide] = false
			}
			if bestLine < bi {
				joined[i], target[i] = true, bestEle-0.01
			}
		}
		// The reach over to the other road tapers away where they part,
		// joinTaperM a sample, rather than stopping in a step where their
		// gap passes mergeGapM: the pavement runs out to a point.
		for side := range 2 {
			w := make([]float64, len(ss))
			for i := range ss {
				w[i] = ss[i].wide[side]
			}
			for i := range ss {
				if ss[i].walk[side] {
					continue
				}
				best := w[i]
				for k := 1; float64(k)*joinTaperM < mergeGapM+0.1; k++ {
					for _, j := range []int{i - k, i + k} {
						if j >= 0 && j < len(ss) {
							best = math.Max(best, w[j]-float64(k)*joinTaperM)
						}
					}
				}
				ss[i].wide[side] = best
			}
		}
		// Heights: the other road's where joined, easing back beside.
		delta := make([]float64, len(ss))
		for i := range ss {
			if joined[i] {
				delta[i] = target[i] - ss[i].ele
			}
		}
		eased := append([]float64(nil), delta...)
		for i := range ss {
			if joined[i] {
				continue
			}
			for k := 1; k <= joinEaseSamples; k++ {
				w := 1 - float64(k)/float64(joinEaseSamples+1)
				for _, j := range []int{i - k, i + k} {
					if j >= 0 && j < len(ss) && joined[j] && math.Abs(delta[j]*w) > math.Abs(eased[i]) {
						eased[i] = delta[j] * w
					}
				}
			}
		}
		for i := range ss {
			ss[i].ele += eased[i]
			if w := math.Max(ss[i].wide[0], ss[i].wide[1]); w > 0 {
				ss[i].edge = math.Max(ss[i].edge, ss[i].hw+w)
				ss[i].flat = ss[i].edge + flatPast(cell)
			}
		}
	}
}

// joinEaseSamples: a road joined to another eases back to its own height
// over this many samples (10 m) either side.
const joinEaseSamples = 5

// joinTaperM: where roads alongside part, the reach over to the other
// narrows this much a sample (2 m).
const joinTaperM = 0.3
