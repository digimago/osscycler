package world

import (
	"container/heap"
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/scenery"
)

// The graph builder's network (graph.go): the drawn roads (roadLines, as
// the classic builder draws them) joined where they share a map node, and
// across each roundabout from every road that meets it to every other
// (straight over a flat one, round the ring with the traffic on one with
// an island). Distances between nodes are along the roads, so the course
// can only change roads where they meet.

// gnet is the road network as a graph.
type gnet struct {
	lines  []*roadLine
	stops  [][]gstop            // per line, the nodes along it by distance
	adj    map[int64][]gedge    // node → the ways on from it
	pos    map[int64][2]float64 // node → where it is (east, north)
	ele    map[int64]float64    // node → its height (the roads' there; after heights)
	from   map[int64]*gDijkstra // shortest ways from a node, within gReachM, made on first use
	lineOf map[*roadLine]int
	// virtual: lines from this index on are ways across roundabouts
	// (linkLine), part of the network but not drawn.
	virtual int
}

// gstop is a node on a line: where along it, and which.
type gstop struct {
	d    float64
	node int64
}

// gedge is a way on from a node: along line, from its stop at a to the
// one at b.
type gedge struct {
	to     int64
	length float64
	line   int
	a, b   float64
}

// glink is the way across a roundabout from one road's end to another's.
type glink struct {
	pts  [][2]float64 // from the first road's end to the other's
	flat bool         // crossed straight (no island)
}

// gReachM: the network is searched this far from a node (more than the
// longest way between two path points, round a roundabout included).
const gReachM = 120.0

// newGnet joins lines where they share nodes; ws are the map's ways (for
// the roundabouts' rings), rbs the roundabouts drawn.
func newGnet(lines []*roadLine, ws []scenery.Way, rbs []roundabout) *gnet {
	g := &gnet{lines: lines[:len(lines):len(lines)], stops: make([][]gstop, len(lines)), adj: map[int64][]gedge{},
		pos: map[int64][2]float64{}, ele: map[int64]float64{}, from: map[int64]*gDijkstra{}, lineOf: map[*roadLine]int{}}
	rings := ringNodes(ws)
	onRing := map[int64]int{}
	for k, r := range rings {
		for n := range r.nodes {
			onRing[n] = k
		}
	}
	count := map[int64]int{}
	for k, l := range lines {
		g.lineOf[l] = k
		if len(l.samples) < 2 {
			continue
		}
		lo, hi := l.samples[0].d, l.samples[len(l.samples)-1].d
		for node, ds := range l.nodes {
			for _, d := range ds {
				if d >= lo-0.5 && d <= hi+0.5 {
					count[node]++
				}
			}
		}
	}
	for k, l := range lines {
		if len(l.samples) < 2 {
			continue
		}
		lo, hi := l.samples[0].d, l.samples[len(l.samples)-1].d
		for node, ds := range l.nodes {
			_, ring := onRing[node]
			if count[node] < 2 && !ring {
				continue
			}
			for _, d := range ds {
				if d >= lo-0.5 && d <= hi+0.5 {
					d = math.Max(lo, math.Min(hi, d))
					g.stops[k] = append(g.stops[k], gstop{d, node})
					s := sampleBetween(l, d)
					g.pos[node] = [2]float64{s.e, s.n}
				}
			}
		}
		st := g.stops[k]
		sort.Slice(st, func(i, j int) bool { return st[i].d < st[j].d || st[i].d == st[j].d && st[i].node < st[j].node })
		for i := 0; i+1 < len(st); i++ {
			a, b := st[i], st[i+1]
			if a.node == b.node {
				continue
			}
			g.adj[a.node] = append(g.adj[a.node], gedge{to: b.node, length: b.d - a.d, line: k, a: a.d, b: b.d})
			g.adj[b.node] = append(g.adj[b.node], gedge{to: a.node, length: b.d - a.d, line: k, a: b.d, b: a.d})
		}
	}
	// Across each roundabout, between the roads that end on its ring: a
	// line of the network not drawn (the roundabout draws itself), so the
	// course can be on it step by step.
	g.virtual = len(g.lines)
	for _, r := range rings {
		var ends []int64
		for n := range r.nodes {
			if _, ok := g.pos[n]; ok {
				ends = append(ends, n)
			}
		}
		sort.Slice(ends, func(i, j int) bool { return ends[i] < ends[j] })
		var rb *roundabout
		for i := range rbs {
			if math.Hypot(rbs[i].c[0]-r.c[0], rbs[i].c[1]-r.c[1]) < 1 {
				rb = &rbs[i]
			}
		}
		hw := ringHalfM
		if rb != nil {
			hw = rb.hw
		}
		for _, x := range ends {
			for _, y := range ends {
				lk := ringLink(g.pos[x], g.pos[y], r.c, rb)
				if x == y || lk.flat && x > y {
					continue // a flat one's crossing goes both ways: one line
				}
				vl := linkLine(lk.pts, x, y, hw, !lk.flat)
				k := len(g.lines)
				g.lines = append(g.lines, vl)
				g.lineOf[vl] = k
				L := vl.samples[len(vl.samples)-1].d
				g.stops = append(g.stops, []gstop{{0, x}, {L, y}})
				g.adj[x] = append(g.adj[x], gedge{to: y, length: L, line: k, a: 0, b: L})
				if lk.flat {
					g.adj[y] = append(g.adj[y], gedge{to: x, length: L, line: k, a: L, b: 0})
				}
			}
		}
	}
	for n := range g.adj {
		es := g.adj[n]
		sort.Slice(es, func(i, j int) bool {
			if es[i].to != es[j].to {
				return es[i].to < es[j].to
			}
			if es[i].line != es[j].line {
				return es[i].line < es[j].line
			}
			return es[i].a < es[j].a
		})
	}
	return g
}

// gring is a roundabout's ring in the map: its nodes and middle.
type gring struct {
	nodes map[int64]bool
	c     [2]float64
}

// ringNodes groups the map's roundabout ways into rings, as
// findRoundabouts does.
func ringNodes(ws []scenery.Way) []gring {
	parent := map[int]int{}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	byNode := map[int64]int{}
	for k, w := range ws {
		if !w.Roundabout || len(w.Line) < 2 {
			continue
		}
		parent[k] = k
		for _, nd := range w.Nodes {
			if o, ok := byNode[nd]; ok {
				parent[find(k)] = find(o)
			} else {
				byNode[nd] = k
			}
		}
	}
	groups := map[int][]int{}
	var order []int
	for k := range ws {
		if _, ok := parent[k]; !ok {
			continue
		}
		r := find(k)
		if groups[r] == nil {
			order = append(order, r)
		}
		groups[r] = append(groups[r], k)
	}
	var out []gring
	for _, root := range order {
		r := gring{nodes: map[int64]bool{}}
		var pts [][2]float64
		for _, k := range groups[root] {
			pts = append(pts, ws[k].Line...)
			for _, n := range ws[k].Nodes {
				r.nodes[n] = true
			}
		}
		for _, p := range pts {
			r.c[0], r.c[1] = r.c[0]+p[0]/float64(len(pts)), r.c[1]+p[1]/float64(len(pts))
		}
		out = append(out, r)
	}
	return out
}

// ringLink is the way from a to b across a roundabout with its middle at
// c: straight over a flat one (rb nil, or without an island), else round
// its ring the way traffic goes, at the ring's middle line.
func ringLink(a, b, c [2]float64, rb *roundabout) *glink {
	if rb == nil || !rb.island {
		return &glink{pts: [][2]float64{a, b}, flat: true}
	}
	r := rb.r + rb.grow
	a0 := math.Atan2(a[1]-c[1], a[0]-c[0])
	a1 := math.Atan2(b[1]-c[1], b[0]-c[0])
	sweep := a1 - a0
	if rb.keepRight { // anticlockwise seen from above: the angle grows
		for sweep <= 0 {
			sweep += 2 * math.Pi
		}
	} else {
		for sweep >= 0 {
			sweep -= 2 * math.Pi
		}
	}
	pts := [][2]float64{a}
	n := max(2, int(math.Ceil(math.Abs(sweep)*r/2)))
	for k := 0; k <= n; k++ {
		ang := a0 + sweep*float64(k)/float64(n)
		pts = append(pts, [2]float64{c[0] + r*math.Cos(ang), c[1] + r*math.Sin(ang)})
	}
	return &glink{pts: append(pts, b)}
}

// linkLine is the way across a roundabout from node a to node b along
// pts, as a line: sampled every metre, half width hw, one way when the
// traffic goes round (oneWay).
func linkLine(pts [][2]float64, a, b int64, hw float64, oneWay bool) *roadLine {
	var d, x, y []float64
	for i, p := range pts {
		if i > 0 {
			step := math.Hypot(p[0]-pts[i-1][0], p[1]-pts[i-1][1])
			if step < 1e-6 {
				continue
			}
			d = append(d, d[len(d)-1]+step)
		} else {
			d = append(d, 0)
		}
		x, y = append(x, p[0]), append(y, p[1])
	}
	if len(d) < 2 {
		d, x, y = append(d, d[0]+0.01), append(x, x[0]), append(y, y[0]+0.01)
	}
	sl := newSmoothLine(d, x, y, false, nil)
	L := sl.length()
	l := &roadLine{smooth: sl, nodes: map[int64][]float64{a: {0}, b: {L}}}
	n := max(1, int(math.Ceil(L)))
	for i := 0; i <= n; i++ {
		at := L * float64(i) / float64(n)
		e, no := sl.at(at)
		s := sample{d: at, e: e, n: no, hw: hw, edge: hw, flat: hw, way: -1}
		if oneWay {
			s.oneway = 1
		}
		l.samples = append(l.samples, s)
	}
	return l
}

// gDijkstra is the shortest ways from one node to those within gReachM.
type gDijkstra struct {
	dist map[int64]float64
	prev map[int64]gedgeFrom
}

// gedgeFrom is how a node was reached: from which node, by which way.
type gedgeFrom struct {
	from int64
	e    gedge
}

type gItem struct {
	node int64
	d    float64
}
type gHeap []gItem

func (h gHeap) Len() int { return len(h) }
func (h gHeap) Less(i, j int) bool {
	return h[i].d < h[j].d || h[i].d == h[j].d && h[i].node < h[j].node
}
func (h gHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *gHeap) Push(x any)   { *h = append(*h, x.(gItem)) }
func (h *gHeap) Pop() any {
	o := *h
	x := o[len(o)-1]
	*h = o[:len(o)-1]
	return x
}

// reach is the shortest ways from node n.
func (g *gnet) reach(n int64) *gDijkstra {
	if r, ok := g.from[n]; ok {
		return r
	}
	r := &gDijkstra{dist: map[int64]float64{n: 0}, prev: map[int64]gedgeFrom{}}
	h := &gHeap{{n, 0}}
	for h.Len() > 0 {
		it := heap.Pop(h).(gItem)
		if it.d > r.dist[it.node] {
			continue
		}
		for _, e := range g.adj[it.node] {
			d := it.d + e.length
			if d > gReachM {
				continue
			}
			if old, ok := r.dist[e.to]; !ok || d < old {
				r.dist[e.to] = d
				r.prev[e.to] = gedgeFrom{it.node, e}
				heap.Push(h, gItem{e.to, d})
			}
		}
	}
	g.from[n] = r
	return r
}

// ahead is the first node on line k from t onwards in direction dir (+1
// along the line, -1 back), and where it lies; false if none.
func (g *gnet) ahead(k int, t float64, dir int8) (gstop, bool) {
	st := g.stops[k]
	if dir > 0 {
		i := sort.Search(len(st), func(i int) bool { return st[i].d >= t })
		if i < len(st) {
			return st[i], true
		}
		return gstop{}, false
	}
	i := sort.Search(len(st), func(i int) bool { return st[i].d > t })
	if i > 0 {
		return st[i-1], true
	}
	return gstop{}, false
}

// behind is the last node on line k before t, coming along it in
// direction dir.
func (g *gnet) behind(k int, t float64, dir int8) (gstop, bool) {
	return g.ahead(k, t, -dir)
}

// between is the shortest way along the network from t1 on line k1 going
// dir1 to t2 on line k2 going dir2: its length and the ways it takes
// (empty when it stays on one line); false when there is none within
// reach.
func (g *gnet) between(k1 int, t1 float64, dir1 int8, k2 int, t2 float64, dir2 int8) (float64, gpath, bool) {
	if k1 == k2 && dir1 == dir2 && float64(dir1)*(t2-t1) >= -gJitterM {
		return math.Max(0, float64(dir1)*(t2-t1)), gpath{}, true
	}
	out, ok := g.ahead(k1, t1, dir1)
	if !ok {
		return 0, gpath{}, false
	}
	in, ok := g.behind(k2, t2, dir2)
	if !ok {
		return 0, gpath{}, false
	}
	r := g.reach(out.node)
	d, ok := r.dist[in.node]
	if !ok {
		return 0, gpath{}, false
	}
	p := gpath{out: out, in: in}
	for n := in.node; n != out.node; {
		pr := r.prev[n]
		p.edges = append(p.edges, pr.e)
		n = pr.from
	}
	for i, j := 0, len(p.edges)-1; i < j; i, j = i+1, j-1 {
		p.edges[i], p.edges[j] = p.edges[j], p.edges[i]
	}
	// No turning straight back along the road it starts or ends on (a way
	// round to come back to where the route already was).
	if len(p.edges) > 0 {
		if e := p.edges[0]; e.line == k1 && (e.b-e.a)*float64(dir1) < 0 {
			return 0, gpath{}, false
		}
		if e := p.edges[len(p.edges)-1]; e.line == k2 && (e.b-e.a)*float64(dir2) < 0 {
			return 0, gpath{}, false
		}
	}
	return math.Abs(out.d-t1) + d + math.Abs(t2-in.d), p, true
}

// gpath is a way through the network: off the first line at out, the
// edges, onto the last line at in.
type gpath struct {
	out, in gstop
	edges   []gedge
}

// Roads that nearly meet, meet: a road whose end lies at another road
// (within that road's half width and gJoinM of its middle) and points at
// it, with no node in common, is carried on to the nearest point of the
// other's line and joined to it there by a node of their own. The map
// often has such a road end at a path beside the main road, which the map
// data leaves out (owner's mark, 2026-10-11: the Bentincklaan ends 5 m
// from the Hoofdstraat's middle in Rheden, 30.26 km on the Posbank Loop;
// with no junction its end stood over the main road as a wall).
const (
	gJoinM     = 3.0                // beyond the other road's half width
	gJoinAngle = 60 * math.Pi / 180 // the end points at the other road within this
)

// joinNearEnds is ws with such ends joined (ws itself unchanged; ridden as
// for drawable).
func joinNearEnds(ws []scenery.Way, ridden map[int]bool) []scenery.Way {
	out := append([]scenery.Way(nil), ws...)
	use := func(k int) bool {
		w := out[k]
		return len(w.Line) >= 2 && len(w.Nodes) == len(w.Line) && drawable(w, ridden[k])
	}
	count := map[int64]int{}
	for k := range out {
		if !use(k) {
			continue
		}
		for i, n := range out[k].Nodes {
			if i == 0 || i == len(out[k].Nodes)-1 {
				count[n]++
			} else {
				count[n] += 2
			}
		}
	}
	copied := map[int]bool{}
	own := func(k int) {
		if !copied[k] {
			out[k].Line = append([][2]float64(nil), out[k].Line...)
			out[k].Nodes = append([]int64(nil), out[k].Nodes...)
			copied[k] = true
		}
	}
	next := int64(-1_000_000_000)
	for k := range out {
		if !use(k) {
			continue
		}
		for _, last := range []bool{false, true} {
			w := out[k]
			i, j := 0, 1 // the end, and the point before it
			if last {
				i, j = len(w.Line)-1, len(w.Line)-2
			}
			if count[w.Nodes[i]] != 1 {
				continue // a junction already, or a road going on
			}
			p, q := w.Line[i], w.Line[j]
			best, bk, bs, bx := math.Inf(1), -1, -1, [2]float64{}
			for k2 := range out {
				if k2 == k || !use(k2) {
					continue
				}
				o := out[k2]
				for s := 0; s+1 < len(o.Line); s++ {
					a, b := o.Line[s], o.Line[s+1]
					de, dn := b[0]-a[0], b[1]-a[1]
					l2 := de*de + dn*dn
					if l2 == 0 {
						continue
					}
					f := math.Max(0, math.Min(1, ((p[0]-a[0])*de+(p[1]-a[1])*dn)/l2))
					x := [2]float64{a[0] + f*de, a[1] + f*dn}
					d := math.Hypot(x[0]-p[0], x[1]-p[1])
					if d > o.WidthM/2+gJoinM || d >= best {
						continue
					}
					// Pointing at it (or already on it).
					ue, un := p[0]-q[0], p[1]-q[1]
					if d > o.WidthM/2 && d > 0.01 {
						cos := (ue*(x[0]-p[0]) + un*(x[1]-p[1])) / (math.Hypot(ue, un) * d)
						if cos < math.Cos(gJoinAngle) {
							continue
						}
					}
					best, bk, bs, bx = d, k2, s, x
				}
			}
			if bk < 0 {
				continue
			}
			// The node there: the other road's own if it has a point
			// close by, else a new one put in its line.
			own(bk)
			o := &out[bk]
			node := int64(0)
			for s, pt := range o.Line {
				if math.Hypot(pt[0]-bx[0], pt[1]-bx[1]) < 0.3 {
					node, bx = o.Nodes[s], pt
					break
				}
			}
			if node == 0 {
				node = next
				next--
				o.Line = append(o.Line[:bs+1], append([][2]float64{bx}, o.Line[bs+1:]...)...)
				o.Nodes = append(o.Nodes[:bs+1], append([]int64{node}, o.Nodes[bs+1:]...)...)
			}
			count[node] += 2
			own(k)
			w2 := &out[k]
			if last {
				w2.Line, w2.Nodes = append(w2.Line, bx), append(w2.Nodes, node)
			} else {
				w2.Line = append([][2]float64{bx}, w2.Line...)
				w2.Nodes = append([]int64{node}, w2.Nodes...)
			}
			count[node]++
		}
	}
	return out
}

// hairpins counts the turns of more than gHairpinRad through the nodes of
// way p from t1 on line k1 (going dir1) to t2 on line k2: up a link and
// back down its twin, a U-turn round a traffic island (owner, 2026-10-11:
// with a residential street's two short links onto the roundabout at
// the polder ride drawn, the course went up one and back down the other rather
// than straight over the roundabout).
func (g *gnet) hairpins(k1 int, t1 float64, dir1 int8, k2 int, t2 float64, p gpath) int {
	type leg struct {
		line int
		a, b float64
	}
	legs := []leg{{k1, t1, p.out.d}}
	for _, e := range p.edges {
		legs = append(legs, leg{e.line, e.a, e.b})
	}
	legs = append(legs, leg{k2, p.in.d, t2})
	way := func(l leg, end bool) (float64, float64) {
		// The leg's way at its end (end) or start, over a few metres.
		line := g.lines[l.line]
		sg := 1.0
		if l.b < l.a {
			sg = -1
		}
		at := l.a
		if end {
			at = l.b
		}
		a, b := sampleBetween(line, at-sg*3), sampleBetween(line, at+sg*3)
		if end {
			a, b = sampleBetween(line, at-sg*3), sampleBetween(line, at)
		} else {
			a, b = sampleBetween(line, at), sampleBetween(line, at+sg*3)
		}
		de, dn := b.e-a.e, b.n-a.n
		if l := math.Hypot(de, dn); l > 1e-6 {
			return de / l, dn / l
		}
		return 0, 0
	}
	n := 0
	for i := 0; i+1 < len(legs); i++ {
		if math.Abs(legs[i].b-legs[i].a) < 0.5 && i > 0 {
			continue
		}
		ae, an := way(legs[i], true)
		be, bn := way(legs[i+1], false)
		if ae*be+an*bn < math.Cos(gHairpinRad) {
			n++
		}
	}
	return n
}
