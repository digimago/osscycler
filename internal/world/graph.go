package world

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// BuildGraph is the graph builder (owner, 2026-10-11): the road network
// first, the land last. The classic builder (Build) lays the terrain,
// fits the roads into it, cuts them out of it and matches the rider's path
// to them point by point, then mends what that leaves (holes, land over
// the road, the rider off the road or hopping between roads). Here each
// stage is settled before the next and never revisited:
//
//  1. The network: the map's roads near the route (as the classic builder
//     draws them), joined into a graph where they share a node and across
//     roundabouts (graph_net.go).
//  2. Heights on the graph: each road the ground model along it, smoothed;
//     every node one height; each road brought to its nodes'
//     (graph_heights.go).
//  3. The course as a way through the graph: on a road, or off the roads,
//     at each path step; changing roads only where they meet
//     (graph_match.go). Where it is off the roads, a way of the course's
//     own is built along it.
//  4. The surfaces: junctions, roundabouts, roads, car parks (the classic
//     builder's code), at the heights from 2.
//  5. The land, round their outline: a mesh that stops at the road's edge
//     rather than one the roads are cut out of, eased to the road's height
//     beside it (graph_terrain.go).
//  6. What grows and stands on the land: plants, buildings, the ground map
//     (the classic builder's code, on this land).
//
// Riding over interesting roads matters more than copying the place: the
// course keeps to the network, and the land gives way to the roads.
func BuildGraph(c *course.Course, o Options) (*World, error) {
	o.defaults()
	if c.Distance < 2*fineStep {
		return nil, errors.New("world: course too short")
	}
	progress := func(f float64) {
		if o.Progress != nil {
			o.Progress(f)
		}
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
		o.Elevation = nil
	}
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
	ground := w.doc.AddMaterial(gltf.Material{Name: "terrain", Color: [4]float32{1, 1, 1, 1}, Roughness: 1})
	waterMat := w.doc.AddMaterial(gltf.Material{Name: "water", Color: [4]float32{0.05, 0.16, 0.36, 1}, Roughness: 0.1})
	roadMats := addRoadMaterials(w.doc)
	roadMats.ground = ground

	t := terrain{o: o, c: c, coarse: coarse, lift: lift,
		flattenM: o.RoadWidthM/2 + flatPast(o.CellM),
		cover:    &cover{m: o.Land, banks: map[int]*banks{}}}

	sc := o.Scenery
	var ways []scenery.Way
	if sc != nil {
		ways = sc.Ways
	}
	hasWays := sc != nil && len(sc.Ways) > 0 && len(sc.RouteOn) > 0 && sc.RouteStepM > 0
	rbs := findRoundabouts(ways, true)
	steps := max(1, int(math.Ceil(c.Distance/pathStep)))
	step := c.Distance / float64(steps)

	// 1-3: the network, its heights, the course on it.
	var lines []*roadLine
	var pd, px, py, pz, pw []float64
	var placed *gpathOut // the course on the network, to place again once the roads are drawn
	if hasWays {
		ridden := riddenWays(sc, route)
		// Roads that nearly meet, meet (the same ways, by index, so the
		// map's own match still points into them).
		ways = joinNearEnds(ways, ridden)
		clip := newRouteGrid(route, clipM)
		near := func(e, no float64) bool {
			found := false
			clip.within(e, no, clipM, func(int) { found = true })
			return found
		}
		lines = networkLines(ways, ridden, near)
		for _, l := range lines {
			finishRoad(l.samples, o.CellM)
			// Bridges span over, tunnels keep low under what lies over them
			// (owner's mark, 2026-10-11: a dike road went up and over the
			// railway embankment where it goes under the track through a
			// tunnel, polder ride 5.17 km).
			heightsStraight(l, &t, ways, func(w scenery.Way) bool { return w.Bridge || w.Tunnel })
		}
		net := newGnet(lines, ways, rbs)
		net.anchorHeights()
		progress(0.05)

		own := make([][4]float64, steps+1)
		heading := make([][2]float64, steps+1)
		fineD := c.Distance / float64(len(route)-1)
		for i := range own {
			at := step * float64(i)
			ox, oy := line.at(at)
			re, rn := routeWay(line, at)
			rs := route[max(0, min(int(math.Round(at/fineD)), len(route)-1))]
			oz := rs.ele
			if o.Elevation != nil {
				key := [2]int{int(math.Floor(ox / o.ChunkM)), int(math.Floor(oy / o.ChunkM))}
				_, cn := t.lists(key)
				oz = t.base(ox, oy, cn)
			}
			own[i] = [4]float64{ox, oy, oz, 2 * rs.hw}
			heading[i] = [2]float64{re, rn}
			// The course line steps back a metre or two just past a rounded
			// corner (courseLine, shared): a point behind the one before
			// holds still there, as the matcher holds a step back along a
			// road (the Dunes course at 15.13 km: a 2.4 m step back in the
			// course's own line made a hairpin in the path).
			if i > 0 && (ox-own[i-1][0])*re+(oy-own[i-1][1])*rn < 0 {
				own[i] = own[i-1]
			}
		}
		if o.Elevation != nil {
			span := int(math.Round(gOwnSmoothM / step))
			raw := make([]float64, len(own))
			for i := range own {
				raw[i] = own[i][2]
			}
			for i := range own {
				a, b := max(0, i-span), min(len(own)-1, i+span)
				sum := 0.0
				for j := a; j <= b; j++ {
					sum += raw[j]
				}
				own[i][2] = sum / float64(b-a+1)
			}
			// Under a bridge the map has, the course's own way goes under it
			// (owner, 2026-10-12: the Posbank Loop takes the cycle tunnel
			// under the N348 at 26.8 km, which the map data lacks; on the
			// ground model, the embankment's fill, the own way rode up to the
			// bridges and was hidden under their carriageways): gUnderM below
			// the bridge where the course crosses it, the approaches ramped at
			// most gUnderGrade. A bridge the course runs along is one it is
			// matched onto.
			type bsample struct {
				e, n, ele, hw, de, dn float64
			}
			var bs []bsample
			for _, l := range lines {
				for i := 0; i+1 < len(l.samples); i++ {
					a, b := l.samples[i], l.samples[i+1]
					if wk := a.way; wk < 0 || wk >= len(ways) || !ways[wk].Bridge {
						continue
					}
					de, dn := b.e-a.e, b.n-a.n
					if l := math.Hypot(de, dn); l > 0 {
						bs = append(bs, bsample{a.e, a.n, a.ele, a.hw, de / l, dn / l})
					}
				}
			}
			var lowered []int
			for i := range own {
				low := math.Inf(1)
				for _, b := range bs {
					if math.Hypot(b.e-own[i][0], b.n-own[i][1]) <= b.hw+1 && math.Abs(b.de*heading[i][0]+b.dn*heading[i][1]) < gUnderCos {
						low = math.Min(low, b.ele-gUnderM)
					}
				}
				if low < own[i][2] {
					own[i][2] = low
					lowered = append(lowered, i)
				}
			}
			// The ramps: within gUnderRampM of the underpass no point stands
			// more than gUnderGrade of a step above either neighbour (the
			// ground model's embankment either side of the bridge is cut
			// through).
			for _, i := range lowered {
				a, b := max(0, i-int(gUnderRampM/step)), min(len(own)-1, i+int(gUnderRampM/step))
				for j := a + 1; j <= b; j++ {
					own[j][2] = math.Min(own[j][2], own[j-1][2]+gUnderGrade*step)
				}
				for j := b - 1; j >= a; j-- {
					own[j][2] = math.Min(own[j][2], own[j+1][2]+gUnderGrade*step)
				}
			}
		}
		mapOn := func(i int) int {
			k := int(math.Round(step * float64(i) / sc.RouteStepM))
			return sc.RouteOn[max(0, min(k, len(sc.RouteOn)-1))]
		}
		oneway := func(k int, at float64) int8 { return sampleBetween(net.lines[k], at).oneway }
		rank := func(k int, at float64) int {
			if wk := sampleBetween(net.lines[k], at).way; wk >= 0 && wk < len(ways) {
				return classRank[ways[wk].Class]
			}
			return 0
		}
		sidepath := func(k int, at float64) bool {
			wk := sampleBetween(net.lines[k], at).way
			return wk >= 0 && wk < len(ways) && ways[wk].Sidepath
		}
		gp := net.matchCourse(steps, step, own, heading, mapOn, oneway, rank, sidepath)
		var on []bool
		pd, px, py, pz, pw, on = gp.place(steps, step, nil)
		placed = &gp
		f := make([]float64, len(on))
		for i, v := range on {
			if v {
				f[i] = 1
			}
		}
		n := &network{lines: lines, stretches: stretches, o: o}
		lines = append(lines, n.ownWays(pd, px, py, pz, f)...)
	} else {
		// No map roads: the road is the route's, as in the classic builder.
		lines = routeLines(c, line, stretches, func(float64) int { return -1 }, func(int) bool { return false }, true, o, t.liftAt)
		for i := 0; i <= steps; i++ {
			at := step * float64(i)
			x, y := line.at(at)
			s := sampleBetween(lines[0], at)
			pd, px, py, pz, pw = append(pd, at), append(px, x), append(py, y), append(pz, s.ele), append(pw, 2*s.hw)
		}
	}
	progress(0.1)

	// 4: the surfaces.
	lines = dropCoveredStubs(lines, rbs, 0, true, func(a, s sample) bool {
		return a.way >= 0 && a.way < len(ways) && ways[a.way].Bridge && a.ele-s.ele > bridgeM
	})
	joinAlongside(lines, o.CellM)
	clearWalksOfBuildings(lines, o.Buildings)
	patches := networkPatches(lines, ways)
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
	cutRoads(lines, patches)
	var overlays []patch
	for _, p := range patches {
		if p.overlay {
			overlays = append(overlays, p)
		}
	}
	var routeLs []*roadLine
	for _, l := range lines {
		clearPatches(l.samples, overlays)
		tidyStrips(l.samples)
		if l.route {
			routeLs = append(routeLs, l)
		}
	}
	markPassesAgain(routeLs)
	if o.Elevation != nil {
		t.markAloft(lines)
		// Under the ground isn't a bridge: no railings, no deck.
		for _, l := range lines {
			for i := range l.samples {
				if wk := l.samples[i].way; wk >= 0 && wk < len(ways) && ways[wk].Tunnel {
					l.samples[i].aloft = false
				}
			}
		}
	}
	markDecks(lines, c.Distance)
	for k, l := range lines {
		for i := range l.samples {
			l.samples[i].line = k
		}
		t.fine = append(t.fine, l.samples...)
		t.loops = append(t.loops, l.loop)
	}
	roads, decks := roadPrims(lines, patches, o.ChunkM, roadMats, nil)
	for k, p := range pavedIslands(roads, ground, roadMats.surface[scenery.SurfaceAsphalt], o.ChunkM) {
		roads[k] = append(roads[k], p)
	}
	// The carriageway as drawn: the rider's path again, its turns swept as
	// wide as it allows, and the riding line kept on it.
	var all []gltf.Primitive
	for _, k := range chunkOrder(roads) {
		all = append(all, roads[k]...)
	}
	for _, k := range chunkOrder(decks) {
		all = append(all, decks[k]...)
	}
	ix := newRoadIndex(all, func(mat int) bool { return carriageway[w.doc.MaterialName(mat)] })
	if placed != nil {
		pd, px, py, pz, pw, _ = placed.place(steps, step, ix.on)
	}
	progress(0.2)

	// 5: the land, round the roads and car parks.
	var under []gltf.Primitive
	for _, k := range chunkOrder(roads) {
		under = append(under, roads[k]...)
	}
	// Car parks first take their heights from the land as it will be: the
	// ground eased to the roads (gland's heights, which don't depend on
	// the car parks).
	var deckPrims []gltf.Primitive
	for _, k := range chunkOrder(decks) {
		deckPrims = append(deckPrims, decks[k]...)
	}
	var deckIx *landSurface
	if len(deckPrims) > 0 {
		deckIx = newSurfaceIndex(deckPrims, func(mat int) bool { return carriageway[w.doc.MaterialName(mat)] }, 200)
	}
	pre := newGland(&t, newSurfaceIndex(under, func(int) bool { return true }, 200), lines)
	pre.decks, pre.water = deckIx, waterMat
	t.landAt = pre.heightAt
	lots := parkingLots(&t, sc, roadMats, roads)
	for _, l := range lots {
		// The car park's own mesh, which follows its heights every metre:
		// its outline's few big triangles stood up to 12 cm off it.
		under = append(under, l.mesh)
	}
	// What plants and grass keep off: the roads and car parks (not the
	// water: its reeds grow along its edge).
	cut := newCutter()
	for _, p := range under {
		cut.add(p)
	}
	t.surfaces = cut
	// The water near the route from its own outline, relative to the roads
	// (its level under the shore as the land stands beside them): a surface
	// the land goes round, as round a road.
	keys := t.chunks()
	sheets := map[[2]int][]gltf.Primitive{}
	var allSheets []gltf.Primitive
	dryIx := newSurfaceIndex(under, func(int) bool { return true }, gOutlineMaxTriM) // before the water joins
	for _, x := range t.waterOutlines(keys, true) {
		// A verge and a bank between a road and the water (ditchBanks).
		x.ring = ditchBanks(x.ring, func(e, n, max float64) float64 { return cut.dist(e, -n, max) })
		p := gWaterSheet(&t, x, pre.dryAt, waterMat)
		sheets[x.key] = append(sheets[x.key], p)
		allSheets = append(allSheets, p)
		under = append(under, p)
	}
	// Every triangle, however big: a long canal's outline has triangles
	// hundreds of metres long, and left out, the land neither went round
	// them nor sank under them (owner, 2026-10-11: land standing through
	// the canal beside the main street at 0.35 km of the polder ride).
	land := newGland(&t, newSurfaceIndex(under, func(int) bool { return true }, gOutlineMaxTriM), lines)
	land.decks, land.water = deckIx, waterMat
	if len(allSheets) > 0 {
		land.waters, land.dry = newSurfaceIndex(allSheets, func(int) bool { return true }, gOutlineMaxTriM), dryIx
	}
	t.landAt = land.heightAt

	meshes := make([]gltf.Primitive, len(keys))
	waters := make([]gltf.Primitive, len(keys))
	var wg sync.WaitGroup
	next := make(chan int)
	var done atomic.Int64
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for k := range next {
				meshes[k], waters[k] = land.chunk(keys[k], ground, waterMat)
				progress(0.2 + 0.5*float64(done.Add(1))/float64(len(keys)))
			}
		})
	}
	for k := range keys {
		next <- k
	}
	close(next)
	wg.Wait()
	land.underRoads(meshes)
	waterChunks := 0
	for k, p := range meshes {
		if len(p.Positions) == 0 {
			continue // a chunk wholly under roads or water (the river at Rotterdam)
		}
		if err := w.add(fmt.Sprintf("terrain %d %d", keys[k][0], keys[k][1]),
			map[string]any{"kind": "terrain", "chunk": keys[k]}, p); err != nil {
			return nil, err
		}
	}
	for _, k := range chunkOrder(sheets) {
		waterChunks++
		if err := w.add(fmt.Sprintf("water exact %d %d", k[0], k[1]),
			map[string]any{"kind": "water", "chunk": k}, mergeByMaterial(sheets[k])...); err != nil {
			return nil, err
		}
	}
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
	for k, ps := range decks {
		roads[k] = append(roads[k], ps...)
	}
	if err := w.addRoads(roads); err != nil {
		return nil, err
	}
	if err := w.addParking(lots); err != nil {
		return nil, err
	}

	// 6: what stands and grows on the land.
	if err := w.addBuildings(&t, o.Buildings); err != nil {
		return nil, err
	}

	if err := w.addBridges(&t, lines, ways); err != nil {
		return nil, err
	}
	if c.Loop {
		if err := w.addStartLine(pd, px, py, pz, pw); err != nil {
			return nil, err
		}
	}
	var parks []scenery.Parking
	if sc != nil {
		parks = append([]scenery.Parking{}, sc.Parking...)
	}
	for _, l := range o.Lawns {
		parks = append(parks, scenery.Parking{Outline: l})
	}
	// The fences: the course's own (the test tracks' field entrances) and
	// the meadows' along the roads.
	fences := append(append([]scenery.Fence{}, o.Fences...), meadowFences(&t, o.Land, o.Buildings, parks)...)
	if err := w.addFences(&t, fences); err != nil {
		return nil, err
	}
	nearRoute := newRouteGrid(coarse, 400)
	progress(0.75)
	drawn := newLandSurface(w.prims, func(m int) bool { return w.doc.MaterialName(m) == "terrain" })
	ps := plants(&t, &surface{t: &t, lists: map[[2]int]chunkLists{}, drawn: drawn}, o.Land, keys, o.Buildings, parks, ways, nearRoute)
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
	var groundKeys [][2]int
	for _, k := range keys {
		for _, s := range coarse {
			if rectDist(s.e, s.n, float64(k[0])*o.ChunkM, float64(k[1])*o.ChunkM, o.ChunkM) <= groundReachM {
				groundKeys = append(groundKeys, k)
				break
			}
		}
	}
	progress(0.9)
	if gm, index := groundMap(newSpots(&t, o.Land, o.Buildings, parks), &t, &surface{t: &t, lists: map[[2]int]chunkLists{}},
		drawn, groundKeys, o.ChunkM, nearRoute); len(index) > 0 {
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
	// The points' own spacing: the course's length over a whole number of
	// steps, a little under pathStep. Rounded to pathStep, a renderer
	// placing a distance at d/StepM was metres off by a course's end (1.7 m
	// at the lapped Posbank Loop's, which it hopped at each lap's wrap).
	m.Path.StepM = pathStep
	if len(pd) > 1 {
		m.Path.StepM = (pd[len(pd)-1] - pd[0]) / float64(len(pd)-1)
	}
	for i := range pd {
		m.Path.XYZ = append(m.Path.XYZ, round(px[i]), round(pz[i]), round(-py[i]))
		m.Path.WidthM = append(m.Path.WidthM, math.Round(10*pw[i])/10)
	}
	// The riding line, as the classic builder's, but nothing mends the
	// path afterwards: it is on the network by construction.
	lanes := ridingLine(newSmoothLine(pd, px, py, c.Loop, nil), pd, m.Path.WidthM, m.Road.Keep == "right")
	for _, rb := range rbs {
		if rb.island {
			continue
		}
		for i := range px {
			if math.Hypot(px[i]-rb.c[0], py[i]-rb.c[1])-rb.outer()-3 < 0 {
				lanes[i] = 0
			}
		}
	}
	// Where the road as drawn is narrower than the line it was matched to
	// (two roads side by side drawn as one, a junction's corner), the
	// riding line comes in towards the path until it is on it.
	for i := range lanes {
		a, b := max(0, i-1), min(len(px)-1, i+1)
		de, dn := px[b]-px[a], py[b]-py[a]
		l := math.Hypot(de, dn)
		if l == 0 || lanes[i] == 0 {
			continue
		}
		rx, ry := dn/l, -de/l // right of the way the path goes
		for _, f := range []float64{1, 0.5, 0} {
			if f == 0 || ix.on(px[i]+rx*(lanes[i]*f+gLaneMarginM), py[i]+ry*(lanes[i]*f+gLaneMarginM)) {
				lanes[i] *= f
				break
			}
		}
	}
	// And between the path's points (a wedge between a road's edge and the
	// cut end of the own way leaving it fell between two points on road).
	for i := 0; i+1 < len(px); i++ {
		if lanes[i] == 0 && lanes[i+1] == 0 {
			continue
		}
		de, dn := px[i+1]-px[i], py[i+1]-py[i]
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		rx, ry := dn/l, -de/l
		mx, my, ml := (px[i]+px[i+1])/2, (py[i]+py[i+1])/2, (lanes[i]+lanes[i+1])/2
		for _, f := range []float64{1, 0.5, 0} {
			if f == 0 || ix.on(mx+rx*(ml*f+gLaneMarginM), my+ry*(ml*f+gLaneMarginM)) {
				lanes[i], lanes[i+1] = lanes[i]*f, lanes[i+1]*f
				break
			}
		}
	}
	limitDrift(lanes, pathStep)
	if c.Lap != nil && !c.Loop {
		// The tail onto the lap: rewritten as eased.
		tailOntoLap(c, pd, px, py, pz, lanes)
		m.Path.XYZ = m.Path.XYZ[:0]
		for i := range pd {
			m.Path.XYZ = append(m.Path.XYZ, round(px[i]), round(pz[i]), round(-py[i]))
		}
	}
	for _, off := range lanes {
		m.Path.LaneM = append(m.Path.LaneM, round(off))
	}
	m.Attribution = append([]string{}, o.Attribution...)
	progress(1)
	return w, nil
}

// gWayM: the route's way at a point is its direction over this far either
// side.
const gWayM = 10.0

// routeWay is the route's way at d along line: its direction from gWayM
// before to gWayM after, which a glitch in the line (it steps back for
// a metre near junction turns, courseLine) doesn't turn round as the
// heading at a point does.
func routeWay(line *smoothLine, d float64) (float64, float64) {
	ax, ay := line.at(d - gWayM)
	bx, by := line.at(d + gWayM)
	l := math.Hypot(bx-ax, by-ay)
	if l < 1e-9 {
		de, dn, _ := line.heading(d)
		return de, dn
	}
	return (bx - ax) / l, (by - ay) / l
}

// riddenWays are the map's ways the route rides on (its own match), and
// the bridges and tunnels it runs along whatever that says (as the classic
// builder has them).
func riddenWays(sc *scenery.Scenery, route []sample) map[int]bool {
	ridden := map[int]bool{}
	for _, k := range sc.RouteOn {
		if k >= 0 {
			ridden[k] = true
		}
	}
	along := newRouteGrid(route, 10)
	for k, w := range sc.Ways {
		if (w.Bridge || w.Tunnel) && !ridden[k] && runsAlong(w.Line, route, along) {
			ridden[k] = true
		}
	}
	return ridden
}

const (
	// A turn at a node (more than gTurnMinRad over gTurnLookM either way)
	// is swept as the classic builder's smoothTurns does (owner,
	// 2026-10-10: a tight right turn at 25.95 km on the Posbank Loop): its
	// rounding has the line gTurnClearM either way, and keeps within the
	// first of gTurnDevs of the node that keeps it on the drawn road.
	gTurnClearM = 20.0
	// A free turn (off the roads, where nothing drawn bounds the sweep)
	// has the line this far either way: within gTurnClearM a 70° turn
	// could be no wider than 14 m, and at speed the rider leaned to the
	// view's 35° limit (owner, 2026-10-13: the Posbank Loop's turn from the
	// Burgemeester Bloemersweg onto the Lange Juffer at 10.07 km, paths the
	// map data lacks; round such turns wider).
	// Only where the course is off the roads all that way: a wider sweep
	// reaching onto a road the course comes back to left the drawn road
	// for 10 m (13.19 km).
	gTurnFreeClearM = 40.0
	gTurnLookM      = 10.0
	gTurnMinRad     = 25 * math.Pi / 180
	// Turns closer than this are one bend: a node reached from two roads
	// is on the line twice, a hair apart (each road's own end), and two
	// corners a hair apart leave each other no room to round (owner's A/B,
	// 2026-10-12: a 101° turn in one step at 25.95 km on the Posbank Loop,
	// where the classic builder's sweep gives 56° over 20 m).
	gTurnNearM = 6.0
	// Two turns within this are a jog through a link (a Z at a staggered
	// junction, Rheden station at 30.75 km: 9 m), swept as one across it.
	gTurnLinkM = 12.0
	// The line either side of a turn is left to the rounding where it stays
	// this close to the straight through the points kept (the roads' own
	// corners are rounded within maxDevM, smooth.go, so their samples lie
	// up to that off the straight through a kink's ends; a gentle bend
	// bulges a little more); the riding line keeps to the carriageway
	// as drawn afterwards.
	gChordTolM = 2.0
	gPaceSpan  = 4 // path steps either way the pace is evened over
	// The course's own heights off the roads are the ground model's,
	// smoothed over this far along it (as every road's are, heights):
	// unsmoothed, the 5 m ground model under a viaduct stepped the rider
	// 2.1 m at 26.8 km on the Posbank Loop.
	gOwnSmoothM = 15.0
	// The course's own way under a bridge the map has: this far below the
	// bridge's road (more than bridgeM, so the bridge is a deck), where the
	// course crosses it (the bridge's way within gUnderCos of across), the
	// approaches at most gUnderGrade over gUnderRampM.
	gUnderM     = 3.0
	gUnderGrade = 0.08
	gUnderRampM = 60.0
	// The riding line keeps this far inside the drawn carriageway's edge
	// (owner's A/B, 2026-10-12: single metres off road on the the polder ride
	// ride at 0.93 and 7.89 km, the rider on the edge of a 2.4 m road).
	gLaneMarginM = 0.3
)

var gUnderCos = math.Cos(45 * math.Pi / 180)

// gOutlineMaxTriM: the land's outline takes triangles up to this across.
const gOutlineMaxTriM = 5000.0

var gTurnDevs = [...]float64{6, 4.5, 3, 2, maxDevM}

// offRoads tells whether the line through the network is off the roads
// for m either way of its point i.
func offRoads(gp gpathOut, i int, m float64) bool {
	for k := i; k >= 0 && gp.s[i]-gp.s[k] <= m; k-- {
		if gp.pts[k].on {
			return false
		}
	}
	for k := i; k < len(gp.pts) && gp.s[k]-gp.s[i] <= m; k++ {
		if gp.pts[k].on {
			return false
		}
	}
	return true
}

// place is the rider's path every step along the course: on the line
// through the network with its turns at nodes rounded (inside the
// junction), each point where its step lies along the line, the pace
// evened over a few steps; the start and finish stay put. on says which
// steps are on the roads.
func (gp gpathOut) place(steps int, step float64, onRoad func(e, n float64) bool) (d, x, y, z, w []float64, on []bool) {
	n := len(gp.pts)
	// The line for rounding: where the course turns at a node, the points
	// near it are left out, so the rounding there has room (the roads'
	// samples lie every 2 m); elsewhere the roads keep their shape.
	// pointAt is the line's point s along it.
	pointAt := func(s float64) (float64, float64) {
		j := sort.SearchFloat64s(gp.s, s)
		if j <= 0 {
			return gp.pts[0].x, gp.pts[0].y
		}
		if j >= n {
			return gp.pts[n-1].x, gp.pts[n-1].y
		}
		f := (s - gp.s[j-1]) / math.Max(gp.s[j]-gp.s[j-1], 1e-9)
		a, b := gp.pts[j-1], gp.pts[j]
		return a.x + f*(b.x-a.x), a.y + f*(b.y-a.y)
	}
	// The turns: at a node where the course changes roads, and wherever
	// the line bends more than gTurnMinRad over gTurnLookM either way, a
	// road's own kink (the map chains the Arnhemsestraatweg through a 43°
	// bend 13 m before the Ellecomsedijk at 26.65 km on the Posbank Loop).
	ang := make([]float64, n)
	for i, p := range gp.pts {
		s := gp.s[i]
		ax, ay := pointAt(s - gTurnLookM)
		bx, by := pointAt(s + gTurnLookM)
		ux, uy, vx, vy := p.x-ax, p.y-ay, bx-p.x, by-p.y
		if math.Hypot(ux, uy) < 1e-6 || math.Hypot(vx, vy) < 1e-6 {
			continue
		}
		ang[i] = math.Abs(math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy))
	}
	var turns []int
	for i, p := range gp.pts {
		if ang[i] <= gTurnMinRad {
			continue
		}
		if !p.node {
			// A kink is where the line bends most within gTurnNearM: every
			// point within gTurnLookM of a corner sees it.
			sharpest := true
			for j := i - 1; j >= 0 && gp.s[i]-gp.s[j] <= gTurnNearM; j-- {
				if ang[j] >= ang[i] {
					sharpest = false
				}
			}
			for j := i + 1; j < n && gp.s[j]-gp.s[i] <= gTurnNearM; j++ {
				if ang[j] > ang[i] {
					sharpest = false
				}
			}
			if !sharpest {
				continue
			}
		}
		turns = append(turns, i)
	}
	// Turns within gTurnNearM of each other are one, at the sharpest (a
	// node reached from two roads is on the line twice, a hair apart).
	freeAt := map[int]bool{}
	var bends []int
	for i := 0; i < len(turns); {
		j := i
		for j+1 < len(turns) && gp.s[turns[j+1]]-gp.s[turns[j]] < gTurnNearM {
			j++
		}
		best := turns[i]
		for _, k := range turns[i : j+1] {
			if ang[k] > ang[best] || ang[k] == ang[best] && gp.pts[k].node && !gp.pts[best].node {
				best = k
			}
		}
		for _, k := range turns[i : j+1] {
			freeAt[best] = freeAt[best] || gp.pts[k].free || !gp.pts[k].on
		}
		bends = append(bends, best)
		i = j + 1
	}
	turns = bends
	// A turn to round: at a point of the line; free off the roads, and at
	// either end of the step across where the course leaves a road for a
	// path beside it (the course's own way follows the path, so there is
	// nothing to keep to); cut
	// where it stands for two turns within gTurnLinkM (a jog through a
	// short link, a Z at a staggered junction, an S in the course's own
	// line), swept as one from the link's middle, across: off the roads
	// always (the own way follows the path), on them only where the sweep
	// stays on the carriageway (checked below; else each is rounded on its
	// own, with half the link).
	type turn struct {
		at        int
		free, cut bool
		pair      int
	}
	rejected := map[int]bool{}
	var sd, sx, sy []float64
	var tl []turn
	var wins [][2]int
	isTurn := map[float64]bool{}
	free := map[float64]bool{}
	devAt := map[float64]int{}
	var remap func(float64) float64
	var atOf map[int]float64
	dev := func(at float64) float64 {
		if isTurn[at] {
			return gTurnDevs[devAt[at]]
		}
		return maxDevM
	}
	var sl *smoothLine
	for {
		tl, sd, sx, sy, wins = nil, nil, nil, nil, nil
		clear(isTurn)
		clear(free)
		clear(devAt)
		for i := 0; i < len(turns); i++ {
			t := turn{at: turns[i], free: freeAt[turns[i]], pair: -1}
			if i+1 < len(turns) && gp.s[turns[i+1]]-gp.s[turns[i]] <= gTurnLinkM && !rejected[i] {
				mid := (gp.s[turns[i]] + gp.s[turns[i+1]]) / 2
				at := turns[i]
				for k := turns[i]; k <= turns[i+1]; k++ {
					if math.Abs(gp.s[k]-mid) < math.Abs(gp.s[at]-mid) {
						at = k
					}
				}
				// Free only when both are: with one on a road, the sweep
				// across is checked against the carriageway (it had cut 20 m
				// over the verge where the course came back onto a road 8 m
				// before a junction's node).
				t = turn{at: at, free: freeAt[turns[i]] && freeAt[turns[i+1]], cut: true, pair: i}
				i++
			}
			tl = append(tl, t)
		}
		// Round each turn, the line's points either side are left out as
		// far as gTurnClearM (to the next turn when that is within twice as
		// far: a window ending short of it would keep a point close by, and
		// leave it no room), but for the points the line needs to stay
		// within gChordTolM of the straights through the points kept
		// (Douglas and Peucker): the rounding has room where the road is
		// straight or bends gently, and follows the road where it bends
		// more. A free or cut turn keeps none of them.
		keep := make([]bool, n)
		for i := range keep {
			keep[i] = true
		}
		// The link a cut turn sweeps across, and gTurnLinkM beyond its
		// corners: never kept (a neighbouring turn's window would find them
		// off its straight to the link's middle, and leave the sweep no
		// room).
		never := make([]bool, n)
		for _, t := range tl {
			// Within gTurnNearM of a turn nothing is kept: the turn's own arc
			// sweeps that (a Z through a 3 m piece of the Beekhuizenseweg,
			// Velp loop 15.36 km: the other node's point, no turn of its own
			// over 10 m either way, lay 2.3 m off the chord and was kept).
			for k := t.at - 1; k >= 0 && gp.s[t.at]-gp.s[k] <= gTurnNearM; k-- {
				never[k] = true
			}
			for k := t.at + 1; k < n && gp.s[k]-gp.s[t.at] <= gTurnNearM; k++ {
				never[k] = true
			}
			if !t.cut {
				continue
			}
			a, b := gp.s[turns[t.pair]]-gTurnLinkM, gp.s[turns[t.pair+1]]+gTurnLinkM
			for k := range n {
				if k != t.at && gp.s[k] >= a && gp.s[k] <= b {
					never[k] = true
				}
			}
		}
		wins = make([][2]int, len(tl))
		for q, t := range tl {
			s := gp.s[t.at]
			lo, hi := 0, n-1
			if q > 0 {
				lo = tl[q-1].at
			}
			if q+1 < len(tl) {
				hi = tl[q+1].at
			}
			clearM := gTurnClearM
			if t.free && offRoads(gp, t.at, gTurnFreeClearM) {
				clearM = gTurnFreeClearM
			}
			jb := t.at
			for jb+1 <= hi && gp.s[jb+1]-s <= clearM {
				jb++
			}
			if q+1 < len(tl) && gp.s[hi]-s <= 2*clearM {
				jb = hi
			}
			ja := t.at
			for ja-1 >= lo && s-gp.s[ja-1] <= clearM {
				ja--
			}
			if q > 0 && s-gp.s[lo] <= 2*clearM {
				ja = lo
			}
			for k := ja + 1; k < jb; k++ {
				keep[k] = false
			}
			wins[q] = [2]int{ja, jb}
		}
		var simplify func(i, j int)
		simplify = func(i, j int) {
			a, b := gp.pts[i], gp.pts[j]
			dx, dy := b.x-a.x, b.y-a.y
			l := math.Hypot(dx, dy)
			worst, wk := 0.0, -1
			for k := i + 1; k < j; k++ {
				if never[k] {
					continue
				}
				p := gp.pts[k]
				d := math.Hypot(p.x-a.x, p.y-a.y)
				if l > 1e-9 {
					d = math.Abs((p.x-a.x)*dy-(p.y-a.y)*dx) / l
				}
				if d > worst {
					worst, wk = d, k
				}
			}
			if wk < 0 || worst <= gChordTolM {
				return
			}
			keep[wk] = true
			simplify(i, wk)
			simplify(wk, j)
		}
		for q, t := range tl {
			keep[wins[q][0]], keep[wins[q][1]], keep[t.at] = true, true, true
			if !t.free && !t.cut {
				simplify(wins[q][0], t.at)
				simplify(t.at, wins[q][1])
			}
		}
		keep[0], keep[n-1] = true, true
		// The kept points, at their straight distances along the line
		// through them (not the line's own: where points were left out
		// round a detour, the line between two kept points was 40 m for a
		// 20 m chord, and the arc's metric tangent and the chord walked by
		// fraction disagreed by metres, a step back in the path at 88.58 km
		// of the Grebbeberg course); each step's place is mapped onto them.
		var orig []float64
		at := make(map[int]float64) // kept pts index → its new distance
		for i, p := range gp.pts {
			if keep[i] && !never[i] {
				d := 0.0
				if len(sx) > 0 {
					d = sd[len(sd)-1] + math.Hypot(p.x-sx[len(sx)-1], p.y-sy[len(sy)-1])
				}
				at[i] = d
				orig = append(orig, gp.s[i])
				sd, sx, sy = append(sd, d), append(sx, p.x), append(sy, p.y)
			}
		}
		remap = func(s float64) float64 {
			j := sort.SearchFloat64s(orig, s)
			if j <= 0 {
				return sd[0]
			}
			if j >= len(orig) {
				return sd[len(sd)-1]
			}
			f := (s - orig[j-1]) / math.Max(orig[j]-orig[j-1], 1e-9)
			return sd[j-1] + f*(sd[j]-sd[j-1])
		}
		atOf = at
		for _, t := range tl {
			isTurn[atOf[t.at]], free[atOf[t.at]] = true, t.free
		}
		// Each turn as wide as the drawn road allows (onRoad, when given):
		// from gTurnDevM down, until its sweep stays on the carriageway.
		sl = newSmoothLine(sd, sx, sy, false, dev)
		for onRoad != nil {
			narrowed := false
			for i, a := range sl.arcs {
				if !a.present || !isTurn[sd[i]] || free[sd[i]] || devAt[sd[i]] == len(gTurnDevs)-1 {
					continue
				}
				for at := a.d0; at <= a.d1; at += 0.5 {
					if e, n := sl.at(at); !onRoad(e, n) {
						devAt[sd[i]]++
						narrowed = true
						break
					}
				}
			}
			if !narrowed {
				break
			}
			sl = newSmoothLine(sd, sx, sy, false, dev)
		}
		if onRoad == nil {
			break
		}
		// A jog cut across on the roads stays on the carriageway all the
		// way, or its turns are rounded on their own.
		// A window's end may lie in a swept link's zone and so not be a
		// kept point: the bound is the nearest kept point outwards.
		bound := func(i, dir int) float64 {
			for ; i > 0 && i < n-1; i += dir {
				if d, ok := atOf[i]; ok {
					return d
				}
			}
			if dir < 0 {
				return sd[0]
			}
			return sd[len(sd)-1]
		}
		bad := -1
		for q, t := range tl {
			if !t.cut || t.free {
				continue
			}
			for at := bound(wins[q][0], -1); at <= bound(wins[q][1], 1) && bad < 0; at += 0.5 {
				if e, n := sl.at(at); !onRoad(e, n) {
					bad = t.pair
				}
			}
			if bad >= 0 {
				break
			}
		}
		if bad < 0 {
			break
		}
		rejected[bad] = true
	}
	// Where each step lies along the line: never back, evened.
	at := append([]float64(nil), gp.at...)
	for i := 1; i < len(at); i++ {
		at[i] = math.Max(at[i], at[i-1])
	}
	even := make([]float64, len(at))
	for i := range at {
		k := min(gPaceSpan, i, len(at)-1-i)
		var s float64
		for j := i - k; j <= i+k; j++ {
			s += at[j]
		}
		even[i] = s / float64(2*k+1)
	}
	interp := func(s float64, v func(p gpoint) float64) float64 {
		j := sort.SearchFloat64s(gp.s, s)
		if j <= 0 {
			return v(gp.pts[0])
		}
		if j >= n {
			return v(gp.pts[n-1])
		}
		f := (s - gp.s[j-1]) / math.Max(gp.s[j]-gp.s[j-1], 1e-9)
		return v(gp.pts[j-1]) + f*(v(gp.pts[j])-v(gp.pts[j-1]))
	}
	for i := 0; i <= steps; i++ {
		s0 := even[min(i, len(even)-1)] // along the line through the network
		px, py := sl.at(remap(s0))
		d = append(d, step*float64(i))
		x, y = append(x, px), append(y, py)
		z = append(z, interp(s0, func(p gpoint) float64 { return p.z }))
		w = append(w, interp(s0, func(p gpoint) float64 { return p.w }))
		on = append(on, gp.on[min(i, len(gp.on)-1)])
	}
	return d, x, y, z, w, on
}
