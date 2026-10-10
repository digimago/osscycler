package world

import (
	"math"
	"slices"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// A junction is one paved patch: the convex hull of the edges of the
// first metres of every road meeting there. A hull has no spikes, and in
// the inside of a turn, or between a side road and the main road, it
// fills in a cut-off corner, as the paved aprons of real junctions. It is
// its own surface (owner, 2026-10-09: a junction is a thing of its own, not
// a through road with side roads stuck on): every road stops at its edge,
// and it lies a little above their ends, at their height, in the surface
// of its main road; its edge has a skirt into the ground. Sidewalks and
// cycle lanes stop short of it.
const (
	apronM       = 3.0  // a junction's corner apron keeps this far off the other roads
	apronAlongM  = 6.0  // the patch reaches this far along each road past the widest road's edge
	cornerM      = 6.0  // its corners between roads are rounded to this radius, room allowing
	patchClearM  = 2.0  // sidewalks and lanes stop this far short of a patch
	overlayLiftM = 0.02 // an overlay junction lies this far above its roads
)

type patch struct {
	at      float64
	poly    [][2]float64 // east, north, counter-clockwise
	arms    []patchArm   // the roads meeting there
	spans   []patchSpan  // the stretches of them it takes over
	cuts    []patchCut   // where they end at it
	surface scenery.Surface
	rank    int          // its main road's class
	node    [2]float64   // where its roads meet (east, north)
	merged  bool         // of several junctions' nodes
	rb      *roundabout  // a roundabout drawn as one (roundabout.go)
	nodes   [][2]float64 // all of them (east, north)
	overlay bool         // laid over its roads (no cuts), a little above
	walks   []walkChain
	edges   []walkChain     // all its free edges (walks: those with sidewalks)
	lanes   [][4][2]float64 // cycle lanes across it (lanes.go)
}

// patchArm is a road at a junction: its line, and where along it the
// junction's node lies.
type patchArm struct {
	l  *roadLine
	at float64
}

// height is the patch's surface at e, n: on a cut, the road's height
// there exactly; elsewhere its roads' heights (along their lines) weighted
// by how near their carriageways are, so on each road it is that road's.
func (p patch) height(e, n float64) float64 {
	for _, c := range p.cuts {
		if segDist(c.p0, c.p1, e, n) < 1e-3 {
			return c.ele
		}
	}
	if p.rb != nil {
		return p.rb.height(e, n)
	}
	var sum, wsum float64
	for _, sp := range p.spans {
		ss := sp.l.samples
		best, bd := 0.0, math.Inf(1)
		var hw float64
		for i := 0; i+1 < len(ss); i++ {
			a, b := ss[i], ss[i+1]
			if b.d < sp.from-20 || a.d > sp.to+20 {
				continue
			}
			de, dn := b.e-a.e, b.n-a.n
			f := 0.0
			if l2 := de*de + dn*dn; l2 > 0 {
				f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
			}
			if d := math.Hypot(e-a.e-f*de, n-a.n-f*dn); d < bd {
				best, bd, hw = a.ele+f*(b.ele-a.ele), d, a.hw+f*(b.hw-a.hw)
			}
		}
		if math.IsInf(bd, 1) {
			continue
		}
		off := math.Max(0, bd-hw)
		w := 1 / (off*off + 1e-6)
		sum += w * best
		wsum += w
	}
	if wsum == 0 {
		return 0
	}
	return sum / wsum
}

// segDist is the distance from e, n to the segment a-b.
func segDist(a, b [2]float64, e, n float64) float64 {
	de, dn := b[0]-a[0], b[1]-a[1]
	f := 0.0
	if l2 := de*de + dn*dn; l2 > 0 {
		f = math.Max(0, math.Min(1, ((e-a[0])*de+(n-a[1])*dn)/l2))
	}
	return math.Hypot(e-a[0]-f*de, n-a[1]-f*dn)
}

// patchMeshes are a junction's surface with its skirt, and the sidewalks
// it carries.
func patchMeshes(p patch, m roadMaterials, v *verger) []gltf.Primitive {
	out := []gltf.Primitive{patchMesh(p, m.surface[p.surface])}
	if len(out[0].Indices) == 0 {
		return nil
	}
	walk, kerb := gltf.Primitive{Material: m.walk}, gltf.Primitive{Material: m.kerb}
	for _, w := range p.walks {
		walkMesh(p, w, &walk, &kerb)
	}
	for _, q := range []gltf.Primitive{walk, kerb} {
		if len(q.Indices) > 0 {
			out = append(out, q)
		}
	}
	if q := laneMesh(p, m.lane); len(q.Indices) > 0 {
		out = append(out, q)
	}
	if v != nil && !p.overlay {
		if q := junctionVerges(p, v); len(q.Indices) > 0 {
			out = append(out, q)
		}
	}
	if p.rb != nil && p.rb.island {
		out = append(out, p.rb.islandMesh(m.island, m.kerb)...)
	}
	if p.rb != nil && p.rb.paved() {
		out = append(out, p.rb.pavedMesh(m.paving, m.kerb)...)
	}
	return out
}

// walkMesh is a sidewalk a junction carries: on a kerb along its outline,
// walkM wide, its ends where the roads' sidewalks end. Where only one road
// has a sidewalk on that side, it goes halfway round and stops.
func walkMesh(p patch, w walkChain, walk, kerb *gltf.Primitive) {
	k := w.kerb
	if len(k) < 2 {
		return
	}
	at, outer := chainShape(w)
	total := at[len(k)-1]
	from, to := 0.0, total
	switch {
	case w.fromA && !w.fromB:
		to = total / 2
	case !w.fromA && w.fromB:
		from = total / 2
	}
	// At the ends, a road's end: the junction's height is the road's there.
	y := func(i int) float64 { return p.height(k[i][0], k[i][1]) }
	vert := func(q *gltf.Primitive, x, h, z float64, n [3]float64) uint32 {
		q.Positions = append(q.Positions, float32(x), float32(h), float32(-z))
		q.Normals = append(q.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		q.UVs = append(q.UVs, float32(x/5), float32(z/5))
		return uint32(len(q.Positions)/3 - 1)
	}
	up := [3]float64{0, 1, 0}
	for i := 0; i+1 < len(k); i++ {
		if at[i+1] <= from+1e-9 || at[i] >= to-1e-9 {
			continue
		}
		a, b := k[i], k[i+1]
		oa, ob := outer[i], outer[i+1]
		ha, hb := y(i), y(i+1)
		// The sidewalk's surface.
		v0 := vert(walk, a[0], ha+kerbM, a[1], up)
		v1 := vert(walk, b[0], hb+kerbM, b[1], up)
		v2 := vert(walk, oa[0], ha+kerbM, oa[1], up)
		v3 := vert(walk, ob[0], hb+kerbM, ob[1], up)
		tri(walk, v0, v1, v2, up)
		tri(walk, v1, v3, v2, up)
		// The kerb's face, towards the junction (inward: left of travel),
		// and the outer face into the ground.
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		in := [3]float64{-dy / l, 0, -dx / l} // east, up, south: left of travel
		k0 := vert(kerb, a[0], ha, a[1], in)
		k1 := vert(kerb, b[0], hb, b[1], in)
		k2 := vert(kerb, a[0], ha+kerbM, a[1], in)
		k3 := vert(kerb, b[0], hb+kerbM, b[1], in)
		tri(kerb, k0, k1, k2, in)
		tri(kerb, k1, k3, k2, in)
		out := [3]float64{-in[0], 0, -in[2]}
		o0 := vert(kerb, oa[0], ha-walkFootM, oa[1], out)
		o1 := vert(kerb, ob[0], hb-walkFootM, ob[1], out)
		o2 := vert(kerb, oa[0], ha+kerbM, oa[1], out)
		o3 := vert(kerb, ob[0], hb+kerbM, ob[1], out)
		tri(kerb, o0, o1, o2, out)
		tri(kerb, o1, o3, o2, out)
	}
}

// patchMesh is a junction's surface with its skirt.
func patchMesh(p patch, material int) gltf.Primitive {
	lift := 0.0
	if p.overlay {
		lift = overlayLiftM
	}
	m := drapedIn(p.height, p.poly, material, lift, 0)
	if len(m.Indices) == 0 {
		return m
	}
	// The skirt: the patch's edge goes on into the ground, in pieces as
	// long as the surface's.
	for k := range p.poly {
		a, b := p.poly[k], p.poly[(k+1)%len(p.poly)]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		if l == 0 || p.cutEdge(a, b) {
			continue // a road goes on from here: no skirt
		}
		// Outward: the polygon runs counter-clockwise, so right of a→b.
		nx, nz := (b[1]-a[1])/l, (b[0]-a[0])/l // east, south
		pieces := max(1, int(math.Ceil(l/drapeCellM)))
		var top, bottom []uint32
		for i := 0; i <= pieces; i++ {
			f := float64(i) / float64(pieces)
			e, no := a[0]+f*(b[0]-a[0]), a[1]+f*(b[1]-a[1])
			h := p.height(e, no) + lift
			for _, y := range []float64{h, h - skirtM} {
				m.Positions = append(m.Positions, float32(e), float32(y), float32(-no))
				m.Normals = append(m.Normals, float32(nx), 0, float32(nz))
				m.UVs = append(m.UVs, float32(f*l/5), float32(y-h))
			}
			v := uint32(len(m.Positions)/3 - 2)
			top, bottom = append(top, v), append(bottom, v+1)
		}
		for i := range pieces {
			n := [3]float64{nx, 0, nz}
			tri(&m, top[i], bottom[i], top[i+1], n)
			tri(&m, top[i+1], bottom[i], bottom[i+1], n)
		}
	}
	return m
}

// markPatches marks the samples whose centre lies on a junction's patch,
// of a road that is not one of its own: the road's surface stops under it.
func markPatches(fine []sample, ps []patch) {
	for _, p := range ps {
		e0, n0, e1, n1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, q := range p.poly {
			e0, n0, e1, n1 = math.Min(e0, q[0]), math.Min(n0, q[1]), math.Max(e1, q[0]), math.Max(n1, q[1])
		}
		for i := range fine {
			s := &fine[i]
			if s.e >= e0 && s.e <= e1 && s.n >= n0 && s.n <= n1 && inPoly(p.poly, s.e, s.n) {
				s.underPatch = true
			}
		}
	}
}

// clearPatches stops the route's sidewalks and cycle lanes wherever they
// would lie on a junction's patch: on every pass of the route by it (a
// loop may come by again where it went straight on).
func clearPatches(fine []sample, ps []patch) {
	if len(ps) == 0 || len(fine) < 3 {
		return
	}
	for _, p := range ps {
		e0, n0, e1, n1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, q := range p.poly {
			e0, n0, e1, n1 = math.Min(e0, q[0]), math.Min(n0, q[1]), math.Max(e1, q[0]), math.Max(n1, q[1])
		}
		const margin = 10.0 // a road's half width and a sidewalk
		for i := 1; i+1 < len(fine); i++ {
			s := &fine[i]
			if s.e < e0-margin || s.e > e1+margin || s.n < n0-margin || s.n > n1+margin {
				continue
			}
			de, dn := fine[i+1].e-fine[i-1].e, fine[i+1].n-fine[i-1].n
			l := math.Hypot(de, dn)
			if l == 0 {
				continue
			}
			rx, rn := dn/l, -de/l
			for side, sign := range []float64{-1, 1} {
				// On the patch, or patchClearM along the road from it: a strip
				// ending right at its edge splays where the road turns into it.
				at := func(off float64) bool {
					x, y := s.e+sign*rx*off, s.n+sign*rn*off
					for _, along := range []float64{0, -patchClearM, patchClearM} {
						if inPoly(p.poly, x+along*de/l, y+along*dn/l) {
							return true
						}
					}
					return false
				}
				if at(s.hw+0.2) || at(s.hw+walkM) {
					s.walk[side], s.open[side] = false, true
				}
				if s.lane[side] && (at(s.hw-0.2) || at(s.hw-laneM)) {
					// On the patch the lane would sit on its edge: the patch
					// reaches out to the carriageway's edge.
					if at(s.hw + 0.5) {
						s.lane[side] = false
					}
				}
			}
		}
	}
}

// convexHull is the convex hull of pts, counter-clockwise (monotone chain).
func convexHull(pts [][2]float64) [][2]float64 {
	if len(pts) < 3 {
		return nil
	}
	p := append([][2]float64(nil), pts...)
	sort.Slice(p, func(i, j int) bool { return p[i][0] < p[j][0] || p[i][0] == p[j][0] && p[i][1] < p[j][1] })
	cross := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	var h [][2]float64
	for pass := range 2 {
		start := len(h)
		for k := range p {
			q := p[k]
			if pass == 1 {
				q = p[len(p)-1-k]
			}
			for len(h) >= start+2 && cross(h[len(h)-2], h[len(h)-1], q) <= 0 {
				h = h[:len(h)-1]
			}
			h = append(h, q)
		}
		h = h[:len(h)-1]
	}
	return h
}

// inPoly tells whether x, y lies inside the polygon (even-odd).
func inPoly(poly [][2]float64, x, y float64) bool {
	in := false
	for i := range poly {
		a, b := poly[i], poly[(i+len(poly)-1)%len(poly)]
		if (a[1] > y) != (b[1] > y) && x < a[0]+(y-a[1])*(b[0]-a[0])/(b[1]-a[1]) {
			in = !in
		}
	}
	return in
}

// tidyStrips takes out what the map's tags leave of sidewalks and cycle
// lanes in pieces too short to be real (a few metres between a junction
// and a change of tags), and fills gaps as short (tags flickering along a
// street), except where a junction opened them. Cycle lanes need a longer
// run (owner, 2026-10-09: between close junctions they showed as red
// patches), unless they run between two junctions, across which they go on
// (lanes.go).
func tidyStrips(fine []sample) {
	const maxGap = 3 // fine samples: 6 m
	for side := range 2 {
		for _, strip := range []struct {
			get    func(*sample) *bool
			minRun int
		}{
			{func(s *sample) *bool { return &s.walk[side] }, 4},  // 8 m
			{func(s *sample) *bool { return &s.lane[side] }, 13}, // 26 m
		} {
			get, minRun := strip.get, strip.minRun
			// Fill short gaps first, then drop short runs.
			for i := 0; i < len(fine); {
				if *get(&fine[i]) {
					i++
					continue
				}
				j := i
				opened := false
				for j < len(fine) && !*get(&fine[j]) {
					opened = opened || fine[j].open[side] || fine[j].onPatch
					j++
				}
				if i > 0 && j < len(fine) && j-i <= maxGap && !opened {
					for k := i; k < j; k++ {
						*get(&fine[k]) = true
					}
				}
				i = j
			}
			// A junction's surface draws no strips: runs end there.
			has := func(i int) bool { return *get(&fine[i]) && !fine[i].onPatch }
			for i := 0; i < len(fine); {
				if !has(i) {
					i++
					continue
				}
				j := i
				for j < len(fine) && has(j) {
					j++
				}
				// Between two junctions it goes on across both: kept.
				between := i > 0 && fine[i-1].onPatch && j < len(fine) && fine[j].onPatch
				if j-i < minRun && !between {
					for k := i; k < j; k++ {
						*get(&fine[k]) = false
					}
				}
				i = j
			}
		}
	}
}

// patchSpan is the stretch of a road a junction's patch takes over, and
// where along it its nodes lie (lo to hi: one node, or several in a
// junction merged from close ones).
type patchSpan struct {
	l                    *roadLine
	from, to, at, lo, hi float64
}

// patchCut is where a road ends at a junction: the end of its carriageway
// (left and right edge, as the road's own mesh puts them) at its height.
// The patch's outline runs through both points, so road and junction
// share the edge and meet without a step.
type patchCut struct {
	l      *roadLine
	d      float64
	p0, p1 [2]float64
	ele    float64
}

// mergePatches makes one patch of junctions whose spans overlap on a
// road (two junctions a few metres apart, a carriageway joining near a
// crossing): one surface, never two over each other.
func mergePatches(ps []patch) []patch {
	parent := make([]int, len(ps))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			for _, a := range ps[i].spans {
				for _, b := range ps[j].spans {
					if a.l == b.l && a.from < b.to+fineStep && b.from < a.to+fineStep {
						parent[find(j)] = find(i)
					}
				}
			}
		}
	}
	byRoot := map[int]*patch{}
	var order []int
	for i := range ps {
		r := find(i)
		m := byRoot[r]
		if m == nil {
			c := ps[i]
			c.spans = append([]patchSpan(nil), c.spans...)
			c.arms = append([]patchArm(nil), c.arms...)
			byRoot[r] = &c
			order = append(order, r)
			continue
		}
		m.arms = append(m.arms, ps[i].arms...)
		m.merged = true
		m.nodes = append(m.nodes, ps[i].nodes...)
		if ps[i].rank > m.rank {
			m.surface, m.rank = ps[i].surface, ps[i].rank
		}
		for _, b := range ps[i].spans {
			joined := false
			for k, a := range m.spans {
				if a.l == b.l && a.from < b.to+fineStep && b.from < a.to+fineStep {
					m.spans[k].from, m.spans[k].to = math.Min(a.from, b.from), math.Max(a.to, b.to)
					m.spans[k].lo, m.spans[k].hi = math.Min(a.lo, b.lo), math.Max(a.hi, b.hi)
					joined = true
					break
				}
			}
			if !joined {
				m.spans = append(m.spans, b)
			}
		}
	}
	out := make([]patch, 0, len(order))
	for _, r := range order {
		out = append(out, *byRoot[r])
	}
	return out
}

// shapePatches gives each patch its outline and its cuts (where a road
// goes on beyond it), built from the node out (owner, 2026-10-09: roads end
// in an edge whose two corners are the junction's): around the node, each
// road's side out to its end, the end, the other side back; between
// roads a straight apron (3 m off the next road, narrower where that would
// cut across a bending road). A junction merged from several nodes goes round
// their middle, each road out from the last node on it. One whose outline
// comes out tangled, or would leave a road off it, is laid over its roads
// instead: the convex hull of their edges, a little above them (overlay).
func shapePatches(ps []patch) []patch {
	var out []patch
	for _, p := range ps {
		ok := false
		for _, apron := range []float64{apronM, apronM / 2, 0.5} {
			if ok = shapeFromNode(&p, apron); ok {
				break
			}
		}
		if !ok {
			shapeOverlay(&p)
		}
		if len(p.poly) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// shapeOverlay makes p an overlay: the convex hull of its roads' edges
// over their spans, no cuts.
func shapeOverlay(p *patch) {
	var pts [][2]float64
	for _, sp := range p.spans {
		for _, s := range sp.l.samples {
			if s.d < sp.from || s.d > sp.to {
				continue
			}
			de, dn, _ := sp.l.smooth.heading(s.d)
			pts = append(pts, [2]float64{s.e + dn*s.hw, s.n - de*s.hw}, [2]float64{s.e - dn*s.hw, s.n + de*s.hw})
		}
	}
	p.overlay, p.cuts, p.poly = true, nil, convexHull(pts)
}

// armOut is a road leaving the node one way, its edge points from
// the node out (right and left of the way out), its end.
type armOut struct {
	angle       float64
	right, left [][2]float64
	end         [2][2]float64 // right, left corner
	outer       [2][2]float64 // there, the outer edges of sidewalks on the right and left
	walk        [2]bool       // sidewalks on its right and left at its end
	lane        [2]bool       // cycle lanes on its right and left at its end
}

// walkChain is a sidewalk a junction carries: along its kerb (a stretch of
// its outline, from one road's end to the next road's), outward of it,
// joining the roads' own sidewalks at their ends.
type walkChain struct {
	kerb         [][2]float64
	outA, outB   [2]float64 // the outer edge at each end, as the roads' sidewalks end
	fromA, fromB bool       // a road's sidewalk joins at that end
}

// shapeFromNode builds p's outline around its node, its corner aprons
// apron off the other roads; false if it comes out tangled or would leave
// a road off it.
func shapeFromNode(p *patch, apron float64) bool {
	var arms []armOut
	p.cuts = nil
	// Round the middle of its nodes (one, unless merged).
	var centre [2]float64
	for _, q := range p.nodes {
		centre[0], centre[1] = centre[0]+q[0]/float64(len(p.nodes)), centre[1]+q[1]/float64(len(p.nodes))
	}
	for _, sp := range p.spans {
		l := sp.l
		first, last := l.samples[0].d, l.samples[len(l.samples)-1].d
		for _, dir := range []float64{-1, 1} {
			end := sp.to
			if dir < 0 {
				end = sp.from
			}
			// Out from the last node on the road that way.
			at := sp.hi
			if dir < 0 {
				at = sp.lo
			}
			if math.Abs(end-at) < 0.5 {
				continue // the road starts or ends at a node
			}
			var a armOut
			edges := func(d float64) (right, left [2]float64) {
				s := sampleBetween(l, d)
				lw, rw := carriagewayEdges(l.smooth, d, s.hw, s.wide)
				if dir < 0 { // going out against the line: its left is our right
					return lw, rw
				}
				return rw, lw
			}
			for _, s := range l.samples {
				if (s.d-at)*dir <= 0 || (end-s.d)*dir <= 1e-6 {
					continue
				}
				r, lft := edges(s.d)
				a.right, a.left = append(a.right, r), append(a.left, lft)
			}
			if dir < 0 { // from the node out
				slices.Reverse(a.right)
				slices.Reverse(a.left)
			}
			r, lft := edges(end)
			a.end = [2][2]float64{r, lft}
			{
				s := sampleBetween(l, end)
				lo, ro := carriagewayEdges(l.smooth, end, s.hw+walkM, [2]float64{})
				a.outer, a.walk = [2][2]float64{ro, lo}, [2]bool{s.walk[1], s.walk[0]}
				a.lane = [2]bool{s.lane[1], s.lane[0]}
				if dir < 0 {
					a.outer, a.walk = [2][2]float64{lo, ro}, [2]bool{s.walk[0], s.walk[1]}
					a.lane = [2]bool{s.lane[0], s.lane[1]}
				}
			}
			if end > first+0.5 && end < last-0.5 {
				s := sampleBetween(l, end)
				p0, p1 := carriagewayEdges(l.smooth, end, s.hw, s.wide)
				p.cuts = append(p.cuts, patchCut{l: l, d: end, p0: p0, p1: p1, ele: s.ele})
			}
			mx, my := (r[0]+lft[0])/2, (r[1]+lft[1])/2
			a.angle = math.Atan2(my-centre[1], mx-centre[0])
			arms = append(arms, a)
		}
	}
	if len(arms) < 2 {
		return false
	}
	sort.Slice(arms, func(i, j int) bool { return arms[i].angle < arms[j].angle })
	// A side's points count where they keep an apron's width off every
	// other road's carriageway near the node.
	clear := func(q [2]float64, self *patchSpan) bool {
		for k := range p.spans {
			sp := &p.spans[k]
			if sp == self {
				continue
			}
			for _, s := range sp.l.samples {
				if s.d < sp.from-1 || s.d > sp.to+1 {
					continue
				}
				if math.Hypot(s.e-q[0], s.n-q[1]) < s.hw+apron {
					return false
				}
			}
		}
		return true
	}
	var poly [][2]float64
	rightAt, leftAt := make([]int, len(arms)), make([]int, len(arms))
	for k := range arms {
		a := arms[k]
		// Which span is it? The one whose edges these are; any other
		// span's carriageway is what the apron keeps off.
		self := p.spanOf(a.end[0], a.end[1])
		for _, q := range a.right {
			if clear(q, self) {
				poly = append(poly, q)
			}
		}
		rightAt[k] = len(poly)
		poly = append(poly, a.end[0], a.end[1])
		leftAt[k] = len(poly) - 1
		for i := len(a.left) - 1; i >= 0; i-- {
			if clear(a.left[i], self) {
				poly = append(poly, a.left[i])
			}
		}
	}
	poly, rightAt, leftAt = roundCorners(poly, rightAt, leftAt)
	// Its free edges: between each road's left end corner and the next
	// road's right one. Sidewalks go along those where either road has one
	// on that side; verges along all.
	p.walks, p.edges = nil, nil
	for k := range arms {
		next := (k + 1) % len(arms)
		a, b := arms[k], arms[next]
		var kerb [][2]float64
		for i := leftAt[k]; ; i = (i + 1) % len(poly) {
			kerb = append(kerb, poly[i])
			if i == rightAt[next] {
				break
			}
		}
		w := walkChain{kerb: kerb, outA: a.outer[1], outB: b.outer[0], fromA: a.walk[1], fromB: b.walk[0]}
		p.edges = append(p.edges, w)
		if w.fromA || w.fromB {
			p.walks = append(p.walks, w)
		}
	}
	if !simplePolygon(poly) {
		return false
	}
	p.lanes = laneBands(arms)
	// Every road it takes over must be on it, or the ground shows through.
	for _, sp := range p.spans {
		for _, s := range sp.l.samples {
			if s.d > sp.from+0.5 && s.d < sp.to-0.5 && !inPoly(poly, s.e, s.n) {
				return false
			}
		}
	}
	p.poly = poly
	return true
}

// spanOf is the span whose road ends in the corners a, b.
func (p patch) spanOf(a, b [2]float64) *patchSpan {
	best, bd := 0, math.Inf(1)
	for k, sp := range p.spans {
		for _, d := range []float64{sp.from, sp.to} {
			s := sampleBetween(sp.l, d)
			if dd := math.Hypot(s.e-(a[0]+b[0])/2, s.n-(a[1]+b[1])/2); dd < bd {
				best, bd = k, dd
			}
		}
	}
	return &p.spans[best]
}

// simplePolygon tells whether poly is a polygon whose edges don't cross,
// wound counter-clockwise.
func simplePolygon(poly [][2]float64) bool {
	n := len(poly)
	if n < 3 {
		return false
	}
	area := 0.0
	for i := range n {
		j := (i + 1) % n
		area += poly[i][0]*poly[j][1] - poly[j][0]*poly[i][1]
	}
	if area <= 0 {
		return false
	}
	cross := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	for i := range n {
		a, b := poly[i], poly[(i+1)%n]
		for j := i + 2; j < n; j++ {
			if i == 0 && j == n-1 {
				continue // neighbours
			}
			c, d := poly[j], poly[(j+1)%n]
			if (cross(a, b, c) > 0) != (cross(a, b, d) > 0) && (cross(c, d, a) > 0) != (cross(c, d, b) > 0) {
				return false
			}
		}
	}
	return true
}

// carriagewayEdges are the left and right edge of a carriageway hw from
// the line at d (wider by wide on the left and right where it joins a
// road alongside), east and north: exactly where the road's mesh puts
// them (the same sums), so a junction's outline meets the road's end. In a bend
// tighter than the road is wide, the inner edge holds back as there.
func carriagewayEdges(line *smoothLine, d, hw float64, wide [2]float64) (left, right [2]float64) {
	x, y := line.at(d)
	de, dn, k := line.heading(d)
	a, b := -hw-wide[0], hw+wide[1]
	if inner := 0.9 / math.Abs(k); k != 0 && inner < hw {
		if k > 0 {
			a, b = math.Max(a, -inner), math.Max(b, -inner)
		} else {
			a, b = math.Min(a, inner), math.Min(b, inner)
		}
	}
	// As roadMesh: east x + rx*off with rx = dn; south -y + rz*off with
	// rz = de, so north is y - de*off.
	return [2]float64{x + dn*a, -(-y + de*a)}, [2]float64{x + dn*b, -(-y + de*b)}
}

// sampleBetween is the road at d, interpolated between its samples (the
// flags from the one before).
func sampleBetween(l *roadLine, d float64) sample {
	ss := l.samples
	i := sort.Search(len(ss), func(i int) bool { return ss[i].d > d })
	if i == 0 {
		return ss[0]
	}
	if i == len(ss) {
		return ss[len(ss)-1]
	}
	a, b := ss[i-1], ss[i]
	if b.d == a.d {
		return a
	}
	f := (d - a.d) / (b.d - a.d)
	lerp := func(x, y float64) float64 { return x + f*(y-x) }
	s := a
	s.d, s.e, s.n, s.ele = d, lerp(a.e, b.e), lerp(a.n, b.n), lerp(a.ele, b.ele)
	s.hw, s.edge, s.flat = lerp(a.hw, b.hw), lerp(a.edge, b.edge), lerp(a.flat, b.flat)
	return s
}

// cutRoads ends the roads at their junctions: a sample at every cut, so
// the road's mesh ends exactly there, and the samples within a patch's
// span marked (the junction draws that surface). Roads crossing a patch
// without being one of its own are marked where they lie on it.
func cutRoads(ls []*roadLine, ps []patch) {
	spans := map[*roadLine][]patchSpan{}
	var overlays []patch
	for _, p := range ps {
		if p.overlay {
			overlays = append(overlays, p)
			continue
		}
		for _, c := range p.cuts {
			insertSample(c.l, c.d)
		}
		for _, sp := range p.spans {
			spans[sp.l] = append(spans[sp.l], sp)
		}
	}
	for _, l := range ls {
		for i := range l.samples {
			s := &l.samples[i]
			for _, sp := range spans[l] {
				if s.d > sp.from+1e-6 && s.d < sp.to-1e-6 {
					s.onPatch = true
				}
			}
		}
		markPatches(l.samples, overlays)
	}
}

// insertSample adds a sample at d to l unless one is already there.
func insertSample(l *roadLine, d float64) {
	ss := l.samples
	i := sort.Search(len(ss), func(i int) bool { return ss[i].d >= d })
	if i < len(ss) && math.Abs(ss[i].d-d) < 1e-6 || i == 0 || i == len(ss) {
		return
	}
	s := sampleBetween(l, d)
	l.samples = append(ss[:i], append([]sample{s}, ss[i:]...)...)
}

// cutEdge tells whether the outline's edge a-b is (part of) a road's end.
func (p patch) cutEdge(a, b [2]float64) bool {
	for _, c := range p.cuts {
		if segDist(c.p0, c.p1, a[0], a[1]) < 1e-3 && segDist(c.p0, c.p1, b[0], b[1]) < 1e-3 {
			return true
		}
	}
	return false
}

// hasCorner tells whether q is a corner of the patch's outline.
func (p patch) hasCorner(q [2]float64) bool {
	return slices.Contains(p.poly, q)
}

// chainShape is a free edge's length along its kerb at each point, and the
// line walkM outward of it (the sidewalk's outer edge): the outline runs
// counter-clockwise, so outward is to the right; at the ends, as the
// roads' sidewalks end.
func chainShape(w walkChain) (at []float64, outer [][2]float64) {
	k := w.kerb
	at = make([]float64, len(k))
	for i := 1; i < len(k); i++ {
		at[i] = at[i-1] + math.Hypot(k[i][0]-k[i-1][0], k[i][1]-k[i-1][1])
	}
	outer = make([][2]float64, len(k))
	for i := range k {
		switch i {
		case 0:
			outer[i] = w.outA
			continue
		case len(k) - 1:
			outer[i] = w.outB
			continue
		}
		nx, ny := 0.0, 0.0
		for _, j := range []int{i - 1, i} {
			dx, dy := k[j+1][0]-k[j][0], k[j+1][1]-k[j][1]
			if l := math.Hypot(dx, dy); l > 0 {
				nx, ny = nx+dy/l, ny-dx/l
			}
		}
		l := math.Hypot(nx, ny)
		if l == 0 {
			outer[i] = k[i]
			continue
		}
		// A mitre, held to twice the width at sharp corners.
		dx, dy := k[i+1][0]-k[i][0], k[i+1][1]-k[i][1]
		cos := math.Abs((nx/l)*dy-(ny/l)*dx) / math.Max(1e-9, math.Hypot(dx, dy))
		f := walkM / math.Max(0.5, cos)
		outer[i] = [2]float64{k[i][0] + nx/l*f, k[i][1] + ny/l*f}
	}
	return at, outer
}

// junctionVerges are the verges along a junction's free edges: from its
// kerb, or its sidewalk's outer edge where it carries one, outward.
func junctionVerges(p patch, v *verger) gltf.Primitive {
	vp := gltf.Primitive{Material: v.mat}
	for _, w := range p.edges {
		k := w.kerb
		if len(k) < 2 {
			continue
		}
		at, outer := chainShape(w)
		total := at[len(k)-1]
		from, to := math.Inf(1), math.Inf(-1) // the sidewalk's stretch, if any
		switch {
		case w.fromA && w.fromB:
			from, to = 0, total
		case w.fromA:
			from, to = 0, total/2
		case w.fromB:
			from, to = total/2, total
		}
		var prev []vpt
		var prevIdx []uint32
		for i := range k {
			dx, dy := outer[i][0]-k[i][0], outer[i][1]-k[i][1]
			l := math.Hypot(dx, dy)
			if l == 0 {
				prev, prevIdx = nil, nil
				continue
			}
			ux, uy := dx/l, dy/l
			y := p.height(k[i][0], k[i][1])
			base, y0 := 0.0, y
			if at[i] >= from-1e-9 && at[i] <= to+1e-9 {
				base, y0 = l, y+kerbM
			}
			// Round a corner whose centre lies outward (the inside of a
			// rounded junction corner: the verge points into the bend), the
			// verge holds back short of the centre, as on a road's inside
			// bend: verges 9 m long folded over each other round a 6 m arc,
			// and the land under them showed through the fold (2.28 km).
			limit := math.Inf(1)
			if i > 0 && i+1 < len(k) {
				if r, cx, cy, ok := circumcircle(k[i-1], k[i], k[i+1]); ok && (cx-k[i][0])*ux+(cy-k[i][1])*uy > 0 {
					limit = r // to the centre: the fan closes the corner, no slivers
				}
			}
			// glTF: x east, z south.
			cur := v.across(k[i][0], -k[i][1], ux, -uy, base, limit, y0, -1)
			if prev != nil {
				prevIdx = vergeStrip(&vp, prev, cur, prevIdx)
			}
			prev = cur
		}
	}
	if len(vp.Indices) > 0 {
		smoothNormals(&vp)
	}
	return vp
}

// circumcircle is the circle through a, b and c: its radius and centre;
// false when they lie on a line.
func circumcircle(a, b, c [2]float64) (r, cx, cy float64, ok bool) {
	d := 2 * (a[0]*(b[1]-c[1]) + b[0]*(c[1]-a[1]) + c[0]*(a[1]-b[1]))
	if math.Abs(d) < 1e-9 {
		return 0, 0, 0, false
	}
	a2, b2, c2 := a[0]*a[0]+a[1]*a[1], b[0]*b[0]+b[1]*b[1], c[0]*c[0]+c[1]*c[1]
	cx = (a2*(b[1]-c[1]) + b2*(c[1]-a[1]) + c2*(a[1]-b[1])) / d
	cy = (a2*(c[0]-b[0]) + b2*(a[0]-c[0]) + c2*(b[0]-a[0])) / d
	return math.Hypot(a[0]-cx, a[1]-cy), cx, cy, true
}

// roundCorners rounds each corner between two roads of a junction's
// outline (owner, 2026-10-09: a right turn was a sharp corner; real
// junctions are rounded for turning traffic) with an arc of cornerM, or as
// large as the roads' ends allow. poly runs, per road, from its right end
// corner (rightAt) to its left one (leftAt), then in along its left edge
// and out along the next road's right edge to that road's right end
// corner: the corner lies between those two edges.
func roundCorners(poly [][2]float64, rightAt, leftAt []int) ([][2]float64, []int, []int) {
	n := len(rightAt)
	var out [][2]float64
	newRight, newLeft := make([]int, n), make([]int, n)
	// Chains from each road's left end corner to the next's right one.
	chains := make([][][2]float64, n)
	for k := range n {
		next := (k + 1) % n
		for i := leftAt[k]; ; i = (i + 1) % len(poly) {
			chains[k] = append(chains[k], poly[i])
			if i == rightAt[next] {
				break
			}
		}
	}
	for k := range n {
		next := (k + 1) % n
		c := fillet(chains[k])
		// The chain ends at the next road's right corner, which starts
		// the next road's end edge: its left corner begins the next chain.
		start := len(out)
		out = append(out, c...)
		newLeft[k] = start
		newRight[next] = len(out) - 1
	}
	return out, newRight, newLeft
}

// fillet rounds a chain's corner: the chain runs in along one road's edge
// from p[0] and out along another's to p[len-1]; the edges' lines through
// their ends meet at the corner, rounded with an arc tangent to both.
// The chain comes back as it was where there is no corner to round (the
// roads nearly in line, or the corner off the chain).
func fillet(p [][2]float64) [][2]float64 {
	m := len(p)
	if m < 4 {
		return p
	}
	unit := func(a, b [2]float64) ([2]float64, float64) {
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		if l == 0 {
			return [2]float64{}, 0
		}
		return [2]float64{dx / l, dy / l}, l
	}
	// The edges' directions at the ends, inwards.
	da, la := unit(p[0], p[1])
	db, lb := unit(p[m-1], p[m-2])
	if la == 0 || lb == 0 {
		return p
	}
	// The corner: p[0] + s·da = p[m-1] + t·db.
	den := da[0]*db[1] - da[1]*db[0]
	if math.Abs(den) < 1e-6 {
		return p
	}
	wx, wy := p[m-1][0]-p[0][0], p[m-1][1]-p[0][1]
	sa := (wx*db[1] - wy*db[0]) / den
	sb := (wx*da[1] - wy*da[0]) / den
	if sa <= 0 || sb <= 0 {
		return p
	}
	c := [2]float64{p[0][0] + sa*da[0], p[0][1] + sa*da[1]}
	// The angle at the corner between the ways back to each end.
	ua, ub := [2]float64{-da[0], -da[1]}, [2]float64{-db[0], -db[1]}
	alpha := math.Acos(math.Max(-1, math.Min(1, ua[0]*ub[0]+ua[1]*ub[1])))
	if alpha > 160*math.Pi/180 || alpha < 20*math.Pi/180 {
		return p
	}
	// The corner near the chain: an apron may have cut it off, but not by
	// more than the roads' ends reach.
	near := math.Inf(1)
	for _, q := range p {
		near = math.Min(near, math.Hypot(q[0]-c[0], q[1]-c[1]))
	}
	if near > 0.75*math.Min(sa, sb) {
		return p
	}
	// As far out as an apron reached (the arc never takes paving away),
	// at least cornerM's arc, within the roads' ends.
	reach := 0.0
	on := func(q [2]float64, u [2]float64) bool {
		return math.Abs((q[0]-c[0])*u[1]-(q[1]-c[1])*u[0]) < 1
	}
	for _, q := range p[1 : m-1] {
		if !on(q, ua) && !on(q, ub) {
			reach = math.Max(reach, math.Max((q[0]-c[0])*ua[0]+(q[1]-c[1])*ua[1], (q[0]-c[0])*ub[0]+(q[1]-c[1])*ub[1]))
		}
	}
	t := math.Min(math.Max(cornerM/math.Tan(alpha/2), reach), math.Min(sa, sb)-0.3)
	if t < 0.5 {
		return p
	}
	r := t * math.Tan(alpha/2)
	t1 := [2]float64{c[0] + ua[0]*t, c[1] + ua[1]*t}
	t2 := [2]float64{c[0] + ub[0]*t, c[1] + ub[1]*t}
	bx, by := ua[0]+ub[0], ua[1]+ub[1]
	bl := math.Hypot(bx, by)
	h := r / math.Sin(alpha/2)
	o := [2]float64{c[0] + bx/bl*h, c[1] + by/bl*h}
	a1, a2 := math.Atan2(t1[1]-o[1], t1[0]-o[0]), math.Atan2(t2[1]-o[1], t2[0]-o[0])
	sweep := math.Remainder(a2-a1, 2*math.Pi)
	steps := max(2, int(math.Ceil(math.Abs(sweep)/(10*math.Pi/180))))
	// Keep the chain's points further from the corner than t, either side.
	out := [][2]float64{p[0]}
	for _, q := range p[1 : m-1] {
		if (q[0]-c[0])*ua[0]+(q[1]-c[1])*ua[1] > t+0.1 && math.Abs((q[0]-c[0])*ua[1]-(q[1]-c[1])*ua[0]) < 1 {
			out = append(out, q)
		}
	}
	for k := 0; k <= steps; k++ {
		ang := a1 + sweep*float64(k)/float64(steps)
		out = append(out, [2]float64{o[0] + r*math.Cos(ang), o[1] + r*math.Sin(ang)})
	}
	for _, q := range p[1 : m-1] {
		if (q[0]-c[0])*ub[0]+(q[1]-c[1])*ub[1] > t+0.1 && math.Abs((q[0]-c[0])*ub[1]-(q[1]-c[1])*ub[0]) < 1 {
			out = append(out, q)
		}
	}
	return append(out, p[m-1])
}
