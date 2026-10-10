package world

import (
	"math"
	"sync"

	"github.com/digimago/osscycler/internal/scenery"
)

// Level of detail (plan step 7): the terrain is a grid of cells
// (Options.CellM) in blocks of lodBlockCells square; a block far from the
// route uses every second cell (lodMidM) or only its corners (beyond).
// The steps nest (1, 2, 10 cells), so where a finer block meets a coarser
// one, the finer one's edge vertices are put on the coarser one's edge
// (stitched): no cracks. Blocks with roads or water near stay fine: the
// roads' cut and verges, and the shores, are made for the full grid.
const (
	lodBlockCells = 10  // a block's side, in cells (50 m)
	lodNearM      = 50  // blocks this near the route: every cell
	lodMidM       = 150 // this near: every second cell; further, the block's corners
	lodMidStep    = 2
	lodFarStep    = lodBlockCells
)

type lod struct {
	t     *terrain
	cell  float64 // the grid's cell
	on    bool    // chunks are whole blocks (else every block is full)
	once  sync.Once
	busy  map[[2]int]bool // blocks with roads or water near: full
	mu    sync.Mutex
	steps map[[2]int]int
	raw   map[[2]int][]float64 // raw heights per chunk, (n+3)² with a cell's margin
}

func newLOD(t *terrain) *lod {
	size := t.o.ChunkM
	n := max(1, int(math.Round(size/t.o.CellM)))
	return &lod{t: t, cell: size / float64(n), on: n%lodBlockCells == 0,
		steps: map[[2]int]int{}, raw: map[[2]int][]float64{}}
}

// blockM is a block's side in metres.
func (l *lod) blockM() float64 { return l.cell * lodBlockCells }

// step is block (bi, bj)'s step in cells (global block indices).
func (l *lod) step(bi, bj int) int {
	if !l.on {
		return 1
	}
	l.once.Do(l.markBusy)
	k := [2]int{bi, bj}
	l.mu.Lock()
	s, ok := l.steps[k]
	l.mu.Unlock()
	if ok {
		return s
	}
	s = 1
	if !l.busy[k] && !l.wet(bi, bj) {
		b := l.blockM()
		d := math.Inf(1)
		for _, c := range l.t.coarse {
			d = math.Min(d, rectDist(c.e, c.n, float64(bi)*b, float64(bj)*b, b))
		}
		switch {
		case d >= lodMidM:
			s = lodFarStep
		case d >= lodNearM:
			s = lodMidStep
		}
	}
	l.mu.Lock()
	l.steps[k] = s
	l.mu.Unlock()
	return s
}

// markBusy marks the blocks a road (with its verges and a cell more)
// reaches.
func (l *lod) markBusy() {
	l.busy = map[[2]int]bool{}
	b := l.blockM()
	reach := vergeM + 2*l.cell + fineStep
	for _, s := range l.t.fine {
		for bi := int(math.Floor((s.e - reach) / b)); bi <= int(math.Floor((s.e+reach)/b)); bi++ {
			for bj := int(math.Floor((s.n - reach) / b)); bj <= int(math.Floor((s.n+reach)/b)); bj++ {
				l.busy[[2]int{bi, bj}] = true
			}
		}
	}
}

// wet tells whether any of block (bi, bj)'s grid vertices is in water.
func (l *lod) wet(bi, bj int) bool {
	if l.t.cover.m == nil {
		return false
	}
	for j := 0; j <= lodBlockCells; j++ {
		for i := 0; i <= lodBlockCells; i++ {
			e, n := float64(bi*lodBlockCells+i)*l.cell, float64(bj*lodBlockCells+j)*l.cell
			if land, _ := l.t.cover.at(e, n); land == scenery.LandWater {
				return true
			}
		}
	}
	return false
}

// height is the mesh's height at grid vertex (I, J): raw(I, J), or on a
// block edge shared with a coarser block, between that block's vertices
// along it.
func (l *lod) height(I, J int, raw func(I, J int) float64) float64 {
	B := lodBlockCells
	bi, bj := floorDiv(I, B), floorDiv(J, B)
	oi, oj := I-bi*B, J-bj*B
	switch {
	case oi == 0 && oj != 0: // on a north-south edge
		s := max(l.step(bi-1, bj), l.step(bi, bj))
		if r := oj % s; r != 0 {
			j0 := J - r
			f := float64(r) / float64(s)
			return raw(I, j0) + f*(raw(I, j0+s)-raw(I, j0))
		}
	case oj == 0 && oi != 0: // on an east-west edge
		s := max(l.step(bi, bj-1), l.step(bi, bj))
		if r := oi % s; r != 0 {
			i0 := I - r
			f := float64(r) / float64(s)
			return raw(i0, J) + f*(raw(i0+s, J)-raw(i0, J))
		}
	}
	return raw(I, J)
}

// at is the mesh's height at e, n: in the cell of its block's step, over
// the triangle the mesh has there. raw gives grid vertices' heights.
func (l *lod) at(e, n float64, raw func(I, J int) float64) float64 {
	B := lodBlockCells
	fi, fj := e/l.cell, n/l.cell
	I, J := int(math.Floor(fi)), int(math.Floor(fj))
	s := l.step(floorDiv(I, B), floorDiv(J, B))
	I0, J0 := I-mod(I, s), J-mod(J, s)
	u, v := (fi-float64(I0))/float64(s), (fj-float64(J0))/float64(s)
	h := func(di, dj int) float64 { return l.height(I0+di*s, J0+dj*s, raw) }
	// Cells split into (0,0), (1,0), (0,1) and (1,0), (1,1), (0,1), as
	// the mesh does. A finer neighbour's stitched edge vertices lie on
	// this cell's edge: the same surface.
	if u+v <= 1 {
		a := h(0, 0)
		return a + u*(h(1, 0)-a) + v*(h(0, 1)-a)
	}
	d := h(1, 1)
	return d + (1-u)*(h(0, 1)-d) + (1-v)*(h(1, 0)-d)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func mod(a, b int) int { return a - floorDiv(a, b)*b }
