package world

import (
	"math"
	"sort"
	"sync"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

// terrain shapes the ground in square chunks along the route.
type terrain struct {
	// walls are the retaining walls' footprints (e, n), from the face's
	// foot to the coping's back: no grass grows there (the ground map's
	// clearance).
	walls [][][2]float64
	// exact are the water areas drawn from their own outline (waters.go):
	// the cells leave their water out.
	exact        map[int]bool
	o            Options
	c            *course.Course
	fine, coarse []sample
	// flattenM: within this distance of the road's centre the ground lies
	// just under the road. It reaches a cell and a half past the shoulder,
	// so no terrain triangle that overlaps the road rises above it.
	flattenM float64
	cover    *cover
	// lift raises the profile onto the ground model at a distance along the
	// course (0 without one): heights are the model's own.
	lift  func(d float64) float64
	loops []bool // by line: the course's own loop
	// motorways: by line, a motorway's carriageway (motorways.go).
	motorways []bool
	// cut: the roads are cut out of the terrain (worlds with a ground
	// model; see verges.go): the terrain is the ground model, unflattened,
	// and drops the cells its roads and their verges cover.
	cut bool
	// grid finds road samples near a point (cut terrain; built once the
	// roads are final).
	grid     *routeGrid
	gridOnce sync.Once
	// surfaces: the road surface triangles (cut terrain): what plants and
	// grass keep off, junctions and roundabouts included.
	surfaces *cutter
	// lodOf is the grid's level of detail (lod.go), made on first use,
	// once the roads are final.
	lodOf   *lod
	lodOnce sync.Once
	// landAt, when set, is the land's height anywhere (the graph builder's
	// land, graph_terrain.go): what surface.at and surface.ground give,
	// in place of this terrain's grid.
	landAt func(e, n float64) float64
}

// lod is the grid's level of detail.
func (t *terrain) lod() *lod {
	t.lodOnce.Do(func() { t.lodOf = newLOD(t) })
	return t.lodOf
}

// roadGrid is the grid of the terrain's road samples, by gridCellM cells.
func (t *terrain) roadGrid() *routeGrid {
	t.gridOnce.Do(func() { t.grid = newRouteGrid(t.fine, gridCellM) })
	return t.grid
}

// gridCellM: the road grid's cells, enough for every lookup within reach
// (the verges, and a sample spacing more).
const gridCellM = vergeM + 3

// chunks lists the chunks within the corridor, row by row from the south.
func (t *terrain) chunks() [][2]int {
	size, r := t.o.ChunkM, t.o.CorridorM
	set := map[[2]int]bool{}
	for _, s := range t.coarse {
		for i := int(math.Floor((s.e - r) / size)); i <= int(math.Floor((s.e+r)/size)); i++ {
			for j := int(math.Floor((s.n - r) / size)); j <= int(math.Floor((s.n+r)/size)); j++ {
				if rectDist(s.e, s.n, float64(i)*size, float64(j)*size, size) <= r {
					set[[2]int{i, j}] = true
				}
			}
		}
	}
	keys := make([][2]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		return keys[a][1] < keys[b][1] || keys[a][1] == keys[b][1] && keys[a][0] < keys[b][0]
	})
	return keys
}

// rectDist is the distance from e, n to the square with its south-west
// corner at e0, n0.
func rectDist(e, n, e0, n0, size float64) float64 {
	de := math.Max(0, math.Max(e0-e, e-(e0+size)))
	dn := math.Max(0, math.Max(n0-n, n-(n0+size)))
	return math.Hypot(de, dn)
}

// lists are the road segments and route samples that can matter in a
// chunk.
func (t *terrain) lists(key [2]int) (segs []int, near []sample) {
	size := t.o.ChunkM
	e0, n0 := float64(key[0])*size, float64(key[1])*size
	for i := 0; i+1 < len(t.fine); i++ {
		a, b := t.fine[i], t.fine[i+1]
		if a.line != b.line {
			continue // two roads, not a segment
		}
		if rectDist((a.e+b.e)/2, (a.n+b.n)/2, e0, n0, size) <= t.o.BlendM+fineStep {
			segs = append(segs, i)
		}
	}
	for _, s := range t.coarse {
		if rectDist(s.e, s.n, e0, n0, size) <= t.o.CorridorM+1.5*size {
			near = append(near, s)
		}
	}
	return segs, near
}

// vertex is a terrain vertex at e, n: its height and land cover. The
// ground under water lies below its level; under the road it stays the
// road's; beside the asphalt (and the sidewalks) a verge of grass.
func (t *terrain) vertex(e, n float64, segs []int, near []sample) (float64, scenery.Land, int) {
	h, rn := t.ground(e, n, segs, near)
	land, id := t.cover.at(e, n)
	if rn.d < rn.edge+verge {
		land = verdure
	}
	switch {
	case land == scenery.LandWater && t.exact[id]:
		// Water drawn from its outline has a bed of its own and the land
		// under it is cut away: the land stands just over the level, so the
		// cells across its shore meet the water at or above it (lowered, they
		// sloped under it at the outline, and from the road the eye looked
		// under the water's edge: owner, 2026-10-11, 0.3-1.4 km of a new course).
		h = math.Max(h, t.cover.level(t, id, e, n)+exactShoreM)
	case land == scenery.LandWater && (rn.d >= rn.flat || t.cut && rn.d >= rn.edge):
		h = math.Min(h, t.cover.level(t, id, e, n)-bedM)
	}
	return h, land, id
}

// height is the terrain at e, n.
func (t *terrain) height(e, n float64, segs []int, near []sample) float64 {
	h, _ := t.ground(e, n, segs, near)
	return h
}

// roadNear is the road nearest a terrain point: how far its centre is,
// how far its edge (with sidewalks), and how far the ground lies flat
// under it.
type roadNear struct{ d, edge, flat float64 }

// liftAt is lift at d (0 without one).
func (t *terrain) liftAt(d float64) float64 {
	if t.lift == nil {
		return 0
	}
	return t.lift(d)
}

// base is the ground at e, n before the road shapes it: the ground model
// at the profile's height, or the profile's own shape.
func (t *terrain) base(e, n float64, near []sample) float64 {
	if z, ok := t.model(e, n, near); ok {
		return z
	}
	return t.fromProfile(e, n, near)
}

// model is the ground model at e, n at the profile's height; false where
// it has none (no model, outside it, or water).
func (t *terrain) model(e, n float64, near []sample) (float64, bool) {
	if t.o.Elevation == nil || len(near) == 0 {
		return 0, false
	}
	lat, lon := t.c.Unproject(e, n)
	z, ok := t.o.Elevation(lat, lon)
	if !ok {
		return 0, false
	}
	return z - nearest(e, n, near).off, true
}

// ground is the terrain at e, n and the road nearest it (at +Inf when no
// road is near).
func (t *terrain) ground(e, n float64, segs []int, near []sample) (float64, roadNear) {
	base := t.base(e, n, near)
	if t.cut {
		return base, t.nearRoad(e, n)
	}

	// The road, piece by piece: each piece's nearest point. Pieces keep
	// the passes of a road apart where it comes back near itself (a
	// crossing, a switchback).
	var ps [8]piece
	pieces := ps[:0]
	for _, i := range segs {
		a, b := t.fine[i], t.fine[i+1]
		if a.aloft && b.aloft {
			continue // a bridge's deck: the ground goes on under it
		}
		de, dn := b.e-a.e, b.n-a.n
		f := 0.0
		if l2 := de*de + dn*dn; l2 > 0 {
			f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
		}
		d := math.Hypot(e-(a.e+f*de), n-(a.n+f*dn))
		k := [2]int{a.line, int(a.d / (pieceSegs * fineStep))}
		if len(pieces) == 0 || pieces[len(pieces)-1].k != k {
			pieces = append(pieces, piece{k: k, d: math.Inf(1)})
		}
		if p := &pieces[len(pieces)-1]; d < p.d {
			p.d, p.ele, p.along = d, a.ele+f*(b.ele-a.ele), a.d+f*(b.d-a.d)
			p.edge, p.flat = a.edge, a.flat
		}
	}
	if len(pieces) == 0 {
		return base, roadNear{math.Inf(1), 0, 0}
	}
	closest := pieces[0]
	for _, p := range pieces[1:] {
		if p.d < closest.d {
			closest = p
		}
	}
	rn := roadNear{closest.d, closest.edge, closest.flat}
	if closest.d < closest.flat {
		// On the road: the ground lies under it, or under another pass of
		// the road that runs below it here, which makes this one a bridge.
		// Another road a little lower nearby (a service road beside the
		// main road, a side street at a junction): the ground goes down to
		// it, as far as the nearer road's skirt hides; following the
		// nearest only, it rose through the lower road in steps along the
		// grid.
		road := closest.ele
		for _, p := range pieces {
			switch {
			case p.d >= p.flat || p.ele >= road:
			case p.ele < closest.ele-bridgeM && t.apart(p, closest) > otherPassM:
				road = p.ele
			default:
				road = math.Max(p.ele, closest.ele-skirtM+sinkM)
			}
		}
		return road - sinkM, rn
	}
	if closest.d >= t.blendOf(closest) {
		return base, rn
	}
	// Beside the road: the pieces' heights weighted so the nearest takes
	// over as it comes close and each fades out at the blend's edge, which
	// keeps the ground continuous between passes.
	var sum, wsum float64
	for _, p := range pieces {
		x := (p.d - p.flat) / (t.blendOf(p) - p.flat)
		if x >= 1 {
			continue
		}
		w := (1 - smoothstep(x)) / (x*x + 1e-6)
		sum += w * p.ele
		wsum += w
	}
	road := sum / wsum
	f := smoothstep((closest.d - closest.flat) / (t.blendOf(closest) - closest.flat))
	return (road-sinkM)*(1-f) + base*f, rn
}

// blendOf is how far from its centre a piece of road shapes the ground.
// With a ground model the road's heights are the model's along it, so
// the ground meets the model a cell past the flat: a sunken lane keeps its
// banks, an embankment its slopes (a 40 m blend had levelled them). Without
// one, the ground is shaped from the profile and blends over BlendM.
func (t *terrain) blendOf(p piece) float64 {
	if t.o.Elevation != nil {
		return p.flat + t.o.CellM
	}
	return math.Max(t.o.BlendM, p.flat+10)
}

// piece is a stretch of road as seen from one terrain point.
type piece struct {
	k             [2]int  // which stretch: its line, and 50 m along it
	d, ele, along float64 // its nearest point: distance, elevation, course distance
	edge, flat    float64 // there: the road's edge (with sidewalks), the flat ground's
}

const (
	// bridgeM: another road this much lower under a road makes it a
	// bridge (the ground stays with the lower); roads at a junction differ
	// less, and the ground follows the nearest.
	bridgeM    = 2.5
	pieceSegs  = 25  // road segments per piece: 50 m
	otherPassM = 100 // pieces further apart along the course are different passes
)

// apart is the distance between two pieces' nearest points along their
// road, the short way round on the course's own loop; pieces of two roads
// are always apart.
func (t *terrain) apart(a, b piece) float64 {
	if a.k[0] != b.k[0] {
		return math.Inf(1)
	}
	d := math.Abs(a.along - b.along)
	if a.k[0] < len(t.loops) && t.loops[a.k[0]] {
		d = math.Min(d, t.c.Distance-d)
	}
	return d
}

// fromProfile is the ground without a ground model: the route's elevation,
// weighted by inverse squared distance, so it rises and falls with the
// road and levels out between.
//
// Only route samples within the corridor and half a chunk of e, n count,
// which every chunk's near list holds for its points: so the ground at a
// point is the same whichever chunk asks, and chunks meet without cracks.
func (t *terrain) fromProfile(e, n float64, near []sample) float64 {
	reach := t.o.CorridorM + 0.5*t.o.ChunkM
	var sum, wsum float64
	for _, s := range near {
		d2 := (s.e-e)*(s.e-e) + (s.n-n)*(s.n-n)
		if d2 > reach*reach {
			continue
		}
		w := 1 / (d2 + 400)
		sum += w * s.ele
		wsum += w
	}
	if wsum == 0 {
		return nearest(e, n, t.coarse).ele
	}
	return sum / wsum
}

func nearest(e, n float64, s []sample) sample {
	best, bd := s[0], math.Inf(1)
	for _, x := range s {
		if d := (x.e-e)*(x.e-e) + (x.n-n)*(x.n-n); d < bd {
			best, bd = x, d
		}
	}
	return best
}

// roadGap is how far e, n is from the edge on its side (with that side's
// sidewalk, or reach over to a road alongside) of the nearest road that
// lies on the ground (bridges cut nothing) among segs, other than line
// skip, measured across it: past a road's dead end it doesn't count (a
// verge runs along a road, not round its end). 0 on one, +Inf with none.
func (t *terrain) roadGap(e, n float64, _ []int, skip int) float64 {
	best := math.Inf(1)
	t.roadGrid().within(e, n, gridCellM, func(i int) {
		if i+1 >= len(t.fine) {
			return
		}
		a, b := t.fine[i], t.fine[i+1]
		if a.line == skip || a.aloft || b.line != a.line {
			return
		}
		de, dn := b.e-a.e, b.n-a.n
		l2 := de*de + dn*dn
		if l2 == 0 {
			return
		}
		raw := ((e-a.e)*de + (n-a.n)*dn) / l2
		first := i == 0 || t.fine[i-1].line != a.line
		last := i+2 >= len(t.fine) || t.fine[i+2].line != a.line
		if first && raw < 0 || last && raw > 1 {
			return
		}
		f := math.Max(0, math.Min(1, raw))
		side := 1
		if de*(n-a.n)-dn*(e-a.e) > 0 {
			side = 0 // left of the way the road runs
		}
		edge := sideEdge(a, side) + f*(sideEdge(b, side)-sideEdge(a, side))
		best = math.Min(best, math.Max(0, math.Hypot(e-a.e-f*de, n-a.n-f*dn)-edge))
	})
	return best
}

// sideEdge is how far a road's edge is from its centre on one side (0
// left): its carriageway, and its sidewalk there or its reach over to a
// road alongside.
func sideEdge(s sample, side int) float64 {
	if s.terrace[side] > 0 {
		return s.terrace[side] // the terrace's slab (terrace.go) is the road's edge there
	}
	if s.walk[side] {
		return s.hw + s.walkWidth(side)
	}
	return s.hw + s.wide[side]
}

// markAloft marks the road samples more than bridgeM off the ground
// model: bridges and viaducts, which cut nothing out of the terrain and
// have no verges.
func (t *terrain) markAloft(ls []*roadLine) {
	g := newRouteGrid(t.coarse, 400)
	for _, l := range ls {
		for i := range l.samples {
			s := &l.samples[i]
			var near []sample
			g.within(s.e, s.n, 400, func(k int) { near = append(near, t.coarse[k]) })
			// Above the model: a bridge. Below it: a cutting, a tunnel, an
			// underpass (the course's own way under a viaduct, graph builder,
			// 2026-10-12: taken for a deck, it got a slab and the land over it).
			if z, ok := t.model(s.e, s.n, near); ok && s.ele-z > bridgeM {
				s.aloft = true
			}
		}
	}
}

// nearRoad is the road nearest e, n within the road grid's reach (cut
// terrain): how far its centre and edge are.
func (t *terrain) nearRoad(e, n float64) roadNear {
	rn := roadNear{math.Inf(1), 0, 0}
	t.roadGrid().within(e, n, gridCellM, func(i int) {
		if i+1 >= len(t.fine) {
			return
		}
		a, b := t.fine[i], t.fine[i+1]
		if b.line != a.line {
			return
		}
		de, dn := b.e-a.e, b.n-a.n
		f := 0.0
		if l2 := de*de + dn*dn; l2 > 0 {
			f = math.Max(0, math.Min(1, ((e-a.e)*de+(n-a.n)*dn)/l2))
		}
		if d := math.Hypot(e-a.e-f*de, n-a.n-f*dn); d < rn.d {
			rn = roadNear{d, a.edge, a.flat}
		}
	})
	return rn
}
