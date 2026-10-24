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
	clipM = 150.0 // roads are drawn this far from the route
	// motorwayClipM: motorways this far, out towards the land's edge
	// (CorridorM, 300 m by default), so one runs on out of the rider's
	// sight rather than ending on its embankment within it.
	motorwayClipM = 250.0
	minLineM      = 10.0 // pieces of road shorter than this are left out, unless they join roads at both ends
	heightSpanM   = 15.0 // a road's height from the ground model is smoothed over this far each way (roads are graded)
	// motorwaySpanM: a motorway's, this far (graded for 120 km/h, it
	// keeps its line over the land's swells rather than following them).
	motorwaySpanM = 100.0
	overlapM      = 10.0 // a stretch built along the route reaches this far onto the roads at its ends
	routeSinkM    = 0.01 // and lies this much below them, so they show on top
)

// roadLine is one road drawn in the world.
type roadLine struct {
	samples []sample // every fineStep or less along it (d: along the line)
	smooth  *smoothLine
	loop    bool // the course's own line on a loop (passes compare round the loop)
	route   bool // built along the route
	// motorway: a motorway's carriageway, never ridden, clipped further
	// from the route (motorwayClipM) and met by other roads only on
	// other levels (layers.go).
	motorway bool
	ways     []int // the map's ways it is made of
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

// trimLinesInLots takes the roads' samples inside a car park or a paved
// area (scenery.Parking) out of the network: the car park is the surface
// there, its aisles and a road's last metres inside it are no roads to
// draw or to match the course onto (owner, 2026-10-17: the Hohneck's
// summit car park, a loop ridden in it; matched onto the service way
// through it, the loop came out as a hairpin on that line). A line is
// cut into its pieces outside the lots; pieces shorter than minLineM go.
// Not inside an apron (a paved junction area): its roads run on over it.
func trimLinesInLots(ls []*roadLine, lots []scenery.Parking) []*roadLine {
	if len(lots) == 0 {
		return ls
	}
	inLot := func(e, n float64) bool {
		for _, lot := range lots {
			if !lot.Apron && inPoly(lot.Outline, e, n) {
				return true
			}
		}
		return false
	}
	var out []*roadLine
	for _, l := range ls {
		if l.route {
			out = append(out, l)
			continue
		}
		var piece []sample
		cutBefore := false // the piece starts at a lot
		flush := func(cutAfter bool) {
			if len(piece) >= 2 && (piece[len(piece)-1].d-piece[0].d >= minLineM || len(piece) == len(l.samples)) {
				c := *l
				c.samples = piece
				// Its ends at a lot (owner, 2026-10-18, the Hohneck's
				// summit car park at 17.52 km: the road stood 6-9 cm over
				// the car park where it entered it, its end open, and from
				// the car park one looked under the road).
				c.samples[0].lotEnd = cutBefore
				c.samples[len(c.samples)-1].lotEnd = cutAfter
				out = append(out, &c)
			}
			piece = nil
		}
		for _, s := range l.samples {
			if inLot(s.e, s.n) {
				if len(piece) > 0 {
					flush(true)
				}
				cutBefore = true
				continue
			}
			piece = append(piece, s)
		}
		flush(false)
	}
	return out
}

// drawable says whether way k is drawn: driveways, and tunnels the route
// doesn't ride are left out. Bridges are drawn, on decks (bridges.go);
// motorways too, though never ridden or met on the level: where one
// crosses the route, one lies over the other (layers.go).
func drawable(w scenery.Way, ridden bool) bool {
	if w.Driveway || w.Roundabout {
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
		// A tunnel's piece goes on from the road it leads under, whatever
		// either is named ("Dike" and "Dike tunnel"): apart, the tunnel was
		// a line of its own with no portals.
		for _, o := range ends[n] {
			if o != k && (sameRoad(ws[o], ws[k]) || ws[o].Tunnel != ws[k].Tunnel) {
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
// with their surface, sidewalks and lanes, clipped where near says (for
// motorways, which near is told: they reach further, motorwayClipM).
func networkLines(ws []scenery.Way, ridden map[int]bool, near func(e, n float64, motorway bool) bool) []*roadLine {
	// How many drawn ways pass each node: a short road with others at both
	// ends links two junctions and is drawn (2026-10-09: an 8.7 m piece of
	// Rhenen's Herenstraat between two junctions was left out, and the
	// rider crossed the grass under it, 1.62 km into a course).
	touch := map[int64]int{}
	for k, w := range ws {
		// A roundabout's ring counts: it isn't drawn as a road (the
		// roundabout draws itself), but a short link onto it links (owner,
		// 2026-10-11: a residential street's two short links onto the
		// roundabout on the polder ride, 10.2 km, were left out, and the road
		// stopped short of the ring with grass between).
		if len(w.Line) >= 2 && len(w.Nodes) == len(w.Line) && (drawable(w, ridden[k]) || w.Roundabout) {
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
		motorway := ws[ch[0].way].Motorway
		var run []sample
		flush := func() {
			if len(run) >= 2 && (run[len(run)-1].d-run[0].d >= minLineM || link) {
				out = append(out, &roadLine{samples: run, smooth: sl, ways: wayIDs, nodes: nodes, motorway: motorway})
			}
			run = nil
		}
		for _, at := range sl.stations(fineStep) {
			e, n := sl.at(at)
			if !near(e, n, motorway) {
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
				// The squared distance rules most out cheaply; the
				// hypotenuse decides, as before, so nothing moves.
				de, dn := g.fine[i].e-e, g.fine[i].n-n
				if de*de+dn*dn <= r*r*1.001 && math.Hypot(de, dn) <= r {
					f(i)
				}
			}
		}
	}
}

// heightsStraight is heights with the ways straight sets spanning straight
// from end to end (heights: bridges; the graph builder: tunnels too, which
// keep low under what lies over them).
func heightsStraight(l *roadLine, t *terrain, ws []scenery.Way, straight func(scenery.Way) bool, byDistance bool) {
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
	bridge := func(i int) bool { return ss[i].way >= 0 && ss[i].way < len(ws) && straight(ws[ss[i].way]) }
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
	if byDistance {
		span := heightSpanM
		if l.motorway {
			span = motorwaySpanM
		}
		smoothByDistance(ss, base, span)
		return
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

// smoothByDistance sets each sample's height to base smoothed over
// span each way along the road, a triangle in metres, each sample
// weighed by the stretch it stands for. Samples aren't evenly spaced
// (finishRoad adds them through bends and at cuts): counted in samples,
// the smoothing shrank to a few metres where they crowd, and a short lane
// climbing beside the Rue du Hohneck (owner, 2026-10-16, 1.34 km of the
// Route des Américains) kept the ground model's wall as a 40 % ramp.
func smoothByDistance(ss []sample, base []float64, span float64) {
	n := len(ss)
	w := make([]float64, n) // the stretch each sample stands for
	for i := range ss {
		lo, hi := ss[max(0, i-1)].d, ss[min(n-1, i+1)].d
		w[i] = math.Max((hi-lo)/2, 1e-3)
	}
	r := span + fineStep
	a := 0
	for i := range ss {
		for a < i && ss[i].d-ss[a].d >= r {
			a++
		}
		var b, bw float64
		for k := a; k < n && ss[k].d-ss[i].d < r; k++ {
			wt := w[k] * (r - math.Abs(ss[k].d-ss[i].d))
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
func networkPatches(ls []*roadLine, ws []scenery.Way, levelM float64) []patch {
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
	// Where along each line the junctions are.
	junctionsOn := map[*roadLine][]float64{}
	for _, node := range nodes {
		is := at[node]
		count := 0
		for _, i := range is {
			count += 1 + boolInt(i.middle)
		}
		if count < 2 || len(is) < 2 {
			continue
		}
		for _, i := range is {
			junctionsOn[i.l] = append(junctionsOn[i.l], i.at)
		}
	}
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
			// Along roads side by side the junction reaches on, but not up
			// to the next junction on the road: that one is a junction of
			// its own (owner, 2026-10-11: a carriageway split's junction
			// reached up to the side street crossing on the polder ride, 10.4 km,
			// merged with it, and its outline paved a triangle of verge).
			stop := func(dir float64) float64 {
				lim := mergeReachM
				for _, n := range junctionsOn[i.l] {
					if d := (n - i.at) * dir; d > 1 && d-(widest+apronAlongM) < lim {
						lim = math.Max(widest+apronAlongM, d-(widest+apronAlongM)-fineStep)
					}
				}
				return lim
			}
			lo := math.Min(overlapReach(is[k].l, i.at, -1, widest+apronAlongM, is, k, levelM), stop(-1))
			hi := math.Min(overlapReach(is[k].l, i.at, 1, widest+apronAlongM, is, k, levelM), stop(1))
			first, last := i.l.samples[0].d, i.l.samples[len(i.l.samples)-1].d
			p.spans = append(p.spans, patchSpan{i.l, math.Max(first, i.at-lo), math.Min(last, i.at+hi), i.at, i.at, i.at})
		}
		s0 := sampleAt(is[0].l, is[0].at)
		p.at, p.node = s0.d, [2]float64{s0.e, s0.n}
		p.nodes = [][2]float64{p.node}
		out = append(out, p)
	}
	ps := mergePatchesApart(out, true)
	// abreast reaches spans out and merging keeps junctions a street apart
	// as two by trimming them back: they can undo each other, so a few
	// rounds at most.
	for round, changed := 0, true; changed && round < abreastRounds; round++ {
		changed = false
		for k := range ps {
			changed = abreast(&ps[k], levelM) || changed
		}
		if changed {
			ps = mergePatchesApart(ps, true)
		}
	}
	return shapePatches(ps)
}

// abreastRounds bounds the rounds of abreast and merging in networkPatches.
const abreastRounds = 8

// abreast cuts roads of one junction that run alongside each other abreast
// (owner, 2026-10-10: at 3.0 km on the Velp loop a service road's junction
// reached 15 m further along it than the main road beside it was cut, and
// the strip between them, under mergeGapM wide, showed as a wedge of
// ground between two road surfaces): where one road's cut (with its reach
// over to the other, joinAlongside's) lies within mergeGapM of another's
// edge, that road's span reaches out to it, so the
// junction's outline spans the strip between them. With levelM over 0,
// not to a road more than levelM above or below (a level of its own,
// behind a wall). True if it changed p.
func abreast(p *patch, levelM float64) bool {
	const reachM = 30.0 // a span grows at most this far
	changed := false
	for i := range p.spans {
		a := p.spans[i]
		for _, end := range []float64{a.from, a.to} {
			e := sampleBetween(a.l, end)
			de, dn, _ := a.l.smooth.heading(end)
			for j := range p.spans {
				b := &p.spans[j]
				if b.l == a.l {
					continue
				}
				d, off, hw := lineNearest(b.l, e.e, e.n)
				first, last := b.l.samples[0].d, b.l.samples[len(b.l.samples)-1].d
				if off >= e.hw+math.Max(e.wide[0], e.wide[1])+hw+mergeGapM || d <= first+0.5 || d >= last-0.5 {
					continue
				}
				if levelM > 0 && math.Abs(sampleBetween(b.l, d).ele-e.ele) >= levelM {
					continue
				}
				be, bn, _ := b.l.smooth.heading(d)
				if math.Abs(de*be+dn*bn) < parallelCosine {
					continue // across it, not alongside
				}
				switch {
				case d < b.from-0.5 && b.from-d <= reachM:
					b.from, changed = d, true
				case d > b.to+0.5 && d-b.to <= reachM:
					b.to, changed = d, true
				}
			}
		}
	}
	return changed
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
// slip road), so where they overlap they are one surface. With levelM
// over 0, only on while the two are within levelM in height: a road
// beside another a step higher is a level of its own, behind a wall
// (the style's junctionLevelM).
func overlapReach(l *roadLine, at, dir, base float64, is []patchInc, self int, levelM float64) float64 {
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
				if math.Abs(t.d-o.at) <= mergeReachM+20 && math.Hypot(t.e-s.e, t.n-s.n) < s.hw+t.hw+mergeGapM && (levelM <= 0 || math.Abs(t.ele-s.ele) < levelM) {
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

	o         Options
	stretches []scenery.RoadStretch
	// underpasses are the stretches (along the course) where its own way
	// goes under a bridge, ramps included: drawn gUnderHalfM wide at least.
	underpasses [][2]float64
}

type sampleRef struct {
	l *roadLine
	i int
}

// ownWays are the course's own ways: along the path wherever it isn't
// wholly on a road (f < 1), a few steps more either side so they meet the
// roads, just under them. On a loop, the way across its start and finish
// is one, from the last stretch off the roads on round into the first
// (2026-10-22: as two, ending at the line, grass showed between them).
func (n *network) ownWays(d, x, y, z, f []float64, loop bool) []*roadLine {
	var out []*roadLine
	const reach = 3 // path steps either side
	first, last := 0, len(d)
	length := 0.0 // a loop's, where its own way runs on past the finish
	if m := len(d); loop && m > 2 && f[0] <= 0.999 && f[m-1] <= 0.999 {
		head := 0
		for head < m && f[head] <= 0.999 {
			head++
		}
		if head < m { // somewhere on the roads
			// The path again from its start (its first point is its last)
			// to past the first stretch, on from the finish.
			end := min(m-1, head-1+reach)
			wrap := func(v []float64, shift float64) []float64 {
				w := append([]float64(nil), v...)
				for k := 1; k <= end; k++ {
					w = append(w, v[k]+shift)
				}
				return w
			}
			d, x, y, z, f = wrap(d, d[m-1]), wrap(x, 0), wrap(y, 0), wrap(z, 0), wrap(f, 0)
			first, last, length = head, m, d[m-1]
		}
	}
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
	for i := first; i < last; {
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
		// Past a loop's finish, the roads from its start again.
		var past []int
		for k := range ss {
			if length > 0 && ss[k].d > length {
				ss[k].d -= length
				past = append(past, k)
			}
		}
		applyRoads(ss, n.stretches, n.o.RoadWidthM, n.o.CellM)
		for _, k := range past {
			ss[k].d += length
		}
		for k := range ss {
			for _, u := range n.underpasses {
				if ss[k].d >= u[0] && ss[k].d <= u[1] {
					ss[k].hw = math.Max(ss[k].hw, gUnderHalfM)
					ss[k].edge = math.Max(ss[k].edge, ss[k].hw)
					ss[k].underpass = true
				}
			}
		}
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

// wrongWayCost: a one-way road against the route's direction costs more
// than the bonus for the course's road and any offset within parallelM.
const wrongWayCost = sidepathM*0.5 + parallelM*0.5 + 5

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

// dropCoveredStubs is dropCovered with stubs shorter than stub dropped (0:
// none), and with keepEnds the covered stretches at a way's ends kept: the
// graph builder keeps both, as the course rides them, and its own ways
// meet the roads they join there (dropped, they stopped a metre short of
// the road with grass between). over, when given, says that map sample a
// passes over the route's sample s (a bridge well above it) and so
// doesn't cover it.
func dropCoveredStubs(ls []*roadLine, rbs []roundabout, stub float64, keepEnds bool, over func(a, s sample) bool) []*roadLine {
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
						slack := 0.5 * s.hw
						if keepEnds && !alongside(ss, i, de, dn) {
							slack = 0
						}
						if math.Hypot(s.e-a.e-f*de, s.n-a.n-f*dn) < a.hw+slack && (over == nil || !over(a, s)) {
							covered[i] = true
							break
						}
					}
				}
			}
		}
		if keepEnds {
			// A short stretch between two roads the way crosses (the
			// median of a dual road) keeps its way: handed to the roads, it
			// was grass under the rider (the Dunes course at 15.27 km).
			flipShortRunsOf(covered, coveredMinSamples, true)
		} else {
			flipShortRuns(covered, coveredMinSamples)
		}
		if keepEnds {
			for i := 0; i < len(covered) && covered[i]; i++ {
				covered[i] = false
			}
			for i := len(covered) - 1; i >= 0 && covered[i]; i-- {
				covered[i] = false
			}
		}
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
			isStub := i > 0 && j < len(ss) && ss[j-1].d-ss[i].d < stub
			if !covered[i] && j-i >= 2 && !isStub {
				out = append(out, &roadLine{samples: append([]sample(nil), ss[i:j]...), smooth: l.smooth, route: true, ways: l.ways})
			}
			i = j
		}
	}
	return out
}

// alongside tells whether the map road's direction de, dn runs along the
// route's own way at sample i (within about 45°) rather than across it.
func alongside(ss []sample, i int, de, dn float64) bool {
	a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
	re, rn := b.e-a.e, b.n-a.n
	l := math.Hypot(re, rn) * math.Hypot(de, dn)
	return l == 0 || math.Abs(re*de+rn*dn)/l > math.Sqrt2/2
}

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

// flipShortRunsOf flips the runs of value v shorter than min samples that
// lie between runs of the other value, leaving the other value's alone.
func flipShortRunsOf(b []bool, min int, v bool) {
	for i := 0; i < len(b); {
		j := i
		for j < len(b) && b[j] == b[i] {
			j++
		}
		if b[i] == v && i > 0 && j < len(b) && j-i < min {
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
// With levelM over 0, roads more than levelM apart in height stay two
// (a lane a step above the road beside it, behind a wall: the style's
// junctionLevelM).
func joinAlongside(ls []*roadLine, cell, levelM float64) {
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
		// apart: a road within mergeGapM on that side, but a level of its
		// own: the join's taper stops short of it (the taper had reached
		// partway over to a road a step higher, a sliver with nothing drawn
		// between the two; the terrace or the bank between them is the
		// land's, terrace.go and bankBetween).
		apart := make([][2]bool, len(ss))
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
						side := 1 // right of the way this road runs
						if de*(pn-s.n)-dn*(pe-s.e) > 0 {
							side = 0
						}
						if levelM > 0 && math.Abs(a.ele+f*(b.ele-a.ele)-s.ele) > levelM {
							if gap < mergeGapM {
								apart[i][side] = true
							}
							continue // a level of its own
						}
						if gap < bestGap {
							bestGap, bestEle, bestSide, bestLine = gap, a.ele+f*(b.ele-a.ele), side, r.l
						}
					}
				}
			}
			if bestLine < 0 || bestGap >= mergeGapM || apart[i][bestSide] {
				continue // (apart: a level candidate a sample behind, found across the diagonal)
			}
			if !s.walk[bestSide] {
				s.butt[bestSide] = true
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
				if ss[i].walk[side] || apart[i][side] {
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

// deepSkirtMaxM: an edge's skirt goes down at most this far to a lower
// road beside it (higher is a retaining wall's); never under a deck.
const deepSkirtMaxM = 3.0

// deepSkirts sets each road edge's skirt down past a lower road lying
// just beyond it (within 1 m of the edge): two roads a level apart, side
// by side, met edge to edge with only the higher one's 0.3 m skirt
// between them, and one looked into the gap under it (owner,
// 2026-10-18, 19.05 km of the Hohneck loop: the Chemin de Schmargult
// dropping away beside the Route des Crêtes, a slit under the shoulder).
func deepSkirts(ls []*roadLine) {
	type ref struct{ l, i int }
	const cellM = 10.0
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range ls {
		for i := range l.samples {
			grid[key(l.samples[i].e, l.samples[i].n)] = append(grid[key(l.samples[i].e, l.samples[i].n)], ref{li, i})
		}
	}
	// lowest is the lowest road surface (other than line self) at e, n,
	// and whether there is one.
	lowest := func(self int, e, n float64) (float64, bool) {
		k := key(e, n)
		best, found := math.Inf(1), false
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					ss := ls[r.l].samples
					// A road in a tunnel is under the ground: an edge over it
					// skirted down into the tube (owner, 2026-10-22: the
					// motorway over the figure 8's tunnel "supported by flaps
					// of dark material").
					if r.l == self || r.i+1 >= len(ss) || ss[r.i].again || inTunnel(ss, r.i) {
						continue
					}
					a, b := ss[r.i], ss[r.i+1]
					de, dn := b.e-a.e, b.n-a.n
					l2 := de*de + dn*dn
					if l2 == 0 {
						continue
					}
					f := math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
					if math.Hypot(e-a.e-f*de, n-a.n-f*dn) <= a.hw+f*(b.hw-a.hw)+math.Max(a.wide[0], a.wide[1]) {
						if h := a.ele + f*(b.ele-a.ele); h < best {
							best, found = h, true
						}
					}
				}
			}
		}
		return best, found
	}
	for li, l := range ls {
		ss := l.samples
		for i := range ss {
			s := &ss[i]
			if s.aloft || s.again {
				continue // a deck stays open beneath (the N348 over the cycle tunnel, 26.78 km of the Posbank Loop)
			}
			a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
			de, dn := b.e-a.e, b.n-a.n
			d := math.Hypot(de, dn)
			if d == 0 {
				continue
			}
			rx, rn := dn/d, -de/d
			for side, sign := range []float64{-1, 1} {
				if s.walk[side] || s.butt[side] {
					continue
				}
				edge := s.hw + s.wide[side]
				for _, off := range []float64{0.3, 1} {
					e, n := s.e+sign*rx*(edge+off), s.n+sign*rn*(edge+off)
					if h, ok := lowest(li, e, n); ok && s.ele-h > skirtM-0.1 {
						s.skirt[side] = math.Max(s.skirt[side], math.Min(deepSkirtMaxM, s.ele-h+0.15))
					}
				}
			}
		}
	}
}
