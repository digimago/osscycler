package world

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
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

// GroundClasses names the ground map's land values, by value.
var GroundClasses = []string{"none", "grass", "meadow", "farmland", "forest", "built", "water", "orchard", "heath",
	"dune sand", "dune grass", "dune scrub", "beach"}

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
						land, _ := sp.landAt(e, no)
						cells[j*n+i] = byte(land) + 1
						heights[j*n+i] = s.ground(e, no)
						if drawn != nil {
							if h, ok := drawn.at(e, no); ok {
								heights[j*n+i] = h
							}
						}
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
	prims []gltf.Primitive
	cells map[[2]int][][2]int32 // per landCellM cell: primitive, triangle
}

const (
	landCellM   = 2.0
	landMaxTriM = 12.0 // larger triangles lie beyond the ground map's reach (far terrain blocks)
)

func newLandSurface(prims []gltf.Primitive, land func(material int) bool) *landSurface {
	ls := &landSurface{cells: map[[2]int][][2]int32{}}
	for _, p := range prims {
		if !land(p.Material) {
			continue
		}
		pi := int32(len(ls.prims))
		ls.prims = append(ls.prims, p)
		pos := p.Positions
		for t := 0; t+2 < len(p.Indices); t += 3 {
			lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
			for k := range 3 {
				v := p.Indices[t+k]
				x, z := float64(pos[3*v]), float64(pos[3*v+2])
				lo, hi = [2]float64{math.Min(lo[0], x), math.Min(lo[1], z)}, [2]float64{math.Max(hi[0], x), math.Max(hi[1], z)}
			}
			if hi[0]-lo[0] > landMaxTriM || hi[1]-lo[1] > landMaxTriM {
				continue
			}
			for gx := int(math.Floor(lo[0] / landCellM)); gx <= int(math.Floor(hi[0]/landCellM)); gx++ {
				for gz := int(math.Floor(lo[1] / landCellM)); gz <= int(math.Floor(hi[1]/landCellM)); gz++ {
					k := [2]int{gx, gz}
					ls.cells[k] = append(ls.cells[k], [2]int32{pi, int32(t)})
				}
			}
		}
	}
	return ls
}

// at is the drawn land's height at e, n (the highest where pieces
// overlap); false where no land is drawn.
func (ls *landSurface) at(e, n float64) (float64, bool) {
	x, z := e, -n
	best, ok := math.Inf(-1), false
	for _, r := range ls.cells[[2]int{int(math.Floor(x / landCellM)), int(math.Floor(z / landCellM))}] {
		p := ls.prims[r[0]]
		var px, py, pz [3]float64
		for k := range 3 {
			v := p.Indices[int(r[1])+k]
			px[k], py[k], pz[k] = float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])
		}
		den := (pz[1]-pz[2])*(px[0]-px[2]) + (px[2]-px[1])*(pz[0]-pz[2])
		if math.Abs(den) < 1e-12 {
			continue
		}
		a := ((pz[1]-pz[2])*(x-px[2]) + (px[2]-px[1])*(z-pz[2])) / den
		b := ((pz[2]-pz[0])*(x-px[2]) + (px[0]-px[2])*(z-pz[2])) / den
		c := 1 - a - b
		const eps = -1e-6
		if a < eps || b < eps || c < eps {
			continue
		}
		if h := a*py[0] + b*py[1] + c*py[2]; h > best {
			best, ok = h, true
		}
	}
	return best, ok
}
