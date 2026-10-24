package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func TestMarkDecks(t *testing.T) {
	// A road 10 m up crossing one on the ground, and a road alongside
	// that one 1 m lower: only the crossing becomes a deck, over the road
	// below and a few metres either side.
	line := func(e0, n0, de, dn, ele float64) *roadLine {
		l := &roadLine{}
		for i := -50; i <= 50; i++ {
			d := float64(i) * fineStep
			l.samples = append(l.samples, sample{d: d + 100, e: e0 + de*d, n: n0 + dn*d, ele: ele, hw: 2.5, edge: 2.5})
		}
		return l
	}
	upper, lower, beside := line(0, 0, 1, 0, 10), line(0, 0, 0, 1, 0), line(7, 0, 0, 1, -1)
	markDecks([]*roadLine{upper, lower, beside}, 0, false, false)
	var from, to float64
	n := 0
	for _, s := range upper.samples {
		if s.aloft {
			if n == 0 {
				from = s.e
			}
			to = s.e
			n++
		}
	}
	if n == 0 || from > -6 || to < 6 || from < -16 || to > 16 {
		t.Errorf("upper deck from %.0f to %.0f m, want across the road below and its neighbour, ending within 16 m", from, to)
	}
	for _, l := range []*roadLine{lower, beside} {
		for _, s := range l.samples {
			if s.aloft {
				t.Fatalf("a road on the ground marked as a deck at %.0f, %.0f", s.e, s.n)
			}
		}
	}
}

// A deck's edge beams are closed at the deck's ends (owner, 2026-10-12:
// the figure 8's overpass showed them open, their faces with nothing
// across): on each end of a full deck, a 0.4 m wide face across each beam
// from the abutment's top to the beam's.
func TestDeckBeamEndsAreClosed(t *testing.T) {
	w := trackWorld(t, course.Tracks()[1])
	ends := 0
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "bridge" {
			continue
		}
		for i := 0; i+2 < len(p.Indices); i += 3 {
			var v [3][3]float64
			for k := range 3 {
				j := p.Indices[i+k]
				v[k] = [3]float64{float64(p.Positions[3*j]), float64(p.Positions[3*j+1]), float64(p.Positions[3*j+2])}
			}
			u, q := sub3(v[1], v[0]), sub3(v[2], v[0])
			n := cross(u, q)
			l := math.Sqrt(dot3(n, n))
			if l == 0 || math.Abs(n[1])/l > 0.1 {
				continue // not a vertical face
			}
			lo, hi := math.Inf(1), math.Inf(-1)
			var across float64
			for a := range 3 {
				lo, hi = math.Min(lo, v[a][1]), math.Max(hi, v[a][1])
				for b := range 3 {
					across = math.Max(across, math.Hypot(v[a][0]-v[b][0], v[a][2]-v[b][2]))
				}
			}
			if across > edgeBeamW-0.05 && across < edgeBeamW+0.05 && hi-lo > edgeBeamH && hi-lo < edgeBeamH+0.05 {
				ends++
			}
		}
	}
	if ends < 8 { // two ends, two beams, two triangles each
		t.Errorf("%d triangles across the beams' ends, want 8 or more", ends)
	}
}

func sub3(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
