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
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

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
	Stamp  string `json:"stamp,omitempty"`
	Course struct {
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
	// Path is the road's centre line every StepM metres from the start,
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
	} `json:"ground"`
	Attribution []string `json:"attribution"`
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
	onPatch        bool       // within a junction's span: the junction draws the surface
	underPatch     bool       // a road not of the junction, lying on its patch
	aloft          bool       // on a bridge or viaduct, off the ground (cut terrain)
	oneway         int8       // traffic only along the line (1), only against it (-1), or both (0)
	way            int        // the map's way it belongs to (network roads)
	line           int        // the road line it belongs to (the terrain's samples)
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

// Build makes the world for c.
func Build(c *course.Course, o Options) (*World, error) {
	o.defaults()
	if c.Distance < 2*fineStep {
		return nil, errors.New("world: course too short")
	}
	line := courseLine(c, o.Scenery)
	route := sampleRoad(c, line, fineStep)
	var stretches []scenery.RoadStretch
	if o.Scenery != nil {
		stretches = o.Scenery.Roads
	}
	applyRoads(route, stretches, o.RoadWidthM, o.CellM)
	var coarse []sample
	for i := 0; i < len(route); i += coarseStep {
		coarse = append(coarse, route[i])
	}
	if o.Elevation != nil && !groundOffsets(c, coarse, o.Elevation) {
		o.Elevation = nil // no ground model anywhere along the route
	}
	// Heights are the ground model's own (owner, 2026-10-10: routes over the
	// same roads build the same world there): the profile is raised onto the
	// model by its offset, rather than the model lowered onto the profile,
	// which gave every course its own heights (a route sharing 7 km with the
	// Posbank Loop: roads up to 0.65 m apart, land 1.1 m).
	lift := func(float64) float64 { return 0 }
	if o.Elevation != nil {
		lift = liftOf(coarse)
		for i := range coarse {
			coarse[i].ele += coarse[i].off
			coarse[i].off = 0
		}
		for i := range route {
			route[i].ele += lift(route[i].d)
		}
	}

	w := &World{doc: gltf.New(o.Generator)}
	// The terrain's colour is in its vertices.
	ground := w.doc.AddMaterial(gltf.Material{Name: "terrain", Color: [4]float32{1, 1, 1, 1}, Roughness: 1})
	waterMat := w.doc.AddMaterial(gltf.Material{Name: "water", Color: [4]float32{0.05, 0.16, 0.36, 1}, Roughness: 0.1})
	roadMats := addRoadMaterials(w.doc)
	roadMats.ground = ground

	t := terrain{o: o, c: c, coarse: coarse, lift: lift,
		flattenM: o.RoadWidthM/2 + flatPast(o.CellM),
		cover:    &cover{m: o.Land, banks: map[int]*banks{}}}

	// The roads: the map's near the route, and the route's own where it
	// rides on none of them.
	net := newNetwork(c, o, line, route, &t)
	var ways []scenery.Way
	if o.Scenery != nil {
		ways = o.Scenery.Ways
	}
	rbs := findRoundabouts(ways, true)
	// Before the rider's path is fitted to them: the route's own ways off
	// the map roads they would lie on and out of roundabouts, and roads
	// alongside joined.
	net.lines = dropCovered(net.lines, rbs)
	joinAlongside(net.lines, o.CellM)
	pd, px, py, pz, pw := net.finish(rbs)
	lines := net.lines
	patches := networkPatches(lines, ways)
	// Roundabouts: junctions of their own, built from their middle; a
	// junction the map has on the ring is part of it.
	for i := range rbs {
		var nodes [][2]float64
		for _, p := range patches {
			nodes = append(nodes, p.nodes...)
		}
		rbs[i].absorb(nodes, lines)
		rbs[i].fit(lines, t.base(rbs[i].c[0], rbs[i].c[1], coarse))
		kept := patches[:0]
		for _, p := range patches {
			if math.Hypot(p.node[0]-rbs[i].c[0], p.node[1]-rbs[i].c[1]) > rbs[i].outer()+5 {
				kept = append(kept, p)
			}
		}
		patches = append(kept, rbs[i].shape(lines))
	}
	var routeLines []*roadLine
	cutRoads(lines, patches)
	// Junctions built from their node carry the sidewalks; overlays stop
	// them short.
	var overlays []patch
	for _, p := range patches {
		if p.overlay {
			overlays = append(overlays, p)
		}
	}
	for _, l := range lines {
		clearPatches(l.samples, overlays)
		tidyStrips(l.samples)
		if l.route {
			routeLines = append(routeLines, l)
		}
	}
	markPassesAgain(routeLines)
	// With a ground model the roads are cut out of the terrain, with
	// verges (verges.go).
	if o.Elevation != nil {
		t.cut = true
		t.markAloft(lines)
	}
	markDecks(lines, c.Distance)
	for k, l := range lines {
		for i := range l.samples {
			l.samples[i].line = k
		}
		t.fine = append(t.fine, l.samples...)
		t.loops = append(t.loops, l.loop)
	}

	// The roads, junctions and their verges first: with a cut terrain, the
	// terrain leaves out exactly what their footprint covers.
	var verges *verger
	if t.cut {
		verges = &verger{t: &t, s: &surface{t: &t, lists: map[[2]int]chunkLists{}}, mat: ground, over: overlays}
	}
	progress := func(f float64) {
		if o.Progress != nil {
			o.Progress(f)
		}
	}
	progress(0.1) // the network, junctions and path
	roads, decks := roadPrims(lines, patches, o.ChunkM, roadMats, verges)
	progress(0.2)
	if t.cut {
		t.footprint = rasterize(roads, o.ChunkM)
	}

	keys := t.chunks()
	meshes := make([]gltf.Primitive, len(keys))
	waters := make([]gltf.Primitive, len(keys))
	var wg sync.WaitGroup
	next := make(chan int)
	var chunksDone atomic.Int64
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for k := range next {
				meshes[k], waters[k] = t.chunk(keys[k], ground, waterMat)
				progress(0.2 + 0.4*float64(chunksDone.Add(1))/float64(len(keys)))
			}
		})
	}
	for k := range keys {
		next <- k
	}
	close(next)
	wg.Wait()
	// (Terrain + verges) − roads and car parks: nothing of the land lies
	// over them.
	lots := parkingLots(&t, o.Scenery, roadMats)
	if t.cut {
		cut := newCutter()
		t.surfaces = cut
		for _, l := range lots {
			cut.add(l.mesh)
		}
		for _, ps := range roads {
			for _, p := range ps {
				if p.Material != ground {
					cut.add(p)
				}
			}
		}
		var cw sync.WaitGroup
		var clipped atomic.Int64
		clips := len(meshes)
		for _, ps := range roads {
			for _, p := range ps {
				if p.Material == ground {
					clips++
				}
			}
		}
		clipDone := func() { progress(0.6 + 0.1*float64(clipped.Add(1))/float64(clips)) }
		for k := range meshes {
			cw.Go(func() { meshes[k] = cut.clip(meshes[k]); clipDone() })
		}
		for k, ps := range roads {
			for i, p := range ps {
				if p.Material == ground {
					cw.Go(func() { roads[k][i] = cut.clip(p); clipDone() })
				}
			}
		}
		cw.Wait()
		progress(0.7)
	}
	for k, p := range meshes {
		if err := w.add(fmt.Sprintf("terrain %d %d", keys[k][0], keys[k][1]),
			map[string]any{"kind": "terrain", "chunk": keys[k]}, p); err != nil {
			return nil, err
		}
	}
	waterChunks := 0
	for k, p := range waters {
		if len(p.Positions) == 0 {
			continue
		}
		waterChunks++
		if err := w.add(fmt.Sprintf("water %d %d", keys[k][0], keys[k][1]),
			map[string]any{"kind": "water", "chunk": keys[k]}, p); err != nil {
			return nil, err
		}
	}
	// Decks cut nothing: the ground stays under a bridge.
	for k, ps := range decks {
		roads[k] = append(roads[k], ps...)
	}
	if err := w.addRoads(roads); err != nil {
		return nil, err
	}
	if err := w.addParking(lots); err != nil {
		return nil, err
	}
	if err := w.addBuildings(&t, o.Buildings); err != nil {
		return nil, err
	}
	if err := w.addFences(&t, o.Fences); err != nil {
		return nil, err
	}
	if err := w.addBridges(&t, lines); err != nil {
		return nil, err
	}
	if c.Loop {
		if err := w.addStartLine(pd, px, py, pz, pw); err != nil {
			return nil, err
		}
	}
	var parks []scenery.Parking
	if o.Scenery != nil {
		parks = append([]scenery.Parking{}, o.Scenery.Parking...)
	}
	// Lawns are kept clear as car parks are: nothing grows on a pitch.
	for _, l := range o.Lawns {
		parks = append(parks, scenery.Parking{Outline: l})
	}
	near := newRouteGrid(coarse, 400)
	progress(0.75)
	ps := plants(&t, &surface{t: &t, lists: map[[2]int]chunkLists{}}, o.Land, keys, o.Buildings, parks, ways, near)
	if len(ps) > 0 {
		if err := w.addTemplates(); err != nil {
			return nil, err
		}
		var groups []instanceGroup
		w.instances, groups = packPlants(ps, o.ChunkM)
		w.Manifest.Instances.File, w.Manifest.Instances.Layout = InstancesFile, "x y z scale yaw"
		w.Manifest.Instances.Groups = groups
		w.Plants = len(ps)
	}
	// The ground map, for the chunks within its reach of the route.
	var groundKeys [][2]int
	for _, k := range keys {
		hit := false
		for _, s := range coarse {
			if rectDist(s.e, s.n, float64(k[0])*o.ChunkM, float64(k[1])*o.ChunkM, o.ChunkM) <= groundReachM {
				hit = true
				break
			}
		}
		if hit {
			groundKeys = append(groundKeys, k)
		}
	}
	progress(0.9)
	if gm, index := groundMap(newSpots(&t, o.Land, o.Buildings, parks), &t, &surface{t: &t, lists: map[[2]int]chunkLists{}},
		newLandSurface(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "terrain" }), groundKeys, o.ChunkM, near); len(index) > 0 {
		w.ground = gm
		g := &w.Manifest.Ground
		g.File, g.CellM, g.Layout, g.Classes, g.Chunks = GroundFile, groundCellM, GroundLayout, GroundClasses, index
	}

	m := &w.Manifest
	m.Format, m.Version, m.Generator, m.Stamp = Format, Version, o.Generator, o.Stamp
	m.Course.ID, m.Course.Name, m.Course.DistanceM, m.Course.Loop = c.ID, c.Name, round(c.Distance), c.Loop
	m.Origin.Lat, m.Origin.Lon = c.Origin()
	m.Frame = frameNote
	m.Model = ModelFile
	m.Terrain.Source = "course profile"
	if o.Elevation != nil {
		m.Terrain.Source = o.TerrainSource
	}
	m.Terrain.CellM, m.Terrain.ChunkM, m.Terrain.CorridorM, m.Terrain.Chunks = o.CellM, o.ChunkM, o.CorridorM, len(keys)
	m.Terrain.LandCover, m.Terrain.WaterChunks = "none", waterChunks
	if o.Land != nil {
		m.Terrain.LandCover = "OpenStreetMap"
	}
	m.Road.WidthM, m.Road.Keep = o.RoadWidthM, "right"
	m.Path.StepM = pathStep
	for i := range pd {
		m.Path.XYZ = append(m.Path.XYZ, round(px[i]), round(pz[i]), round(-py[i]))
		m.Path.WidthM = append(m.Path.WidthM, math.Round(10*pw[i])/10)
	}
	lanes := ridingLine(newSmoothLine(pd, px, py, c.Loop, nil), pd, m.Path.WidthM, m.Road.Keep == "right")
	keepOnRoad(px, py, lanes, lines, patches)
	skirtRound(px, py, pz, pw, rbs)
	rideAround(px, py, pz, pw, rbs)
	passThrough(px, py, pz, rbs)
	// Across a flat roundabout the rider takes the shortest way, on the
	// line itself: no lane to keep to there, and back to it gently after.
	for _, rb := range rbs {
		if rb.island {
			continue
		}
		for i := range px {
			if d := math.Hypot(px[i]-rb.c[0], py[i]-rb.c[1]) - rb.outer() - 3; d < 0 {
				lanes[i] = 0
			}
		}
	}
	limitDrift(lanes, pathStep)
	evenPace(px, py, pz, pw, lanes, c.Loop)
	despike(px, py, c.Loop)
	settleOnRoad(px, py, lanes, c.Loop, newRoadIndex(w.prims, func(mat int) bool { return riddenMaterials[w.doc.MaterialName(mat)] }))
	limitDrift(lanes, pathStep) // a lane settling narrowed eases there (it only narrows)
	m.Path.XYZ = m.Path.XYZ[:0]
	for i := range pd {
		m.Path.XYZ = append(m.Path.XYZ, round(px[i]), round(pz[i]), round(-py[i]))
	}
	for _, off := range lanes {
		m.Path.LaneM = append(m.Path.LaneM, round(off))
	}
	m.Attribution = append([]string{}, o.Attribution...)
	return w, nil
}

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
	var keys [][2]int
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
		buildingMesh(p, s, f)
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
func roadPrims(lines []*roadLine, patches []patch, chunkM float64, m roadMaterials, v *verger) (byChunk, decks map[[2]int][]gltf.Primitive) {
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / chunkM)), int(math.Floor(n / chunkM))} }
	byChunk, decks = map[[2]int][]gltf.Primitive{}, map[[2]int][]gltf.Primitive{}
	for _, l := range lines {
		ss := l.samples
		mid := func(i int) [2]int { return key((ss[i].e+ss[i+1].e)/2, (ss[i].n+ss[i+1].n)/2) }
		for i := 0; i+1 < len(ss); {
			k, dk := mid(i), onDeck(ss, i)
			j := i + 1
			for j+1 < len(ss) && mid(j) == k && onDeck(ss, j) == dk {
				j++
			}
			to := byChunk
			if dk {
				to = decks // a bridge's road: kept apart, it cuts nothing
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
	return byChunk, decks
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
		if err := w.add(fmt.Sprintf("car park %d", k), l.extras, l.mesh); err != nil {
			return err
		}
	}
	return nil
}

// parkingLot is a car park's mesh and its node's extras.
type parkingLot struct {
	mesh   gltf.Primitive
	extras map[string]any
}

// parkingLots are the car parks, laid on the terrain.
func parkingLots(t *terrain, sc *scenery.Scenery, m roadMaterials) []parkingLot {
	if sc == nil {
		return nil
	}
	s := &surface{t: t, lists: map[[2]int]chunkLists{}}
	var out []parkingLot
	for _, lot := range sc.Parking {
		p := parkingMesh(s, lot.Outline, m.surface[lot.Surface])
		if len(p.Indices) == 0 {
			continue
		}
		extras := map[string]any{"kind": "parking", "at_m": round(lot.DistanceM)}
		if lot.Name != "" {
			extras["name"] = lot.Name
		}
		out = append(out, parkingLot{p, extras})
	}
	return out
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
