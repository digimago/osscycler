package world

import (
	"fmt"
	"math"

	"github.com/digimago/osscycler/internal/gltf"
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
)

// markDecks marks the road samples on a deck: over another road more than
// bridgeM lower (within its edge and deckClearM, more the deeper it lies), or aloft. Short gaps
// are bridged, and runs too short for a deck dropped.
func markDecks(lines []*roadLine, loopM float64) {
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
			k := key(s.e, s.n)
			for gx := k[0] - 1; gx <= k[0]+1 && !deck[i]; gx++ {
				for gy := k[1] - 1; gy <= k[1]+1 && !deck[i]; gy++ {
					for _, r := range grid[[2]int{gx, gy}] {
						o := lines[r.l].samples[r.i]
						if o.ele > s.ele-bridgeM || math.Hypot(o.e-s.e, o.n-s.n) > o.edge+deckClearM+deckSlope*(s.ele-o.ele-bridgeM) {
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

// onDeck tells whether the segment from sample i to i+1 is a deck's.
func onDeck(ss []sample, i int) bool { return ss[i].aloft && ss[i+1].aloft }

// addBridges builds the decks' structure, a mesh per terrain chunk.
func (w *World) addBridges(t *terrain, lines []*roadLine) error {
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
			deck(prim, s, l, i, j, concrete, steel)
			i = j
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
}

// deck builds the structure of line l's deck from sample i to j.
func deck(prim func(e, n float64) *gltf.Primitive, s *surface, l *roadLine, i, j int, concrete, steel [3]float32) {
	ss := l.samples
	frames := make([]deckFrame, 0, j-i+1)
	for k := i; k <= j; k++ {
		a := ss[k]
		x, y := l.smooth.at(a.d)
		de, dn, _ := l.smooth.heading(a.d)
		left, right := a.hw+a.wide[0], a.hw+a.wide[1]
		if a.walk[0] {
			left = a.hw + walkM
		}
		if a.walk[1] {
			right = a.hw + walkM
		}
		frames = append(frames, deckFrame{c: [3]float64{x, a.ele, -y}, r: [3]float64{dn, 0, de}, along: [3]float64{de, 0, -dn}, left: left, right: right})
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
		quad(p, at(a, la, -deckDepthM), at(a, ra, -deckDepthM), at(b, rb, -deckDepthM), at(b, lb, -deckDepthM), down, concrete)
		for _, side := range []struct {
			in, out [2]float64 // a's and b's inner and outer offsets
			sign    float64
		}{
			{[2]float64{-a.left, -b.left}, [2]float64{la, lb}, -1},
			{[2]float64{a.right, b.right}, [2]float64{ra, rb}, 1},
		} {
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
		for _, m := range []float64{-f.left - edgeBeamW/2, f.right + edgeBeamW/2} {
			beam(p, at(f, m, edgeBeamH), at(f, m, railH+0.05), 0.07, 0.07, steel)
		}
	}
	// Abutments at both ends: a wall across under the deck's end, down
	// into the ground, its face towards the span.
	for _, end := range []struct {
		f    deckFrame
		back float64 // along the road, away from the span
	}{{frames[0], -1}, {frames[len(frames)-1], 1}} {
		f := end.f
		w0, w1 := -f.left-edgeBeamW, f.right+edgeBeamW
		low := math.Inf(1)
		for x := w0; x <= w1+0.01; x += 1 {
			q := at(f, x, 0)
			low = math.Min(low, s.at(q[0], -q[2]))
		}
		box(p, f, w0, w1, 0, end.back*abutmentT, low-footM, -0.02, concrete)
	}
	// Piers under a long span: two columns and a crosshead.
	span := ss[j].d - ss[i].d
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
}
