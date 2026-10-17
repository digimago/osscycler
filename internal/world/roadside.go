package world

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// The roadside of a mountain road (owner, 2026-10-15, approved: road
// markings, crash barriers, retaining walls, snow poles, kilometre
// markers; his Street View shots of the D13B, the D430 and the
// Hohneck's summit road): what a style with roadside on stands along the
// roads near the route. All of it follows the ground model beside the
// road: where the land falls away steeply a barrier stands at the edge;
// where it rises steeply a wall holds the loose soil, or netting the
// rock.
const (
	roadsideNearM = 80.0 // only along roads this near the route

	// Markings (French norms, IISR 7e partie: T1 centre 3 m line, 10 m
	// gap; T2 edge 3 m, 3.5 m gap; 12 cm wide on departmental roads), on
	// carriageways at least markMinW wide (a single-lane road has none:
	// the Route des Américains).
	markW            = 0.12
	markMinW         = 5.0
	markEdgeMinW     = 5.6
	markEdgeIn       = 0.25 // the edge line's middle in from the carriageway's edge
	markLift         = 0.015
	centreOn, centre = 3.0, 13.0 // line, period
	edgeOn, edgePer  = 3.0, 6.5

	// The land beside the road, measured on the ground model across from
	// the road's centre (both on the model, so a road cut into a slope
	// counts its slope).
	fallNearM, fallFarM = 4.0, 8.0 // a barrier where it falls fallDrop over fallFarM from the edge
	fallDrop            = 3.5
	riseNearM           = 3.0
	riseWall            = 2.2  // a wall where it rises this much within riseNearM of the edge
	riseRock            = 3.6  // rock netted where it rises this much: too steep for loose soil
	runMinSamples       = 5    // runs shorter than this many samples (10 m) left out
	runGapSamples       = 3    // gaps this short between runs closed
	railGapM            = 30.0 // a barrier runs on over a gap this long where the fall eases
	railLeastM          = 16.0 // and a barrier shorter than this isn't one

	riseTown        = 1.0  // in town (or behind a sidewalk): a low wall where the land rises this much within riseNearM
	townWallM       = 1.0  // and at most this high
	railOffM        = 0.5  // a barrier's posts this far off the carriageway's edge
	abreastReachM   = 3.0  // a road surface this near behind a road's edge and sidewalk, higher by a step, gets a wall there (the land between filled in level)
	abreastStepM    = 0.35 // when it stands more than this higher somewhere along it
	abreastLowM     = 0.03 // and on while it stands this much higher (its ends taper)
	abreastMaxM     = 4.0  // and less than this (higher is a hillside or a bridge, not a lane behind a wall)
	abreastCrossCos = 0.5  // not across a side road's mouth: one crossing within 60° of square
	abreastAcrossM  = 0.6  // or one whose surface reaches this far in over the sidewalk
	abreastEndM     = 0.5  // its ends: a block this high over the sidewalk, square
	abreastThickM   = 0.25 // and this thick
	barrierPostM    = 4.0
	wallOffM        = 1.0 // a wall's foot this far off the edge: a strip of grass and the gutter
	wallAboveM      = 1.0 // a road surface this much above a road's own stands behind its wall, not in its way
	besideLevelM    = 1.5 // a paved surface beside the road no lower than this is beside it, not a drop: no barrier

	snowPoleStepM = 45.0 // snow poles along the road on each side, staggered
	snowPoleOffM  = 0.7
	kmMarkerOffM  = 0.9
	bendPostStepM = 7.0
)

type sideRun struct{ from, to int } // fine samples, inclusive

// addRoadside adds what stands along the roads (style roadside), a mesh
// per terrain chunk.
func (w *World) addRoadside(t *terrain, ways []scenery.Way, fps []scenery.Footprint, parks []scenery.Parking, ground int) error {
	st := styleFor(t.o.Region)
	if !st.roadside || len(t.fine) < 2 {
		return nil
	}
	near := newRouteGrid(t.coarse, roadsideNearM)
	isNear := func(e, n float64) bool {
		ok := false
		near.within(e, n, roadsideNearM, func(int) { ok = true })
		return ok
	}
	sp := newSpots(t, t.o.Land, fps, parks)
	builtUp := func(e, n float64) bool {
		l, _ := sp.landAt(e, n)
		return l == scenery.LandBuilt
	}
	model := func(e, n float64) (float64, bool) {
		if t.o.Elevation == nil {
			return 0, false
		}
		lat, lon := t.c.Unproject(e, n)
		return t.o.Elevation(lat, lon)
	}

	mark := w.doc.AddMaterial(gltf.Material{Name: "road marking", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.6})
	mat := w.doc.AddMaterial(gltf.Material{Name: "roadside", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.75})
	marks := map[[2]int]*gltf.Primitive{}
	items := map[[2]int]*gltf.Primitive{}
	// Surfaces the renderer details (shaders/surfaces.gdshader: courses,
	// rock and netting, setts, a grate's bars), each its own material.
	stoneMat := w.doc.AddMaterial(gltf.Material{Name: "stone wall", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.95})
	rockMat := w.doc.AddMaterial(gltf.Material{Name: "rock", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.9})
	settsMat := w.doc.AddMaterial(gltf.Material{Name: "setts", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.9})
	grateMat := w.doc.AddMaterial(gltf.Material{Name: "grate", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.6})
	walls, rocks := map[[2]int]*gltf.Primitive{}, map[[2]int]*gltf.Primitive{}
	// The land filled in behind walls, in the land's own material (so the
	// ground map, grass and plants take it as the land there).
	fills := map[[2]int]*gltf.Primitive{}
	srf := &surface{t: t, lists: map[[2]int]chunkLists{}}
	land := func(e, n float64) float64 { return srf.at(e, n) }
	landColour := func(e, n float64) [3]float32 {
		l, _ := t.cover.at(e, n)
		return st.landColour(l)
	}
	gutters, grates := map[[2]int]*gltf.Primitive{}, map[[2]int]*gltf.Primitive{}
	prim := func(m map[[2]int]*gltf.Primitive, e, n float64, material int) *gltf.Primitive {
		k := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
		if m[k] == nil {
			m[k] = &gltf.Primitive{Material: material}
		}
		return m[k]
	}

	// The roads as lines of samples.
	type lineRun struct{ from, to int }
	var lines []lineRun
	for i := 0; i < len(t.fine); {
		j := i
		for j+1 < len(t.fine) && t.fine[j+1].line == t.fine[i].line {
			j++
		}
		if j > i {
			lines = append(lines, lineRun{i, j})
		}
		i = j + 1
	}
	usable := func(a sample) bool { return !a.onPatch && !a.again && !a.aloft }
	white := srgbLinear(0xe9e8e2)
	for _, ln := range lines {
		// Along the line: distance, the way's direction, the right normal.
		n := ln.to - ln.from + 1
		dist := make([]float64, n)
		dir := make([][2]float64, n)
		for k := range n {
			i := ln.from + k
			if k > 0 {
				dist[k] = dist[k-1] + math.Hypot(t.fine[i].e-t.fine[i-1].e, t.fine[i].n-t.fine[i-1].n)
			}
			a, b := t.fine[max(ln.from, i-1)], t.fine[min(ln.to, i+1)]
			de, dn := b.e-a.e, b.n-a.n
			if l := math.Hypot(de, dn); l > 0 {
				dir[k] = [2]float64{de / l, dn / l}
			}
		}
		at := func(k int, off float64) (float64, float64) {
			a := t.fine[ln.from+k]
			return a.e + dir[k][1]*off, a.n - dir[k][0]*off // right of the way's direction
		}
		way := t.fine[ln.from].way
		var wy scenery.Way
		if way >= 0 && way < len(ways) {
			wy = ways[way]
		}
		// onSurface: a drawn road surface (another road's, a junction's
		// apron, a car park) is at e, n: nothing stands there.
		onSurface := func(e, n float64) bool {
			return t.surfaces != nil && t.surfaces.dist(e, -n, 0.3) < 0.3
		}
		near := make([]bool, n)
		anyNear := false
		for k := range n {
			a := t.fine[ln.from+k]
			near[k] = usable(a) && isNear(a.e, a.n)
			anyNear = anyNear || near[k]
		}
		if !anyNear {
			continue
		}

		// Markings: dashes laid along the line by distance, between
		// samples straight (2 m apart).
		dash := func(k0 int, s0, s1, off float64) {
			a, b := t.fine[ln.from+k0], t.fine[ln.from+k0+1]
			l := dist[k0+1] - dist[k0]
			if l <= 0 {
				return
			}
			pt := func(s, o float64) [3]float64 {
				f := (s - dist[k0]) / l
				e := a.e + f*(b.e-a.e) + (dir[k0][1]*(1-f)+dir[k0+1][1]*f)*o
				nn := a.n + f*(b.n-a.n) - (dir[k0][0]*(1-f)+dir[k0+1][0]*f)*o
				return [3]float64{e, a.ele + f*(b.ele-a.ele) + markLift, -nn}
			}
			p := prim(marks, a.e, a.n, mark)
			quad(p, pt(s0, off-markW/2), pt(s0, off+markW/2), pt(s1, off+markW/2), pt(s1, off-markW/2), [3]float64{0, 1, 0}, white)
		}
		pattern := func(off func(k int) float64, on, per, phase float64, want func(k int) bool) {
			for k := 0; k+1 < n; k++ {
				if !want(k) || !want(k+1) {
					continue
				}
				s0, s1 := dist[k], dist[k+1]
				for c := math.Floor((s0-phase)/per)*per + phase; c < s1; c += per {
					lo, hi := math.Max(s0, c), math.Min(s1, c+on)
					if hi > lo+0.01 {
						dash(k, lo, hi, (off(k)+off(k+1))/2)
					}
				}
			}
		}
		if st.markings && (wy.Ref != "" || wy.Class == "primary" || wy.Class == "secondary" || wy.Class == "tertiary") {
			phase := float64(splitmix(uint64(wy.ID))%1000) / 1000 * centre
			pattern(func(int) float64 { return 0 }, centreOn, centre, phase, func(k int) bool {
				a := t.fine[ln.from+k]
				return near[k] && 2*a.hw >= markMinW && a.oneway == 0
			})
			for _, sign := range []float64{-1, 1} {
				pattern(func(k int) float64 { return sign * (t.fine[ln.from+k].hw - markEdgeIn) }, edgeOn, edgePer, phase, func(k int) bool {
					a := t.fine[ln.from+k]
					return near[k] && 2*a.hw >= markEdgeMinW && !builtUp(a.e, a.n)
				})
			}
		}

		// The land either side: barriers where it falls, walls or netted
		// rock where it rises.
		type sideInfo struct {
			rail, wall, rock, town []bool
			h                      []float64 // a wall's height
		}
		var sides [2]sideInfo
		for si, sign := range []float64{-1, 1} {
			s := &sides[si]
			s.rail, s.wall, s.rock, s.town, s.h = make([]bool, n), make([]bool, n), make([]bool, n), make([]bool, n), make([]float64, n)
			for k := range n {
				a := t.fine[ln.from+k]
				if !near[k] || a.open[si] {
					continue
				}
				edge := sideEdge(a, si)
				zc, ok := model(a.e, a.n)
				if !ok {
					continue
				}
				e1, n1 := at(k, sign*(edge+fallNearM))
				e2, n2 := at(k, sign*(edge+fallFarM))
				e3, n3 := at(k, sign*(edge+riseNearM))
				z1, ok1 := model(e1, n1)
				z2, ok2 := model(e2, n2)
				z3, ok3 := model(e3, n3)
				town := a.walk[si] || builtUp(a.e, a.n)
				// A wall's line and the strip behind it clear of every other
				// road surface at about this road's level (a side road's
				// mouth, its junction). A road standing well above it is
				// what the wall holds up (1.52 km of the Route des
				// Américains: a lane turning uphill from beside the Rue du
				// Hohneck; counting it, no wall stood and the land was a
				// cliff).
				wallClear := true
				for _, off := range [3]float64{wallOffM, wallOffM + 1, wallOffM + 2.5} {
					we, wn := at(k, sign*(edge+off))
					if j := sp.roadNear(we, wn, 0.5, a.line); j >= 0 && t.fine[j].ele < a.ele+wallAboveM {
						wallClear = false
					}
					if t.surfaces != nil {
						if h, ok := t.surfaces.highest(we, -wn, 0.5); ok && h < a.ele+wallAboveM {
							wallClear = false
						}
					}
				}
				// A barrier where the land falls away from the road itself:
				// not where a car park (or another road) lies beside it at
				// about its level and the drop is beyond that (the Hohneck
				// car parks, 15.33 km: a guardrail stood on the asphalt in
				// front of the car park). A road well below (the Chemin de
				// Schmargult under the Route des Crêtes, 15.85 km) is a drop
				// like any other.
				paved := func(e, n float64) bool {
					if t.surfaces == nil {
						return false
					}
					h, ok := t.surfaces.highest(e, -n, 0.5)
					return ok && h > zc-besideLevelM
				}
				if ok1 && ok2 && zc-z2 > fallDrop && zc-z1 > fallDrop*0.4 && !a.walk[si] && !paved(e1, n1) && !paved(e2, n2) {
					s.rail[k] = true
				}
				switch {
				case !wallClear:
				case town && ok3 && z3-zc > riseWall:
					// In town too a bank this steep is held by a wall to its
					// top (a low wall left it standing over it as a cliff).
					s.wall[k] = true
					s.h[k] = math.Min(3.6, z3-zc+0.4)
				case a.walk[si] && ok3 && z3-zc > riseTown:
					// In town a low wall at the bank's foot, behind the
					// sidewalk, the lawn rising on above it (owner's shot of
					// the Rue du Hohneck at 0.89 km, 2026-10-16); only behind
					// a sidewalk: along a lane without one the bank is a lawn
					// (the wall and its flat fill had made a terrace with a
					// grey rim below a house at 1.35 km, where the aerial
					// shows grass sloping to the lane).
					s.wall[k], s.town[k] = true, true
					s.h[k] = math.Min(townWallM, 0.3+0.4*(z3-zc))
				case !town && ok3 && z3-zc > riseWall:
					s.wall[k] = true
					s.rock[k] = z3-zc > riseRock
					s.h[k] = math.Min(3.6, z3-zc+0.4)
				}
			}
		}
		for si, sign := range []float64{-1, 1} {
			s := &sides[si]
			for _, r := range railRuns(s.rail, near, dist) {
				guardRail(prim(items, t.fine[ln.from+r.from].e, t.fine[ln.from+r.from].n, mat), t, ln.from, r, dist, func(k int) (float64, float64) {
					a := t.fine[ln.from+k]
					return at(k, sign*(a.hw+a.wide[si]+railOffM))
				})
			}
			for _, r := range runs(s.wall) {
				rock, town := 0, 0
				for k := r.from; k <= r.to; k++ {
					if s.rock[k] {
						rock++
					}
					if s.town[k] {
						town++
					}
				}
				// A garden's low wall in town ends square, as the block walls
				// along the Rue du Hohneck do (owner's photos, 2026-10-16:
				// eased down into the bank, its coping dived into the lawn
				// like a road poking through).
				square := 2*town > r.to-r.from+1
				// Smoothed heights, so the wall's top runs evenly; easing down
				// into the bank over its first and last 6 m (a retaining wall
				// in the hills; a town's ends square).
				h := make([]float64, r.to-r.from+1)
				for k := r.from; k <= r.to; k++ {
					sum, c := 0.0, 0
					for j := max(r.from, k-4); j <= min(r.to, k+4); j++ {
						if s.h[j] > 0 {
							sum, c = sum+s.h[j], c+1
						}
					}
					if c > 0 {
						h[k-r.from] = sum / float64(c)
					}
					if end := min(k-r.from, r.to-k); end < 3 && !square {
						h[k-r.from] *= (float64(end) + 1) / 4
					}
				}
				isRock := 2*rock > r.to-r.from+1
				p := prim(walls, t.fine[ln.from+r.from].e, t.fine[ln.from+r.from].n, stoneMat)
				if isRock {
					p = prim(rocks, t.fine[ln.from+r.from].e, t.fine[ln.from+r.from].n, rockMat)
				}
				f := prim(fills, t.fine[ln.from+r.from].e, t.fine[ln.from+r.from].n, ground)
				line := t.fine[ln.from].line
				clear := func(e, n float64) bool { // no other road within 1.5 m (a hairpin's next leg)
					return sp.roadNear(e, n, 1.5, line) < 0 && (t.surfaces == nil || t.surfaces.dist(e, -n, 1.5) >= 1.5)
				}
				retaining(p, f, land, clear, landColour, t, ln.from, r, h, sign, isRock, square, func(k int, off float64) (float64, float64) {
					a := t.fine[ln.from+k]
					return at(k, sign*(sideEdge(a, si)+off))
				})
			}
		}

		// Terraces (terrace.go): the wall and its fill in front of a higher
		// road alongside (the slab is drawn with the road meshes).
		terraceRuns(t.fine[ln.from:ln.to+1], func(si int, r sideRun, pos func(k int, d float64) (float64, float64)) {
			e0, n0 := t.fine[ln.from+r.from].e, t.fine[ln.from+r.from].n
			terraceWall(t, ln.from, r, si, prim(items, e0, n0, mat), pos)
		})

		// Snow poles, staggered either side, on roads up where snow lies
		// (style's snowPoleAboveM), clear of barriers and walls; bend
		// posts on the inside of tight bends; kilometre markers on
		// numbered roads.
		if st.snowPoleAboveM > 0 {
			for si, sign := range []float64{-1, 1} {
				first := snowPoleStepM / 2 * float64(si)
				for k := 0; k < n; k++ {
					a := t.fine[ln.from+k]
					if k > 0 && math.Floor((dist[k]-first)/snowPoleStepM) == math.Floor((dist[k-1]-first)/snowPoleStepM) {
						continue
					}
					if !near[k] || a.ele < st.snowPoleAboveM || sides[si].rail[k] || sides[si].wall[k] || a.walk[si] || builtUp(a.e, a.n) {
						continue
					}
					e, nn := at(k, sign*(a.hw+a.wide[si]+snowPoleOffM))
					// Never on a drawn surface: another road's, a junction's
					// apron, a car park (owner, 2026-10-17: poles standing in
					// the road, a cluster of them across the apron where the
					// summit road leaves the Route des Crêtes, 16.1 km).
					if onSurface(e, nn) {
						continue
					}
					snowPole(prim(items, e, nn, mat), e, a.ele, nn)
				}
			}
		}
		// The inside of tight bends: a paved gutter (caniveau) of setts
		// along the edge with a white kerb stone beyond (owner's hairpin
		// shot of the D13B), in its runs.
		inside := [2][]bool{make([]bool, n), make([]bool, n)}
		for k := 3; k+3 < n; k++ {
			a := t.fine[ln.from+k]
			if !near[k] || a.walk[0] || a.walk[1] {
				continue
			}
			turn := dir[k-3][0]*dir[k+3][1] - dir[k-3][1]*dir[k+3][0]
			if math.Abs(turn) < math.Sin(28*math.Pi/180) {
				continue
			}
			si := 1
			if turn > 0 {
				si = 0
			}
			if !sides[si].wall[k] && !a.open[si] {
				inside[si][k] = true
			}
		}
		for si, sign := range []float64{-1, 1} {
			for _, r := range runs(inside[si]) {
				from, to := max(0, r.from-2), min(n-1, r.to+2)
				gutter(prim(gutters, t.fine[ln.from+from].e, t.fine[ln.from+from].n, settsMat), prim(items, t.fine[ln.from+from].e, t.fine[ln.from+from].n, mat),
					t, ln.from, sideRun{from, to}, func(k int, off float64) (float64, float64) {
						a := t.fine[ln.from+k]
						return at(k, sign*(a.hw+a.wide[si]+off))
					})
			}
		}
		// Drains: a grate in the carriageway at the kerb, every 30 m in
		// town.
		for si, sign := range []float64{-1, 1} {
			for k := 1; k < n; k++ {
				a := t.fine[ln.from+k]
				if math.Floor((dist[k]+7*float64(si))/30) == math.Floor((dist[k-1]+7*float64(si))/30) {
					continue
				}
				if !near[k] || !a.walk[si] || a.open[si] || !builtUp(a.e, a.n) {
					continue
				}
				e, nn := at(k, sign*(a.hw-0.2))
				drain(prim(grates, e, nn, grateMat), e, a.ele+0.008, nn, dir[k])
			}
		}

		for k := 3; k+3 < n; k++ {
			a := t.fine[ln.from+k]
			if !near[k] || builtUp(a.e, a.n) {
				continue
			}
			turn := dir[k-3][0]*dir[k+3][1] - dir[k-3][1]*dir[k+3][0] // + left
			if math.Abs(turn) < math.Sin(28*math.Pi/180) {
				continue
			}
			if k > 0 && math.Floor(dist[k]/bendPostStepM) == math.Floor(dist[k-1]/bendPostStepM) {
				continue
			}
			si := 1 // turning right: the right is the inside
			if turn > 0 {
				si = 0
			}
			sign := float64(2*si - 1)
			if sides[si].wall[k] || sides[si].rail[k] {
				continue
			}
			e, nn := at(k, sign*(a.hw+a.wide[si]+snowPoleOffM))
			if onSurface(e, nn) {
				continue
			}
			bendPost(prim(items, e, nn, mat), e, a.ele, nn, dir[k])
		}
		if wy.Ref != "" {
			cap := uint32(0xe2b21e) // departmental roads: a yellow top
			if strings.HasPrefix(wy.Ref, "N") {
				cap = 0xc8402e // national: red
			}
			for k := 1; k < n; k++ {
				if math.Floor(dist[k]/1000) == math.Floor(dist[k-1]/1000) || !near[k] {
					continue
				}
				a := t.fine[ln.from+k]
				if sides[1].wall[k] || sides[1].rail[k] {
					continue
				}
				e, nn := at(k, a.hw+a.wide[1]+kmMarkerOffM)
				if onSurface(e, nn) {
					continue
				}
				kmMarker(prim(items, e, nn, mat), e, a.ele, nn, dir[k], cap)
			}
		}
	}

	add := func(m map[[2]int]*gltf.Primitive, kind string) error {
		keys := make([][2]int, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return keys[i][1] < keys[j][1] || keys[i][1] == keys[j][1] && keys[i][0] < keys[j][0]
		})
		for _, k := range keys {
			if p := m[k]; len(p.Indices) > 0 {
				if err := w.add(fmt.Sprintf("%s %d %d", kind, k[0], k[1]), map[string]any{"kind": kind, "chunk": k}, *p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// The fills (land behind walls and terraces) clipped by the road
	// surfaces, as the verges are: a fill over a mouth's thin junction
	// triangle slipped between overRoad's samples (1.07 and 1.6 km of the
	// Route des Américains).
	if t.surfaces != nil {
		for k, p := range fills {
			c := t.surfaces.clip(*p)
			fills[k] = &c
		}
	}
	if err := add(fills, "terrain"); err != nil {
		return err
	}
	if err := add(marks, "markings"); err != nil {
		return err
	}
	for _, g := range []struct {
		m    map[[2]int]*gltf.Primitive
		kind string
	}{{walls, "stone walls"}, {rocks, "rock"}, {gutters, "gutters"}, {grates, "drains"}} {
		if err := add(g.m, g.kind); err != nil {
			return err
		}
	}
	return add(items, "roadside")
}

// runs are the stretches where on holds, gaps of up to runGapSamples
// closed, shorter than runMinSamples left out.
// railRuns is runs for a barrier: a barrier is one run along a slope, not
// pieces with gaps where the fall eases for a few samples (owner,
// 2026-10-17: short segments a few metres apart instead of one longer
// one), so gaps up to railGapSamples are closed where the road is still
// drawn there (near: not a junction's mouth), and pieces shorter than
// railMinSamples are left out.
func railRuns(on, near []bool, dist []float64) []sideRun {
	closed := append([]bool(nil), on...)
	for i := 0; i < len(on); i++ {
		if !on[i] {
			continue
		}
		j := i + 1
		for j < len(on) && !on[j] && near[j] && dist[j]-dist[i] <= railGapM {
			j++
		}
		if j < len(on) && on[j] {
			for k := i + 1; k < j; k++ {
				closed[k] = true
			}
		}
	}
	var out []sideRun
	for _, r := range runs(closed) {
		if dist[r.to]-dist[r.from] >= railLeastM {
			out = append(out, r)
		}
	}
	return out
}

func runs(on []bool) []sideRun {
	var out []sideRun
	for i := 0; i < len(on); {
		if !on[i] {
			i++
			continue
		}
		j := i
		for {
			for j+1 < len(on) && on[j+1] {
				j++
			}
			g := j + 1
			for g < len(on) && !on[g] && g-j <= runGapSamples {
				g++
			}
			if g < len(on) && on[g] && g-j <= runGapSamples {
				j = g
				continue
			}
			break
		}
		if j-i+1 >= runMinSamples {
			out = append(out, sideRun{i, j})
		}
		i = j + 1
	}
	return out
}

// guardRail is a steel W-beam barrier on posts along run, its line
// where pos puts each sample; its ends lowered into the ground.
func guardRail(p *gltf.Primitive, t *terrain, from int, r sideRun, dist []float64, pos func(k int) (float64, float64)) {
	steel, post := srgbLinear(0xb9bfc4), srgbLinear(0x8d9398)
	pt := func(k int, y float64) [3]float64 {
		e, n := pos(k)
		return [3]float64{e, t.fine[from+k].ele + y, -n}
	}
	lastPost := math.Inf(-1)
	for k := r.from; k <= r.to; k++ {
		if dist[k]-lastPost >= barrierPostM || k == r.from || k == r.to {
			beam(p, pt(k, -0.3), pt(k, 0.72), 0.1, 0.12, post)
			lastPost = dist[k]
		}
		if k == r.to {
			break
		}
		// The rail: 0.31 m deep, its middle at 0.6 m; down to 0.2 m over
		// the first and last 4 m (a terminal).
		y0, y1 := 0.6, 0.6
		if k-r.from < 2 {
			y0 = 0.2 + 0.2*float64(k-r.from)
			y1 = 0.2 + 0.2*float64(k+1-r.from)
		}
		if r.to-k <= 2 {
			y0 = math.Min(y0, 0.2+0.2*float64(r.to-k))
			y1 = math.Min(y1, 0.2+0.2*float64(r.to-k-1))
		}
		beam(p, pt(k, y0), pt(k+1, y1), 0.06, 0.31, steel)
	}
}

// retaining is a wall of dressed stone leaning back into the slope
// (or rock under netting) along run, h its height at each sample.
func retaining(p, fill *gltf.Primitive, land func(e, n float64) float64, clear func(e, n float64) bool, landColour func(e, n float64) [3]float32, t *terrain, from int, r sideRun, h []float64, sign float64, rock, square bool, pos func(k int, off float64) (float64, float64)) {
	stone, coping := srgbLinear(0x6c665c), srgbLinear(0x5c5850)
	rockC := srgbLinear(0x5e5a54)
	batter := 0.18 // the face leans back this much per metre up
	if rock {
		batter = 0.35
	}
	foot := func(k int) [3]float64 {
		e, n := pos(k, wallOffM)
		return [3]float64{e, t.fine[from+k].ele - 0.3, -n}
	}
	top := func(k int, f float64) [3]float64 {
		hk := h[k-r.from] * f
		e, n := pos(k, wallOffM+hk*batter)
		y := t.fine[from+k].ele + hk
		if rock && f > 0 && f < 1 {
			// Rock juts in and out.
			j := unit(splitmix(uint64(int64(e*7))<<32^uint64(int64(n*7))^uint64(int64(f*10)))) - 0.5
			e2, n2 := pos(k, wallOffM+hk*batter+0.35*j)
			e, n = e2, n2
		}
		return [3]float64{e, y, -n}
	}
	// Courses of stone 0.45 m high, each block its own shade; rock in
	// rows that jut in and out.
	// The courses, the rock's grain and its netting are the renderer's
	// (stone wall, rock: shaders/surfaces.gdshader); rock keeps rows that
	// jut in and out, its outline.
	rows := []float64{0, 1}
	if rock {
		hMax := 0.0
		for _, x := range h {
			hMax = math.Max(hMax, x)
		}
		rows = []float64{0}
		nr := max(2, int(math.Ceil(hMax/0.6)))
		for i := 1; i <= nr; i++ {
			rows = append(rows, float64(i)/float64(nr))
		}
	}
	for k := r.from; k < r.to; k++ {
		for ri := 0; ri+1 < len(rows); ri++ {
			c := stone
			if rock {
				c = rockC
			}
			a0, a1, b0, b1 := top(k, rows[ri]), top(k+1, rows[ri]), top(k, rows[ri+1]), top(k+1, rows[ri+1])
			if ri == 0 {
				a0, a1 = foot(k), foot(k+1)
			}
			nrm := wallNormal(a0, a1, b0, sign)
			quad(p, a0, a1, b1, b0, nrm, c)
		}
		if !rock {
			// The coping: a top 0.4 m deep, back into the slope.
			t0, t1 := top(k, 1), top(k+1, 1)
			hk0, hk1 := h[k-r.from], h[k+1-r.from]
			e0, n0 := pos(k, wallOffM+hk0*batter+0.4)
			e1, n1 := pos(k+1, wallOffM+hk1*batter+0.4)
			quad(p, t0, t1, [3]float64{e1, t1[1], -n1}, [3]float64{e0, t0[1], -n0}, [3]float64{0, 1, 0}, coping)
		}
	}
	if square {
		// Its ends: a face across the wall from its foot to the coping's
		// back, at each end.
		for _, k := range []int{r.from, r.to} {
			hk := h[k-r.from]
			ef, nf := pos(k, wallOffM)
			eb, nb := pos(k, wallOffM+hk*batter+0.4)
			y0 := t.fine[from+k].ele - 0.3
			a, b := [3]float64{ef, y0, -nf}, [3]float64{eb, y0, -nb}
			c, d := [3]float64{eb, t.fine[from+k].ele + hk, -nb}, top(k, 1)
			nrm := wallNormal(a, b, d, 1)
			quad(p, a, b, c, d, nrm, stone)
			quad(p, b, a, d, c, [3]float64{-nrm[0], -nrm[1], -nrm[2]}, stone)
		}
	}
	backfill(fill, land, clear, landColour, t, from, r, h, batter, rock, square, pos)
}

// backfill fills the land in behind a wall (owner, 2026-10-15: the walls
// stood as free faces): from its top back into the slope until it meets
// the land as drawn (or 12 m on, or short of another road, then down to
// the land), so the slope ends at the wall's top as a cutting's does.
func backfill(p *gltf.Primitive, land func(e, n float64) float64, clear func(e, n float64) bool, landColour func(e, n float64) [3]float32, t *terrain, from int, r sideRun, h []float64, batter float64, rock, square bool, pos func(k int, off float64) (float64, float64)) {
	const far, step = 12.0, 0.5
	back := 0.0 // the top's back edge past the face (the coping)
	if !rock {
		back = 0.4
	}
	type row [3][3]float64 // the top's back edge, halfway, where it meets the land
	rows := make([]row, r.to-r.from+1)
	for k := r.from; k <= r.to; k++ {
		hk := h[k-r.from]
		o0 := wallOffM + hk*batter + back
		top := t.fine[from+k].ele + hk
		meet, z := far, 0.0
		for d := step; d <= far; d += step {
			e, n := pos(k, o0+d)
			if !clear(e, n) {
				meet = math.Max(step, d-step) // short of another road: down to the land there
				break
			}
			if z = land(e, n); z >= top-0.05 {
				meet = d
				break
			}
		}
		e0, n0 := pos(k, o0-0.02)
		e1, n1 := pos(k, o0+meet/2)
		e2, n2 := pos(k, o0+meet)
		z2 := land(e2, n2) + 0.03
		zm := top + (z2-top)*0.5
		rows[k-r.from] = row{{e0, top, -n0}, {e1, zm, -n1}, {e2, z2, -n2}}
	}
	if square {
		// At a square end the fill's edge comes down to the land beside
		// it, an earth face under each piece of its end row.
		for _, k := range []int{0, len(rows) - 1} {
			for j := range 2 {
				a, b := rows[k][j], rows[k][j+1]
				a2 := [3]float64{a[0], land(a[0], -a[2]) - 0.05, a[2]}
				b2 := [3]float64{b[0], land(b[0], -b[2]) - 0.05, b[2]}
				if a2[1] >= a[1] && b2[1] >= b[1] {
					continue
				}
				nrm := wallNormal(a2, b2, a, 1)
				col := landColour(a[0], -a[2])
				for _, q := range [2][4][3]float64{{a2, b2, b, a}, {b2, a2, a, b}} {
					base := uint32(len(p.Positions) / 3)
					for _, v := range q {
						p.Positions = append(p.Positions, float32(v[0]), float32(v[1]), float32(v[2]))
						p.Normals = append(p.Normals, float32(nrm[0]), float32(nrm[1]), float32(nrm[2]))
						p.Colors = append(p.Colors, col[0], col[1], col[2])
					}
					tri(p, base, base+1, base+2, nrm)
					tri(p, base, base+2, base+3, nrm)
					nrm = [3]float64{-nrm[0], -nrm[1], -nrm[2]}
				}
			}
		}
	}
	for k := 0; k+1 < len(rows); k++ {
		for j := range 2 {
			a, b, c, d := rows[k][j], rows[k+1][j], rows[k+1][j+1], rows[k][j+1]
			if overRoad(t, [4][3]float64{a, b, c, d}) {
				continue
			}
			n := cross([3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}, [3]float64{d[0] - a[0], d[1] - a[1], d[2] - a[2]})
			if n[1] < 0 {
				n = [3]float64{-n[0], -n[1], -n[2]}
				a, b, c, d = b, a, d, c
			}
			if l := math.Sqrt(dot3(n, n)); l > 0 {
				n = scale3(n, 1/l)
			}
			base := uint32(len(p.Positions) / 3)
			for _, q := range [4][3]float64{a, b, c, d} {
				col := landColour(q[0], -q[2])
				p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
				p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
				p.Colors = append(p.Colors, col[0], col[1], col[2])
			}
			tri(p, base, base+1, base+2, n)
			tri(p, base, base+2, base+3, n)
		}
	}
}

// wallNormal is the normal of the face a, b (along), c (up), facing the
// road (side sign: the face looks the other way).
func wallNormal(a, b, c [3]float64, sign float64) [3]float64 {
	u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := cross(u, v)
	l := math.Sqrt(dot3(n, n))
	if l == 0 {
		return [3]float64{0, 1, 0}
	}
	n = scale3(n, 1/l)
	_ = sign
	return n
}

// snowPole is a jalon: a 2 m pole, black and yellow, its top reflecting.
func snowPole(p *gltf.Primitive, e, y, n float64) {
	black, yellow := srgbLinear(0x1e1e1e), srgbLinear(0xe8c21a)
	for i := range 8 {
		c := black
		if i%2 == 1 {
			c = yellow
		}
		y0, y1 := y-0.3+0.025, y+0.25*float64(i+1)
		if i > 0 {
			y0 = y + 0.25*float64(i)
		}
		beam(p, [3]float64{e, y0, -n}, [3]float64{e, y1, -n}, 0.045, 0.045, c)
	}
}

// bendPost is a red and white post on the inside of a bend, 1.1 m.
func bendPost(p *gltf.Primitive, e, y, n float64, dir [2]float64) {
	white, red := srgbLinear(0xeeeeea), srgbLinear(0xc2281e)
	for i, c := range [][3]float32{white, red, white, red} {
		y0, y1 := y+0.28*float64(i), y+0.28*float64(i+1)
		if i == 0 {
			y0 = y - 0.3
		}
		beam(p, [3]float64{e, y0, -n}, [3]float64{e, y1, -n}, 0.12, 0.05, c)
	}
	_ = dir
}

// kmMarker is a borne kilométrique: a white post with a coloured top,
// its face to the road.
func kmMarker(p *gltf.Primitive, e, y, n float64, dir [2]float64, top uint32) {
	white, capC := srgbLinear(0xeceae2), srgbLinear(top)
	beam(p, [3]float64{e, y - 0.3, -n}, [3]float64{e, y + 0.62, -n}, 0.36, 0.16, white)
	beam(p, [3]float64{e, y + 0.62, -n}, [3]float64{e, y + 0.82, -n}, 0.36, 0.16, capC)
	beam(p, [3]float64{e, y + 0.82, -n}, [3]float64{e, y + 0.9, -n}, 0.24, 0.12, capC)
	_ = dir
}

// gutter is a paved gutter along run: setts 0.45 m wide from the
// carriageway's edge (pos's offset 0) at its height, and a white kerb
// stone beyond, 12 cm up.
func gutter(setts, kerb *gltf.Primitive, t *terrain, from int, r sideRun, pos func(k int, off float64) (float64, float64)) {
	grey, white := srgbLinear(0x8c8a84), srgbLinear(0xdcdad2)
	pt := func(k int, off, y float64) [3]float64 {
		e, n := pos(k, off)
		return [3]float64{e, t.fine[from+k].ele + y, -n}
	}
	for k := r.from; k < r.to; k++ {
		quad(setts, pt(k, -0.02, 0.006), pt(k+1, -0.02, 0.006), pt(k+1, 0.45, 0.006), pt(k, 0.45, 0.006), [3]float64{0, 1, 0}, grey)
		// The kerb's face, looking back at the road.
		in0, out0 := pt(k, 0, 0), pt(k, 1, 0)
		face := [3]float64{in0[0] - out0[0], 0, in0[2] - out0[2]}
		if l := math.Hypot(face[0], face[2]); l > 0 {
			face = [3]float64{face[0] / l, 0, face[2] / l}
		}
		quad(kerb, pt(k, 0.45, 0.006), pt(k+1, 0.45, 0.006), pt(k+1, 0.45, 0.12), pt(k, 0.45, 0.12), face, white)
		quad(kerb, pt(k, 0.45, 0.12), pt(k+1, 0.45, 0.12), pt(k+1, 0.6, 0.12), pt(k, 0.6, 0.12), [3]float64{0, 1, 0}, white)
	}
}

// drain is a cast-iron grate, 0.5 m along the road and 0.3 across, at y;
// its UVs run across (the bars along the road).
func drain(p *gltf.Primitive, e, y, n float64, dir [2]float64) {
	ax, an := dir[0]*0.25, dir[1]*0.25  // half along
	cx, cn := dir[1]*0.15, -dir[0]*0.15 // half across
	c := srgbLinear(0x3a3a3c)
	base := uint32(len(p.Positions) / 3)
	for _, q := range [4][3]float64{{-1, -1, 0}, {1, -1, 0}, {1, 1, 1}, {-1, 1, 1}} {
		p.Positions = append(p.Positions, float32(e+q[0]*ax+q[1]*cx), float32(y), float32(-(n + q[0]*an + q[1]*cn)))
		p.Normals = append(p.Normals, 0, 1, 0)
		p.Colors = append(p.Colors, c[0], c[1], c[2])
		p.UVs = append(p.UVs, float32(q[2]), float32((q[0]+1)/2))
	}
	tri(p, base, base+1, base+2, [3]float64{0, 1, 0})
	tri(p, base, base+2, base+3, [3]float64{0, 1, 0})
}

// overRoad tells whether the land filled in as quad q (x y z corners)
// would lie over a road surface below it: one crossing between the
// samples the fill was measured at (1.605 km of the Route des Américains:
// a fill 0.4 m over a side road).
func overRoad(t *terrain, q [4][3]float64) bool {
	if t.surfaces == nil {
		return false
	}
	for _, f := range [...][2]float64{{0.5, 0.5}, {0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}, {0.5, 0.1}, {0.5, 0.9}, {0.1, 0.5}, {0.9, 0.5}} {
		u, v := f[0], f[1]
		var p [3]float64
		for c := range 3 {
			p[c] = (1-u)*(1-v)*q[0][c] + u*(1-v)*q[1][c] + u*v*q[2][c] + (1-u)*v*q[3][c]
		}
		if h, ok := t.surfaces.highest(p[0], p[2], 0); ok && h < p[1]-0.02 {
			return true
		}
	}
	return false
}
