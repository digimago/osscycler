package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// Small pieces of land shut in by road surfaces all round, where roads
// meet at angles their surfaces don't close (owner, 2026-10-11: a green
// triangle of ground in the asphalt where three roads meet on a new
// course), are paved: asphalt just under the roads round them, so their
// edges stay the roads'. Larger enclosed land (splitter islands, verges
// between carriageways) stays land.
const (
	islandCellM  = 0.5
	islandMaxM2  = 6.0   // larger enclosed land is left as it is
	islandBelowM = 0.005 // under the roads round it, so theirs are the edges
)

// pavedIslands finds, per chunk, the small pieces of land enclosed by the
// road surfaces in roads (those of material ground excepted: verges), and
// returns a mesh of material mat covering each chunk's.
func pavedIslands(roads map[[2]int][]gltf.Primitive, ground, mat int, chunkM float64) map[[2]int]gltf.Primitive {
	out := map[[2]int]gltf.Primitive{}
	for _, k := range chunkOrder(roads) {
		if p, ok := chunkIslands(roads[k], k, ground, mat, chunkM); ok {
			out[k] = p
		}
	}
	return out
}

func chunkIslands(ps []gltf.Primitive, key [2]int, ground, mat int, chunkM float64) (gltf.Primitive, bool) {
	side := int(math.Round(chunkM / islandCellM))
	x0, z0 := float64(key[0])*chunkM, -float64(key[1]+1)*chunkM // glTF x east, z south
	h := make([]float64, side*side)
	for i := range h {
		h[i] = math.NaN()
	}
	for _, p := range ps {
		if p.Material == ground {
			continue
		}
		for t := 0; t+2 < len(p.Indices); t += 3 {
			var x, y, z [3]float64
			for c := range 3 {
				v := p.Indices[t+c]
				x[c], y[c], z[c] = float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])
			}
			den := (z[1]-z[2])*(x[0]-x[2]) + (x[2]-x[1])*(z[0]-z[2])
			if math.Abs(den) < 1e-9 {
				continue // a wall: covers nothing from above
			}
			i0 := max(0, int(math.Floor((min(x[0], x[1], x[2])-x0)/islandCellM)))
			i1 := min(side-1, int(math.Floor((max(x[0], x[1], x[2])-x0)/islandCellM)))
			j0 := max(0, int(math.Floor((min(z[0], z[1], z[2])-z0)/islandCellM)))
			j1 := min(side-1, int(math.Floor((max(z[0], z[1], z[2])-z0)/islandCellM)))
			for j := j0; j <= j1; j++ {
				for i := i0; i <= i1; i++ {
					px, pz := x0+(float64(i)+0.5)*islandCellM, z0+(float64(j)+0.5)*islandCellM
					a := ((z[1]-z[2])*(px-x[2]) + (x[2]-x[1])*(pz-z[2])) / den
					b := ((z[2]-z[0])*(px-x[2]) + (x[0]-x[2])*(pz-z[2])) / den
					c := 1 - a - b
					if a < 0 || b < 0 || c < 0 {
						continue
					}
					hy := a*y[0] + b*y[1] + c*y[2]
					if q := &h[j*side+i]; math.IsNaN(*q) || hy > *q {
						*q = hy
					}
				}
			}
		}
	}
	maxCells := int(islandMaxM2 / (islandCellM * islandCellM))
	seen := make([]bool, side*side)
	var pos []float32
	var idx []uint32
	for start := range h {
		if seen[start] || !math.IsNaN(h[start]) {
			continue
		}
		// The piece of land this cell is in, unless it grows too big or
		// reaches the chunk's edge (open land, or out of this chunk's view).
		comp := []int{start}
		seen[start] = true
		open := false
		var rim float64
		rimN := 0
		for q := 0; q < len(comp); q++ {
			c := comp[q]
			i, j := c%side, c/side
			if i == 0 || j == 0 || i == side-1 || j == side-1 {
				open = true
			}
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				ni, nj := i+d[0], j+d[1]
				if ni < 0 || nj < 0 || ni >= side || nj >= side {
					continue
				}
				n := nj*side + ni
				if !math.IsNaN(h[n]) {
					rim += h[n]
					rimN++
					continue
				}
				if !seen[n] {
					seen[n] = true
					comp = append(comp, n)
				}
			}
			if len(comp) > maxCells {
				open = true
			}
		}
		if open || rimN == 0 {
			continue
		}
		y := float32(rim/float64(rimN) - islandBelowM)
		for _, c := range comp {
			i, j := c%side, c/side
			// A cell and a little more, so neighbouring cells and the roads
			// round it overlap rather than leave hairlines.
			ax, az := x0+float64(i)*islandCellM-0.05, z0+float64(j)*islandCellM-0.05
			bx, bz := ax+islandCellM+0.1, az+islandCellM+0.1
			base := uint32(len(pos) / 3)
			pos = append(pos, float32(ax), y, float32(az), float32(bx), y, float32(az), float32(bx), y, float32(bz), float32(ax), y, float32(bz))
			idx = append(idx, base, base+2, base+1, base, base+3, base+2)
		}
	}
	if len(idx) == 0 {
		return gltf.Primitive{}, false
	}
	nrm := make([]float32, len(pos))
	for i := 1; i < len(nrm); i += 3 {
		nrm[i] = 1
	}
	uv := make([]float32, 0, len(pos)/3*2)
	for i := 0; i < len(pos); i += 3 {
		uv = append(uv, pos[i]/4, pos[i+2]/4) // the road's texture scale: a tile every 4 m
	}
	return gltf.Primitive{Material: mat, Positions: pos, Normals: nrm, UVs: uv, Indices: idx}, true
}
