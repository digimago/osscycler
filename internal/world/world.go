// Package world builds a course's 3D world for renderers: terrain around
// the route, coloured by land use with flat water where map data is
// given, the road on it, and a manifest that ties both to the ride.
//
// A world is an engine-neutral package in a directory: world.glb (glTF 2.0)
// and world.json (the Manifest). Renderers load the model and place the
// rider from the state stream's distance along the course with
// Manifest.Path, so the world never decides where the rider is.
//
// The frame is glTF's: metres, +Y up, x east and z south of the course's
// start (a flat projection, as course.Track), y the elevation the course
// uses. The road follows the course profile exactly, since that is what
// the ride simulates; the terrain comes from a ground model (a DEM) where
// one is given, shifted to agree with the profile, and is pressed flat
// under the road and blended back to the ground model beside it. Without
// a ground model the terrain is shaped from the profile alone.
package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Files in a world directory.
const (
	ModelFile    = "world.glb"
	ManifestFile = "world.json"
	Format       = "osscycler-world"
	Version      = 1
)

// Options shape a world. Zero values take the defaults.
type Options struct {
	CellM      float64 // terrain grid spacing, default 5 m
	ChunkM     float64 // side of a terrain chunk (a node of its own), default 250 m
	CorridorM  float64 // terrain reaches at least this far from the road, default 300 m
	RoadWidthM float64 // where the map doesn't have the road, default 5 m
	BlendM     float64 // without a ground model, the terrain meets the profile shape this far from the road, default 40 m

	// Elevation is the ground model, in the course's height datum; nil
	// shapes the terrain from the course profile alone.
	Elevation func(lat, lon float64) (float64, bool)
	// TerrainSource names the ground model for the manifest.
	TerrainSource string
	// Land is the land use around the course (from map data), which
	// colours the terrain and puts water in it; nil leaves it all grass.
	Land *scenery.LandMap
	// Scenery is what lies along the course (from the same map data): the
	// road the route rides on (its width, surface, sidewalks and cycle
	// lanes), the roads branching off and the car parks; nil gives a
	// plain road RoadWidthM wide.
	Scenery *scenery.Scenery
	// Buildings are the footprints of the buildings near the road (from
	// the same map data), built with walls and roofs.
	Buildings []scenery.Footprint
	// Fences stand along the roads (the test tracks' polder).
	Fences []scenery.Fence
	// Lawns are mown grass where nothing grows (a pitch), in metres east
	// and north of the course's start.
	Lawns [][][2]float64
	// Region is the country's style (RegionNL, RegionFR; style.go): what
	// grows, what houses, gardens and roads look like. "" is the Dutch.
	Region string
	// FarElevation is a coarse ground model (farCellM) out to FarM from
	// the route, in the same datum as Elevation, for the distant terrain
	// (far.go); nil or FarM 0: none.
	FarElevation func(lat, lon float64) (float64, bool)
	FarM         float64
	// SeaDistance is how far a point is from the sea, in metres (+Inf far
	// inland): trees grow lower towards it. Nil: no coast known.
	SeaDistance func(lat, lon float64) float64
	// Progress, when set, hears how far the build is, 0 to 1 (from the
	// terrain's goroutines too: it must be safe for that).
	Progress func(done float64)
	// Attribution lists the credits the sources require.
	Attribution []string
	Generator   string
	Stamp       string // for the manifest: what the world is built from
}

func (o *Options) defaults() {
	def := func(v *float64, d float64) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&o.CellM, 5)
	def(&o.ChunkM, 250)
	def(&o.CorridorM, 300)
	def(&o.RoadWidthM, 5)
	def(&o.BlendM, 40)
	if o.Generator == "" {
		o.Generator = "osscycler"
	}
}

// Manifest describes a world package; it is world.json.
type Manifest struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	Generator string `json:"generator"`
	// Stamp identifies what the world was built from (the course and the
	// builder, worlds.Stamp): a world whose stamp differs from the
	// course's now is out of date.
	Stamp string `json:"stamp,omitempty"`
	// Builder names the builder that made it (engine.go): "graph" (worlds
	// from before 2026-10-21 may say "classic", or nothing).
	Builder string `json:"builder,omitempty"`
	Course  struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		DistanceM float64 `json:"distance_m"`
		Loop      bool    `json:"loop"`
	} `json:"course"`
	// Origin is the course's start, the frame's 0, 0.
	Origin struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"origin"`
	Frame   string `json:"frame"`
	Model   string `json:"model"`
	Terrain struct {
		Source    string  `json:"source"`
		CellM     float64 `json:"cell_m"`
		ChunkM    float64 `json:"chunk_m"`
		CorridorM float64 `json:"corridor_m"`
		Chunks    int     `json:"chunks"`
		// LandCover names where the terrain's colours (COLOR_0, by land
		// use) and water come from: "OpenStreetMap", or "none" (grass).
		LandCover string `json:"land_cover"`
		// WaterChunks counts the chunks with a water node.
		WaterChunks int `json:"water_chunks"`
		// FarM: distant terrain out to this from the route (nodes "far",
		// far.go), for the renderer's haze and view distance; 0: none.
		FarM float64 `json:"far_m,omitempty"`
	} `json:"terrain"`
	Road struct {
		// WidthM is the road's width where the map doesn't have it.
		WidthM float64 `json:"width_m"`
		// Keep is the side riders keep to: "right" (continental Europe),
		// or "left". Renderers put the rider's eyes there, about 1 m in
		// from that edge of the carriageway; the ride itself follows the
		// centre line (Path), which is what distance is measured on.
		Keep string `json:"keep"`
	} `json:"road"`
	// Path is the road's centre line every StepM metres from the start
	// (exactly: the course's length over the points' number, a little under
	// 5 m),
	// x y z triples in the frame: the rider at distance d is between
	// points d/StepM and the next. WidthM is the carriageway's width at
	// each point (without sidewalks); LaneM is the riding line there: how
	// far right of the centre line (negative: left) a rider rides, in the
	// middle of their half and through bends out-in-out.
	Path struct {
		StepM  float64   `json:"step_m"`
		XYZ    []float64 `json:"xyz"`
		WidthM []float64 `json:"width_m"`
		LaneM  []float64 `json:"lane_m"`
	} `json:"path"`
	// Instances are the vegetation: groups of instances of a template node
	// (hidden in the model), by kind and terrain chunk, their places in
	// File (float32 little-endian, Layout per instance, glTF axes).
	Instances struct {
		File   string          `json:"file,omitempty"`
		Layout string          `json:"layout,omitempty"`
		Groups []instanceGroup `json:"groups,omitempty"`
	} `json:"instances"`
	// Ground is the ground map near the route (see GroundFile): CellM
	// cells over each listed terrain chunk, their values named by Classes.
	Ground struct {
		File    string        `json:"file,omitempty"`
		CellM   float64       `json:"cell_m,omitempty"`
		Layout  string        `json:"layout,omitempty"`
		Classes []string      `json:"classes,omitempty"`
		Chunks  []groundChunk `json:"chunks,omitempty"`
		// DryAboveM: grass grown on the ground above this height (the
		// frame's y) dries towards straw, as the summer pastures above
		// the treeline do; 0 none (the style's dryAboveM).
		DryAboveM float64 `json:"dry_above_m,omitempty"`
	} `json:"ground"`
	Attribution []string `json:"attribution"`
	// Region is the style the world was built in when not the Dutch one
	// ("fr"): renderers draw its own details (shutters).
	Region string `json:"region,omitempty"`
}

const frameNote = "metres; x east, y up (the course's elevation), z south of the origin; glTF axes"

// World is a built world, ready to write.
type World struct {
	Manifest  Manifest
	doc       *gltf.Doc
	instances []byte // InstancesFile
	ground    []byte // GroundFile
	// Stats, for the builder's report.
	Vertices, Triangles, Plants int
	prims                       []gltf.Primitive // everything drawn, for Check
}

// sample is a point on the road's centre line.
type sample struct {
	d, e, n, ele float64 // along the course, east, north, elevation
	off          float64 // ground model minus profile (coarse samples only)

	// The road there (fine samples): half the carriageway's width, its
	// edge with sidewalks, where the ground beside it lies flat; its
	// surface, and sidewalks and cycle lanes on the left [0] and right.
	hw, edge, flat float64
	surface        scenery.Surface
	walk, lane     [2]bool
	open           [2]bool    // a junction's patch on that side: no sidewalk or lane
	again          bool       // an earlier pass of the route rides the same road here: drawn once, by it
	wide           [2]float64 // the carriageway reaches this much further left and right, to a road alongside
	// butt: another road's surface continues at that edge (joinAlongside):
	// no skirt there, whose face showed as a dark line along the seam
	// (owner, 2026-10-11: two carriageways side by side, 1 cm apart).
	butt [2]bool
	// walkW: the sidewalk's width on that side where a building leaves it
	// less than walkM (0: walkM; walls.go).
	walkW [2]float64
	// terrace: a terrace's band (the sidewalk's outer offset) on that side,
	// where a higher road runs alongside behind it (terrace.go); 0: none.
	terrace [2]float64
	// terraceWalk: the terrace's sidewalk width on that side (0: the wall
	// stands at the carriageway's edge).
	terraceWalk [2]float64
	// terraceBack, terraceBehind: the surface behind the terrace, its
	// height and its offset from the line (terrace.go).
	terraceBack   [2]float64
	terraceBehind [2]float64
	// terraceMouth: a road crosses the sidewalk's line here, or a junction
	// takes it: a terrace ending against it ends at a mouth (terrace.go).
	terraceMouth [2]bool
	// walkOn: the sidewalk on that side runs on over a junction's span (a
	// French road's at a service road's mouth, as a dropped kerb); drop:
	// how far its kerb is lowered there, 0 (full) to 1 (dropKerbM)
	// (walksOnSpans).
	walkOn [2]bool
	drop   [2]float64
	// lotEnd: the road stops here at a car park's edge (trimLinesInLots):
	// its end gets a face down past the car park's surface, which lies a
	// little under it.
	lotEnd bool
	// skirt: how far the edge on that side goes down where a lower road
	// lies just beyond it (0: skirtM; deepSkirts).
	skirt      [2]float64
	onPatch    bool // within a junction's span: the junction draws the surface
	underPatch bool // a road not of the junction, lying on its patch
	aloft      bool // on a bridge or viaduct, off the ground (cut terrain)
	tunnel     bool // in a tunnel: the land closes over it (tunnels.go)
	oneway     int8 // traffic only along the line (1), only against it (-1), or both (0)
	way        int  // the map's way it belongs to (network roads)
	line       int  // the road line it belongs to (the terrain's samples)
}

const (
	fineStep   = 2.0  // m between road samples
	coarseStep = 10   // fine samples per coarse sample
	pathStep   = 5.0  // m between manifest path points
	sinkM      = 0.10 // terrain under the road sits this much below it (terrain triangles span dips in the road)
)

// flatPast is how far past a road's edge the ground lies flat under it, on
// a grid of cell: a terrain triangle reaches a cell's diagonal (1.41 cells)
// from any point in it, so with no vertex within 1.5 cells of the edge
// above the road, no triangle overlapping the road rises above it.
func flatPast(cell float64) float64 { return 1.5 * cell }

func (w *World) add(name string, extras any, ps ...gltf.Primitive) error {
	mesh, err := w.doc.AddMesh(name, ps...)
	if err != nil {
		return err
	}
	w.doc.AddNode(name, mesh, extras)
	w.prims = append(w.prims, ps...)
	for _, p := range ps {
		w.Vertices += len(p.Positions) / 3
		w.Triangles += len(p.Indices) / 3
	}
	return nil
}

// addBuildings builds the buildings, one mesh per terrain chunk (by where
// a building's middle is), coloured in its vertices.
func (w *World) addBuildings(t *terrain, fps []scenery.Footprint) error {
	if len(fps) == 0 {
		return nil
	}
	mat := w.doc.AddMaterial(gltf.Material{Name: "building", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.9})
	s := &surface{t: t, lists: map[[2]int]chunkLists{}}
	byChunk := map[[2]int]*gltf.Primitive{}
	// The nearest road's direction from a point (within 40 m), for which
	// way a house's ends face.
	roads := newRouteGrid(t.fine, 40)
	toRoad := func(e, n float64) (float64, float64, bool) {
		best, be, bn := 40.0, 0.0, 0.0
		roads.within(e, n, 40, func(i int) {
			if d := math.Hypot(t.fine[i].e-e, t.fine[i].n-n); d < best {
				best, be, bn = d, t.fine[i].e-e, t.fine[i].n-n
			}
		})
		if best >= 40 || best == 0 {
			return 0, 0, false
		}
		return be / best, bn / best, true
	}
	var keys [][2]int
	// Whether a point lies in another building than id (one adjoining it:
	// a terrace's next house, an annex), by 20 m cell.
	type foot struct {
		id   int64
		poly [][2]float64
	}
	cells := map[[2]int][]foot{}
	for _, f := range fps {
		lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
		for _, q := range f.Outline {
			lo = [2]float64{math.Min(lo[0], q[0]), math.Min(lo[1], q[1])}
			hi = [2]float64{math.Max(hi[0], q[0]), math.Max(hi[1], q[1])}
		}
		for gx := int(math.Floor(lo[0] / 20)); gx <= int(math.Floor(hi[0]/20)); gx++ {
			for gy := int(math.Floor(lo[1] / 20)); gy <= int(math.Floor(hi[1]/20)); gy++ {
				cells[[2]int{gx, gy}] = append(cells[[2]int{gx, gy}], foot{f.ID, f.Outline})
			}
		}
	}
	other := func(id int64, e, n float64) bool {
		for _, f := range cells[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}] {
			if f.id != id && inPoly(f.poly, e, n) {
				return true
			}
		}
		return false
	}
	for _, f := range fps {
		var cx, cy float64
		for _, q := range f.Outline {
			cx, cy = cx+q[0]/float64(len(f.Outline)), cy+q[1]/float64(len(f.Outline))
		}
		k := [2]int{int(math.Floor(cx / t.o.ChunkM)), int(math.Floor(cy / t.o.ChunkM))}
		p := byChunk[k]
		if p == nil {
			p = &gltf.Primitive{Material: mat}
			byChunk[k] = p
			keys = append(keys, k)
		}
		buildingMesh(p, s, styleFor(t.o.Region), f, toRoad, func(e, n float64) bool { return other(f.ID, e, n) })
	}
	for _, k := range keys {
		if p := byChunk[k]; len(p.Indices) > 0 {
			if err := w.add(fmt.Sprintf("buildings %d %d", k[0], k[1]), map[string]any{"kind": "buildings", "chunk": k}, *p); err != nil {
				return err
			}
		}
	}
	return nil
}

// roadPrims are the roads and junctions (with their verges), grouped by
// area (owner, 2026-10-09): by terrain chunk (chunkM square), the stretches
// of road whose middle lies in it and the junctions whose middle does.
// Chunks are what renderers cull and later thin out by distance, as with
// the terrain and buildings; a stretch split at a chunk's edge ends and
// goes on at the same sample, so its two halves share their vertices.
func roadPrims(lines []*roadLine, patches []patch, chunkM float64, m roadMaterials, v *verger) (byChunk, decks, tunnels map[[2]int][]gltf.Primitive) {
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / chunkM)), int(math.Floor(n / chunkM))} }
	byChunk, decks, tunnels = map[[2]int][]gltf.Primitive{}, map[[2]int][]gltf.Primitive{}, map[[2]int][]gltf.Primitive{}
	for _, l := range lines {
		ss := l.samples
		mid := func(i int) [2]int { return key((ss[i].e+ss[i+1].e)/2, (ss[i].n+ss[i+1].n)/2) }
		for i := 0; i+1 < len(ss); {
			k, dk, tk := mid(i), onDeck(ss, i), inTunnel(ss, i)
			j := i + 1
			for j+1 < len(ss) && mid(j) == k && onDeck(ss, j) == dk && inTunnel(ss, j) == tk {
				j++
			}
			to := byChunk
			switch {
			case dk:
				to = decks // a bridge's road: kept apart, it cuts nothing
			case tk:
				to = tunnels // a tunnel's: the land closes over it (tunnels.go)
			}
			to[k] = append(to[k], roadMesh(ss[i:j+1], ss, i, l.smooth, l.loop, m, v)...)
			i = j
		}
	}
	for _, p := range patches {
		var ce, cn float64
		for _, q := range p.poly {
			ce, cn = ce+q[0]/float64(len(p.poly)), cn+q[1]/float64(len(p.poly))
		}
		k := key(ce, cn)
		byChunk[k] = append(byChunk[k], patchMeshes(p, m, v)...)
	}
	return byChunk, decks, tunnels
}

// addRoads adds the roads and junctions: a node per chunk, a primitive
// per material.
func (w *World) addRoads(byChunk map[[2]int][]gltf.Primitive) error {
	for _, k := range chunkOrder(byChunk) {
		ps := mergeByMaterial(byChunk[k])
		if len(ps) == 0 {
			continue
		}
		if err := w.add(fmt.Sprintf("roads %d %d", k[0], k[1]), map[string]any{"kind": "roads", "chunk": k}, ps...); err != nil {
			return err
		}
	}
	return nil
}

// mergeByMaterial joins primitives of the same material into one.
func mergeByMaterial(ps []gltf.Primitive) []gltf.Primitive {
	byMat := map[int]*gltf.Primitive{}
	var order []int
	for _, p := range ps {
		if len(p.Indices) == 0 {
			continue
		}
		m := byMat[p.Material]
		if m == nil {
			m = &gltf.Primitive{Material: p.Material}
			byMat[p.Material] = m
			order = append(order, p.Material)
		}
		base := uint32(len(m.Positions) / 3)
		m.Positions = append(m.Positions, p.Positions...)
		m.Normals = append(m.Normals, p.Normals...)
		m.UVs = append(m.UVs, p.UVs...)
		m.Colors = append(m.Colors, p.Colors...)
		for _, i := range p.Indices {
			m.Indices = append(m.Indices, base+i)
		}
	}
	out := make([]gltf.Primitive, 0, len(order))
	for _, mat := range order {
		out = append(out, *byMat[mat])
	}
	return out
}

// addParking lays the car parks on the terrain.
func (w *World) addParking(lots []parkingLot) error {
	for k, l := range lots {
		ps := []gltf.Primitive{l.mesh}
		if len(l.rim.Indices) > 0 {
			ps = append(ps, l.rim)
		}
		if err := w.add(fmt.Sprintf("car park %d", k), l.extras, ps...); err != nil {
			return err
		}
	}
	return nil
}

// parkingLot is a car park's mesh and its node's extras.
type parkingLot struct {
	mesh    gltf.Primitive
	cut     gltf.Primitive // its outline's own triangles, what cuts the land (the 1 m mesh fragmented the verges round it)
	rim     gltf.Primitive // its bank face to the land beside it (rims.go)
	extras  map[string]any
	height  func(e, n float64) float64
	outline [][2]float64
}

// hideOwnWaysInLots fits the roads to the car parks and paved areas
// (scenery.Parking) they cross: the course's own ways (the route where it
// rides on no map road) aren't drawn inside one, the car park being the
// surface there (its samples count as a junction's, onPatch, which the
// road mesh leaves out); and a road's edge lying on one gets no skirt,
// the surface going on there (a road across the Hohneck col's apron,
// 16.1 km of the Route des Américains, 5 cm above it as a car park lies
// under the roads it meets, showed its skirt as a dark seam).
func hideOwnWaysInLots(lines []*roadLine, patches []patch, sc *scenery.Scenery) {
	if sc == nil || len(sc.Parking) == 0 {
		return
	}
	inLot := func(e, n float64) bool {
		for _, lot := range sc.Parking {
			if inPoly(lot.Outline, e, n) {
				return true
			}
		}
		return false
	}
	for i := range patches {
		patches[i].noSkirt = inLot // a junction's edge over a car park: no skirt either
	}
	for _, l := range lines {
		ss := l.samples
		for i := range ss {
			s := &ss[i]
			if l.route && inLot(s.e, s.n) {
				s.onPatch = true
				continue
			}
			a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
			de, dn := b.e-a.e, b.n-a.n
			d := math.Hypot(de, dn)
			if d == 0 {
				continue
			}
			for si, sign := range []float64{-1, 1} {
				off := sign * (s.hw + s.wide[si] + 0.2)
				if !s.butt[si] && inLot(s.e+dn/d*off, s.n-de/d*off) {
					s.butt[si] = true
				}
			}
		}
	}
}

// parkingLots are the car parks, laid on the terrain and meeting the
// roads beside them (roads: by chunk, as roadPrims made them).
func parkingLots(t *terrain, sc *scenery.Scenery, m roadMaterials, roads map[[2]int][]gltf.Primitive) []parkingLot {
	if sc == nil {
		return nil
	}
	s := &surface{t: t, lists: map[[2]int]chunkLists{}}
	paved := map[int]bool{m.lane: true, m.walk: true}
	for _, mat := range m.surface {
		paved[mat] = true
	}
	var prims []gltf.Primitive
	for _, k := range chunkOrder(roads) {
		prims = append(prims, roads[k]...)
	}
	rs := newSurfaceIndex(prims, func(mat int) bool { return paved[mat] }, 200)
	height := func(e, n float64) float64 {
		g := 0.0
		for _, de := range [3]float64{-parkSmoothM, 0, parkSmoothM} {
			for _, dn := range [3]float64{-parkSmoothM, 0, parkSmoothM} {
				g += s.at(e+de, n+dn) / 9
			}
		}
		return parkingHeight(g, e, n, rs)
	}
	var out []parkingLot
	for _, lot := range sc.Parking {
		outline := parkingOntoRoads(lot.Outline, rs)
		p := gridDraped(height, outline, m.surface[lot.Surface], 0, parkCellM)
		if len(p.Indices) == 0 {
			continue
		}
		extras := map[string]any{"kind": "parking", "at_m": round(lot.DistanceM)}
		if lot.Name != "" {
			extras["name"] = lot.Name
		}
		out = append(out, parkingLot{mesh: p, cut: drapedIn(height, outline, p.Material, 0, 0, false), extras: extras, height: height, outline: outline})
	}
	return out
}

// A car park's outline running just off a road's edge is taken onto the
// road: a map outline a few centimetres outside the drawn carriageway
// left a slit between the two surfaces too narrow for the land to fill
// (the rim stands at the outline, closing a step, not a gap): the Route
// des Crêtes' car parks at 15.3-15.5 km of the Route des Américains,
// 2.4 m² of slits along the road's edges.
const (
	parkSnapM     = 0.8  // an outline point this near a road surface moves onto it (twice the land's tuck: nearer, the land can't fill the gap)
	parkLapM      = 0.15 // this far in over the road's edge (just under it)
	parkChordM    = 0.05 // points within this of the line through their neighbours are dropped (the lap keeps the edge under the road)
	parkSnapStepM = 0.5  // the outline's edges sampled this finely for it
)

// parkingOntoRoads is outline, its edges sampled every parkSnapStepM (a
// map outline's corners are metres apart, the slit runs along its
// edges), with each point off the roads (rs) but within parkSnapM of one,
// along the outline's normal, moved parkLapM in over that road's edge.
func parkingOntoRoads(outline [][2]float64, rs *landSurface) [][2]float64 {
	if len(outline) < 3 {
		return outline
	}
	// snap is v moved onto a road within parkSnapM along the normal
	// (ne, nn), and whether it moved.
	snap := func(v [2]float64, ne, nn float64) ([2]float64, bool) {
		if _, on := rs.at(v[0], v[1]); on {
			return v, false
		}
		for d := 0.05; d <= parkSnapM+1e-9; d += 0.05 {
			for _, sgn := range [2]float64{1, -1} {
				if _, ok := rs.at(v[0]+sgn*ne*d, v[1]+sgn*nn*d); ok {
					return [2]float64{v[0] + sgn*ne*(d+parkLapM), v[1] + sgn*nn*(d+parkLapM)}, true
				}
			}
		}
		return v, false
	}
	n := len(outline)
	var out [][2]float64
	for i, a := range outline {
		b := outline[(i+1)%n]
		de, dn := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		ne, nn := dn/l, -de/l
		// The corner itself, by the normal of the edges either side.
		p := outline[(i+n-1)%n]
		pe, pn := a[0]-p[0], a[1]-p[1]
		if pl := math.Hypot(pe, pn); pl > 0 {
			ce, cn := ne+pn/pl, nn-pe/pl
			if cl := math.Hypot(ce, cn); cl > 0 {
				ce, cn = ce/cl, cn/cl
			} else {
				ce, cn = ne, nn
			}
			q, _ := snap(a, ce, cn)
			out = append(out, q)
		} else {
			q, _ := snap(a, ne, nn)
			out = append(out, q)
		}
		// Along the edge, only the samples that move (the rest would be
		// collinear points, which only slow the ear clipping).
		k := int(math.Ceil(l / parkSnapStepM))
		for j := 1; j < k; j++ {
			f := float64(j) / float64(k)
			if q, moved := snap([2]float64{a[0] + f*de, a[1] + f*dn}, ne, nn); moved {
				out = append(out, q)
			}
		}
	}
	// Points in line with their neighbours go (along a straight road's
	// edge every sample moved onto it): the ear clipping is quadratic in
	// the outline's points, and a car park ringed by them took minutes.
	// Along a road's edge a point within parkChordM of the line through
	// its neighbours goes only while that line stays on the road (dropped
	// by distance alone, a chord across a bend left a slit again).
	onRoad := func(p, r [2]float64) bool {
		for _, f := range [3]float64{0.25, 0.5, 0.75} {
			if _, ok := rs.at(p[0]+f*(r[0]-p[0]), p[1]+f*(r[1]-p[1])); !ok {
				return false
			}
		}
		return true
	}
	for {
		n := len(out)
		kept := out[:0]
		dropped := false
		for i, q := range out {
			p, r := out[(i+n-1)%n], out[(i+1)%n]
			if !dropped && n > 3 && math.Hypot(r[0]-p[0], r[1]-p[1]) < 20 {
				if d := segDist(p, r, q[0], q[1]); d < 0.005 || d < parkChordM && onRoad(p, r) {
					dropped = true
					continue
				}
			}
			kept = append(kept, q)
		}
		out = kept
		if !dropped {
			return out
		}
	}
}

// A car park meets the road it lies against at the road's height
// (owner, 2026-10-11, marks at Zandvoort, 13.0-15.0 km: car parks along
// the boulevard lay on the 5 m terrain, 0.15-0.6 m off the road beside
// them, and the step showed as dark slits and gashes along the road): within
// parkBlendM of a road surface it takes the road's height, just under it,
// easing to the ground further in.
const (
	parkBlendM = 8.0
	// parkSmoothM: a car park is graded, not the ground model's 5 m cells
	// as they are: its height off the roads is the land's mean over a 3 × 3
	// of points this far apart (the Hohneck's summit car park, 2026-10-17:
	// the model's bumps and a 1.5 m bank inside its entrance rippled the
	// surface).
	parkSmoothM = 10.0
	// parkCellM: a car park's triangles near a road follow it this finely
	// (4 m rose above the road's edge between their corners).
	parkCellM = 1.0
)

// pathOverLots puts the rider's path over a car park at the car park's
// surface (the course's own way through one is the car park's: not
// drawn, and its heights are the ground model's, 1.5 m over the summit
// car park of the Hohneck, where the path floated), eased over pathLotEaseM
// either side.
func pathOverLots(px, py, pz []float64, lots []parkingLot) {
	if len(lots) == 0 {
		return
	}
	n := len(pz)
	want := make([]float64, n)
	in := make([]bool, n)
	for i := range pz {
		for _, l := range lots {
			if inPoly(l.outline, px[i], py[i]) {
				want[i], in[i] = l.height(px[i], py[i]), true
				break
			}
		}
	}
	out := append([]float64(nil), pz...)
	for i := range pz {
		if in[i] {
			out[i] = want[i]
			continue
		}
		// Near a lot: the lot's correction at its nearest point in the
		// lot, eased out (the heights themselves blended had pulled a
		// point on a 12 % climb down by 0.4 m, a step: the correction is
		// what carries over, not the height 5 m away).
		for k := 1; k <= pathLotEaseN; k++ {
			for _, j := range [2]int{i - k, i + k} {
				if j >= 0 && j < n && in[j] {
					f := float64(k) / float64(pathLotEaseN+1)
					out[i] = pz[i] + (want[j]-pz[j])*(1-f)
					k = pathLotEaseN + 1
					break
				}
			}
		}
	}
	copy(pz, out)
}

const pathLotEaseN = 2 // path points (10 m) over which the path eases onto a car park's surface

// parkingHeight is a car park's surface at e, n over ground g: the nearest
// road surface (rs) within parkBlendM sets it, eased out to g + the lift.
func parkingHeight(g, e, n float64, rs *landSurface) float64 {
	// A road it meets stays on top: a junction's long fan triangles crease,
	// and a car park on the road's height rose through between creases.
	const under = 0.05
	if h, ok := rs.at(e, n); ok {
		return h - under
	}
	for r := 0.25; r <= parkBlendM; r += 0.25 {
		best, found := 0.0, false
		for k := range 16 {
			a := float64(k) * math.Pi / 8
			if h, ok := rs.at(e+r*math.Cos(a), n+r*math.Sin(a)); ok && (!found || h > best) {
				best, found = h, true
			}
		}
		if found {
			w := r / parkBlendM
			w = w * w * (3 - 2*w)
			return (best-under)*(1-w) + (g+parkingLiftM)*w
		}
	}
	return g + parkingLiftM
}

// Write puts the world in dir as world.glb and world.json, each replaced
// whole.
func (w *World) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The manifest goes first and comes back last: a build stopped midway
	// (its renderer quit) leaves no manifest beside files of another build.
	if err := os.Remove(filepath.Join(dir, ManifestFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err := writeAtomic(filepath.Join(dir, ModelFile), func(f *os.File) error { return w.doc.WriteGLB(f) })
	if err != nil {
		return err
	}
	for _, x := range []struct {
		name string
		b    []byte
	}{{InstancesFile, w.instances}, {GroundFile, w.ground}} {
		if len(x.b) == 0 {
			continue
		}
		err = writeAtomic(filepath.Join(dir, x.name), func(f *os.File) error {
			_, err := f.Write(x.b)
			return err
		})
		if err != nil {
			return err
		}
	}
	return writeAtomic(filepath.Join(dir, ManifestFile), func(f *os.File) error {
		return json.NewEncoder(f).Encode(w.Manifest)
	})
}

// chunkOrder is m's chunks in a fixed order (north to south rows, west
// to east), so nodes are added the same way every build: a world is the
// same every build.
func chunkOrder[V any](m map[[2]int]V) [][2]int {
	keys := make([][2]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		return keys[a][1] < keys[b][1] || keys[a][1] == keys[b][1] && keys[a][0] < keys[b][0]
	})
	return keys
}

func writeAtomic(name string, write func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+"-*")
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), name)
}

// junctionDevM is how far from its point the route's corner may be
// rounded where it turns at a junction (junctionNearM either side).
const (
	junctionDevM  = 3.0
	junctionNearM = 20.0
)

// courseLine is the course's track with its corners rounded off, wider
// where it turns at one of the junctions in sc (nil: none).
func courseLine(c *course.Course, sc *scenery.Scenery) *smoothLine {
	d, e, n := c.Vertices()
	var turns []float64
	if sc != nil {
		for _, j := range sc.Junctions {
			if j.Kind == scenery.JunctionTurn {
				turns = append(turns, j.DistanceM)
			}
		}
	}
	near := func(at float64) (float64, bool) {
		for _, t := range turns {
			if math.Abs(at-t) < junctionNearM {
				return t, true
			}
		}
		return 0, false
	}
	// Near a junction turn only the point nearest it stays: the map often
	// has several within metres there, which would keep its rounded
	// corner tight (each may use half its straights).
	var kd, ke, kn []float64
	for i := range d {
		if t, ok := near(d[i]); ok && i > 0 && i < len(d)-1 {
			best := i
			for k := i - 1; k <= i+1; k += 2 {
				if math.Abs(d[k]-t) < math.Abs(d[best]-t) {
					best = k
				}
			}
			if best != i {
				continue
			}
		}
		kd, ke, kn = append(kd, d[i]), append(ke, e[i]), append(kn, n[i])
	}
	dev := func(at float64) float64 {
		if _, ok := near(at); ok {
			return junctionDevM
		}
		return maxDevM
	}
	return newSmoothLine(kd, ke, kn, c.Loop, dev)
}

// sampleRoad samples the centre line (smoothed) in equal steps of at most
// step, the finish included.
func sampleRoad(c *course.Course, line *smoothLine, step float64) []sample {
	n := int(math.Ceil(c.Distance / step))
	s := make([]sample, n+1)
	for i := range s {
		d := c.Distance * float64(i) / float64(n)
		e, no := line.at(d)
		ele, _ := c.At(d)
		s[i] = sample{d: d, e: e, n: no, ele: ele}
	}
	return s
}

// liftOf is the ground model's offset against the profile at any distance
// along the course, from samples s (by distance, their off set).
func liftOf(s []sample) func(d float64) float64 {
	d, off := make([]float64, len(s)), make([]float64, len(s))
	for i := range s {
		d[i], off[i] = s[i].d, s[i].off
	}
	return func(at float64) float64 {
		i := sort.SearchFloat64s(d, at)
		switch {
		case i <= 0:
			return off[0]
		case i >= len(d):
			return off[len(off)-1]
		}
		f := (at - d[i-1]) / math.Max(d[i]-d[i-1], 1e-9)
		return off[i-1] + f*(off[i]-off[i-1])
	}
}

// groundOffsets sets each sample's offset of the ground model against the
// profile, smoothed along the route (a running median over 500 m), so
// the terrain can follow the ground model's shape at the profile's height.
// False when the ground model covers none of the samples.
func groundOffsets(c *course.Course, s []sample, ground func(lat, lon float64) (float64, bool)) bool {
	raw := make([]float64, len(s))
	any := false
	for i := range s {
		lat, lon := c.Unproject(s[i].e, s[i].n)
		if z, ok := ground(lat, lon); ok {
			raw[i], any = z-s[i].ele, true
		} else {
			raw[i] = math.NaN()
		}
	}
	if !any {
		return false
	}
	const half = 25 // samples each side: 250 m
	win := make([]float64, 0, 2*half+1)
	for i := range s {
		win = win[:0]
		for j := max(0, i-half); j <= min(len(s)-1, i+half); j++ {
			if !math.IsNaN(raw[j]) {
				win = append(win, raw[j])
			}
		}
		if len(win) == 0 {
			s[i].off = math.NaN()
			continue
		}
		sort.Float64s(win)
		s[i].off = win[len(win)/2]
	}
	// Stretches without data take the nearest offset.
	last := math.NaN()
	for i := range s {
		if math.IsNaN(s[i].off) {
			s[i].off = last
		} else {
			last = s[i].off
		}
	}
	last = math.NaN()
	for i := len(s) - 1; i >= 0; i-- {
		if math.IsNaN(s[i].off) {
			s[i].off = last
		} else {
			last = s[i].off
		}
	}
	return true
}

// round keeps centimetres; + 0 turns -0 into 0.
func round(v float64) float64 { return math.Round(v*100)/100 + 0 }

func smoothstep(x float64) float64 {
	x = math.Max(0, math.Min(1, x))
	return x * x * (3 - 2*x)
}
