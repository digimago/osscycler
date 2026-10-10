package world

import (
	"math"
	"testing"
)

func TestLODIsContinuous(t *testing.T) {
	// A route along the diagonal: blocks near it full, then every second
	// cell, then corners only, changing both east-west and north-south. Over a bumpy ground, the surface is the
	// same on both sides of every block edge (no cracks), and exact at
	// the vertices a block keeps.
	tr := &terrain{o: Options{ChunkM: 250, CellM: 5}, cover: &cover{}}
	for d := -500.0; d <= 500; d += 10 {
		tr.coarse = append(tr.coarse, sample{e: d, n: d})
	}
	l := newLOD(tr)
	raw := func(I, J int) float64 {
		e, n := float64(I)*l.cell, float64(J)*l.cell
		return 10*math.Sin(e/37) + 7*math.Cos(n/23) + 0.01*e*n/50
	}
	seen := map[int]bool{}
	for bj := -1; bj <= 5; bj++ {
		seen[l.step(-2, bj)] = true
	}
	if !seen[1] || !seen[lodMidStep] || !seen[lodFarStep] {
		t.Fatalf("steps %v, want all three going away from the route", seen)
	}
	const eps = 1e-7
	b := l.blockM()
	for bj := -1; bj <= 5; bj++ {
		for bi := -6; bi <= 6; bi++ {
			for f := 0.013; f < 1; f += 0.0731 {
				// Across the block's west edge, and its south edge.
				e, n := float64(bi)*b, (float64(bj)+f)*b
				if a, c := l.at(e-eps, n, raw), l.at(e+eps, n, raw); math.Abs(a-c) > 1e-4 {
					t.Fatalf("crack across the edge at %.1f, %.1f: %.4f west, %.4f east", e, n, a, c)
				}
				e, n = (float64(bi)+f)*b, float64(bj)*b
				if a, c := l.at(e, n-eps, raw), l.at(e, n+eps, raw); math.Abs(a-c) > 1e-4 {
					t.Fatalf("crack across the edge at %.1f, %.1f: %.4f south, %.4f north", e, n, a, c)
				}
			}
			// The block's own vertices, inside it.
			s := l.step(bi, bj)
			for j := s; j < lodBlockCells; j += s {
				for i := s; i < lodBlockCells; i += s {
					I, J := bi*lodBlockCells+i, bj*lodBlockCells+j
					if got, want := l.at(float64(I)*l.cell, float64(J)*l.cell, raw), raw(I, J); math.Abs(got-want) > 1e-6 {
						t.Fatalf("vertex %d, %d at %.4f, want %.4f", I, J, got, want)
					}
				}
			}
		}
	}
}
