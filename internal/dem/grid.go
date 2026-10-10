// Package dem reads digital elevation models: terrain heights on a regular
// grid in a projected coordinate system, fetched from a web service and
// cached as GeoTIFF tiles.
//
// The first source is the Dutch AHN (Actueel Hoogtebestand Nederland), the
// bare-ground model (DTM) from PDOK, in RD New (EPSG:28992) with heights
// above NAP. AHN is open data under CC0.
package dem

import "math"

// Grid is a raster of heights in metres. Cell (i, j), column i and row j,
// covers x from X0+i·DX and y down from Y0−j·DY, so X0, Y0 is the top-left
// corner of the first cell. Cells without data are NaN.
type Grid struct {
	X0, Y0 float64
	DX, DY float64
	W, H   int
	Z      []float32
}

// At returns the height at x, y, interpolated between the four nearest
// cell centres; false outside the grid or where a neighbour has no data.
func (g *Grid) At(x, y float64) (float64, bool) {
	fx := (x-g.X0)/g.DX - 0.5
	fy := (g.Y0-y)/g.DY - 0.5
	i, j := int(math.Floor(fx)), int(math.Floor(fy))
	// Half a cell at the edges takes the edge value.
	i, tx := clampCell(i, fx-float64(i), g.W)
	j, ty := clampCell(j, fy-float64(j), g.H)
	if i < 0 || j < 0 {
		return 0, false
	}
	i1, j1 := min(i+1, g.W-1), min(j+1, g.H-1)
	z00, z10 := g.Z[j*g.W+i], g.Z[j*g.W+i1]
	z01, z11 := g.Z[j1*g.W+i], g.Z[j1*g.W+i1]
	z := (float64(z00)*(1-tx)+float64(z10)*tx)*(1-ty) + (float64(z01)*(1-tx)+float64(z11)*tx)*ty
	if math.IsNaN(z) {
		return 0, false
	}
	return z, true
}

// clampCell keeps a cell index in 0..n-1, holding the edge value over the
// half cell beyond the outermost centre; -1 when outside the grid.
func clampCell(i int, t float64, n int) (int, float64) {
	switch {
	case i < -1 || i > n-1 || (i == -1 && t < 0.5) || (i == n-1 && t > 0.5):
		return -1, 0
	case i == -1:
		return 0, 0
	case i == n-1:
		return n - 1, 0
	}
	return i, t
}

// Fill gives cells without data the mean of their neighbours with data,
// growing inwards from the edges of each hole, for at most passes rings.
// AHN's ground model has holes under buildings and on water.
func (g *Grid) Fill(passes int) {
	next := make([]float32, len(g.Z))
	for range passes {
		copy(next, g.Z)
		changed := false
		for j := range g.H {
			for i := range g.W {
				if !isNaN(g.Z[j*g.W+i]) {
					continue
				}
				var sum float64
				n := 0
				for dj := -1; dj <= 1; dj++ {
					for di := -1; di <= 1; di++ {
						ii, jj := i+di, j+dj
						if ii < 0 || jj < 0 || ii >= g.W || jj >= g.H {
							continue
						}
						if z := g.Z[jj*g.W+ii]; !isNaN(z) {
							sum += float64(z)
							n++
						}
					}
				}
				if n > 0 {
					next[j*g.W+i] = float32(sum / float64(n))
					changed = true
				}
			}
		}
		g.Z, next = next, g.Z
		if !changed {
			return
		}
	}
}

func isNaN(z float32) bool { return z != z }
