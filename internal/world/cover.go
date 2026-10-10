package world

import (
	"math"
	"sort"
	"sync"

	"github.com/digimago/osscycler/internal/scenery"
)

// cover is the land cover on the terrain: the land use from map data, and
// the banks of each water area, which set its level.
type cover struct {
	m *scenery.LandMap // nil: grass everywhere

	mu    sync.Mutex
	banks map[int]*banks
}

const (
	verge   = 1.0 // m of grass beside the asphalt, whatever the land beyond
	verdure = scenery.LandNone
	bedM    = 1.0 // the ground under water lies this far below its level
	// bankStepM spaces the points on a water's outline whose ground sets
	// its level.
	bankStepM = 5.0
	// The level sits below nearly all of the bank: under bankPercentile
	// of its heights by freeboardM. Too low shows a bank down to the water,
	// as along a canal; too high would spill the water over the land.
	bankPercentile = 0.05
	freeboardM     = 0.5 // lower than first set (owner, 2026-10-09: ditches and ponds stand out more)
	// localM: the level at a point comes from the banks this close, so a
	// river falls along its length and two crossings of it, kilometres
	// apart, each get their own; minLocal bank points are needed for that,
	// else the whole area's banks count.
	localM   = 400.0
	minLocal = 8
	bankCell = 100.0
)

func (c *cover) at(e, n float64) (scenery.Land, int) {
	if c == nil || c.m == nil {
		return scenery.LandNone, -1
	}
	return c.m.Area(e, n)
}

// banks are the points on a water area's outline with their ground, by
// grid cell, and the level from all of them.
type banks struct {
	e, n, h []float64
	cells   map[[2]int][]int32
	all     float64
}

// level is the water's surface at e, n in area id: low among the heights
// of its banks nearby. It depends on the position alone, so the vertices
// chunks share agree and the water of neighbouring chunks meets.
func (c *cover) level(t *terrain, id int, e, n float64) float64 {
	b := c.banksOf(t, id)
	var hs []float64
	for gx := int(math.Floor((e - localM) / bankCell)); gx <= int(math.Floor((e+localM)/bankCell)); gx++ {
		for gy := int(math.Floor((n - localM) / bankCell)); gy <= int(math.Floor((n+localM)/bankCell)); gy++ {
			for _, k := range b.cells[[2]int{gx, gy}] {
				if math.Hypot(b.e[k]-e, b.n[k]-n) <= localM {
					hs = append(hs, b.h[k])
				}
			}
		}
	}
	if len(hs) < minLocal {
		return b.all
	}
	return lowLevel(hs)
}

func lowLevel(hs []float64) float64 {
	sort.Float64s(hs)
	return hs[int(bankPercentile*float64(len(hs)-1))] - freeboardM
}

func (c *cover) banksOf(t *terrain, id int) *banks {
	c.mu.Lock()
	b, ok := c.banks[id]
	c.mu.Unlock()
	if ok {
		return b
	}
	b = c.findBanks(t, id) // the same every time: no harm if two chunks race
	c.mu.Lock()
	c.banks[id] = b
	c.mu.Unlock()
	return b
}

// findBanks takes the ground along the water's outline where the terrain
// reaches. The ground model has no heights on water, so the banks are
// what it knows: only points where it has one count (the profile's shape
// says nothing about a bank). With no ground model at all the banks are
// the ground as drawn, the road's shaping included: the profile's shape
// alone had stood the figure 8's ditches up to 0.5 m above the road beside
// them where it dips below the course around it (owner, 2026-10-09, 1 km).
func (c *cover) findBanks(t *terrain, id int) *banks {
	reach := t.o.CorridorM + 1.5*t.o.ChunkM
	b := &banks{cells: map[[2]int][]int32{}}
	var farH []float64
	type lists struct {
		segs []int
		near []sample
	}
	byChunk := map[[2]int]lists{}
	drawn := func(e, n float64) float64 {
		key := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
		l, ok := byChunk[key]
		if !ok {
			l.segs, l.near = t.lists(key)
			byChunk[key] = l
		}
		return t.height(e, n, l.segs, l.near)
	}
	for _, ed := range c.m.Edges(id) {
		l := math.Hypot(ed[2]-ed[0], ed[3]-ed[1])
		steps := max(1, int(math.Ceil(l/bankStepM)))
		for k := range steps {
			f := float64(k) / float64(steps)
			e, n := ed[0]+f*(ed[2]-ed[0]), ed[1]+f*(ed[3]-ed[1])
			h, ok := t.model(e, n, t.coarse)
			near := nearest(e, n, t.coarse)
			far := math.Hypot(near.e-e, near.n-n) > reach
			if t.o.Elevation == nil {
				h, ok = t.fromProfile(e, n, t.coarse), true
				if !far {
					h = drawn(e, n)
				}
			}
			if !ok {
				continue
			}
			if far {
				if len(farH) < 64 {
					farH = append(farH, h)
				}
				continue
			}
			key := [2]int{int(math.Floor(e / bankCell)), int(math.Floor(n / bankCell))}
			b.cells[key] = append(b.cells[key], int32(len(b.h)))
			b.e, b.n, b.h = append(b.e, e), append(b.n, n), append(b.h, h)
		}
	}
	switch {
	case len(b.h) > 0:
		b.all = lowLevel(append([]float64(nil), b.h...))
	case len(farH) > 0:
		b.all = lowLevel(farH)
	}
	return b
}

// landColors are the terrain's vertex colours per land use, linear RGB
// (glTF's COLOR_0), multiplied by the white terrain material: the TUI's
// palette, which renderers may replace with textures by class later.
var landColors = func() [12][3]float32 {
	srgb := srgbLinear
	var p [12][3]float32
	p[scenery.LandNone] = srgb(0x5a9e3a)     // grass
	p[scenery.LandMeadow] = srgb(0x6aa848)   // a lighter green
	p[scenery.LandFarmland] = srgb(0x8fa03c) // crops
	p[scenery.LandForest] = srgb(0x2e5227)   // forest floor
	p[scenery.LandBuilt] = srgb(0x8a937c)    // gardens, yards, paving
	p[scenery.LandWater] = srgb(0x4a5038)    // the bed, under the water
	p[scenery.LandOrchard] = srgb(0x66a644)
	p[scenery.LandHeath] = srgb(0x866a78) // heather
	// Dunes: pale sand, the grey dune's grey-green turf and moss, the
	// thickets' darker floor; the beach paler still, dry sand.
	p[scenery.LandDuneSand] = srgb(0xcdbd94)
	p[scenery.LandDuneGrass] = srgb(0x8a9064)
	p[scenery.LandDuneScrub] = srgb(0x6c7248)
	p[scenery.LandBeach] = srgb(0xddd0ae)
	return p
}()

// srgbLinear is an sRGB colour (0xRRGGBB) in linear RGB, as glTF wants it.
func srgbLinear(hex uint32) [3]float32 {
	var out [3]float32
	for i, shift := range []uint{16, 8, 0} {
		v := float64(hex>>shift&0xff) / 255
		if v <= 0.04045 {
			v /= 12.92
		} else {
			v = math.Pow((v+0.055)/1.055, 2.4)
		}
		out[i] = float32(v)
	}
	return out
}
