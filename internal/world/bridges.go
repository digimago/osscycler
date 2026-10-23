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
)

// deckReach is how far past the edge of road o, drop metres below, a deck
// reaches. Over the course's own underpass (a cut through an embankment):
// over the shoulder and the fill up to its bank seat, and the abutment on
// it. Elsewhere as before (deckClearM, deckSlope): the fill's reach on
// every road under another would also lay decks between roads merely
// passing near each other at different heights (hairpin legs).
func deckReach(o sample, drop float64) float64 {
	r := deckClearM + deckSlope*(drop-bridgeM)
	if o.underpass {
		r = math.Max(deckClearM, fillShoulderM+math.Max(0, drop-deckDepthM-bankSeatM)/fillSlope+abutmentT)
	}
	return r
}

// markDecks marks the road samples on a deck: over another road more than
// bridgeM lower (within its edge and deckClearM, more the deeper it lies), or aloft. Short gaps
// are bridged, and runs too short for a deck dropped. With beside, a road
// running alongside the lower one isn't over it (a road climbing beside
// another on a mountainside stands behind a wall, owner, 2026-10-16,
// 1.48 km of the Route des Américains: drawn as a bridge, with an
// abutment and grass spikes beside the Rue du Hohneck).
func markDecks(lines []*roadLine, loopM float64, beside bool) {
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
						if o.tunnel || o.ele > s.ele-bridgeM || math.Hypot(o.e-s.e, o.n-s.n) > o.edge+deckReach(o, s.ele-o.ele) {
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
			deck(prim, s, l, i, j, concrete, steel, true)
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
				deck(prim, s, l, i, j, concrete, steel, false)
			}
			i = j + 1
		}
	}
	for _, k := range chunkOrder(byChunk) {
		p := byChunk[k]
		if err := w.add(fmt.Sprintf("bridges %d %d", k[0], k[1]), map[string]any{"kind": "bridges", "chunk": k}, *p); err != nil {
			return err
		}
	}
	return nil
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
func deck(prim func(e, n float64) *gltf.Primitive, s *surface, l *roadLine, i, j int, concrete, steel [3]float32, full bool) {
	ss := l.samples
	frames := make([]deckFrame, 0, j-i+1)
	for k := i; k <= j; k++ {
		frames = append(frames, frameOf(l, ss[k]))
	}
	mid := frames[len(frames)/2].c
	p := prim(mid[0], -mid[2])
	at := func(f deckFrame, across, up float64) [3]float64 {
		return [3]float64{f.c[0] + f.r[0]*across, f.c[1] + up, f.c[2] + f.r[2]*across}
	}
	down := [3]float64{0, -1, 0}
	for k := 0; k+1 < len(frames); k++ {
		a, b := frames[k], frames[k+1]
		la, lb := -a.left-edgeBeamW, -b.left-edgeBeamW
		ra, rb := a.right+edgeBeamW, b.right+edgeBeamW
		// The slab's underside, and the edge beams: inner face, top,
		// outer face down to the underside.
		if full {
			quad(p, at(a, la, -deckDepthM), at(a, ra, -deckDepthM), at(b, rb, -deckDepthM), at(b, lb, -deckDepthM), down, concrete)
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
			quad(p, at(a, side.in[0], -0.05), at(b, side.in[1], -0.05), at(b, side.in[1], edgeBeamH), at(a, side.in[0], edgeBeamH), inN, concrete)
			quad(p, at(a, side.in[0], edgeBeamH), at(b, side.in[1], edgeBeamH), at(b, side.out[1], edgeBeamH), at(a, side.out[0], edgeBeamH), [3]float64{0, 1, 0}, concrete)
			quad(p, at(a, side.out[0], -deckDepthM), at(b, side.out[1], -deckDepthM), at(b, side.out[1], edgeBeamH), at(a, side.out[0], edgeBeamH), outN, concrete)
			// The railing's rails, on the beam's middle.
			ma, mb := (side.in[0]+side.out[0])/2, (side.in[1]+side.out[1])/2
			for _, h := range []float64{0.65, railH} {
				beam(p, at(a, ma, h), at(b, mb, h), 0.06, 0.06, steel)
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
	for _, end := range []struct {
		f   deckFrame
		dir float64 // along the road, away from the span
	}{{frames[0], -1}, {frames[len(frames)-1], 1}} {
		f := end.f
		n := scale3(f.along, end.dir)
		for si, side := range [][2]float64{{-f.left, -f.left - edgeBeamW}, {f.right, f.right + edgeBeamW}} {
			if f.butt[si] {
				continue
			}
			quad(p, at(f, side[0], foot), at(f, side[1], foot), at(f, side[1], edgeBeamH), at(f, side[0], edgeBeamH), n, concrete)
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
			beam(p, at(f, m, edgeBeamH), at(f, m, railH+0.05), 0.07, 0.07, steel)
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
	for _, end := range []struct {
		f    deckFrame
		back float64 // along the road, away from the span
	}{{frames[0], -1}, {frames[len(frames)-1], 1}} {
		f := end.f
		w0, w1 := -f.left-edgeBeamW, f.right+edgeBeamW
		if span < pierSpanM {
			box(p, f, w0, w1, 0, end.back*abutmentT, f.c[1]-deckDepthM-bankSeatM, -0.02, concrete)
			continue
		}
		low := math.Inf(1)
		for x := w0; x <= w1+0.01; x += 1 {
			q := at(f, x, 0)
			low = math.Min(low, s.at(q[0], -q[2]))
		}
		box(p, f, w0, w1, 0, end.back*abutmentT, low-footM, -0.02, concrete)
	}
	// Piers under a long span: two columns and a crosshead.
	if piers := int(span / pierSpanM); piers > 0 {
		for q := 1; q <= piers; q++ {
			f := frames[len(frames)*q/(piers+1)]
			for _, x := range []float64{-f.left * 0.55, f.right * 0.55} {
				top := at(f, x, -deckDepthM)
				box(p, f, x-0.45, x+0.45, -0.45, 0.45, s.at(top[0], -top[2])-footM, -deckDepthM-0.8, concrete)
			}
			box(p, f, -f.left, f.right, -0.6, 0.6, f.c[1]-deckDepthM-0.8, -deckDepthM, concrete)
		}
	}
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
