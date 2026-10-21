package world

import (
	"fmt"
	"math"
	"runtime"
	"sync"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Distant terrain (owner, 2026-10-18: the lack of far-away terrain
// bothered on the Hohneck, where the road climbs out over the valleys).
// Beyond the corridor's chunks, out to Options.FarM, the ground model at
// farCellM is laid as a coarse mesh, but only where it can be seen from the
// route: a cell is kept when some point of the path (the rider's eye, the
// chase camera a little higher) sees its top over the land between, and it
// stands at least farMinDeg above or below a level gaze from there (a flat
// valley floor kilometres off is a line in the haze). Measured on the
// Hohneck loop: 32 % of the land within 5 km is ever seen; a polder keeps
// next to nothing. The mesh meets the near land at the chunks' edges at the
// near land's own height, with a skirt down behind the seam.
const (
	farCellM      = 50.0   // the far mesh's cells, and the ground model's there
	farTileM      = 1000.0 // a node per tile of this size
	farEyeM       = 2.5    // the eye over the road the viewshed looks from (the chase camera's)
	farTopM       = 5.0    // a cell counts as seen when its ground this much up is in sight
	farViewEveryM = 100.0  // the path's viewpoints this far apart
	farMinDeg     = 0.1    // a seen cell under this angle from a level gaze is left out
	farSkirtM     = 4.0    // the skirt down behind the seam with the near land
)

// farGrid is the ground model on the far mesh's grid: corner heights, and
// which cells are within reach of the route.
type farGrid struct {
	i0, j0 int // the south-west corner's cell index (east, north) in farCellM
	w, h   int // cells
	z      []float64
	in     []bool
}

func (g *farGrid) corner(i, j int) float64 { return g.z[j*(g.w+1)+i] }

// cellTop is cell i, j's height (the mean of its corners).
func (g *farGrid) cellTop(i, j int) float64 {
	return (g.corner(i, j) + g.corner(i+1, j) + g.corner(i, j+1) + g.corner(i+1, j+1)) / 4
}

// at is the ground at e, n on the grid (bilinear), NaN outside it.
func (g *farGrid) at(e, n float64) float64 {
	x, y := e/farCellM-float64(g.i0), n/farCellM-float64(g.j0)
	i, j := int(math.Floor(x)), int(math.Floor(y))
	if i < 0 || j < 0 || i >= g.w || j >= g.h {
		return math.NaN()
	}
	fx, fy := x-float64(i), y-float64(j)
	return (1-fy)*((1-fx)*g.corner(i, j)+fx*g.corner(i+1, j)) + fy*((1-fx)*g.corner(i, j+1)+fx*g.corner(i+1, j+1))
}

// addFarTerrain adds the distant terrain (nodes "far e n" per farTileM
// tile, extras kind "far"); keys are the near terrain's chunks. It returns
// how many cells it laid.
func (w *World) addFarTerrain(t *terrain, keys [][2]int, material int) (int, error) {
	o := t.o
	if o.FarElevation == nil || o.FarM <= 0 || len(t.coarse) < 2 {
		return 0, nil
	}
	sty := styleFor(o.Region)
	// The route's points every farViewEveryM: viewpoints, and what "within
	// reach" is measured from.
	var pts [][3]float64
	next := 0.0
	for _, s := range t.coarse {
		if s.d >= next {
			pts = append(pts, [3]float64{s.e, s.n, s.ele})
			next = s.d + farViewEveryM
		}
	}
	lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		lo = [2]float64{math.Min(lo[0], p[0]), math.Min(lo[1], p[1])}
		hi = [2]float64{math.Max(hi[0], p[0]), math.Max(hi[1], p[1])}
	}
	g := &farGrid{
		i0: int(math.Floor((lo[0] - o.FarM) / farCellM)),
		j0: int(math.Floor((lo[1] - o.FarM) / farCellM)),
	}
	g.w = int(math.Ceil((hi[0]+o.FarM)/farCellM)) - g.i0
	g.h = int(math.Ceil((hi[1]+o.FarM)/farCellM)) - g.j0
	g.z = make([]float64, (g.w+1)*(g.h+1))
	missing := make([]bool, len(g.z))
	parallel(g.h+1, func(j int) {
		for i := 0; i <= g.w; i++ {
			e, n := float64(g.i0+i)*farCellM, float64(g.j0+j)*farCellM
			lat, lon := t.c.Unproject(e, n)
			if h, ok := o.FarElevation(lat, lon); ok {
				g.z[j*(g.w+1)+i] = h
			} else {
				missing[j*(g.w+1)+i] = true
			}
		}
	})
	// Within reach of the route: a disc of FarM round each point.
	g.in = make([]bool, g.w*g.h)
	r := int(math.Ceil(o.FarM / farCellM))
	for _, p := range pts {
		ci, cj := int(math.Floor(p[0]/farCellM))-g.i0, int(math.Floor(p[1]/farCellM))-g.j0
		for dj := -r; dj <= r; dj++ {
			for di := -r; di <= r; di++ {
				if float64(di*di+dj*dj)*farCellM*farCellM > o.FarM*o.FarM {
					continue
				}
				if i, j := ci+di, cj+dj; i >= 0 && j >= 0 && i < g.w && j < g.h {
					g.in[j*g.w+i] = true
				}
			}
		}
	}
	// Cells without heights (beyond the ground model's coverage) are out.
	for j := 0; j < g.h; j++ {
		for i := 0; i < g.w; i++ {
			if missing[j*(g.w+1)+i] || missing[j*(g.w+1)+i+1] || missing[(j+1)*(g.w+1)+i] || missing[(j+1)*(g.w+1)+i+1] {
				g.in[j*g.w+i] = false
			}
		}
	}
	// The near terrain's cells (whole chunks: a multiple of farCellM).
	near := make([]bool, g.w*g.h)
	chunkCells := int(math.Round(o.ChunkM / farCellM))
	for _, k := range keys {
		for dj := 0; dj < chunkCells; dj++ {
			for di := 0; di < chunkCells; di++ {
				i, j := k[0]*chunkCells+di-g.i0, k[1]*chunkCells+dj-g.j0
				if i >= 0 && j >= 0 && i < g.w && j < g.h {
					near[j*g.w+i] = true
				}
			}
		}
	}
	// Viewpoints: the eye over the road.
	vps := make([][3]float64, len(pts))
	for k, p := range pts {
		vps[k] = [3]float64{p[0], p[1], p[2] + farEyeM}
	}
	seen := make([]bool, g.w*g.h)
	minTan := math.Tan(farMinDeg * math.Pi / 180)
	parallel(g.h, func(j int) {
		for i := 0; i < g.w; i++ {
			k := j*g.w + i
			if !g.in[k] || near[k] {
				continue
			}
			te, tn := (float64(g.i0+i)+0.5)*farCellM, (float64(g.j0+j)+0.5)*farCellM
			top := g.cellTop(i, j) + farTopM
		views:
			for _, v := range vps {
				de, dn := te-v[0], tn-v[1]
				l := math.Hypot(de, dn)
				if l < farCellM || math.Abs(top-v[2]) < minTan*l {
					continue // too near to matter, or too flat to see
				}
				steps := int(l / (farCellM / 2))
				for s := 1; s < steps; s++ {
					f := float64(s) / float64(steps)
					if h := g.at(v[0]+de*f, v[1]+dn*f); !math.IsNaN(h) && h > v[2]+(top-v[2])*f {
						continue views
					}
				}
				seen[k] = true
				break
			}
		}
	})
	// Kept: seen, and the cells round them (the silhouette's edge).
	keep := make([]bool, g.w*g.h)
	for j := 0; j < g.h; j++ {
		for i := 0; i < g.w; i++ {
			if !seen[j*g.w+i] {
				continue
			}
			for dj := -1; dj <= 1; dj++ {
				for di := -1; di <= 1; di++ {
					if a, b := i+di, j+dj; a >= 0 && b >= 0 && a < g.w && b < g.h && g.in[b*g.w+a] && !near[b*g.w+a] {
						keep[b*g.w+a] = true
					}
				}
			}
		}
	}
	// A corner on the near terrain's edge takes the near land's height
	// there (the land as drawn), so the two meet.
	nearCorner := func(i, j int) bool {
		for _, d := range [4][2]int{{-1, -1}, {0, -1}, {-1, 0}, {0, 0}} {
			if a, b := i+d[0], j+d[1]; a >= 0 && b >= 0 && a < g.w && b < g.h && near[b*g.w+a] {
				return true
			}
		}
		return false
	}
	height := func(i, j int) float64 {
		if nearCorner(i, j) && t.landAt != nil {
			if h := t.landAt(float64(g.i0+i)*farCellM, float64(g.j0+j)*farCellM); !math.IsNaN(h) {
				return h
			}
		}
		return g.corner(i, j)
	}
	meadow, forest := sty.landColour(scenery.LandMeadow), sty.landColour(scenery.LandForest)
	colour := func(i, j int) [3]float32 {
		// Steep slopes wooded, open land in patches between (no land use
		// is known out here).
		gx := (g.corner(min(i+1, g.w), j) - g.corner(max(i-1, 0), j)) / (2 * farCellM)
		gy := (g.corner(i, min(j+1, g.h)) - g.corner(i, max(j-1, 0))) / (2 * farCellM)
		e, n := float64(g.i0+i)*farCellM, float64(g.j0+j)*farCellM
		f := math.Max(0, math.Min(1, 0.25+1.6*math.Hypot(gx, gy)+0.6*(valueNoise(e/400, n/400, 0xfa7)-0.5)))
		var c [3]float32
		for k := range 3 {
			c[k] = meadow[k] + float32(f)*(forest[k]-meadow[k])
		}
		return c
	}
	// The tiles.
	tile := int(math.Round(farTileM / farCellM))
	type tkey [2]int
	tiles := map[tkey]*gltf.Primitive{}
	idx := map[tkey]map[[2]int]uint32{}
	var order []tkey
	vertex := func(p *gltf.Primitive, m map[[2]int]uint32, i, j int) uint32 {
		if v, ok := m[[2]int{i, j}]; ok {
			return v
		}
		c := colour(i, j)
		p.Positions = append(p.Positions, float32(float64(g.i0+i)*farCellM), float32(height(i, j)), float32(-float64(g.j0+j)*farCellM))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, c[0], c[1], c[2])
		v := uint32(len(p.Positions)/3 - 1)
		m[[2]int{i, j}] = v
		return v
	}
	up := [3]float64{0, 1, 0}
	cells := 0
	for j := 0; j < g.h; j++ {
		for i := 0; i < g.w; i++ {
			if !keep[j*g.w+i] {
				continue
			}
			k := tkey{int(math.Floor(float64(g.i0+i) / float64(tile))), int(math.Floor(float64(g.j0+j) / float64(tile)))}
			p := tiles[k]
			if p == nil {
				p = &gltf.Primitive{Material: material}
				tiles[k], idx[k] = p, map[[2]int]uint32{}
				order = append(order, k)
			}
			m := idx[k]
			a, b, c, d := vertex(p, m, i, j), vertex(p, m, i+1, j), vertex(p, m, i+1, j+1), vertex(p, m, i, j+1)
			tri(p, a, b, c, up)
			tri(p, a, c, d, up)
			cells++
			// The skirt down behind the seam with the near land, and
			// along the far land's own edge (seen from the route only
			// from the near side, it hides the void under the edge).
			for _, s := range [4]struct{ di, dj, a0, b0, a1, b1 int }{
				{0, -1, 0, 0, 1, 0}, {1, 0, 1, 0, 1, 1}, {0, 1, 1, 1, 0, 1}, {-1, 0, 0, 1, 0, 0},
			} {
				ni, nj := i+s.di, j+s.dj
				if ni >= 0 && nj >= 0 && ni < g.w && nj < g.h && keep[nj*g.w+ni] {
					continue
				}
				x0, y0, x1, y1 := i+s.a0, j+s.b0, i+s.a1, j+s.b1
				h0, h1 := height(x0, y0), height(x1, y1)
				pa := [3]float64{float64(g.i0+x0) * farCellM, h0, -float64(g.j0+y0) * farCellM}
				pb := [3]float64{float64(g.i0+x1) * farCellM, h1, -float64(g.j0+y1) * farCellM}
				nrm := [3]float64{float64(s.di), 0, -float64(s.dj)}
				col := colour(x0, y0)
				base := uint32(len(p.Positions) / 3)
				for _, q := range [4][3]float64{pa, pb, {pb[0], h1 - farSkirtM, pb[2]}, {pa[0], h0 - farSkirtM, pa[2]}} {
					p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
					p.Normals = append(p.Normals, float32(nrm[0]), 0, float32(nrm[2]))
					p.Colors = append(p.Colors, col[0], col[1], col[2])
				}
				tri(p, base, base+1, base+2, nrm)
				tri(p, base, base+2, base+3, nrm)
			}
		}
	}
	// In a fixed order: the same world every build.
	sortTiles := func(a, b tkey) bool { return a[1] < b[1] || a[1] == b[1] && a[0] < b[0] }
	for x := 1; x < len(order); x++ {
		for y := x; y > 0 && sortTiles(order[y], order[y-1]); y-- {
			order[y], order[y-1] = order[y-1], order[y]
		}
	}
	for _, k := range order {
		p := tiles[k]
		smoothNormals(p)
		if err := w.add(fmt.Sprintf("far %d %d", k[0], k[1]), map[string]any{"kind": "far", "tile": k}, *p); err != nil {
			return 0, err
		}
	}
	return cells, nil
}

// parallel runs f(0..n-1) on every core.
func parallel(n int, f func(int)) {
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range next {
				f(k)
			}
		}()
	}
	for k := range n {
		next <- k
	}
	close(next)
	wg.Wait()
}
