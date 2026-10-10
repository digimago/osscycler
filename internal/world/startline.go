package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// The start and finish line of a loop: a chequered band across the
// carriageway, painted on the road, from the line itself forward (as the
// TUI draws it).
const (
	lineRows    = 3
	lineSquareM = 0.5
	lineLiftM   = 0.012 // above the road, as cycle lanes are
)

// addStartLine paints the line at the start of the path (d, x east, y
// north, z elevation, w the carriageway's width).
func (w *World) addStartLine(d, x, y, z, wd []float64) error {
	n := len(d)
	if n < 3 {
		return nil
	}
	// The heading through the start: from the point before it (round the
	// loop; the last point is the first again) to the one after.
	pe, pn := x[n-2], y[n-2]
	if math.Hypot(x[n-1]-x[0], y[n-1]-y[0]) > 1 {
		pe, pn = x[0], y[0] // not closed: from the start itself
	}
	de, dn := x[1]-pe, y[1]-pn
	l := math.Hypot(de, dn)
	if l == 0 {
		return nil
	}
	de, dn = de/l, dn/l
	re, rn := dn, -de // to the right
	grade := (z[1] - z[0]) / math.Max(d[1]-d[0], 1e-6)
	cols := max(2, int(math.Round(wd[0]/lineSquareM)))
	sq := wd[0] / float64(cols)
	white, black := srgbLinear(0xe8e8e4), srgbLinear(0x1e1e1e)
	mat := w.doc.AddMaterial(gltf.Material{Name: "start line", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.7})
	p := gltf.Primitive{Material: mat}
	at := func(along, across float64) [3]float64 {
		e := x[0] + de*along + re*across
		no := y[0] + dn*along + rn*across
		return [3]float64{e, z[0] + grade*along + lineLiftM, -no}
	}
	up := [3]float64{0, 1, 0}
	for r := range lineRows {
		a0, a1 := float64(r)*lineSquareM, float64(r+1)*lineSquareM
		for c := range cols {
			c0, c1 := -wd[0]/2+float64(c)*sq, -wd[0]/2+float64(c+1)*sq
			col := white
			if (r+c)%2 == 1 {
				col = black
			}
			// Counter-clockwise seen from above.
			quad(&p, at(a0, c0), at(a0, c1), at(a1, c1), at(a1, c0), up, col)
		}
	}
	return w.add("start line", map[string]any{"kind": "start line"}, p)
}
