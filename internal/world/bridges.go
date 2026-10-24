package world

import (
	"fmt"
	"math"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Bridges (plan step 6): where a road passes over another, or stands off
// the ground model (aloft), it is a deck: a concrete slab under the road,
// an edge beam and a steel railing along each side, an abutment at each
// end and piers under a long span. The terrain isn't cut under a deck, so
// the ground and what runs on it stay below.
const (
	deckClearM = 3.0  // a deck reaches this far past the edge of a road below,
	deckSlope  = 0.5  // and this much further per metre the road below is lower than bridgeM
	deckDepthM = 0.9  // the slab, under the road surface
	edgeBeamW  = 0.4  // the edge beam, outside the road's edge
	edgeBeamH  = 0.3  // above the road
	railH      = 1.1  // the railing's top rail, above the road
	railPostM  = 2.0  // between railing posts
	abutmentT  = 1.0  // an abutment's thickness, back under the embankment
	pierSpanM  = 24.0 // a span longer than this rests on piers
	footM      = 0.6  // walls and piers reach this far into the ground
	deckMinM   = 6.0  // a run of deck shorter than this stays a road
	railMinM   = 3.0  // a tagged bridge shorter than this gets no railings
	bankSeatM  = 0.8  // a short span's bank seat, under its slab
	// The fill under a deck, from fillShoulderM past the edge of the road
	// below up to the deck's bank seat, stands no steeper than fillSlope
	// (rise over run; owner, 2026-10-23: packed sand stands at 47° at
	// most, so a road lowered under a deck widens the gap the deck spans):
	// 1:1.5, 34°, an embankment's usual slope, as the land's mesh steepens
	// the first metre off the shoulder (at 1:1 single metres rose at 52°).
	fillSlope     = 1 / 1.5
	fillShoulderM = 0.5
	// dikeSlope: where the builder shapes the land (no ground model), the
	// fill by a deck over a road falls no steeper than 1:2, grassed, for a
	// natural look (owner, 2026-10-24); dikeReachM along each road from
	// the deck.
	dikeSlope  = 0.5
	dikeReachM = 40.0
	// underpassClearM: nothing is planted this far past the edge of the
	// course's own way in an underpass, ramps included (its cut banks).
	underpassClearM = 6.0
)

// deckReach is how far past the edge of road o, drop metres below, a deck
// reaches. Over the course's own underpass (a cut through an embankment):
// over the shoulder and the fill up to its bank seat, and the abutment on
// it. Elsewhere as before (deckClearM, deckSlope): the fill's reach on
// every road under another would also lay decks between roads merely
// passing near each other at different heights (hairpin legs).
//
// With dike (a world without a ground model: its land shaped by the
// builder), a deck spans as far as a dike at dikeSlope needs (owner,
// 2026-10-24: the figure 8's crossing stood unnaturally steep).
func deckReach(o sample, drop float64, dike bool) float64 {
	fill := func(slope float64) float64 {
		return math.Max(deckClearM, fillShoulderM+math.Max(0, drop-deckDepthM-bankSeatM)/slope+abutmentT)
	}
	switch {
	case o.underpass:
		return fill(fillSlope)
	case dike:
		return fill(dikeSlope)
	}
	return deckClearM + deckSlope*(drop-bridgeM)
}

// markDecks marks the road samples on a deck: over another road more than
// bridgeM lower (within its edge and deckClearM, more the deeper it lies), or aloft. Short gaps
// are bridged, and runs too short for a deck dropped. With beside, a road
// running alongside the lower one isn't over it (a road climbing beside
// another on a mountainside stands behind a wall, owner, 2026-10-16,
// 1.48 km of the Route des Américains: drawn as a bridge, with an
// abutment and grass spikes beside the Rue du Hohneck).
func markDecks(lines []*roadLine, loopM float64, beside, dike bool) {
	type ref struct{ l, i int }
	const cellM = 10.0
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range lines {
		for i, s := range l.samples {
			k := key(s.e, s.n)
			grid[k] = append(grid[k], ref{li, i})
		}
	}
	for li, l := range lines {
		deck := make([]bool, len(l.samples))
		for i, s := range l.samples {
			deck[i] = s.aloft
			if s.tunnel {
				deck[i] = false
				continue // under the ground
			}
			k := key(s.e, s.n)
			for gx := k[0] - 1; gx <= k[0]+1 && !deck[i]; gx++ {
				for gy := k[1] - 1; gy <= k[1]+1 && !deck[i]; gy++ {
					for _, r := range grid[[2]int{gx, gy}] {
						o := lines[r.l].samples[r.i]
						// Over a tunnel the road lies on the ground (an
						// embankment), not on a deck.
						if o.tunnel || o.ele > s.ele-bridgeM || math.Hypot(o.e-s.e, o.n-s.n) > o.edge+deckReach(o, s.ele-o.ele, dike) {
							continue
						}
						if beside && r.l != li && alongsideAt(l.samples, i, lines[r.l].samples, r.i) {
							continue
						}
						if r.l == li {
							sep := math.Abs(o.d - s.d)
							if l.loop {
								sep = math.Min(sep, loopM-sep)
							}
							if sep < otherPassM {
								continue // the same pass, going downhill
							}
						}
						deck[i] = true
						break
					}
				}
			}
		}
		// Bridge gaps of a sample or two; drop short runs.
		for i := 1; i+1 < len(deck); i++ {
			if !deck[i] && deck[i-1] && (deck[i+1] || i+2 < len(deck) && deck[i+2]) {
				deck[i] = true
			}
		}
		for i := 0; i < len(deck); {
			if !deck[i] {
				i++
				continue
			}
			j := i
			for j < len(deck) && deck[j] {
				j++
			}
			if l.samples[j-1].d-l.samples[i].d < deckMinM {
				for k := i; k < j; k++ {
					deck[k] = false
				}
			}
			i = j
		}
		for i := range deck {
			l.samples[i].aloft = deck[i]
		}
	}
}

// alongsideAt tells whether sample i of a and sample j of b run within
// parallelCosine of each other's way.
func alongsideAt(a []sample, i int, b []sample, j int) bool {
	way := func(ss []sample, k int) (float64, float64) {
		p, q := ss[max(0, k-1)], ss[min(len(ss)-1, k+1)]
		l := math.Hypot(q.e-p.e, q.n-p.n)
		if l == 0 {
			return 0, 0
		}
		return (q.e - p.e) / l, (q.n - p.n) / l
	}
	ae, an := way(a, i)
	be, bn := way(b, j)
	return math.Abs(ae*be+an*bn) >= parallelCosine
}

// onDeck tells whether the segment from sample i to i+1 is a deck's.
func onDeck(ss []sample, i int) bool { return ss[i].aloft && ss[i+1].aloft }

// addBridges builds the decks' structure, a mesh per terrain chunk, and
// the edge beams and railings of every bridge the map tags that stays a
// road (ways: what the samples' way indices refer to).
func (w *World) addBridges(t *terrain, lines []*roadLine, ways []scenery.Way) error {
	s := &surface{t: t, lists: map[[2]int]chunkLists{}}
	concrete, steel := srgbLinear(0xa9a59c), srgbLinear(0x8e979c)
	var mat int
	byChunk := map[[2]int]*gltf.Primitive{}
	prim := func(e, n float64) *gltf.Primitive {
		k := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
		p := byChunk[k]
		if p == nil {
			if len(byChunk) == 0 {
				mat = w.doc.AddMaterial(gltf.Material{Name: "bridge", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.85})
			}
			p = &gltf.Primitive{Material: mat}
			byChunk[k] = p
		}
		return p
	}
	var paving map[[2]int]*gltf.Primitive
	if len(t.pavings) > 0 {
		paving = drawPavings(t.pavings, t.o.ChunkM, w.pavingMaterial())
	}
	below := roadsBelow(lines)
	for _, l := range lines {
		ss := l.samples
		for i := 0; i+1 < len(ss); {
			if !onDeck(ss, i) {
				i++
				continue
			}
			j := i + 1
			for j+1 < len(ss) && onDeck(ss, j) {
				j++
			}
			deck(prim, s, l, i, j, below(l, i, j).way, concrete, steel, true)
			i = j
		}
		// A bridge in the map that isn't a deck (a low bridge over a canal
		// or ditch, within bridgeM of the ground model) is drawn as road;
		// it still has its edge beams and railings (owner, 2026-10-11: the
		// bridges at 2.7 and 3.93 km of a new course had none).
		tagged := func(k int) bool {
			return ss[k].way >= 0 && ss[k].way < len(ways) && ways[ss[k].way].Bridge && !ss[k].aloft && !ss[k].onPatch && !ss[k].again
		}
		for i := 0; i < len(ss); {
			if !tagged(i) {
				i++
				continue
			}
			j := i
			for j+1 < len(ss) && tagged(j+1) {
				j++
			}
			if ss[j].d-ss[i].d >= railMinM {
				deck(prim, s, l, i, j, [2]float64{}, concrete, steel, false)
			}
			i = j + 1
		}
	}
	for _, k := range chunkOrder(byChunk) {
		p := byChunk[k]
		ps := []gltf.Primitive{*p}
		if q := paving[k]; q != nil {
			ps = append(ps, *q)
			delete(paving, k)
		}
		if err := w.add(fmt.Sprintf("bridges %d %d", k[0], k[1]), map[string]any{"kind": "bridges", "chunk": k}, ps...); err != nil {
			return err
		}
	}
	for _, k := range chunkOrder(paving) { // paving in a chunk with no deck
		if err := w.add(fmt.Sprintf("bridges %d %d", k[0], k[1]), map[string]any{"kind": "bridges", "chunk": k}, *paving[k]); err != nil {
			return err
		}
	}
	return nil
}

// roadsBelow gives, for a deck (line l's samples i to j), the way of the
// road under it nearest its middle (unit, east and north; zero: none, a
// deck over water or a valley): a deck over a road is a skew bridge, its
// ends parallel to the road below (owner, 2026-10-23: the N348's decks
// over the Posbank Loop's cycle underpass, cut square to their own road,
// each had a corner of its bank seat out over the slope, in mid air).
// deckBelow is the road under a deck: its way there (unit, east and north)
// and its sample nearest the deck's middle; zero: none.
type deckBelow struct {
	way  [2]float64
	o    sample
	line *roadLine // o's
}

func roadsBelow(lines []*roadLine) func(l *roadLine, i, j int) deckBelow {
	type ref struct{ l, i int }
	const cellM = 10.0
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range lines {
		for i, s := range l.samples {
			if !s.aloft && !s.tunnel {
				grid[key(s.e, s.n)] = append(grid[key(s.e, s.n)], ref{li, i})
			}
		}
	}
	return func(l *roadLine, i, j int) deckBelow {
		m := l.samples[(i+j)/2]
		best, below := math.Inf(1), deckBelow{}
		k := key(m.e, m.n)
		for gx := k[0] - 1; gx <= k[0]+1; gx++ {
			for gy := k[1] - 1; gy <= k[1]+1; gy++ {
				for _, r := range grid[[2]int{gx, gy}] {
					o := lines[r.l].samples[r.i]
					if lines[r.l] == l || o.ele > m.ele-bridgeM {
						continue
					}
					// Under the deck: within its width of one of its samples.
					under := false
					for q := i; q <= j && !under; q++ {
						s := l.samples[q]
						under = math.Hypot(o.e-s.e, o.n-s.n) <= s.hw+o.hw
					}
					if d := math.Hypot(o.e-m.e, o.n-m.n); under && d < best {
						de, dn, _ := lines[r.l].smooth.heading(o.d)
						if h := math.Hypot(de, dn); h > 0 {
							best, below = d, deckBelow{[2]float64{de / h, dn / h}, o, lines[r.l]}
						}
					}
				}
			}
		}
		return below
	}
}

// deckFrame is a cross-section of a deck: its centre, the direction to
// its right, and how far its sides reach left and right of the centre.
type deckFrame struct {
	c, r, along [3]float64
	left, right float64
	butt        [2]bool // another road's surface continues at that edge: no beam or railing there
}

// frameOf is line l's cross-section at sample a: its edges with the
// sidewalks, or the verges' widening without.
func frameOf(l *roadLine, a sample) deckFrame {
	x, y := l.smooth.at(a.d)
	de, dn, _ := l.smooth.heading(a.d)
	left, right := a.hw+a.wide[0], a.hw+a.wide[1]
	if a.walk[0] {
		left = a.hw + a.walkWidth(0)
	}
	if a.walk[1] {
		right = a.hw + a.walkWidth(1)
	}
	return deckFrame{c: [3]float64{x, a.ele, -y}, r: [3]float64{dn, 0, de}, along: [3]float64{de, 0, -dn}, left: left, right: right, butt: a.butt}
}

// deck builds the structure of line l's deck from sample i to j; not
// full, only its edge beams and railings (a low bridge drawn as road).
// skew is the way of the road below (unit, east and north; zero: none):
// the deck's ends run along it, through the ends' middles.
func deck(prim func(e, n float64) *gltf.Primitive, s *surface, l *roadLine, i, j int, skew [2]float64, concrete, steel [3]float32, full bool) {
	ss := l.samples
	frames := make([]deckFrame, 0, j-i+1)
	for k := i; k <= j; k++ {
		frames = append(frames, frameOf(l, ss[k]))
	}
	mid := frames[len(frames)/2].c
	p := prim(mid[0], -mid[2])
	dS, dE := ss[i].d, ss[j].d
	// shift is how far along the road (from an end's middle at d) the end
	// lies at across x, for it to run along the road below; none where
	// that road runs nearly along this one, and at most a third of the
	// span.
	shift := func(d, x float64) float64 {
		if skew == ([2]float64{}) {
			return 0
		}
		f := frameOf(l, sampleBetween(l, d))
		re, rn, ae, an := f.r[0], -f.r[2], f.along[0], -f.along[2]
		den := ae*skew[1] - an*skew[0]
		if math.Abs(den) < 0.3 {
			return 0
		}
		lim := (dE - dS) / 3
		return math.Max(-lim, math.Min(lim, -x*(re*skew[1]-rn*skew[0])/den))
	}
	// point is the deck's point at across x, up over the road there, at d
	// along the road from end end's middle (0 the start, 1 the end): the
	// end's skew carried across the span, fading to the other end's.
	point := func(t, x, up, back float64) [3]float64 {
		d := dS + t*(dE-dS) + (1-t)*shift(dS, x) + t*shift(dE, x) + back
		f := frameOf(l, sampleBetween(l, d))
		return [3]float64{f.c[0] + f.r[0]*x, f.c[1] + up, f.c[2] + f.r[2]*x}
	}
	tOf := func(k int) float64 { return (ss[i+k].d - dS) / math.Max(dE-dS, 1e-9) }
	at := func(k int, across, up float64) [3]float64 { return point(tOf(k), across, up, 0) }
	down := [3]float64{0, -1, 0}
	for k := 0; k+1 < len(frames); k++ {
		a, b := frames[k], frames[k+1]
		ka, kb := k, k+1
		la, lb := -a.left-edgeBeamW, -b.left-edgeBeamW
		ra, rb := a.right+edgeBeamW, b.right+edgeBeamW
		// The slab's underside, and the edge beams: inner face, top,
		// outer face down to the underside.
		if full {
			quad(p, at(ka, la, -deckDepthM), at(ka, ra, -deckDepthM), at(kb, rb, -deckDepthM), at(kb, lb, -deckDepthM), down, concrete)
		}
		for si, side := range []struct {
			in, out [2]float64 // a's and b's inner and outer offsets
			sign    float64
		}{
			{[2]float64{-a.left, -b.left}, [2]float64{la, lb}, -1},
			{[2]float64{a.right, b.right}, [2]float64{ra, rb}, 1},
		} {
			// Two carriageways joined side by side: none down the middle
			// of the road between them (a dual carriageway's two bridges).
			if a.butt[si] && b.butt[si] {
				continue
			}
			outN := scale3(a.r, side.sign)
			inN := scale3(a.r, -side.sign)
			quad(p, at(ka, side.in[0], -0.05), at(kb, side.in[1], -0.05), at(kb, side.in[1], edgeBeamH), at(ka, side.in[0], edgeBeamH), inN, concrete)
			quad(p, at(ka, side.in[0], edgeBeamH), at(kb, side.in[1], edgeBeamH), at(kb, side.out[1], edgeBeamH), at(ka, side.out[0], edgeBeamH), [3]float64{0, 1, 0}, concrete)
			quad(p, at(ka, side.out[0], -deckDepthM), at(kb, side.out[1], -deckDepthM), at(kb, side.out[1], edgeBeamH), at(ka, side.out[0], edgeBeamH), outN, concrete)
			// The railing's rails, on the beam's middle.
			ma, mb := (side.in[0]+side.out[0])/2, (side.in[1]+side.out[1])/2
			for _, h := range []float64{0.65, railH} {
				beam(p, at(ka, ma, h), at(kb, mb, h), 0.06, 0.06, steel)
			}
		}
	}
	// The edge beams' ends (owner, 2026-10-12, the figure 8's overpass:
	// the beams stood open at the deck's ends, their inner and outer faces
	// with nothing across them): a face across each, from the beam's foot
	// (on a full deck the abutment's top, which closes the rest) to its
	// top, facing away from the span.
	foot := -deckDepthM
	if full {
		foot = -0.02
	}
	ends := []struct {
		f   deckFrame
		k   int
		dir float64 // along the road, away from the span
	}{{frames[0], 0, -1}, {frames[len(frames)-1], len(frames) - 1, 1}}
	for _, end := range ends {
		f := end.f
		for si, side := range [][2]float64{{-f.left, -f.left - edgeBeamW}, {f.right, f.right + edgeBeamW}} {
			if f.butt[si] {
				continue
			}
			a, b := at(end.k, side[0], foot), at(end.k, side[1], foot)
			quad(p, a, b, at(end.k, side[1], edgeBeamH), at(end.k, side[0], edgeBeamH), outward(a, b, scale3(f.along, end.dir)), concrete)
		}
	}
	// Railing posts along each side, every railPostM.
	var run float64
	for k := range frames {
		if k > 0 {
			run += math.Hypot(frames[k].c[0]-frames[k-1].c[0], frames[k].c[2]-frames[k-1].c[2])
		}
		if k > 0 && k < len(frames)-1 && math.Mod(run+0.5, railPostM) > fineStep {
			continue
		}
		f := frames[k]
		for si, m := range []float64{-f.left - edgeBeamW/2, f.right + edgeBeamW/2} {
			if f.butt[si] {
				continue
			}
			beam(p, at(k, m, edgeBeamH), at(k, m, railH+0.05), 0.07, 0.07, steel)
		}
	}
	if !full {
		return
	}
	// Abutments at both ends: a wall across under the deck's end, down
	// into the ground, its face towards the span. A short span (no piers)
	// rests on a bank seat on the embankment instead, the slope under it
	// paved (gland, slopePaving): its abutment, down to the lowest ground
	// across the deck, stood out of the bank as a pillar (owner,
	// 2026-10-22, the N348 over the Posbank Loop's cycle underpass).
	span := ss[j].d - ss[i].d
	joint := srgbLinear(0x3a3d40)
	for _, end := range ends {
		f := end.f
		t := float64(end.k) / float64(len(frames)-1)
		w0, w1 := -f.left-edgeBeamW, f.right+edgeBeamW
		// The bank seat or abutment: a block along the end, back under
		// the embankment, in plan the end's line and abutmentT behind it.
		plan := [4][3]float64{point(t, w0, 0, 0), point(t, w1, 0, 0), point(t, w1, 0, end.dir*abutmentT), point(t, w0, 0, end.dir*abutmentT)}
		low := f.c[1] - deckDepthM - bankSeatM
		if span >= pierSpanM {
			low = math.Inf(1)
			for x := w0; x <= w1+0.01; x += 1 {
				q := point(t, x, 0, 0)
				low = math.Min(low, s.at(q[0], -q[2]))
			}
			low -= footM
		}
		// Its top under the road at each corner, as the road falls along
		// a skewed end (owner, 2026-10-24: level at the road's middle, the
		// seat showed through the road where it fell away, figure 8).
		var tops [4]float64
		for k, q := range plan {
			tops[k] = q[1] - seatCoverM
		}
		block(p, plan, low, tops, concrete)
		// The expansion joint across the road at the deck's end, along the
		// road below as the end is: a dark steel strip on the surface.
		a, b := point(t, -f.left, 0.012, 0), point(t, f.right, 0.012, 0)
		c, d := point(t, f.right, 0.012, -end.dir*jointW), point(t, -f.left, 0.012, -end.dir*jointW)
		quad(p, a, b, c, d, [3]float64{0, 1, 0}, joint)
	}
	// Piers under a long span: two columns and a crosshead.
	if piers := int(span / pierSpanM); piers > 0 {
		for q := 1; q <= piers; q++ {
			f := frames[len(frames)*q/(piers+1)]
			for _, x := range []float64{-f.left * 0.55, f.right * 0.55} {
				top := [3]float64{f.c[0] + f.r[0]*x, f.c[1] - deckDepthM, f.c[2] + f.r[2]*x}
				box(p, f, x-0.45, x+0.45, -0.45, 0.45, s.at(top[0], -top[2])-footM, -deckDepthM-0.8, concrete)
			}
			box(p, f, -f.left, f.right, -0.6, 0.6, f.c[1]-deckDepthM-0.8, -deckDepthM, concrete)
		}
	}
}

// jointW: an expansion joint's width along the road.
const jointW = 0.12

// outward is the horizontal normal of a face along a to b (seen from
// above) that points the way of hint.
func outward(a, b, hint [3]float64) [3]float64 {
	n := [3]float64{b[2] - a[2], 0, -(b[0] - a[0])}
	if l := math.Hypot(n[0], n[2]); l > 0 {
		n = scale3(n, 1/l)
	}
	if dot3(n, hint) < 0 {
		n = scale3(n, -1)
	}
	return n
}

// seatCoverM: a bank seat's top lies this far under the road over it.
const seatCoverM = 0.2

// block is a prism over the quadrilateral plan (its corners in order, at
// any height), from low up to each corner's top, closed on every side.
func block(p *gltf.Primitive, plan [4][3]float64, low float64, top [4]float64, c [3]float32) {
	var mid [3]float64
	for _, q := range plan {
		mid[0], mid[2] = mid[0]+q[0]/4, mid[2]+q[2]/4
	}
	lo := func(k int) [3]float64 { return [3]float64{plan[k%4][0], low, plan[k%4][2]} }
	hi := func(k int) [3]float64 { return [3]float64{plan[k%4][0], top[k%4], plan[k%4][2]} }
	for k := range 4 {
		a, b := lo(k), lo(k+1)
		out := [3]float64{(a[0]+b[0])/2 - mid[0], 0, (a[2]+b[2])/2 - mid[2]}
		quad(p, a, b, hi(k+1), hi(k), outward(a, b, out), c)
	}
	quad(p, hi(0), hi(1), hi(2), hi(3), upNormal(hi(0), hi(1), hi(3)), c)
	quad(p, lo(0), lo(1), lo(2), lo(3), [3]float64{0, -1, 0}, c)
}

// upNormal is the upward unit normal of the plane through a, b and c.
func upNormal(a, b, c [3]float64) [3]float64 {
	n := cross([3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}, [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]})
	if n[1] < 0 {
		n = scale3(n, -1)
	}
	if l := math.Sqrt(dot3(n, n)); l > 0 {
		n = scale3(n, 1/l)
	}
	return n
}

// box is a block in frame f: across from x0 to x1, along from a0 to a1
// (in either order), from the absolute height low up to top above the
// road's centre.
func box(p *gltf.Primitive, f deckFrame, x0, x1, a0, a1, low, top float64, c [3]float32) {
	if a0 > a1 {
		a0, a1 = a1, a0
	}
	pt := func(x, a, h float64) [3]float64 {
		return [3]float64{f.c[0] + f.r[0]*x + f.along[0]*a, h, f.c[2] + f.r[2]*x + f.along[2]*a}
	}
	lo := func(x, a float64) [3]float64 { return pt(x, a, low) }
	hi := func(x, a float64) [3]float64 { return pt(x, a, f.c[1]+top) }
	r, al := f.r, f.along
	quad(p, lo(x0, a0), lo(x1, a0), hi(x1, a0), hi(x0, a0), scale3(al, -1), c)
	quad(p, lo(x0, a1), lo(x1, a1), hi(x1, a1), hi(x0, a1), al, c)
	quad(p, lo(x0, a0), lo(x0, a1), hi(x0, a1), hi(x0, a0), scale3(r, -1), c)
	quad(p, lo(x1, a0), lo(x1, a1), hi(x1, a1), hi(x1, a0), r, c)
	quad(p, hi(x0, a0), hi(x1, a0), hi(x1, a1), hi(x0, a1), [3]float64{0, 1, 0}, c)
	// Closed below too (owner, 2026-10-22: a pier's crosshead over the
	// figure 8's motorway had no bottom face; every block a manifold).
	quad(p, lo(x0, a0), lo(x1, a0), lo(x1, a1), lo(x0, a1), [3]float64{0, -1, 0}, c)
}

// markDikes marks the roads by each deck over another road: the lower road
// within dikeReachM of the deck's middle along it (dike: the land rises from
// it at dikeSlope at most), the deck's own road within dikeReachM of its
// ends (dikeTop: the land falls from it at dikeSlope at most).
func markDikes(lines []*roadLine) {
	below := roadsBelow(lines)
	for _, l := range lines {
		ss := l.samples
		for i := 0; i+1 < len(ss); {
			if !onDeck(ss, i) {
				i++
				continue
			}
			j := i + 1
			for j+1 < len(ss) && onDeck(ss, j) {
				j++
			}
			u := below(l, i, j)
			if u.line != nil {
				for k := range u.line.samples {
					if math.Abs(u.line.samples[k].d-u.o.d) < dikeReachM {
						u.line.samples[k].dike = true
					}
				}
				for k := range ss {
					if !ss[k].aloft && (math.Abs(ss[k].d-ss[i].d) < dikeReachM || math.Abs(ss[k].d-ss[j].d) < dikeReachM) {
						ss[k].dikeTop = true
					}
				}
			}
			i = j
		}
	}
}
