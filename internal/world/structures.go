package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Bridges as structures (owner, 2026-11-17, the Erasmusbrug: its pylon and
// stays wanted, read from the map's bridge type and where its piers stand,
// not a landmark modelled for one place). Where the map outlines a bridge
// (man_made=bridge) over a deck the course's roads make, the whole outline
// is deck: the strip between its carriageways (tram tracks and cycle
// paths, which aren't fetched) had shown the river through it. Its kind
// (bridge:structure) says what stands on it:
//
//   - cable-stayed: a pylon on the larger support at either end of the
//     longest span, an inverted Y over the deck (its legs outside the
//     deck's edges, meeting in one mast), with a fan of stays in two
//     planes to the deck's edges, along the main span and back over the
//     back spans to the furthest support;
//     Where both supports of the main span are alike (the smaller at least
//     half the larger's area), a pylon on each, each with half the span
//     (the Willemsbrug);
//   - suspension: portal towers on the supports at both ends of the
//     longest span, main cables sagging between them and back to the
//     deck's ends, hangers down to the deck: only for a main span of
//     suspensionMinM or more between alike supports. Shorter, or without
//     two alike supports, it is cable-stayed, the map's kind a common
//     mistake (the Erasmusbrug is tagged suspension: its main span runs
//     from its pylon's pier to the bascule's, a fifth of the size).
//
// A pylon's height over the deck is the map's (height, less the deck's
// height above the ground) or, not said, a share of the main span:
// pylonShare for a cable-stayed pylon (the Erasmusbrug's 139 m over the
// water on a 284 m span), towerShare for a suspension tower.
const (
	structBinM     = 5.0  // the deck's height is known by band of this along the bridge
	structFillDrop = 0.03 // the outline's deck lies this far under the roads' surface
	pylonShare     = 0.42
	towerShare     = 0.12
	pylonMinM      = 25.0
	pylonMaxM      = 200.0
	stayEveryM     = 12.0 // stays meet the deck this far apart
	stayStartM     = 20.0 // the first stay this far from the pylon
	hangerEveryM   = 10.0
	towerAlikeArea = 0.5 // a span's supports at least this alike in area to both carry towers
	// outlineMinM: a bridge whose deck runs shorter than this along its
	// outline is left as its roads' decks make it: the N348's short decks
	// over the Posbank Loop's cycle underpass (their bank seats and paved
	// slopes the owner's, 2026-10-22/23) got a second slab out over their
	// banks, railed again. The outline's deck is for big bridges, their
	// carriageways apart.
	outlineMinM = 50.0
	// suspensionMinM: a "suspension" bridge with a shorter main span is
	// taken for cable-stayed. The map's kind is often wrong that way (the
	// Erasmusbrug and the Willemsbrug, both cable-stayed, are tagged
	// suspension); road bridges hung from main cables span more.
	suspensionMinM = 400.0
)

// structFrame is a bridge outline's own frame: along its long axis (a)
// and across it (c, to the left), and the deck's height along it.
type structFrame struct {
	m, u, v [2]float64   // centre, axis, left of it (east, north)
	ac      [][2]float64 // the outline in (a, c)
	a0, a1  float64      // where its deck is (the ridden roads aloft within it)
	h       []float64    // the deck's road height by band from a0
}

func (f *structFrame) toAC(e, n float64) (float64, float64) {
	de, dn := e-f.m[0], n-f.m[1]
	return de*f.u[0] + dn*f.u[1], de*f.v[0] + dn*f.v[1]
}

func (f *structFrame) toEN(a, c float64) (float64, float64) {
	return f.m[0] + f.u[0]*a + f.v[0]*c, f.m[1] + f.u[1]*a + f.v[1]*c
}

// height is the deck's road height at a (the ends' beyond the deck).
func (f *structFrame) height(a float64) float64 {
	x := (a - f.a0) / structBinM
	k := int(math.Floor(x))
	switch {
	case k < 0:
		return f.h[0]
	case k >= len(f.h)-1:
		return f.h[len(f.h)-1]
	}
	t := x - float64(k)
	return f.h[k]*(1-t) + f.h[k+1]*t
}

// across is how far the outline reaches either side at a (its edges'
// crossings of the line across there): false where a is outside it.
func (f *structFrame) across(a float64) (float64, float64, bool) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for k := range f.ac {
		p, q := f.ac[k], f.ac[(k+1)%len(f.ac)]
		if (p[0] <= a) == (q[0] <= a) {
			continue
		}
		c := p[1] + (a-p[0])/(q[0]-p[0])*(q[1]-p[1])
		lo, hi = math.Min(lo, c), math.Max(hi, c)
	}
	return lo, hi, lo < hi
}

// newStructFrame is bridge b's frame, its deck from the samples aloft
// inside it; nil when no deck lies in it.
func newStructFrame(b scenery.Bridge, lines []*roadLine) *structFrame {
	f := &structFrame{}
	for _, q := range b.Outline {
		f.m[0], f.m[1] = f.m[0]+q[0]/float64(len(b.Outline)), f.m[1]+q[1]/float64(len(b.Outline))
	}
	var sxx, syy, sxy float64
	for _, q := range b.Outline {
		x, y := q[0]-f.m[0], q[1]-f.m[1]
		sxx, syy, sxy = sxx+x*x, syy+y*y, sxy+x*y
	}
	ang := 0.5 * math.Atan2(2*sxy, sxx-syy)
	f.u = [2]float64{math.Cos(ang), math.Sin(ang)}
	f.v = [2]float64{-f.u[1], f.u[0]}
	for _, q := range b.Outline {
		a, c := f.toAC(q[0], q[1])
		f.ac = append(f.ac, [2]float64{a, c})
	}
	type hit struct{ a, ele float64 }
	var hits []hit
	for _, l := range lines {
		for _, s := range l.samples {
			if s.aloft && inPoly(b.Outline, s.e, s.n) {
				a, _ := f.toAC(s.e, s.n)
				hits = append(hits, hit{a, s.ele})
			}
		}
	}
	if len(hits) < 2 {
		return nil
	}
	f.a0, f.a1 = math.Inf(1), math.Inf(-1)
	for _, h := range hits {
		f.a0, f.a1 = math.Min(f.a0, h.a), math.Max(f.a1, h.a)
	}
	if f.a1-f.a0 < 2*structBinM {
		return nil
	}
	n := int(math.Ceil((f.a1-f.a0)/structBinM)) + 1
	sum, cnt := make([]float64, n), make([]float64, n)
	for _, h := range hits {
		k := int(math.Round((h.a - f.a0) / structBinM))
		sum[k], cnt[k] = sum[k]+h.ele, cnt[k]+1
	}
	// Bands no sample fell in take their neighbours' heights, in between.
	f.h = make([]float64, n)
	last := -1
	for k := range n {
		if cnt[k] == 0 {
			continue
		}
		f.h[k] = sum[k] / cnt[k]
		if last >= 0 {
			for m := last + 1; m < k; m++ {
				t := float64(m-last) / float64(k-last)
				f.h[m] = f.h[last]*(1-t) + f.h[k]*t
			}
		} else {
			for m := 0; m < k; m++ {
				f.h[m] = f.h[k]
			}
		}
		last = k
	}
	for m := last + 1; m < n; m++ {
		f.h[m] = f.h[last]
	}
	return f
}

// clipBand is polygon pts (a, c) cut to a0 ≤ a ≤ a1 (Sutherland-Hodgman).
func clipBand(pts [][2]float64, a0, a1 float64) [][2]float64 {
	clip := func(in [][2]float64, keep func(p [2]float64) bool, at func(p, q [2]float64) [2]float64) [][2]float64 {
		var out [][2]float64
		for k := range in {
			p, q := in[k], in[(k+1)%len(in)]
			switch {
			case keep(p) && keep(q):
				out = append(out, q)
			case keep(p):
				out = append(out, at(p, q))
			case keep(q):
				out = append(out, at(p, q), q)
			}
		}
		return out
	}
	cut := func(x float64) func(p, q [2]float64) [2]float64 {
		return func(p, q [2]float64) [2]float64 {
			t := (x - p[0]) / (q[0] - p[0])
			return [2]float64{x, p[1] + t*(q[1]-p[1])}
		}
	}
	pts = clip(pts, func(p [2]float64) bool { return p[0] >= a0 }, cut(a0))
	if len(pts) < 3 {
		return nil
	}
	return clip(pts, func(p [2]float64) bool { return p[0] <= a1 }, cut(a1))
}

// structSupport is a support within a bridge: where along it, and how big.
type structSupport struct {
	a, area float64
	tower   bool // a tower or pylon may stand on it
}

// longBridges are the bridges whose deck (the ridden roads aloft within
// them) runs outlineMinM or more along them: those drawn as structures.
func longBridges(bridges []scenery.Bridge, lines []*roadLine) []scenery.Bridge {
	var out []scenery.Bridge
	for _, b := range bridges {
		if f := newStructFrame(b, lines); f != nil && f.a1-f.a0 >= outlineMinM {
			out = append(out, b)
		}
	}
	return out
}

// addStructures draws the bridges' outline decks, railings, pylons and
// stays (see the top of this file).
func addStructures(prim func(e, n float64) *gltf.Primitive, s *surface, bridges []scenery.Bridge, sups []scenery.Support, lines []*roadLine, concrete, steel [3]float32) {
	stay, pylon := srgbLinear(0xe4e8ea), srgbLinear(0xc9d3da)
	for _, b := range bridges {
		f := newStructFrame(b, lines)
		if f == nil {
			continue
		}
		outlineDeck(prim, f, concrete, steel)
		var ss []structSupport
		for _, sp := range sups {
			var ce, cn float64
			for _, q := range sp.Outline {
				ce, cn = ce+q[0]/float64(len(sp.Outline)), cn+q[1]/float64(len(sp.Outline))
			}
			if !inPoly(b.Outline, ce, cn) {
				continue
			}
			a, _ := f.toAC(ce, cn)
			if a <= f.a0 || a >= f.a1 {
				continue
			}
			ss = append(ss, structSupport{a: a, area: polyArea(sp.Outline), tower: sp.Kind != "abutment"})
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].a < ss[j].a })
		switch b.Structure {
		case "cable-stayed", "cable_stayed", "suspension":
		default:
			continue
		}
		// The spans: between the deck's ends and the supports, in order.
		stops := append([]structSupport{{a: f.a0}}, ss...)
		stops = append(stops, structSupport{a: f.a1})
		longest := 1
		for k := 2; k < len(stops); k++ {
			if stops[k].a-stops[k-1].a > stops[longest].a-stops[longest-1].a {
				longest = k
			}
		}
		p, q := stops[longest-1], stops[longest]
		span := q.a - p.a
		alike := p.tower && q.tower && math.Min(p.area, q.area) >= towerAlikeArea*math.Max(p.area, q.area)
		if b.Structure == "suspension" && alike && span >= suspensionMinM {
			suspension(prim, s, f, b, p.a, q.a, pylon, steel)
			continue
		}
		// The back spans of a pylon at at: away from the main span (the way
		// of dir) to the furthest support on that side.
		backOf := func(at, dir float64) float64 {
			back := 0.0
			for _, sp := range ss {
				if (sp.a-at)*dir < 0 {
					back = math.Max(back, math.Abs(sp.a-at))
				}
			}
			if back < stayStartM*2 {
				back = math.Max(stayStartM*2, math.Min(span, math.Min(math.Abs(at-f.a0), math.Abs(f.a1-at))))
			}
			return back
		}
		if alike {
			// Two pylons, each with half the main span.
			cableStayed(prim, s, f, b, p.a, 1, span/2, backOf(p.a, 1), pylon, stay)
			cableStayed(prim, s, f, b, q.a, -1, span/2, backOf(q.a, -1), pylon, stay)
			continue
		}
		// One pylon: on the larger of the span's supports (an end of the
		// deck is none), over the span's middle with none in reach.
		at, dir := p.a, 1.0
		if !p.tower || q.tower && q.area > p.area {
			at, dir = q.a, -1
		}
		if !p.tower && !q.tower {
			at = (p.a + q.a) / 2
		}
		cableStayed(prim, s, f, b, at, dir, span, backOf(at, dir), pylon, stay)
	}
}

// outlineDeck lays the deck over the bridge's outline where it is a deck:
// a slab structFillDrop under the roads, in bands along it, its edges
// closed and railed along the long sides.
func outlineDeck(prim func(e, n float64) *gltf.Primitive, f *structFrame, concrete, steel [3]float32) {
	pt := func(a, c, up float64) [3]float64 {
		e, n := f.toEN(a, c)
		return [3]float64{e, f.height(a) + up, -n}
	}
	for a := f.a0; a < f.a1; a += structBinM {
		piece := clipBand(f.ac, a, math.Min(a+structBinM, f.a1))
		if len(piece) < 3 {
			continue
		}
		mid := pt(a+structBinM/2, 0, 0)
		p := prim(mid[0], -mid[2])
		for _, t := range earClip(piece) {
			var top, bot [3][3]float64
			for k, ix := range t {
				top[k] = pt(piece[ix][0], piece[ix][1], -structFillDrop)
				bot[k] = pt(piece[ix][0], piece[ix][1], -deckDepthM)
			}
			triangle(p, top[0], top[1], top[2], [3]float64{0, 1, 0}, concrete)
			triangle(p, bot[0], bot[2], bot[1], [3]float64{0, -1, 0}, concrete)
		}
	}
	// The outline's own edges within the deck: a face from the slab's top
	// to its underside, and where an edge runs along the bridge a railing.
	for k := range f.ac {
		p, q := f.ac[k], f.ac[(k+1)%len(f.ac)]
		lo, hi := math.Max(math.Min(p[0], q[0]), f.a0), math.Min(math.Max(p[0], q[0]), f.a1)
		if hi <= lo {
			continue
		}
		at := func(a float64) [2]float64 { // the edge's point at a
			t := (a - p[0]) / (q[0] - p[0])
			return [2]float64{a, p[1] + t*(q[1]-p[1])}
		}
		along := math.Abs(q[0]-p[0]) / math.Hypot(q[0]-p[0], q[1]-p[1])
		for a := lo; a < hi; a += railPostM {
			b := math.Min(a+railPostM, hi)
			x, y := at(a), at(b)
			top0, top1 := pt(x[0], x[1], -structFillDrop), pt(y[0], y[1], -structFillDrop)
			bot0, bot1 := pt(x[0], x[1], -deckDepthM), pt(y[0], y[1], -deckDepthM)
			// Outward: the outline runs counter-clockwise (in (a, c) as in
			// (e, n): a rotation), so outward is right of the edge's way:
			// (dn, -de) in (e, n), (dn, 0, de) in (x, y, z).
			pe, pn := f.toEN(p[0], p[1])
			qe, qn := f.toEN(q[0], q[1])
			out := [3]float64{qn - pn, 0, qe - pe}
			if l := math.Hypot(out[0], out[2]); l > 0 {
				out = scale3(out, 1/l)
			}
			quad(p0(prim, top0), bot0, bot1, top1, top0, out, concrete)
			if along > 0.8 {
				for _, h := range []float64{0.65, railH} {
					beam(p0(prim, top0), pt(x[0], x[1], h), pt(y[0], y[1], h), 0.06, 0.06, steel)
				}
				beam(p0(prim, top0), pt(x[0], x[1], 0), pt(x[0], x[1], railH+0.05), 0.07, 0.07, steel)
			}
		}
	}
}

// p0 is the primitive for a point (x, y, z).
func p0(prim func(e, n float64) *gltf.Primitive, q [3]float64) *gltf.Primitive {
	return prim(q[0], -q[2])
}

// pylonHeight is how high a pylon or tower stands over the deck at a.
func pylonHeight(b scenery.Bridge, f *structFrame, a, ground, span, share float64) float64 {
	h := share * span
	if b.HeightM > 0 {
		h = b.HeightM - (f.height(a) - ground)
	}
	return math.Max(pylonMinM, math.Min(pylonMaxM, h))
}

// cableStayed draws a pylon at a and its stays: over reach metres of the
// main span the way of dir, and back metres of back spans the other way.
func cableStayed(prim func(e, n float64) *gltf.Primitive, s *surface, f *structFrame, b scenery.Bridge, at, dir, reach, back float64, pylon, stay [3]float32) {
	lo, hi, ok := f.across(at)
	if !ok {
		return
	}
	mid := (lo + hi) / 2
	deck := f.height(at)
	pt := func(a, c, y float64) [3]float64 {
		e, n := f.toEN(a, c)
		return [3]float64{e, y, -n}
	}
	ge, gn := f.toEN(at, mid)
	H := pylonHeight(b, f, at, s.at(ge, gn), reach, pylonShare)
	p := p0(prim, pt(at, mid, deck))
	foot := deck - deckDepthM - 1.5 // on the pier, under the deck
	joint := deck + 0.45*H
	// The legs, outside the deck's edges, leaning in to the joint; the mast.
	for _, c := range []float64{lo - 2, hi + 2} {
		beam(p, pt(at, c, foot), pt(at, mid, joint), 3.0, 3.0, pylon)
	}
	beam(p, pt(at, mid, joint-1.5), pt(at, mid, deck+H), 3.5, 3.5, pylon)
	// A crossbeam under the deck between the legs, carrying it.
	beam(p, pt(at, lo-2, deck-deckDepthM-0.75), pt(at, hi+2, deck-deckDepthM-0.75), 1.5, 1.5, pylon)
	// The stays: in two planes, from the mast's upper part to the deck's
	// edges, the furthest from the top.
	fan := func(sign, reach float64) {
		end := f.a1 - at // the deck's end that way
		if sign < 0 {
			end = at - f.a0
		}
		reach = math.Min(reach*0.92, end*0.98)
		n := int(math.Round((reach - stayStartM) / stayEveryM))
		if n < 2 {
			return
		}
		for k := 0; k <= n; k++ {
			a := at + sign*(stayStartM+float64(k)*(reach-stayStartM)/float64(n))
			y := deck + 0.6*H + (0.37*H)*float64(k)/float64(n)
			l, r, ok := f.across(a)
			if !ok {
				continue
			}
			for _, side := range [][2]float64{{l + 0.4, mid - 1}, {r - 0.4, mid + 1}} {
				beam(p, pt(at, side[1], y), pt(a, side[0], f.height(a)+0.3), 0.25, 0.25, stay)
			}
		}
	}
	fan(dir, reach)
	fan(-dir, back)
}

// suspension draws towers at a0 and a1, the main cables between them and
// back to the deck's ends, and the hangers.
func suspension(prim func(e, n float64) *gltf.Primitive, s *surface, f *structFrame, b scenery.Bridge, a0, a1 float64, tower, steel [3]float32) {
	span := a1 - a0
	pt := func(a, c, y float64) [3]float64 {
		e, n := f.toEN(a, c)
		return [3]float64{e, y, -n}
	}
	lo0, hi0, _ := f.across(a0)
	ge, gn := f.toEN(a0, (lo0+hi0)/2)
	H := pylonHeight(b, f, a0, s.at(ge, gn), span, towerShare)
	tops := map[float64]float64{}
	for _, at := range []float64{a0, a1} {
		lo, hi, ok := f.across(at)
		if !ok {
			return
		}
		deck := f.height(at)
		p := p0(prim, pt(at, (lo+hi)/2, deck))
		tops[at] = deck + H
		for _, c := range []float64{lo - 1.5, hi + 1.5} {
			beam(p, pt(at, c, deck-deckDepthM-1.5), pt(at, c, deck+H), 2.5, 2.5, tower)
		}
		for _, y := range []float64{deck + H - 2, deck + 0.6*H, deck - deckDepthM - 0.75} {
			beam(p, pt(at, lo-1.5, y), pt(at, hi+1.5, y), 1.5, 1.8, tower)
		}
	}
	// A cable over each edge: a parabola between the towers' tops down to
	// 2 m over the deck at the middle, straight back to the deck's ends.
	cable := func(a float64) float64 {
		switch {
		case a < a0:
			t := (a - f.a0) / (a0 - f.a0)
			return f.height(f.a0) + 1 + t*(tops[a0]-f.height(f.a0)-1)
		case a > a1:
			t := (f.a1 - a) / (f.a1 - a1)
			return f.height(f.a1) + 1 + t*(tops[a1]-f.height(f.a1)-1)
		}
		t := (a - a0) / span
		low := f.height((a0+a1)/2) + 2
		top := tops[a0] + t*(tops[a1]-tops[a0])
		return top - 4*t*(1-t)*(top-low)
	}
	for side := range 2 {
		edge := func(a float64) float64 {
			lo, hi, _ := f.across(a)
			if side == 0 {
				return lo - 1.5
			}
			return hi + 1.5
		}
		for a := f.a0; a < f.a1; a += structBinM {
			b := math.Min(a+structBinM, f.a1)
			p := p0(prim, pt(a, edge(a), cable(a)))
			beam(p, pt(a, edge(a), cable(a)), pt(b, edge(b), cable(b)), 0.6, 0.6, steel)
		}
		for a := a0 + hangerEveryM; a < a1-hangerEveryM/2; a += hangerEveryM {
			p := p0(prim, pt(a, edge(a), cable(a)))
			beam(p, pt(a, edge(a), cable(a)), pt(a, edge(a), f.height(a)), 0.12, 0.12, steel)
		}
	}
}
