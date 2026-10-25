package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Splitter islands (owner, 2026-10-24: at the Velp loop's roundabouts the
// strip between an arm's two carriageways was grass reaching into the
// ring, a hedge on it up to the ring's edge, and at 2.47 km a sidewalk
// round it ending ragged at the ring). Where the map draws a road as two
// one-way carriageways side by side, traffic on them opposite (an arm of
// a roundabout split round an island, a dual road whose carriageways part
// at a junction), the strip between them is a raised island with a kerb
// round it: from where the strip is splitNoseW wide, or a junction's or
// roundabout's edge, to where it widens past splitMaxW or the roads part.
// Its noses are rounded. Its top is paved (grey concrete pavers) near its
// noses and where it is narrow, grass where it is wider (owner, the same
// day). Only where the map splits the road: an arm drawn as one two-way
// road gets none (that would be a guess, as some have none). CROW's
// splitter islands (middengeleiders) are at least 1 m wide, raised
// behind a kerb; the nose rounded at 0.5-1 m.
const (
	splitNoseW     = 1.2  // the island starts where the strip is this wide
	splitMaxW      = 8.0  // wider, the strip is a median of land (as hedgeMaxM)
	splitPavedW    = 2.5  // narrower, the island is paved; wider, grass
	splitPavedM    = 4.0  // and paved this far from either nose
	splitNoseR     = 0.75 // a nose is round, this radius (or half the strip where narrower)
	splitTaperK    = 0.25 // and the island narrows to it at 1:4 a side, bullet-shaped (the asphalt beside it the strip's)
	splitRingBackM = 1.0  // a nose stops this far short of a roundabout's edge (circulating traffic sweeps past it)
	splitMinM      = 8.0  // shorter islands are left out (a scrap between links and carriageways)
	splitOverlapM  = 0.10 // the kerb this far over the carriageway's edge, so no land shows at it
	splitKerbTopM  = 0.15 // the kerbstones' top along the edge (a 13/15 stoepband), the top inside it
	splitLevelM    = 0.5  // carriageways further apart in height are no pair
	splitAlignCos  = 0.7  // alongside: within 45° (an arm's carriageways flare at the ring)
	splitHedgeM    = 15.0 // a hedge on a grass island keeps this far from its noses (sight at the ring)
	splitStepM     = 0.1  // the search for a nose's place between samples
	splitClearM    = 0.75 // an island keeps this far from a road across or between
	splitStripM    = 2.0  // the asphalt under an island goes on this far past a nose
	splitStripGapM = 0.05 // while the strip is this wide
	splitArcSteps  = 6    // segments per rounded corner
)

// splitSection is a cross-section of an island: its edges at the two
// carriageways (a on the line that found it), the road's surface there,
// the strip's width and the distance along the line.
type splitSection struct {
	a, b   [2]float64
	ya, yb float64
	gap    float64
	d      float64
}

// splitIsland is an island: its outline (east, north, counter-clockwise)
// and the road's surface under each outline point, its top kerbM above.
type splitIsland struct {
	pts    [][2]float64   // its points: the outline's and the noses' middles
	ys     []float64      // the road's surface under each
	ring   []int          // the outline, counter-clockwise, as indices into pts
	poly   [][2]float64   // the outline's points
	tris   [][3]int       // its top
	paved  []bool         // per triangle of the top: paved, else grass
	secs   []splitSection // the body's sections
	strip  []splitSection // the strip under it, ends square
	d0, d1 float64        // its ends along the line that found it
	lo, hi [2]float64
}

// splitterIslands finds the islands between the lines' carriageways;
// patches are the junctions and roundabouts, whose edges end them.
func splitterIslands(ls []*roadLine, patches []patch) []splitIsland {
	const cellM = 10.0
	type ref struct{ l, i int }
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	use := func(l *roadLine) bool { return !l.route && !l.motorway && len(l.samples) >= 2 }
	for li, l := range ls {
		if !use(l) {
			continue
		}
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			grid[k] = append(grid[k], ref{li, i})
		}
	}
	type pref struct {
		poly   [][2]float64
		lo, hi [2]float64
	}
	var rings []pref
	for _, p := range patches {
		if p.rb == nil {
			continue // a junction ends an island only where a road crosses the strip (clear)
		}
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, q := range p.poly {
			lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[1])}
			hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[1])}
		}
		rings = append(rings, pref{p.poly, lo, hi})
	}
	in := func(ps []pref, e, n float64) bool {
		for _, p := range ps {
			if e >= p.lo[0] && e <= p.hi[0] && n >= p.lo[1] && n <= p.hi[1] && inPoly(p.poly, e, n) {
				return true
			}
		}
		return false
	}
	inRing := func(e, n float64) bool { return in(rings, e, n) }
	// Every line by cell, the course's own ways too, for the roads crossing
	// a strip (a side road across the median: the island breaks there).
	all := map[[2]int][]ref{}
	for li, l := range ls {
		for i := range l.samples {
			k := key(l.samples[i].e, l.samples[i].n)
			all[k] = append(all[k], ref{li, i})
		}
	}
	// clear says whether the strip from a to b is free of every other
	// line's carriageway by splitClearM (one between them, or a road
	// across: no island there; checked every half metre, as a road across
	// may run at a slant).
	clear := func(a, b [2]float64, la, lb int, de, dn float64) bool {
		steps := max(4, int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1])/0.5)))
		for k := 0; k <= steps; k++ {
			t := float64(k) / float64(steps)
			e, n := a[0]+t*(b[0]-a[0]), a[1]+t*(b[1]-a[1])
			c := key(e, n)
			for gx := c[0] - 1; gx <= c[0]+1; gx++ {
				for gy := c[1] - 1; gy <= c[1]+1; gy++ {
					for _, r := range all[[2]int{gx, gy}] {
						if r.i+1 >= len(ls[r.l].samples) {
							continue
						}
						x, y := ls[r.l].samples[r.i], ls[r.l].samples[r.i+1]
						l := math.Hypot(y.e-x.e, y.n-x.n)
						if l == 0 {
							continue
						}
						margin := splitClearM
						if math.Abs((y.e-x.e)*de+(y.n-x.n)*dn)/l > splitAlignCos {
							// Along the strip: the pair's own lines are its
							// sides; another (a lane between them) only
							// counts in the strip's middle, as a branch
							// forking off beside one grazes its edge.
							if r.l == la || r.l == lb || t < 0.25 || t > 0.75 {
								continue
							}
							margin = 0
						}
						if segDist([2]float64{x.e, x.n}, [2]float64{y.e, y.n}, e, n) < math.Max(x.hw, y.hw)+margin {
							return false
						}
					}
				}
			}
		}
		return true
	}
	dir := func(ss []sample, i int) (float64, float64) {
		a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
		l := math.Hypot(b.e-a.e, b.n-a.n)
		if l == 0 {
			return 0, 0
		}
		return (b.e - a.e) / l, (b.n - a.n) / l
	}

	// section is the island's cross-section at d along line A on side
	// (0 left), with the line across it; ok false where there is none.
	section := func(ai, side int, d float64, intoPatch bool, minGap float64) (splitSection, int, bool) {
		A := ls[ai]
		ss := A.samples
		k := sort.Search(len(ss), func(i int) bool { return ss[i].d >= d })
		if k == len(ss) {
			k = len(ss) - 1
		}
		if k > 0 && ss[k].d > d {
			k--
		}
		k2 := min(k+1, len(ss)-1)
		p, q := ss[k], ss[k2]
		f := 0.0
		if q.d > p.d {
			f = math.Max(0, math.Min(1, (d-p.d)/(q.d-p.d)))
		}
		if p.aloft || q.aloft || p.tunnel || q.tunnel || p.walk[side] || q.walk[side] || p.again || q.again {
			return splitSection{}, -1, false
		}
		ow := p.oneway
		if p.onPatch {
			ow = q.oneway
		}
		if ow == 0 {
			return splitSection{}, -1, false
		}
		e, n := p.e+f*(q.e-p.e), p.n+f*(q.n-p.n)
		hw, ele := p.hw+f*(q.hw-p.hw), p.ele+f*(q.ele-p.ele)
		de, dn := dir(ss, k)
		if de == 0 && dn == 0 {
			return splitSection{}, -1, false
		}
		sign := 1.0 // right: (dn, -de)
		if side == 0 {
			sign = -1
		}
		re, rn := sign*dn, -sign*de
		edge := [2]float64{e + re*hw, n + rn*hw}
		bestDist, bestL := math.Inf(1), -1
		var best splitSection
		c := key(edge[0], edge[1])
		for gx := c[0] - 2; gx <= c[0]+2; gx++ {
			for gy := c[1] - 2; gy <= c[1]+2; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					if r.l == ai || r.i+1 >= len(ls[r.l].samples) {
						continue
					}
					B := ls[r.l]
					x, y := B.samples[r.i], B.samples[r.i+1]
					if x.aloft || y.aloft || x.tunnel || y.tunnel || x.again || y.again {
						continue
					}
					be, bn := y.e-x.e, y.n-x.n
					bl := math.Hypot(be, bn)
					if bl == 0 {
						continue
					}
					be, bn = be/bl, bn/bl
					if math.Abs(be*de+bn*dn) < splitAlignCos {
						continue // not alongside
					}
					bow := x.oneway
					if x.onPatch {
						bow = y.oneway
					}
					if bow == 0 || (be*de+bn*dn)*float64(bow)*float64(ow) >= 0 {
						continue // traffic the same way: not a split road
					}
					g := math.Max(0, math.Min(1, ((edge[0]-x.e)*be+(edge[1]-x.n)*bn)/bl))
					pe, pn := x.e+g*bl*be, x.n+g*bl*bn
					// B lies on side: its line beyond A's edge.
					if (pe-e)*re+(pn-n)*rn <= hw {
						continue
					}
					bhw := x.hw + g*(y.hw-x.hw)
					dist := math.Hypot(edge[0]-pe, edge[1]-pn)
					gap := dist - bhw
					if gap >= bestDist || dist == 0 {
						continue
					}
					// B's side facing A: its sidewalk there makes the strip
					// the sidewalk's verge, not an island.
					bs := 1
					if be*(edge[1]-pn)-bn*(edge[0]-pe) > 0 {
						bs = 0
					}
					bele := x.ele + g*(y.ele-x.ele)
					if x.walk[bs] || y.walk[bs] || math.Abs(bele-ele) > splitLevelM {
						continue
					}
					ue, un := (edge[0]-pe)/dist, (edge[1]-pn)/dist
					bestDist, bestL = gap, r.l
					best = splitSection{
						a:  [2]float64{edge[0] - re*splitOverlapM, edge[1] - rn*splitOverlapM},
						b:  [2]float64{pe + ue*(bhw-splitOverlapM), pn + un*(bhw-splitOverlapM)},
						ya: ele, yb: bele, gap: gap, d: d,
					}
				}
			}
		}
		if bestL < 0 || best.gap < minGap || best.gap > splitMaxW {
			return splitSection{}, -1, false
		}
		me, mn := (best.a[0]+best.b[0])/2, (best.a[1]+best.b[1])/2
		if !intoPatch && (inRing(me, mn) || !clear(best.a, best.b, ai, bestL, de, dn)) {
			return splitSection{}, -1, false
		}
		return best, bestL, true
	}

	// The runs: per line and side, the sections in a row with the same
	// line across, out to the island's ends between samples, with the
	// strip under it on past its ends (pre, post).
	var runs []splitRun
	for ai, A := range ls {
		if !use(A) {
			continue
		}
		ss := A.samples
		for side := range 2 {
			at := func(d float64) (splitSection, int, bool) { return section(ai, side, d, false, splitNoseW) }
			// out walks from d0 by step (±splitStepM) to limit while the
			// island goes on: its furthest section, and then the strip's
			// sections past it (a step into a junction, a strip narrowing).
			// The line across may change on the way (a carriageway's line
			// ending at a junction's node, the next beginning there) while
			// the strip's far edge carries on.
			out := func(d0, step, limit float64, bl int) (end *splitSection, strip []splitSection) {
				prev, _, _ := section(ai, side, d0, true, 0)
				same := func(s splitSection, l int) bool {
					return l == bl || math.Hypot(s.b[0]-prev.b[0], s.b[1]-prev.b[1]) < 2*splitStepM+0.2
				}
				d := d0 + step
				for ; (step < 0 && d > limit) || (step > 0 && d < limit); d += step {
					s, l, ok := at(d)
					if !ok || !same(s, l) {
						break
					}
					end, prev, bl = &s, s, l
				}
				k := 0.0
				for ; k < splitStripM; k += splitStepM {
					s, l, ok := section(ai, side, d, true, splitStripGapM)
					if !ok || !same(s, l) {
						break
					}
					strip = append(strip, s)
					prev, bl = s, l
					d += step
				}
				// Where the line across stops while the strip is still open
				// (it ends at a junction's node, a road beyond taking its
				// place), the strip on straight for the rest: else a scrap
				// of the median is left between it and the junction, with
				// slivers round it (the Velp loop's 1.4 km crossroads).
				if n := len(strip); n >= 2 && prev.gap > splitNoseW {
					p, q := strip[n-2], strip[n-1]
					m0 := [2]float64{(p.a[0] + p.b[0]) / 2, (p.a[1] + p.b[1]) / 2}
					m1 := [2]float64{(q.a[0] + q.b[0]) / 2, (q.a[1] + q.b[1]) / 2}
					ue, un := m1[0]-m0[0], m1[1]-m0[1]
					if ul := math.Hypot(ue, un); ul > 0 {
						ue, un = ue/ul, un/ul
						for x := splitStepM; k+x <= splitStripM; x += splitStepM {
							s := q
							s.a = [2]float64{q.a[0] + ue*x, q.a[1] + un*x}
							s.b = [2]float64{q.b[0] + ue*x, q.b[1] + un*x}
							s.d = q.d + step*x/splitStepM
							if inRing((s.a[0]+s.b[0])/2, (s.a[1]+s.b[1])/2) {
								break
							}
							strip = append(strip, s)
						}
					}
				}
				return end, strip
			}
			i := 0
			for i < len(ss) {
				sec, bl, ok := at(ss[i].d)
				if !ok {
					i++
					continue
				}
				r := splitRun{secs: []splitSection{sec}}
				bl0 := bl // the line across at the run's start
				j := i + 1
				for ; j < len(ss); j++ {
					s, l, ok := at(ss[j].d)
					last := r.secs[len(r.secs)-1]
					if !ok || l != bl && math.Hypot(s.b[0]-last.b[0], s.b[1]-last.b[1]) > fineStep+splitJoinM {
						break
					}
					r.secs, bl = append(r.secs, s), l
				}
				lo, hi := ss[i].d-fineStep, ss[j-1].d+fineStep
				if i > 0 {
					lo = ss[i-1].d
				}
				if j < len(ss) {
					hi = ss[j].d
				}
				end, strip := out(ss[i].d, -splitStepM, lo, bl0)
				if end != nil {
					r.secs = append([]splitSection{*end}, r.secs...)
				}
				for _, s := range strip {
					r.pre = append([]splitSection{s}, r.pre...)
				}
				end, r.post = out(ss[j-1].d, splitStepM, hi, bl)
				if end != nil {
					r.secs = append(r.secs, *end)
				}
				runs = append(runs, r)
				i = j
			}
		}
	}
	runs = mergeRuns(dedupeRuns(runs))
	var isls []splitIsland
	mid := func(s splitSection) [2]float64 { return [2]float64{(s.a[0] + s.b[0]) / 2, (s.a[1] + s.b[1]) / 2} }
	for _, r := range runs {
		// At a roundabout the nose stands back from the ring's edge; the
		// strip's asphalt goes on under the island to it.
		body := r.secs
		d0, d1 := body[0].d, body[len(body)-1].d
		// (A ring ends a run where the strip's next step lies in it.)
		if len(r.pre) > 0 {
			if m := mid(r.pre[len(r.pre)-1]); inRing(m[0], m[1]) {
				d0 += splitRingBackM
			}
		}
		if len(r.post) > 0 {
			if m := mid(r.post[0]); inRing(m[0], m[1]) {
				d1 -= splitRingBackM
			}
		}
		if d1-d0 < splitMinM {
			continue
		}
		body = sectionsBetween(body, d0, d1)
		if isl, ok := shapeIsland(body); ok {
			isl.strip = append(append(append([]splitSection{}, r.pre...), r.secs...), r.post...)
			isls = append(isls, isl)
		}
	}
	return isls
}

// sectionsBetween are secs from d0 to d1 along them, the ends
// interpolated.
func sectionsBetween(secs []splitSection, d0, d1 float64) []splitSection {
	at := func(d float64) splitSection {
		k := sort.Search(len(secs), func(i int) bool { return secs[i].d >= d })
		if k == 0 {
			return secs[0]
		}
		if k == len(secs) {
			return secs[len(secs)-1]
		}
		p, q := secs[k-1], secs[k]
		f := (d - p.d) / (q.d - p.d)
		lerp := func(x, y float64) float64 { return x + f*(y-x) }
		return splitSection{
			a:  [2]float64{lerp(p.a[0], q.a[0]), lerp(p.a[1], q.a[1])},
			b:  [2]float64{lerp(p.b[0], q.b[0]), lerp(p.b[1], q.b[1])},
			ya: lerp(p.ya, q.ya), yb: lerp(p.yb, q.yb), gap: lerp(p.gap, q.gap), d: d,
		}
	}
	out := []splitSection{at(d0)}
	for _, s := range secs {
		if s.d > d0+0.05 && s.d < d1-0.05 {
			out = append(out, s)
		}
	}
	return append(out, at(d1))
}

// splitRun is an island's sections in a row, before it is shaped; pre
// and post the strip's under it past its ends.
type splitRun struct {
	secs, pre, post []splitSection
}

func (r splitRun) length() float64 { return r.secs[len(r.secs)-1].d - r.secs[0].d }

// stripPoly is the strip a run covers: along its a side and back along b.
func (r splitRun) stripPoly() [][2]float64 {
	var out [][2]float64
	for _, s := range r.secs {
		out = append(out, s.a)
	}
	for k := len(r.secs) - 1; k >= 0; k-- {
		out = append(out, r.secs[k].b)
	}
	return out
}

// dedupeRuns drops the parts of runs another, longer run already covers
// (a pair found from either line where the lines' ends differ).
func dedupeRuns(runs []splitRun) []splitRun {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].length() > runs[j].length() })
	var kept []splitRun
	for _, r := range runs {
		var piece []splitSection
		var pieces [][]splitSection
		for _, s := range r.secs {
			m := [2]float64{(s.a[0] + s.b[0]) / 2, (s.a[1] + s.b[1]) / 2}
			covered := false
			for _, k := range kept {
				if inPoly(k.stripPoly(), m[0], m[1]) {
					covered = true
					break
				}
			}
			if covered {
				if len(piece) > 0 {
					pieces = append(pieces, piece)
				}
				piece = nil
				continue
			}
			piece = append(piece, s)
		}
		if len(piece) > 0 {
			pieces = append(pieces, piece)
		}
		for _, p := range pieces {
			if len(p) < 2 {
				continue
			}
			n := splitRun{secs: p}
			if p[0].d == r.secs[0].d {
				n.pre = r.pre
			}
			if p[len(p)-1].d == r.secs[len(r.secs)-1].d {
				n.post = r.post
			}
			kept = append(kept, n)
		}
	}
	return kept
}

// splitJoinM: runs whose ends lie this close (both corners) are one island
// (a carriageway's line ending at a node of the map, the next beginning).
const splitJoinM = 0.6

// mergeRuns joins runs that meet end to end into one, its distances
// measured along its middle.
func mergeRuns(runs []splitRun) []splitRun {
	near := func(p, q [2]float64) bool { return math.Hypot(p[0]-q[0], p[1]-q[1]) < splitJoinM }
	flip := func(r splitRun) splitRun {
		n := splitRun{pre: rev(r.post), post: rev(r.pre)}
		n.secs = rev(r.secs)
		return n
	}
	swap := func(r splitRun) splitRun {
		for _, ss := range [][]splitSection{r.secs, r.pre, r.post} {
			for k := range ss {
				ss[k].a, ss[k].b, ss[k].ya, ss[k].yb = ss[k].b, ss[k].a, ss[k].yb, ss[k].ya
			}
		}
		return r
	}
	for {
		joined := false
		for i := 0; i < len(runs) && !joined; i++ {
			for j := 0; j < len(runs) && !joined; j++ {
				if i == j {
					continue
				}
				for _, a := range []splitRun{runs[i], flip(runs[i])} {
					if joined {
						break
					}
					e := a.secs[len(a.secs)-1]
					for _, c := range []splitRun{runs[j], flip(runs[j])} {
						s := c.secs[0]
						switch {
						case near(e.a, s.a) && near(e.b, s.b):
						case near(e.a, s.b) && near(e.b, s.a):
							c = swap(c)
						default:
							continue
						}
						m := splitRun{pre: a.pre, post: c.post}
						m.secs = append(append([]splitSection{}, a.secs...), c.secs[1:]...)
						runs[i] = m
						runs = append(runs[:j], runs[j+1:]...)
						joined = true
						break
					}
				}
			}
		}
		if !joined {
			break
		}
	}
	// Distances along each run's middle, from its first section.
	for k := range runs {
		r := &runs[k]
		mid := func(s splitSection) [2]float64 { return [2]float64{(s.a[0] + s.b[0]) / 2, (s.a[1] + s.b[1]) / 2} }
		d := 0.0
		for i := range r.secs {
			if i > 0 {
				p, q := mid(r.secs[i-1]), mid(r.secs[i])
				d += math.Hypot(q[0]-p[0], q[1]-p[1])
			}
			r.secs[i].d = d
		}
	}
	return runs
}

func rev(ss []splitSection) []splitSection {
	out := make([]splitSection, len(ss))
	for i, s := range ss {
		out[len(ss)-1-i] = s
	}
	return out
}

// shapeIsland is the island through secs (in order along the line that
// found it, on its side): rounded noses at both ends, the outline, its
// top's triangles and which of them are paved.
func shapeIsland(in []splitSection) (splitIsland, bool) {
	n := len(in)
	if n < 2 {
		return splitIsland{}, false
	}
	// Bullet-shaped: no wider than splitNoseR plus splitTaperK a metre
	// past the nose, narrowed about the strip's middle.
	secs := make([]splitSection, n)
	for k, s := range in {
		// (From where the round nose ends: it meets the body there at its
		// own width, else the body steps out beside it.)
		x := math.Min(s.d-in[0].d, in[n-1].d-s.d)
		h := splitNoseR + splitTaperK*math.Max(0, x-splitNoseR)
		w := math.Hypot(s.b[0]-s.a[0], s.b[1]-s.a[1])
		if w > 2*h {
			t0, t1 := 0.5-h/w, 0.5+h/w
			a, b, ya, yb := s.a, s.b, s.ya, s.yb
			s.a = [2]float64{a[0] + t0*(b[0]-a[0]), a[1] + t0*(b[1]-a[1])}
			s.b = [2]float64{a[0] + t1*(b[0]-a[0]), a[1] + t1*(b[1]-a[1])}
			s.ya, s.yb = ya+t0*(yb-ya), ya+t1*(yb-ya)
			s.gap = 2 * h // the island's width now, not the strip's
		}
		secs[k] = s
	}
	mid := func(s splitSection) [2]float64 { return [2]float64{(s.a[0] + s.b[0]) / 2, (s.a[1] + s.b[1]) / 2} }
	at := func(d float64) splitSection {
		k := sort.Search(n, func(i int) bool { return secs[i].d >= d })
		if k == 0 {
			return secs[0]
		}
		if k == n {
			return secs[n-1]
		}
		p, q := secs[k-1], secs[k]
		f := (d - p.d) / (q.d - p.d)
		lerp := func(x, y float64) float64 { return x + f*(y-x) }
		return splitSection{
			a:  [2]float64{lerp(p.a[0], q.a[0]), lerp(p.a[1], q.a[1])},
			b:  [2]float64{lerp(p.b[0], q.b[0]), lerp(p.b[1], q.b[1])},
			ya: lerp(p.ya, q.ya), yb: lerp(p.yb, q.yb), gap: lerp(p.gap, q.gap), d: d,
		}
	}
	type pt struct {
		q [2]float64
		y float64
	}
	// nose is the rounded end at e, its inward neighbour in: points from
	// its a side to its b side, the first and last where the body starts.
	nose := func(e, in splitSection) ([]pt, float64) {
		me, mi := mid(e), mid(in)
		ue, un := me[0]-mi[0], me[1]-mi[1]
		l := math.Hypot(ue, un)
		if l == 0 {
			return nil, 0
		}
		ue, un = ue/l, un/l
		ae, an := e.b[0]-e.a[0], e.b[1]-e.a[1]
		w := math.Hypot(ae, an)
		if w == 0 {
			return nil, 0
		}
		ae, an = ae/w, an/w
		rc := math.Min(splitNoseR, w/2)
		y := func(q [2]float64) float64 {
			t := math.Max(0, math.Min(1, ((q[0]-e.a[0])*ae+(q[1]-e.a[1])*an)/w))
			return e.ya + t*(e.yb-e.ya)
		}
		var out []pt
		ca := [2]float64{e.a[0] - ue*rc + ae*rc, e.a[1] - un*rc + an*rc}
		for k := 0; k <= splitArcSteps; k++ {
			th := math.Pi / 2 * float64(k) / splitArcSteps
			q := [2]float64{ca[0] + rc*(-math.Cos(th)*ae+math.Sin(th)*ue), ca[1] + rc*(-math.Cos(th)*an+math.Sin(th)*un)}
			out = append(out, pt{q, y(q)})
		}
		cb := [2]float64{e.b[0] - ue*rc - ae*rc, e.b[1] - un*rc - an*rc}
		for k := 0; k <= splitArcSteps; k++ {
			th := math.Pi / 2 * float64(k) / splitArcSteps
			q := [2]float64{cb[0] + rc*(math.Cos(th)*ue+math.Sin(th)*ae), cb[1] + rc*(math.Cos(th)*un+math.Sin(th)*an)}
			if k == 0 && math.Hypot(q[0]-out[len(out)-1].q[0], q[1]-out[len(out)-1].q[1]) < 1e-6 {
				continue // a semicircle: the corners meet
			}
			out = append(out, pt{q, y(q)})
		}
		return out, rc
	}
	d0, d1 := secs[0].d, secs[n-1].d
	start, r0 := nose(secs[0], at(d0+math.Min(1, (d1-d0)/2)))
	end, r1 := nose(secs[n-1], at(d1-math.Min(1, (d1-d0)/2)))
	if start == nil || end == nil || d1-d0 < r0+r1+0.1 {
		return splitIsland{}, false
	}
	// The body between the noses: their inner ends, and the sections
	// between.
	body := []splitSection{at(d0 + r0)}
	for _, s := range secs {
		if s.d > d0+r0+0.05 && s.d < d1-r1-0.05 {
			body = append(body, s)
		}
	}
	body = append(body, at(d1-r1))
	// The noses' first and last points are the body's ends.
	body[0].a, body[0].b = start[0].q, start[len(start)-1].q
	body[len(body)-1].a, body[len(body)-1].b = end[0].q, end[len(end)-1].q

	var isl splitIsland
	isl.secs, isl.strip = body, secs
	add := func(q [2]float64, y float64) int {
		isl.pts = append(isl.pts, q)
		isl.ys = append(isl.ys, y)
		return len(isl.pts) - 1
	}
	// The start's nose from its b side to its a side, the a side, the
	// end's nose from a to b, the b side.
	var startIx, endIx []int
	for k := len(start) - 1; k >= 0; k-- {
		startIx = append(startIx, add(start[k].q, start[k].y))
	}
	aIx := []int{startIx[len(startIx)-1]}
	for _, s := range body[1 : len(body)-1] {
		aIx = append(aIx, add(s.a, s.ya))
	}
	for _, p := range end {
		endIx = append(endIx, add(p.q, p.y))
	}
	aIx = append(aIx, endIx[0])
	bIx := []int{startIx[0]}
	for _, s := range body[1 : len(body)-1] {
		bIx = append(bIx, add(s.b, s.yb))
	}
	bIx = append(bIx, endIx[len(endIx)-1])
	isl.ring = append(append(append([]int{}, startIx...), aIx[1:len(aIx)-1]...), endIx...)
	for k := len(bIx) - 2; k >= 1; k-- {
		isl.ring = append(isl.ring, bIx[k])
	}
	outline := make([][2]float64, len(isl.ring))
	for k, ix := range isl.ring {
		outline[k] = isl.pts[ix]
	}
	if area2(outline) < 0 {
		for x, y := 0, len(isl.ring)-1; x < y; x, y = x+1, y-1 {
			isl.ring[x], isl.ring[y] = isl.ring[y], isl.ring[x]
			outline[x], outline[y] = outline[y], outline[x]
		}
	}
	isl.poly = outline

	pavedAt := func(s splitSection) bool {
		return s.gap < splitPavedW || s.d-d0 < splitPavedM || d1-s.d < splitPavedM
	}
	// The top: the body's quads, each nose a fan from its middle.
	for k := 0; k+1 < len(body); k++ {
		p := pavedAt(body[k]) || pavedAt(body[k+1])
		isl.tris = append(isl.tris, [3]int{aIx[k], aIx[k+1], bIx[k+1]}, [3]int{aIx[k], bIx[k+1], bIx[k]})
		isl.paved = append(isl.paved, p, p)
	}
	fan := func(ix []int) {
		var c [2]float64
		var y float64
		for _, k := range ix {
			c[0], c[1], y = c[0]+isl.pts[k][0], c[1]+isl.pts[k][1], y+isl.ys[k]
		}
		m := float64(len(ix))
		ci := add([2]float64{c[0] / m, c[1] / m}, y/m)
		for k := range ix {
			isl.tris = append(isl.tris, [3]int{ci, ix[k], ix[(k+1)%len(ix)]})
			isl.paved = append(isl.paved, true)
		}
	}
	fan(startIx)
	fan(endIx)
	isl.d0, isl.d1 = d0, d1
	isl.lo, isl.hi = [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, q := range isl.poly {
		isl.lo = [2]float64{math.Min(isl.lo[0], q[0]), math.Min(isl.lo[1], q[1])}
		isl.hi = [2]float64{math.Max(isl.hi[0], q[0]), math.Max(isl.hi[1], q[1])}
	}
	return isl, true
}

// islandPrims are the islands' meshes by terrain chunk (of their middle):
// the strip under each in asphalt, ends square, just under the roads
// round it (so no land is left in the corners its noses round off); the
// top kerbM over the road, grass (in the roundabout island's material,
// coloured in its vertices) or paved (grey concrete pavers, the renderer's
// pattern); and the kerb round it from its top down into the ground.
// paving gives the pavers' material (made only when an island needs it).
func islandPrims(isls []splitIsland, chunkM float64, asphalt, kerb, grass int, paving func() int) map[[2]int][]gltf.Primitive {
	out := map[[2]int][]gltf.Primitive{}
	up := [3]float64{0, 1, 0}
	g := landColors[scenery.LandNone]
	for _, isl := range isls {
		k := [2]int{int(math.Floor((isl.lo[0] + isl.hi[0]) / 2 / chunkM)), int(math.Floor((isl.lo[1] + isl.hi[1]) / 2 / chunkM))}
		base := gltf.Primitive{Material: asphalt}
		for j, sc := range isl.strip {
			for _, q := range []struct {
				p [2]float64
				y float64
			}{{sc.a, sc.ya}, {sc.b, sc.yb}} {
				base.Positions = append(base.Positions, float32(q.p[0]), float32(q.y-islandBelowM), float32(-q.p[1]))
				base.Normals = append(base.Normals, 0, 1, 0)
				base.UVs = append(base.UVs, float32(q.p[0]/4), float32(-q.p[1]/4)) // the road's texture scale
			}
			if j > 0 {
				v := uint32(2 * j)
				tri(&base, v-2, v, v+1, up)
				tri(&base, v-2, v+1, v-1, up)
			}
		}
		out[k] = append(out[k], base)
		// The kerbstones' tops (the band of a stoepband) along the edge,
		// the grass or pavers inside them: each outline point in by
		// splitKerbTopM, along the mean of its edges' inward normals.
		in := map[int][2]float64{}
		nr := len(isl.ring)
		for j, ix := range isl.ring {
			p0, p1, p2 := isl.pts[isl.ring[(j+nr-1)%nr]], isl.pts[ix], isl.pts[isl.ring[(j+1)%nr]]
			inward := func(a, b [2]float64) [2]float64 { // counter-clockwise: inside is left
				de, dn := b[0]-a[0], b[1]-a[1]
				l := math.Hypot(de, dn)
				if l == 0 {
					return [2]float64{}
				}
				return [2]float64{-dn / l, de / l}
			}
			na, nb := inward(p0, p1), inward(p1, p2)
			m := [2]float64{na[0] + nb[0], na[1] + nb[1]}
			ml := math.Hypot(m[0], m[1])
			if ml < 1e-9 {
				in[ix] = p1
				continue
			}
			m = [2]float64{m[0] / ml, m[1] / ml}
			// As far in along the mean as keeps the band its width from
			// both edges (at most twice it, at a sharp corner).
			k := splitKerbTopM / math.Max(0.5, m[0]*na[0]+m[1]*na[1])
			in[ix] = [2]float64{p1[0] + m[0]*k, p1[1] + m[1]*k}
		}
		band := gltf.Primitive{Material: kerb}
		for j := range isl.ring {
			ia, ib := isl.ring[j], isl.ring[(j+1)%nr]
			base := uint32(len(band.Positions) / 3)
			for _, q := range []struct {
				p [2]float64
				y float64
			}{{isl.pts[ia], isl.ys[ia]}, {isl.pts[ib], isl.ys[ib]}, {in[ia], isl.ys[ia]}, {in[ib], isl.ys[ib]}} {
				band.Positions = append(band.Positions, float32(q.p[0]), float32(q.y+kerbM), float32(-q.p[1]))
				band.Normals = append(band.Normals, 0, 1, 0)
				band.UVs = append(band.UVs, float32((q.p[0]+q.p[1])/5), 0)
			}
			tri(&band, base, base+1, base+3, up)
			tri(&band, base, base+3, base+2, up)
		}
		out[k] = append(out[k], band)
		for _, paved := range []bool{false, true} {
			var p gltf.Primitive
			vx := map[int]uint32{}
			vert := func(i int) uint32 {
				if v, ok := vx[i]; ok {
					return v
				}
				q := isl.pts[i]
				if c, ok := in[i]; ok {
					q = c
				}
				p.Positions = append(p.Positions, float32(q[0]), float32(isl.ys[i]+kerbM), float32(-q[1]))
				p.Normals = append(p.Normals, 0, 1, 0)
				c := g
				if paved {
					c = slopePaving
				}
				p.Colors = append(p.Colors, c[0], c[1], c[2])
				vx[i] = uint32(len(p.Positions)/3 - 1)
				return vx[i]
			}
			for t, tr := range isl.tris {
				if isl.paved[t] == paved {
					tri(&p, vert(tr[0]), vert(tr[1]), vert(tr[2]), up)
				}
			}
			if len(p.Indices) == 0 {
				continue
			}
			p.Material = grass
			if paved {
				p.Material = paving()
			}
			out[k] = append(out[k], p)
		}
		wall := gltf.Primitive{Material: kerb}
		vert := func(q [2]float64, y, dy float64, n [3]float64) uint32 {
			wall.Positions = append(wall.Positions, float32(q[0]), float32(y+dy), float32(-q[1]))
			wall.Normals = append(wall.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
			wall.UVs = append(wall.UVs, float32((q[0]+q[1])/5), float32(dy))
			return uint32(len(wall.Positions)/3 - 1)
		}
		for j := range isl.ring {
			ia, ib := isl.ring[j], isl.ring[(j+1)%len(isl.ring)]
			a, b := isl.pts[ia], isl.pts[ib]
			de, dn := b[0]-a[0], b[1]-a[1]
			l := math.Hypot(de, dn)
			if l < 1e-6 {
				continue
			}
			// Counter-clockwise: outward is to the right of the way round.
			n := [3]float64{dn / l, 0, de / l}
			v0, v1 := vert(a, isl.ys[ia], -walkFootM, n), vert(b, isl.ys[ib], -walkFootM, n)
			v2, v3 := vert(a, isl.ys[ia], kerbM, n), vert(b, isl.ys[ib], kerbM, n)
			tri(&wall, v0, v1, v2, n)
			tri(&wall, v1, v3, v2, n)
		}
		out[k] = append(out[k], wall)
	}
	return out
}

// islandIndex finds the island at a place: its top's height there, and
// whether a hedge may stand there (on grass, splitHedgeM from its noses).
type islandIndex struct {
	isls  []splitIsland
	cells map[[2]int][]int
}

const splitCellM = 20.0

func newIslandIndex(isls []splitIsland) *islandIndex {
	ix := &islandIndex{isls: isls, cells: map[[2]int][]int{}}
	for i, isl := range isls {
		for gx := int(math.Floor(isl.lo[0] / splitCellM)); gx <= int(math.Floor(isl.hi[0]/splitCellM)); gx++ {
			for gy := int(math.Floor(isl.lo[1] / splitCellM)); gy <= int(math.Floor(isl.hi[1]/splitCellM)); gy++ {
				ix.cells[[2]int{gx, gy}] = append(ix.cells[[2]int{gx, gy}], i)
			}
		}
	}
	return ix
}

// at is the island's top at e, n and whether a hedge may stand there; on
// is false off every island.
func (ix *islandIndex) at(e, n float64) (top float64, hedge, on bool) {
	if ix == nil {
		return 0, false, false
	}
	for _, i := range ix.cells[[2]int{int(math.Floor(e / splitCellM)), int(math.Floor(n / splitCellM))}] {
		isl := &ix.isls[i]
		if e < isl.lo[0] || e > isl.hi[0] || n < isl.lo[1] || n > isl.hi[1] || !inPoly(isl.poly, e, n) {
			continue
		}
		best, bk := math.Inf(1), 0
		for k, s := range isl.secs {
			if d := math.Hypot(e-(s.a[0]+s.b[0])/2, n-(s.a[1]+s.b[1])/2); d < best {
				best, bk = d, k
			}
		}
		s := isl.secs[bk]
		ae, an := s.b[0]-s.a[0], s.b[1]-s.a[1]
		t := 0.0
		if l2 := ae*ae + an*an; l2 > 0 {
			t = math.Max(0, math.Min(1, ((e-s.a[0])*ae+(n-s.a[1])*an)/l2))
		}
		top = s.ya + t*(s.yb-s.ya) + kerbM
		hedge = s.gap >= splitPavedW && s.d-isl.d0 >= splitHedgeM && isl.d1-s.d >= splitHedgeM
		return top, hedge, true
	}
	return 0, false, false
}

// buttOnStrips marks the edges of lines along an island's strip as butting
// on its asphalt: no skirt down into the ground there, which showed as a
// dark line along the edge where the strip lies 5 mm under the road
// beside a nose's narrowing.
func buttOnStrips(ls []*roadLine, isls []splitIsland) {
	if len(isls) == 0 {
		return
	}
	type strip struct {
		poly   [][2]float64
		lo, hi [2]float64
	}
	var ss []strip
	for _, isl := range isls {
		var p [][2]float64
		for _, s := range isl.strip {
			p = append(p, s.a)
		}
		for k := len(isl.strip) - 1; k >= 0; k-- {
			p = append(p, isl.strip[k].b)
		}
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, q := range p {
			lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[1])}
			hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[1])}
		}
		ss = append(ss, strip{p, lo, hi})
	}
	on := func(e, n float64) bool {
		for _, s := range ss {
			if e >= s.lo[0] && e <= s.hi[0] && n >= s.lo[1] && n <= s.hi[1] && inPoly(s.poly, e, n) {
				return true
			}
		}
		return false
	}
	for _, l := range ls {
		ss := l.samples
		for i := range ss {
			a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
			de, dn := b.e-a.e, b.n-a.n
			dl := math.Hypot(de, dn)
			if dl == 0 {
				continue
			}
			de, dn = de/dl, dn/dl
			for side, sign := range []float64{-1, 1} {
				// Just past the edge, on the strip (its edge lies
				// splitOverlapM over the carriageway).
				off := ss[i].hw + ss[i].wide[side] + 0.05
				if on(ss[i].e+sign*dn*off, ss[i].n-sign*de*off) {
					ss[i].butt[side] = true
				}
			}
		}
	}
}
