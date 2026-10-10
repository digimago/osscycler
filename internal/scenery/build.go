package scenery

import (
	"math"
	"strconv"
	"strings"

	"github.com/digimago/osscycler/internal/course"
)

// Land is a kind of land use; the values up to LandHeath match the API's
// LandUse. The dune classes after it are the 3D world's own (ByTheSea),
// never in a Scenery the API sends.
type Land uint8

const (
	LandNone Land = iota
	LandMeadow
	LandFarmland
	LandForest
	LandBuilt
	LandWater
	LandOrchard
	LandHeath
	LandDuneSand  // dunes.go
	LandDuneGrass //
	LandDuneScrub //
	LandBeach     //
)

// BuildingKind matches the API's BuildingKind.
type BuildingKind uint8

const (
	KindHouse BuildingKind = iota + 1
	KindFlat
	KindBarn
)

// Building is a building as a box aligned with the road.
type Building struct {
	DistanceM float64 // along the course, of its centre
	OffsetM   float64 // from the centre line to its centre; right is positive
	LengthM   float64 // along the road
	DepthM    float64 // across it
	HeightM   float64 // to the top of the roof
	Kind      BuildingKind
}

// Scenery is what lies along a course.
type Scenery struct {
	// Land has four entries per profile sample: left far, left near, right
	// near, right far (FarM and NearM from the centre line).
	Land      []Land
	Buildings []Building
	// The roads the route meets where it matters, car parks and the places
	// it enters, by distance along the course.
	Junctions []Junction
	Parking   []Parking
	Signs     []PlaceSign
	// Roads is what the route rides on, stretch by stretch, from start to
	// finish (for the 3D world; the TUI draws its own road).
	Roads []RoadStretch
	// Ways are the roads of the map near the route, as the 3D world draws
	// them; RouteOn says which one the route rides on every RouteStepM
	// (an index into Ways, -1 where it rides on none of them).
	Ways       []Way
	RouteOn    []int
	RouteStepM float64
}

const (
	NearM = 15.0
	FarM  = 60.0
	// buildingRangeM is how far from the road buildings are kept.
	buildingRangeM = 80.0
	// buildingClearM keeps buildings off the road where the GPX wanders.
	buildingClearM = 4.0
	gridM          = 250.0
)

// Build classifies the land along c and places the buildings near it.
func Build(c *course.Course, d *Data) *Scenery {
	east, north := c.Track()
	n := len(east)
	lm := NewLandMap(c, d)
	landAt := func(x, y float64) Land {
		land, _ := lm.Area(x, y)
		return land
	}

	sc := &Scenery{Land: make([]Land, 0, 4*n)}
	for i := range n {
		dx, dy := direction(east, north, i)
		rx, ry := dy, -dx // right of the direction of travel
		for _, off := range []float64{-FarM, -NearM, NearM, FarM} {
			sc.Land = append(sc.Land, landAt(east[i]+rx*off, north[i]+ry*off))
		}
	}
	sc.Buildings = buildings(c, d, east, north)

	r := sampleRoute(c)
	roads := roadsOf(c, d)
	own := ownRoads(r, roads)
	sc.Junctions = junctions(r, roads, own)
	sc.Ways = ways(roads, func(x, y float64) bool { land, _ := lm.Area(x, y); return land == LandBuilt })
	sc.RouteOn, sc.RouteStepM = bridgeCorners(own), r[len(r)-1].d/float64(len(r)-1)
	sc.Roads = routeRoads(r, roads, own, func(d float64, side int) bool {
		i := max(0, min(int(math.Round(d/c.Spacing)), n-1))
		return sc.Land[4*i+1+side] == LandBuilt // left near, right near
	})
	var entrances []Junction
	sc.Parking, entrances = parkings(c, d, r, roads, east, north)
	for _, j := range entrances {
		sc.Junctions = addJunction(sc.Junctions, j)
	}
	sortJunctions(sc.Junctions)
	sc.Signs = placeSigns(c, d, r)
	return sc
}

// direction is the unit direction of travel at sample i, over a few
// samples to steady GPS wobble.
func direction(east, north []float64, i int) (dx, dy float64) {
	a, b := max(0, i-2), min(len(east)-1, i+2)
	dx, dy = east[b]-east[a], north[b]-north[a]
	l := math.Hypot(dx, dy)
	if l == 0 {
		return 0, 1
	}
	return dx / l, dy / l
}

func cell(v float64) int { return int(math.Floor(v / gridM)) }

// LandMap tells the land use anywhere near a course, in its frame
// (course.Project: metres east and north of the start).
type LandMap struct {
	polys []*polygon
	grid  map[[2]int][]int
}

// NewLandMap indexes the areas in d.
func NewLandMap(c *course.Course, d *Data) *LandMap {
	m := &LandMap{polys: polygons(c, d), grid: map[[2]int][]int{}}
	for i, p := range m.polys {
		for gx := cell(p.minX); gx <= cell(p.maxX); gx++ {
			for gy := cell(p.minY); gy <= cell(p.maxY); gy++ {
				m.grid[[2]int{gx, gy}] = append(m.grid[[2]int{gx, gy}], i)
			}
		}
	}
	return m
}

// Area is the land use at x, y and the area that has it (-1 for none):
// the smallest of the areas there, so a park inside a residential area is
// a park.
func (m *LandMap) Area(x, y float64) (Land, int) {
	best, bestArea, id := LandNone, math.Inf(1), -1
	for _, i := range m.grid[[2]int{cell(x), cell(y)}] {
		if p := m.polys[i]; p.area < bestArea && p.contains(x, y) {
			best, bestArea, id = p.land, p.area, i
		}
	}
	return best, id
}

// LeafType is area id's leaf_type tag ("needleleaved", "broadleaved",
// "mixed"), "" when the map doesn't say.
func (m *LandMap) LeafType(id int) string {
	if id < 0 || id >= len(m.polys) {
		return ""
	}
	return m.polys[id].leaf
}

// Edges are the outline of area id, as x0, y0, x1, y1 segments (rings of
// a multipolygon, inner ones too, possibly in pieces).
func (m *LandMap) Edges(id int) [][4]float64 { return m.polys[id].edges }

// Waters lists the ids of the water areas.
func (m *LandMap) Waters() []int {
	var out []int
	for i, p := range m.polys {
		if p.land == LandWater {
			out = append(out, i)
		}
	}
	return out
}

// Size is area id's area in m² and its outline's length in m (rings of a
// multipolygon counted alike: holes add to the area).
func (m *LandMap) Size(id int) (area, outline float64) {
	for _, e := range m.polys[id].edges {
		area += e[0]*e[3] - e[2]*e[1]
		outline += math.Hypot(e[2]-e[0], e[3]-e[1])
	}
	return math.Abs(area) / 2, outline
}

// polygon is an area in metres around the start, as edges: rings of a
// multipolygon may come in pieces, and even-odd counting over all of them
// still tells inside from outside.
type polygon struct {
	land                   Land
	leaf                   string // OpenStreetMap's leaf_type (forests)
	natural                string // its natural tag (ByTheSea tells dunes by it)
	edges                  [][4]float64
	minX, minY, maxX, maxY float64
	area                   float64 // of the bounding box; smaller wins
	// bands lists the edges crossing each bandM-high band from minY, so
	// a point is tested against the edges at its height only (a forest
	// can have thousands).
	bands [][]int32
}

const bandM = 25.0

func (p *polygon) index() {
	p.bands = make([][]int32, int((p.maxY-p.minY)/bandM)+1)
	for i, e := range p.edges {
		lo, hi := math.Min(e[1], e[3]), math.Max(e[1], e[3])
		for b := int((lo - p.minY) / bandM); b <= int((hi-p.minY)/bandM) && b < len(p.bands); b++ {
			p.bands[b] = append(p.bands[b], int32(i))
		}
	}
}

func (p *polygon) contains(x, y float64) bool {
	if x < p.minX || x > p.maxX || y < p.minY || y > p.maxY {
		return false
	}
	in := false
	for _, i := range p.bands[min(int((y-p.minY)/bandM), len(p.bands)-1)] {
		e := p.edges[i]
		if (e[1] > y) != (e[3] > y) && x < e[0]+(y-e[1])*(e[2]-e[0])/(e[3]-e[1]) {
			in = !in
		}
	}
	return in
}

func polygons(c *course.Course, d *Data) []*polygon {
	var out []*polygon
	for _, el := range d.Elements {
		land := classify(el.Tags)
		if land == LandNone {
			continue
		}
		p := &polygon{land: land, leaf: el.Tags["leaf_type"], natural: el.Tags["natural"], minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
		add := func(ring []LatLon) {
			for j := 1; j < len(ring); j++ {
				x0, y0 := c.Project(ring[j-1].Lat, ring[j-1].Lon)
				x1, y1 := c.Project(ring[j].Lat, ring[j].Lon)
				p.edges = append(p.edges, [4]float64{x0, y0, x1, y1})
				p.minX, p.maxX = math.Min(p.minX, math.Min(x0, x1)), math.Max(p.maxX, math.Max(x0, x1))
				p.minY, p.maxY = math.Min(p.minY, math.Min(y0, y1)), math.Max(p.maxY, math.Max(y0, y1))
			}
		}
		switch el.Type {
		case "way":
			if g := el.Geometry; len(g) < 4 || g[0] != g[len(g)-1] {
				continue // not an area
			}
			add(el.Geometry)
		case "relation":
			if el.Tags["type"] != "multipolygon" {
				continue
			}
			for _, m := range el.Members {
				if m.Type == "way" && (m.Role == "outer" || m.Role == "inner" || m.Role == "") {
					add(m.Geometry)
				}
			}
		}
		if len(p.edges) < 3 {
			continue
		}
		p.area = (p.maxX - p.minX) * (p.maxY - p.minY)
		p.index()
		out = append(out, p)
	}
	return out
}

// classify maps OpenStreetMap area tags to a land use.
func classify(t map[string]string) Land {
	switch t["natural"] {
	case "wood":
		return LandForest
	case "water":
		return LandWater
	case "grassland", "wetland":
		return LandMeadow
	case "scrub", "heath", "sand", "beach", "fell":
		return LandHeath
	}
	switch t["landuse"] {
	case "forest":
		return LandForest
	case "farmland":
		return LandFarmland
	case "meadow", "grass", "village_green", "recreation_ground", "cemetery", "allotments", "flowerbed":
		return LandMeadow
	case "residential", "commercial", "retail", "industrial", "construction", "garages",
		"farmyard", "railway", "education", "greenhouse_horticulture", "religious":
		return LandBuilt
	case "orchard", "vineyard", "plant_nursery":
		return LandOrchard
	case "reservoir", "basin":
		return LandWater
	}
	switch t["leisure"] {
	case "park", "garden", "golf_course":
		return LandMeadow
	}
	return LandNone
}

// buildings places every building near the road as a box aligned with it.
func buildings(c *course.Course, d *Data, east, north []float64) []Building {
	// Samples by grid cell, to find the nearest one quickly.
	samples := map[[2]int][]int{}
	for i := range east {
		k := [2]int{cell(east[i]), cell(north[i])}
		samples[k] = append(samples[k], i)
	}
	var out []Building
	for _, el := range d.Elements {
		b := el.Tags["building"]
		if el.Type != "way" || b == "" || b == "no" || len(el.Geometry) < 4 {
			continue
		}
		pts := el.Geometry[:len(el.Geometry)-1] // closed: the last repeats the first
		xs, ys := make([]float64, len(pts)), make([]float64, len(pts))
		var cx, cy float64
		for j, p := range pts {
			xs[j], ys[j] = c.Project(p.Lat, p.Lon)
			cx, cy = cx+xs[j]/float64(len(pts)), cy+ys[j]/float64(len(pts))
		}
		near, nearD := -1, math.Inf(1)
		for gx := cell(cx) - 1; gx <= cell(cx)+1; gx++ {
			for gy := cell(cy) - 1; gy <= cell(cy)+1; gy++ {
				for _, i := range samples[[2]int{gx, gy}] {
					if dd := math.Hypot(east[i]-cx, north[i]-cy); dd < nearD {
						near, nearD = i, dd
					}
				}
			}
		}
		if near < 0 || nearD > buildingRangeM+30 {
			continue
		}
		// Extents along the road and across it.
		dx, dy := direction(east, north, near)
		rx, ry := dy, -dx
		a0, a1, o0, o1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for j := range xs {
			px, py := xs[j]-east[near], ys[j]-north[near]
			a, o := px*dx+py*dy, px*rx+py*ry
			a0, a1, o0, o1 = math.Min(a0, a), math.Max(a1, a), math.Min(o0, o), math.Max(o1, o)
		}
		length, depth := a1-a0, o1-o0
		if length*depth < 12 { // sheds and kiosks don't show
			continue
		}
		off := (o0 + o1) / 2
		if math.Abs(off) > buildingRangeM {
			continue
		}
		// Keep the near wall off the road.
		if clear := math.Abs(off) - depth/2; clear < buildingClearM {
			off = math.Copysign(buildingClearM+depth/2, off)
		}
		kind, height := buildingShape(el.Tags, length*depth)
		out = append(out, Building{
			DistanceM: float64(near)*c.Spacing + (a0+a1)/2, OffsetM: off,
			LengthM: length, DepthM: depth, HeightM: height, Kind: kind,
		})
	}
	sortBuildings(out)
	return out
}

func sortBuildings(bs []Building) {
	for i := 1; i < len(bs); i++ { // nearly sorted already: insertion sort
		for j := i; j > 0 && bs[j].DistanceM < bs[j-1].DistanceM; j-- {
			bs[j], bs[j-1] = bs[j-1], bs[j]
		}
	}
}

// buildingShape guesses the roof and height from the tags, then the
// footprint.
func buildingShape(t map[string]string, area float64) (BuildingKind, float64) {
	kind, height := KindHouse, 8.0
	switch t["building"] {
	case "apartments", "commercial", "retail", "office", "school", "hospital", "hotel", "university", "public", "civic", "dormitory":
		kind, height = KindFlat, 12
	case "industrial", "warehouse", "supermarket", "sports_hall", "factory":
		kind, height = KindFlat, 9
	case "garage", "garages", "carport":
		kind, height = KindFlat, 3
	case "barn", "farm_auxiliary", "cowshed", "stable", "sty", "greenhouse", "shed", "hut", "agricultural":
		kind, height = KindBarn, 8
	case "church", "chapel", "cathedral":
		kind, height = KindHouse, 18
	case "yes":
		if area > 600 {
			kind, height = KindFlat, 9
		}
	}
	if kind == KindBarn && area < 40 {
		height = 3.5
	}
	if lv, err := strconv.ParseFloat(t["building:levels"], 64); err == nil && lv > 0 {
		height = lv * 3
		if kind != KindFlat {
			height += 3 // the roof
		}
	}
	if h, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t["height"]), "m")), 64); err == nil && h > 0 {
		height = h
	}
	return kind, math.Min(height, 150)
}
