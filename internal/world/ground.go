package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"sync"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// The ground map: what the ground is like in every groundCellM (1 m) cell
// near the route, for renderers to grow grass and small clutter around the
// rider as they go (generated there, not stored: it would be millions of
// instances). Two planes of a byte per cell, each row by row from the
// south-west corner of a terrain chunk, east first: the land use (1 + the
// land use, GroundClasses; 0 beyond the map's reach of the route), then
// the clearance at the cell's middle (the distance to the nearest road
// edge with its sidewalks, building wall or car park edge, in steps of
// groundClearStepM, 0 on or in one). Interpolated between cells, the
// clearance puts the edge of what grows to within centimetres of a road's
// edge, whatever the cell size: a renderer keeps a plant where its
// clearance there is more than half the plant's width (owner, 2026-10-09:
// grass to the verge's edge). Each chunk is compressed (zlib) on its own,
// in GroundFile; chunks farther than groundReachM from the route are left
// out: the rider never leaves it.
const (
	GroundFile       = "ground.bin"
	groundCellM      = 1.0
	groundReachM     = 50.0
	groundClearStepM = 0.1 // so a byte holds up to 25.5 m
	// waterClearM: grass near water has no more room than half its
	// distance from the water (short tufts on a bank).
	waterClearM = 3.0
)

// GroundLayout describes the ground map's planes, for the manifest: then
// the ground's height in every cell (as the verges and the terrain have
// it), in centimetres above the chunk's lowest (its first 4 bytes, an
// int32 of centimetres), each cell as an int16 difference from the one
// west of it (from 0 at a row's start), little-endian; 0 beyond the map.
const GroundLayout = "land, clearance_0.1m, height_cm_delta"

// GroundClasses names the ground map's land values, by value. A sown
// field (scenery.Fields) is its crop and the way its rows run, in eighths
// of a half turn anticlockwise from east: "maize 3" (rows at 67.5°), from
// groundFieldBase on.
var GroundClasses = func() []string {
	out := []string{"none", "grass", "meadow", "farmland", "forest", "built", "water", "orchard", "heath",
		"dune sand", "dune grass", "dune scrub", "beach"}
	for _, crop := range groundCrops {
		for d := range groundRowDirs {
			out = append(out, fmt.Sprintf("%s %d", crop, d))
		}
	}
	return out
}()

// groundCrops are the sown fields' names in the ground map, in the order of
// scenery.LandPloughed on.
var groundCrops = []string{"ploughed", "maize", "wheat", "potatoes"}

const (
	groundFieldBase = 13 // the first sown field's value
	groundRowDirs   = 8  // row directions per crop
)

// groundValue is the ground map's land value at e, n.
func groundValue(sp *spots, e, n float64) (byte, scenery.Land) {
	if sp.land == nil {
		return byte(scenery.LandNone) + 1, scenery.LandNone
	}
	l, id := sp.land.Area(e, n)
	if l >= scenery.LandPloughed && l <= scenery.LandPotatoes {
		d := int(math.Round(sp.land.RowDir(id)/(math.Pi/groundRowDirs))) % groundRowDirs
		return byte(groundFieldBase + int(l-scenery.LandPloughed)*groundRowDirs + d), l
	}
	return byte(l) + 1, l
}

// groundChunk is where one chunk's map is in GroundFile.
type groundChunk struct {
	Chunk  [2]int `json:"chunk"`
	Offset int    `json:"offset"`
	Size   int    `json:"size"`
}

// groundMap builds the ground map of the chunks keys (those near the
// route), in parallel: the file's bytes and where each chunk is.
func groundMap(sp *spots, t *terrain, s *surface, drawn *landSurface, keys [][2]int, chunkM float64, route *routeGrid) ([]byte, []groundChunk) {
	n := int(math.Round(chunkM / groundCellM))
	// The route's samples on a grid of the reach itself: the grid given
	// has 400 m cells, and a cell's worth of samples was scanned for
	// every metre of the map.
	route = newRouteGrid(route.fine, groundReachM)
	near := func(e, no float64) bool {
		hit := false
		route.within(e, no, groundReachM, func(int) { hit = true })
		return hit
	}
	maps := make([][]byte, len(keys))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for k := range next {
				e0, n0 := float64(keys[k][0])*chunkM, float64(keys[k][1])*chunkM
				cells := make([]byte, 2*n*n) // land, then clearance
				heights := make([]float64, n*n)
				any := false
				for j := range n {
					for i := range n {
						e, no := e0+(float64(i)+0.5)*groundCellM, n0+(float64(j)+0.5)*groundCellM
						if !near(e, no) {
							continue
						}
						v, land := groundValue(sp, e, no)
						cells[j*n+i] = v
						// The land as drawn; the ground model only where
						// nothing is drawn (a hole), as it costs a road search.
						h, ok := 0.0, false
						if drawn != nil {
							h, ok = drawn.at(e, no)
						}
						if !ok {
							h = s.ground(e, no)
						}
						heights[j*n+i] = h
						if land != scenery.LandWater {
							c := sp.clearance(e, no, 255*groundClearStepM)
							cells[n*n+j*n+i] = byte(math.Round(c / groundClearStepM))
						}
						any = true
					}
				}
				if !any {
					continue
				}
				// Banks: little room for grass within waterClearM of water
				// (owner, 2026-10-09: less clutter on the banks), as by a
				// road's edge.
				water := byte(scenery.LandWater) + 1
				const reach = int(waterClearM / groundCellM)
				for j := range n {
					for i := range n {
						k := j*n + i
						if cells[k] == 0 || cells[k] == water {
							continue
						}
						best := waterClearM
						for dj := -reach; dj <= reach; dj++ {
							for di := -reach; di <= reach; di++ {
								ii, jj := i+di, j+dj
								if ii < 0 || jj < 0 || ii >= n || jj >= n || cells[jj*n+ii] != water {
									continue
								}
								best = math.Min(best, math.Max(0, math.Hypot(float64(di), float64(dj))*groundCellM-groundCellM/2))
							}
						}
						if c := best / 2; best < waterClearM {
							cells[n*n+k] = min(cells[n*n+k], byte(math.Round(c/groundClearStepM)))
						}
					}
				}
				// Heights: centimetres above the lowest, as differences
				// along each row.
				low := math.Inf(1)
				for k, h := range heights {
					if cells[k] != 0 {
						low = math.Min(low, h)
					}
				}
				hb := make([]byte, 4+2*n*n)
				binary.LittleEndian.PutUint32(hb, uint32(int32(math.Floor(low*100))))
				base := math.Floor(low * 100)
				for j := range n {
					prev := int64(0)
					for i := range n {
						k := j*n + i
						cm := int64(0)
						if cells[k] != 0 {
							cm = int64(math.Round(heights[k]*100 - base))
						}
						binary.LittleEndian.PutUint16(hb[4+2*k:], uint16(int16(cm-prev)))
						prev = cm
					}
				}
				var buf bytes.Buffer
				z := zlib.NewWriter(&buf)
				z.Write(cells)
				z.Write(hb)
				z.Close()
				maps[k] = buf.Bytes()
			}
		})
	}
	for k := range keys {
		next <- k
	}
	close(next)
	wg.Wait()

	var out []byte
	var index []groundChunk
	for k, m := range maps {
		if m == nil {
			continue
		}
		index = append(index, groundChunk{Chunk: keys[k], Offset: len(out), Size: len(m)})
		out = append(out, m...)
	}
	return out, index
}

// landSurface is the land as drawn (terrain and verges, after the roads
// are cut out): what the ground map's heights come from, so the grass a
// renderer grows stands on what is drawn. The ground model sampled every
// metre stood up to a metre off the drawn surface on steep banks (the
// terrain's cells are 5 m, the verges skip cross-sections), and grass
// floated over the Diepesteeg's banks (owner, 2026-10-09, 3.35 km).
type landSurface struct {
	// tri holds each triangle's corners (x y z, three times) as float64,
	// mat its material; cells lists the triangles per landCellM cell.
	// Flat copies rather than the primitives' indices and positions: a
	// lookup reads 10 words per triangle instead of 18; under the race
	// detector, which counts every read, a graph build of the figure 8
	// went from 36 to 26 s.
	tri   []float64
	mat   []int32
	cells map[[2]int][]int32
}

const (
	landCellM   = 2.0
	landMaxTriM = 12.0 // larger triangles lie beyond the ground map's reach (far terrain blocks)
)

func newLandSurface(prims []gltf.Primitive, land func(material int) bool) *landSurface {
	return newSurfaceIndex(prims, land, landMaxTriM)
}

// newSurfaceIndex is a landSurface over the triangles of the selected
// materials up to maxTri across (roads: their junctions' triangles are
// tens of metres).
func newSurfaceIndex(prims []gltf.Primitive, land func(material int) bool, maxTri float64) *landSurface {
	ls := &landSurface{cells: map[[2]int][]int32{}}
	for _, p := range prims {
		if !land(p.Material) {
			continue
		}
		pos := p.Positions
		for t := 0; t+2 < len(p.Indices); t += 3 {
			lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
			var c [9]float64
			for k := range 3 {
				v := p.Indices[t+k]
				c[3*k], c[3*k+1], c[3*k+2] = float64(pos[3*v]), float64(pos[3*v+1]), float64(pos[3*v+2])
				x, z := c[3*k], c[3*k+2]
				lo, hi = [2]float64{math.Min(lo[0], x), math.Min(lo[1], z)}, [2]float64{math.Max(hi[0], x), math.Max(hi[1], z)}
			}
			if hi[0]-lo[0] > maxTri || hi[1]-lo[1] > maxTri {
				continue
			}
			id := int32(len(ls.mat))
			ls.tri = append(ls.tri, c[:]...)
			ls.mat = append(ls.mat, int32(p.Material))
			for gx := int(math.Floor(lo[0] / landCellM)); gx <= int(math.Floor(hi[0]/landCellM)); gx++ {
				for gz := int(math.Floor(lo[1] / landCellM)); gz <= int(math.Floor(hi[1]/landCellM)); gz++ {
					k := [2]int{gx, gz}
					ls.cells[k] = append(ls.cells[k], id)
				}
			}
		}
	}
	return ls
}

// heightOf is triangle id's height at x, z (glTF axes) by barycentric
// coordinates; false where the point is outside it or it has no area.
func (ls *landSurface) heightOf(id int32, x, z float64) (float64, bool) {
	c := ls.tri[9*id : 9*id+9]
	px, py, pz := [3]float64{c[0], c[3], c[6]}, [3]float64{c[1], c[4], c[7]}, [3]float64{c[2], c[5], c[8]}
	den := (pz[1]-pz[2])*(px[0]-px[2]) + (px[2]-px[1])*(pz[0]-pz[2])
	if math.Abs(den) < 1e-12 {
		return 0, false
	}
	a := ((pz[1]-pz[2])*(x-px[2]) + (px[2]-px[1])*(z-pz[2])) / den
	b := ((pz[2]-pz[0])*(x-px[2]) + (px[0]-px[2])*(z-pz[2])) / den
	c3 := 1 - a - b
	const eps = -1e-6
	if a < eps || b < eps || c3 < eps {
		return 0, false
	}
	return a*py[0] + b*py[1] + c3*py[2], true
}

// at is the drawn land's height at e, n (the highest where pieces
// overlap); false where no land is drawn.
func (ls *landSurface) at(e, n float64) (float64, bool) {
	return ls.top(e, n, true)
}

// matAt is the material of the highest surface at e, n.
func (ls *landSurface) matAt(e, n float64) (int, bool) {
	x, z := e, -n
	best, mat, ok := math.Inf(-1), 0, false
	for _, id := range ls.cells[[2]int{int(math.Floor(x / landCellM)), int(math.Floor(z / landCellM))}] {
		if h, in := ls.heightOf(id, x, z); in && h > best {
			best, mat, ok = h, int(ls.mat[id]), true
		}
	}
	return mat, ok
}

// low is the lowest surface at e, n (where surfaces overlap: a junction's
// over a road's end, with a step between).
func (ls *landSurface) low(e, n float64) (float64, bool) {
	return ls.top(e, n, false)
}

// anyWithin says whether any surface triangle may lie within r of e, n:
// whether the cells the square of that reach covers hold any. False means
// at, low and top are false at every point within r (its cell is among
// these), so a caller probing all round a point far from every surface
// can skip the probes: the land blend probed six rings of eight points
// at every land vertex, most of them in open land, a third of a build.
func (ls *landSurface) anyWithin(e, n, r float64) bool {
	x, z := e, -n
	for gx := int(math.Floor((x - r) / landCellM)); gx <= int(math.Floor((x+r)/landCellM)); gx++ {
		for gz := int(math.Floor((z - r) / landCellM)); gz <= int(math.Floor((z+r)/landCellM)); gz++ {
			if len(ls.cells[[2]int{gx, gz}]) > 0 {
				return true
			}
		}
	}
	return false
}

func (ls *landSurface) top(e, n float64, highest bool) (float64, bool) {
	x, z := e, -n
	best, ok := math.Inf(-1), false
	if !highest {
		best = math.Inf(1)
	}
	for _, id := range ls.cells[[2]int{int(math.Floor(x / landCellM)), int(math.Floor(z / landCellM))}] {
		if h, in := ls.heightOf(id, x, z); in && (h > best == highest || !ok) {
			best, ok = h, true
		}
	}
	return best, ok
}
