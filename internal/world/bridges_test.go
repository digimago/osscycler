package world

import "testing"

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
	markDecks([]*roadLine{upper, lower, beside}, 0)
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
