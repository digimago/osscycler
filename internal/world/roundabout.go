package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Roundabouts are drawn as one, not as the ways of their ring (owner,
// 2026-10-09: a synthetic roundabout in place of the ring's pieces): the
// ring's ways aren't drawn, its approaches end at it, and a roundabout
// lies over the spot, on a plane through the approaches' heights. A ring
// of one lane is a flat paved disc the rider crosses straight through
// along the route (a route through one is often a cycle path round it,
// which the map data leaves out, and a disc reads right either way); a
// bigger one has an island in the middle, raised on a kerb and grassed,
// and the rider goes round it the way traffic does.
type roundabout struct {
	c         [2]float64 // centre, east and north
	r, hw     float64    // the ring's centre line radius and half width
	grow      float64    // taken out by this much to take in junctions close by (absorb)
	island    bool
	plane     [3]float64 // height a + b·east + c·north
	keepRight bool       // traffic goes round anticlockwise (seen from above)
	arms      []rbArm    // the roads leaving it (shape)
}

// rbArm is a road leaving a roundabout: its line, where it's cut at the
// edge, and which way along the line is away from it.
type rbArm struct {
	l      *roadLine
	d, dir float64
}

const (
	islandMinR  = 20.0 // a ring this big (centre line radius) has an island
	rbSides     = 40   // its circles' segments
	islandKerbM = 0.15
	rbFitReachM = 15.0 // approaches this far beyond the ring set its plane
	rbOverhangM = 0.5  // the disc reaches this far past the ring's edge
	ringHalfM   = 2.9  // half a one-lane ring's width, at least
	ringHalf2M  = 4.5  // half a two-lane ring's width, at least
	// A flat one with its ring this big (owner, 2026-10-09: 10-20 m) has
	// a paved centre circle inside its ring: mountable, slightly raised.
	centreMinR, centreMaxR = 10.0, 20.0
	centreRiseM            = 0.04
)

func (rb roundabout) outer() float64 { return rb.r + rb.grow + rb.hw + rbOverhangM }
func (rb roundabout) inner() float64 { return math.Max(1, rb.r+rb.grow-rb.hw) }

// absorbM: a junction this near a roundabout's edge (a slip road joining
// a road just beyond it) is taken into it: the roundabout grows to have it
// absorbInsideM inside (owner, 2026-10-09: rather a bigger roundabout with
// more roads than turns just before and after it; the flow is smoother).
// The ring keeps its width, and its lanes: one stays a flat disc, crossed
// straight (owner: the shortest way over a small roundabout).
const (
	absorbM       = 20.0
	absorbInsideM = 4.0
	// absorbMaxM: a roundabout grows no wider than this across (owner,
	// 2026-10-09: past that, the road around it is better left as it is).
	absorbMaxM = 30.0
)

// absorb grows the roundabout over junctions close beyond its edge: at
// nodes, and where two of its roads touch (the map may chain a slip road
// and the road it joins into one, with no node between).
func (rb *roundabout) absorb(nodes [][2]float64, ls []*roadLine) {
	take := func(q [2]float64) {
		d := math.Hypot(q[0]-rb.c[0], q[1]-rb.c[1])
		over := d - (rb.r + rb.hw + rbOverhangM)
		grown := 2 * (rb.r + rb.hw + rbOverhangM + over + absorbInsideM)
		if over > 0 && over < absorbM && over+absorbInsideM > rb.grow && grown <= absorbMaxM {
			rb.grow = over + absorbInsideM
		}
	}
	for _, q := range nodes {
		take(q)
	}
	// Its roads: the lines reaching it, and their samples in the zone
	// just beyond it.
	R := rb.r + rb.hw + rbOverhangM
	type pt struct {
		s    sample
		line int
	}
	var zone []pt
	for li, l := range ls {
		reaches := false
		for _, s := range l.samples {
			reaches = reaches || math.Hypot(s.e-rb.c[0], s.n-rb.c[1]) < R
		}
		if !reaches {
			continue
		}
		for _, s := range l.samples {
			if d := math.Hypot(s.e-rb.c[0], s.n-rb.c[1]); d >= R && d < R+absorbM {
				zone = append(zone, pt{s, li})
			}
		}
	}
	for i := range zone {
		for j := i + 1; j < len(zone); j++ {
			a, b := zone[i], zone[j]
			// Two roads touching, or one coming back to itself (a slip
			// road chained to the road it joins), far apart along it.
			if (a.line != b.line || math.Abs(a.s.d-b.s.d) > 3*absorbM) &&
				math.Hypot(a.s.e-b.s.e, a.s.n-b.s.n) < a.s.hw+b.s.hw {
				take([2]float64{(a.s.e + b.s.e) / 2, (a.s.n + b.s.n) / 2})
			}
		}
	}
}

func (rb *roundabout) height(e, n float64) float64 {
	return rb.plane[0] + rb.plane[1]*e + rb.plane[2]*n
}

// findRoundabouts are the map's roundabouts: rings of ways joined at their
// nodes, each its centre (the mean of its points), radius, width; an island
// if it has two lanes or more, or is big.
func findRoundabouts(ws []scenery.Way, keepRight bool) []roundabout {
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
	var out []roundabout
	for _, root := range order {
		var c [2]float64
		var pts [][2]float64
		hw, lanes := 0.0, 0
		for _, k := range groups[root] {
			pts = append(pts, ws[k].Line...)
			hw = math.Max(hw, ws[k].WidthM/2)
			lanes = max(lanes, ws[k].Lanes)
		}
		for _, p := range pts {
			c[0], c[1] = c[0]+p[0]/float64(len(pts)), c[1]+p[1]/float64(len(pts))
		}
		r := 0.0
		for _, p := range pts {
			r += math.Hypot(p[0]-c[0], p[1]-c[1]) / float64(len(pts))
		}
		if r < 2 {
			continue // a mini roundabout drawn as a point: nothing to replace
		}
		// The ring is wider than the roads it serves (owner, 2026-10-09:
		// lorries and buses need the room to turn in; CROW: 5-6 m for one
		// lane, 8-10 m for two), so the middle is smaller.
		hw = math.Max(hw, ringHalfM)
		if lanes >= 2 {
			hw = math.Max(hw, ringHalf2M)
		}
		out = append(out, roundabout{c: c, r: r, hw: hw, island: lanes >= 2 || r >= islandMinR, keepRight: keepRight})
	}
	return out
}

// fit sets the roundabout's plane through the heights of the roads near
// it (least squares; level at base when there are too few).
func (rb *roundabout) fit(ls []*roadLine, base float64) {
	var s [3][4]float64 // normal equations, augmented
	n := 0
	for _, l := range ls {
		for _, p := range l.samples {
			if math.Hypot(p.e-rb.c[0], p.n-rb.c[1]) > rb.outer()+rbFitReachM {
				continue
			}
			x, y := p.e-rb.c[0], p.n-rb.c[1]
			row := [3]float64{1, x, y}
			for i := range 3 {
				for j := range 3 {
					s[i][j] += row[i] * row[j]
				}
				s[i][3] += row[i] * p.ele
			}
			n++
		}
	}
	rb.plane = [3]float64{base, 0, 0}
	if n < 6 {
		return
	}
	// Gauss-Jordan on the 3×3 system.
	for col := range 3 {
		piv := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(s[r][col]) > math.Abs(s[piv][col]) {
				piv = r
			}
		}
		if math.Abs(s[piv][col]) < 1e-9 {
			return
		}
		s[col], s[piv] = s[piv], s[col]
		for r := range 3 {
			if r == col {
				continue
			}
			f := s[r][col] / s[col][col]
			for k := col; k < 4; k++ {
				s[r][k] -= f * s[col][k]
			}
		}
	}
	a, b, c := s[0][3]/s[0][0], s[1][3]/s[1][1], s[2][3]/s[2][2]
	// The plane in east and north, from offsets to the centre.
	rb.plane = [3]float64{a - b*rb.c[0] - c*rb.c[1], b, c}
}

// circle is a circle of radius r round the centre, anticlockwise.
func (rb roundabout) circle(r float64) [][2]float64 {
	out := make([][2]float64, rbSides)
	for k := range out {
		a := 2 * math.Pi * float64(k) / rbSides
		out[k] = [2]float64{rb.c[0] + r*math.Cos(a), rb.c[1] + r*math.Sin(a)}
	}
	return out
}

// shape is the roundabout as a junction built from its middle (owner,
// 2026-10-09: node-first, as every junction): each road reaching it is cut
// where it crosses its edge, the cut's corners corners of its outline, at
// the road's height; between the roads the outline follows the circle;
// sidewalks and verges go along those arcs. Roads it can't take that way
// (outlines that would tangle) leave it laid over them instead.
func (rb *roundabout) shape(ls []*roadLine) patch {
	R := rb.outer()
	p := patch{rb: rb, surface: scenery.SurfaceAsphalt, node: rb.c, nodes: [][2]float64{rb.c}}
	type arm struct {
		angle      float64
		end, outer [2][2]float64 // right, left (looking out)
		walk       [2]bool
	}
	var arms []arm
	dist := func(s sample) float64 { return math.Hypot(s.e-rb.c[0], s.n-rb.c[1]) }
	// crossing is where the road from a to b crosses the edge.
	crossing := func(a, b sample) float64 {
		dx, dy := b.e-a.e, b.n-a.n
		fx, fy := a.e-rb.c[0], a.n-rb.c[1]
		qa, qb, qc := dx*dx+dy*dy, 2*(fx*dx+fy*dy), fx*fx+fy*fy-R*R
		disc := qb*qb - 4*qa*qc
		if qa == 0 || disc < 0 {
			return a.d
		}
		sq := math.Sqrt(disc)
		for _, t := range []float64{(-qb - sq) / (2 * qa), (-qb + sq) / (2 * qa)} {
			if t >= 0 && t <= 1 {
				return a.d + t*(b.d-a.d)
			}
		}
		return a.d
	}
	for _, l := range ls {
		ss := l.samples
		for i := 0; i < len(ss); {
			if dist(ss[i]) >= R {
				i++
				continue
			}
			j := i
			for j < len(ss) && dist(ss[j]) < R {
				j++
			}
			from, to := ss[i].d, ss[j-1].d
			var cuts []struct {
				d, dir float64
			}
			if i > 0 {
				from = crossing(ss[i-1], ss[i])
				cuts = append(cuts, struct{ d, dir float64 }{from, -1})
			}
			if j < len(ss) {
				to = crossing(ss[j-1], ss[j])
				cuts = append(cuts, struct{ d, dir float64 }{to, 1})
			}
			p.spans = append(p.spans, patchSpan{l, from, to, (from + to) / 2, from, to})
			for _, c := range cuts {
				s := sampleBetween(l, c.d)
				lw, rw := carriagewayEdges(l.smooth, c.d, s.hw, s.wide)
				lo, ro := carriagewayEdges(l.smooth, c.d, s.hw+walkM, [2]float64{})
				a := arm{end: [2][2]float64{rw, lw}, outer: [2][2]float64{ro, lo}, walk: [2]bool{s.walk[1], s.walk[0]}}
				if c.dir < 0 { // going out against the line: its left is our right
					a = arm{end: [2][2]float64{lw, rw}, outer: [2][2]float64{lo, ro}, walk: [2]bool{s.walk[0], s.walk[1]}}
				}
				a.angle = math.Atan2((lw[1]+rw[1])/2-rb.c[1], (lw[0]+rw[0])/2-rb.c[0])
				arms = append(arms, a)
				rb.arms = append(rb.arms, rbArm{l, c.d, c.dir})
				p.cuts = append(p.cuts, patchCut{l: l, d: c.d, p0: lw, p1: rw, ele: s.ele})
			}
			i = j
		}
	}
	if len(arms) == 0 {
		p.poly, p.overlay = rb.circle(R), true
		return p
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i].angle < arms[j].angle })
	circle := rb.circle(R)
	angleOf := func(q [2]float64) float64 { return math.Atan2(q[1]-rb.c[1], q[0]-rb.c[0]) }
	// arc are the circle's points strictly between angles a0 and a1,
	// anticlockwise.
	arc := func(a0, a1 float64) [][2]float64 {
		span := math.Mod(a1-a0+4*math.Pi, 2*math.Pi)
		var out [][2]float64
		for k := range circle {
			a := 2 * math.Pi * float64(k) / rbSides
			off := math.Mod(a-a0+4*math.Pi, 2*math.Pi)
			if off > 1e-6 && off < span-1e-6 {
				out = append(out, circle[k])
			}
		}
		sort.Slice(out, func(i, j int) bool {
			return math.Mod(angleOf(out[i])-a0+4*math.Pi, 2*math.Pi) < math.Mod(angleOf(out[j])-a0+4*math.Pi, 2*math.Pi)
		})
		return out
	}
	var poly [][2]float64
	for k, a := range arms {
		next := arms[(k+1)%len(arms)]
		poly = append(poly, a.end[0], a.end[1])
		between := arc(angleOf(a.end[1]), angleOf(next.end[0]))
		poly = append(poly, between...)
		kerb := append(append([][2]float64{a.end[1]}, between...), next.end[0])
		w := walkChain{kerb: kerb, outA: a.outer[1], outB: next.outer[0], fromA: a.walk[1], fromB: next.walk[0]}
		p.edges = append(p.edges, w)
		if w.fromA || w.fromB {
			p.walks = append(p.walks, w)
		}
	}
	if !simplePolygon(poly) {
		p.poly, p.overlay, p.cuts, p.spans, p.edges, p.walks = circle, true, nil, nil, nil, nil
		return p
	}
	p.poly = poly
	return p
}

// islandMesh is a roundabout's island, on top of its surface: grass (in
// the terrain's material, coloured in its vertices) on a kerb.
func (rb *roundabout) islandMesh(ground, kerb int) []gltf.Primitive {
	return rb.centreMesh(ground, kerb, islandKerbM, true)
}

// pavedMesh is a flat roundabout's paved centre circle, in material paving.
func (rb *roundabout) pavedMesh(paving, kerb int) []gltf.Primitive {
	return rb.centreMesh(paving, kerb, centreRiseM, false)
}

// paved tells whether a roundabout has a paved centre circle.
func (rb roundabout) paved() bool {
	r := rb.r + rb.grow
	return !rb.island && r >= centreMinR && r <= centreMaxR
}

// centreMesh is a disc inside the ring, rise up with a kerb round it:
// grass (in vertex colours) or paving (with UVs).
func (rb *roundabout) centreMesh(mat, kerb int, rise float64, grassy bool) []gltf.Primitive {
	top := gltf.Primitive{Material: mat}
	wall := gltf.Primitive{Material: kerb}
	in := rb.circle(rb.inner())
	grass := landColors[scenery.LandNone]
	up := [3]float64{0, 1, 0}
	y := func(q [2]float64) float64 { return rb.height(q[0], q[1]) + overlayLiftM }
	ctr := uint32(0)
	add := func(q [2]float64) {
		top.Positions = append(top.Positions, float32(q[0]), float32(y(q)+rise), float32(-q[1]))
		top.Normals = append(top.Normals, 0, 1, 0)
		if grassy {
			top.Colors = append(top.Colors, grass[0], grass[1], grass[2])
		} else {
			top.UVs = append(top.UVs, float32(q[0]/5), float32(q[1]/5))
		}
	}
	add(rb.c)
	for k := range rbSides {
		add(in[k])
	}
	for k := range rbSides {
		tri(&top, ctr, uint32(1+k), uint32(1+(k+1)%rbSides), up)
	}
	vert := func(q [2]float64, dy float64, n [3]float64) uint32 {
		wall.Positions = append(wall.Positions, float32(q[0]), float32(y(q)+dy), float32(-q[1]))
		wall.Normals = append(wall.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		wall.UVs = append(wall.UVs, float32(q[0]/5), float32(dy))
		return uint32(len(wall.Positions)/3 - 1)
	}
	for k := range rbSides {
		k2 := (k + 1) % rbSides
		nx, ny := in[k][0]-rb.c[0], in[k][1]-rb.c[1]
		l := math.Hypot(nx, ny)
		side := [3]float64{nx / l, 0, -ny / l} // outward, towards the ring
		a, b, c, d := vert(in[k], 0, side), vert(in[k2], 0, side), vert(in[k], rise, side), vert(in[k2], rise, side)
		tri(&wall, a, b, c, side)
		tri(&wall, b, d, c, side)
	}
	return []gltf.Primitive{top, wall}
}

// rideAround puts the rider's path (points every pathStep: east x, north
// y, height z, width w) round each island roundabout it passes through the
// way traffic goes, from where it enters to where it leaves (the course's
// distance stays the route's: the rider covers the arc in the time the
// route takes across).
func rideAround(x, y, z, w []float64, rbs []roundabout) {
	for _, rb := range rbs {
		if !rb.island {
			continue
		}
		inside := func(i int) bool { return math.Hypot(x[i]-rb.c[0], y[i]-rb.c[1]) < rb.outer() }
		for i := 1; i < len(x)-1; i++ {
			if !inside(i) || inside(i-1) {
				continue
			}
			j := i
			for j < len(x)-1 && inside(j) {
				j++
			}
			if j >= len(x)-1 {
				break
			}
			// From the last point before to the first after.
			a0 := math.Atan2(y[i-1]-rb.c[1], x[i-1]-rb.c[0])
			a1 := math.Atan2(y[j]-rb.c[1], x[j]-rb.c[0])
			sweep := a1 - a0
			if rb.keepRight { // anticlockwise: the angle grows
				for sweep <= 0 {
					sweep += 2 * math.Pi
				}
			} else {
				for sweep >= 0 {
					sweep -= 2 * math.Pi
				}
			}
			ride := rb.r + rb.grow // the ring's centre line (the riding line moves the rider over)
			for k := i; k < j; k++ {
				f := float64(k-(i-1)) / float64(j-(i-1))
				a := a0 + sweep*f
				x[k], y[k] = rb.c[0]+ride*math.Cos(a), rb.c[1]+ride*math.Sin(a)
				z[k] = rb.height(x[k], y[k])
				w[k] = 2 * rb.hw
			}
			i = j
		}
	}
}

// passThrough puts the rider's path (points every pathStep: east x, north
// y, height z) straight through each flat roundabout, as at any junction
// on a closed course (owner, 2026-10-09): from where it enters the disc
// across to the first road of the roundabout's it is on again after
// leaving it, and along that road to there, rather than round whatever
// way the route's points take (a cycle path round it, a slip road).
func passThrough(x, y, z []float64, rbs []roundabout) {
	const lookM = 200.0 // how far on to look for the road it leaves by
	for _, rb := range rbs {
		if rb.island || len(rb.arms) == 0 {
			continue
		}
		// A path skirting the edge counts too (the route's points may run
		// round just outside it).
		R := rb.outer() + passSkirtM
		inside := func(i int) bool { return math.Hypot(x[i]-rb.c[0], y[i]-rb.c[1]) < R }
		for a := 1; a < len(x); a++ {
			if !inside(a) || inside(a-1) {
				continue
			}
			// The first point after the disc on one of its roads.
			b, arm, at := -1, rbArm{}, 0.0
			for i := a + 1; i < len(x) && float64(i-a)*pathStep < lookM; i++ {
				if inside(i) {
					continue
				}
				for _, ar := range rb.arms {
					if d, ok := onLine(ar.l, x[i], y[i]); ok && (d-ar.d)*ar.dir > 0 {
						b, arm, at = i, ar, d
						break
					}
				}
				if b >= 0 {
					break
				}
			}
			if b < 0 {
				continue
			}
			// The way: from before the disc straight to the road's edge,
			// then along it.
			var way [][3]float64
			way = append(way, [3]float64{x[a-1], y[a-1], z[a-1]})
			for d := arm.d; (at-d)*arm.dir >= 0; d += arm.dir * fineStep {
				s := sampleBetween(arm.l, d)
				way = append(way, [3]float64{s.e, s.n, s.ele})
			}
			s := sampleBetween(arm.l, at)
			way = append(way, [3]float64{s.e, s.n, s.ele})
			// Its points evenly spread over the path's steps a..b-1.
			total := 0.0
			cum := make([]float64, len(way))
			for k := 1; k < len(way); k++ {
				total += math.Hypot(way[k][0]-way[k-1][0], way[k][1]-way[k-1][1])
				cum[k] = total
			}
			for i := a; i < b; i++ {
				want := total * float64(i-(a-1)) / float64(b-(a-1))
				k := 1
				for k < len(way)-1 && cum[k] < want {
					k++
				}
				f := 0.0
				if cum[k] > cum[k-1] {
					f = (want - cum[k-1]) / (cum[k] - cum[k-1])
				}
				x[i] = way[k-1][0] + f*(way[k][0]-way[k-1][0])
				y[i] = way[k-1][1] + f*(way[k][1]-way[k-1][1])
				z[i] = way[k-1][2] + f*(way[k][2]-way[k-1][2])
				if math.Hypot(x[i]-rb.c[0], y[i]-rb.c[1]) < rb.outer() {
					z[i] = rb.height(x[i], y[i])
				}
			}
			a = b
		}
	}
}

// passSkirtM: a path this near a flat roundabout's edge goes through it.
const passSkirtM = 4.0

// onLine is where along line l e, n lies, if on its carriageway.
func onLine(l *roadLine, e, n float64) (float64, bool) {
	best, at, hw := math.Inf(1), 0.0, 0.0
	for i := 0; i+1 < len(l.samples); i++ {
		a, b := l.samples[i], l.samples[i+1]
		de, dn := b.e-a.e, b.n-a.n
		f := 0.0
		if l2 := de*de + dn*dn; l2 > 0 {
			f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
		}
		if d := math.Hypot(e-a.e-f*de, n-a.n-f*dn); d < best {
			best, at, hw = d, a.d+f*(b.d-a.d), a.hw
		}
	}
	return at, best <= hw
}
