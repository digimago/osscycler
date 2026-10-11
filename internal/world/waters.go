package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Water drawn from its own outline (owner, 2026-10-11: the water was
// drawn per 5 m terrain cell, in stair steps, and the ground cells along
// its edges stood out into it as green triangles; ditches in the map are
// areas running alongside the roads, and should look it). With a cut
// terrain, each water area with a simple outline is drawn as that
// outline: its surface at its level (from its banks, as before), the land
// under it cut away as under a road, a bed bedM lower, and a bank face
// round the outline from the land's edge down to the bed. Areas with
// islands, and very long outlines, keep the cells.
const (
	exactMaxPoints = 2000 // a longer outline (a river) keeps the cells
	// The surface's triangles are split to at most waterCellM across (the
	// level varies along a long water, slowly: it comes from banks within
	// localM), each by its own longest edge; a whole-outline split made a
	// 160 m ditch 19,000 triangles, and the cut through them took minutes.
	waterCellM = 50.0
	// The bank face's points along the outline, besides its corners: the
	// land's edge it follows is the 5 m terrain's (1 m made 3 M triangles
	// of bank face on the Grebbeberg course, every ditch side metre by metre).
	bankFaceStepM = 5.0
	// Only water within this of the route is drawn from its outline: the
	// stair steps show near the road, while beyond it the cut through the
	// land and the bank faces doubled a world with much water (Grebbeberg
	// to Rotterdam: 16 -> 28 M triangles, every polder ditch in 300 m).
	exactReachM = 60.0
)

type exactWater struct {
	id   int
	ring [][2]float64
	key  [2]int // the chunk its first point is in: where its meshes go
}

// waterRings chains an area's edges (in ring order) into rings.
func waterRings(edges [][4]float64) [][][2]float64 {
	var rings [][][2]float64
	var cur [][2]float64
	for _, e := range edges {
		if len(cur) > 0 {
			last := cur[len(cur)-1]
			if math.Hypot(last[0]-e[0], last[1]-e[1]) > 1e-6 {
				rings = append(rings, cur)
				cur = nil
			}
		}
		if len(cur) == 0 {
			cur = append(cur, [2]float64{e[0], e[1]})
		}
		cur = append(cur, [2]float64{e[2], e[3]})
	}
	if len(cur) > 0 {
		rings = append(rings, cur)
	}
	for i, r := range rings {
		if n := len(r); n > 1 && math.Hypot(r[0][0]-r[n-1][0], r[0][1]-r[n-1][1]) < 1e-6 {
			rings[i] = r[:n-1]
		}
	}
	return rings
}

func ringArea(r [][2]float64) float64 {
	a := 0.0
	for i := range r {
		j := (i + 1) % len(r)
		a += r[i][0]*r[j][1] - r[j][0]*r[i][1]
	}
	return a / 2
}

// exactWaters picks the water areas over the terrain that are drawn from
// their outline, and marks them so the terrain's cells leave them out.
func (t *terrain) exactWaters(keys [][2]int) []exactWater {
	if t.cover == nil || t.cover.m == nil || !t.cut {
		return nil
	}
	size := t.o.ChunkM
	have := map[[2]int]bool{}
	for _, k := range keys {
		have[k] = true
	}
	// The route's coarse samples by 50 m cell, for the reach.
	const cell = 50.0
	route := map[[2]int][][2]float64{}
	for _, c := range t.coarse {
		k := [2]int{int(math.Floor(c.e / cell)), int(math.Floor(c.n / cell))}
		route[k] = append(route[k], [2]float64{c.e, c.n})
	}
	// near: a coarse sample (20 m apart) within exactReachM of e, n, give
	// or take half their spacing.
	near := func(e, n float64) bool {
		r := exactReachM + coarseStep*fineStep/2
		k := [2]int{int(math.Floor(e / cell)), int(math.Floor(n / cell))}
		for dx := -2; dx <= 2; dx++ {
			for dy := -2; dy <= 2; dy++ {
				for _, p := range route[[2]int{k[0] + dx, k[1] + dy}] {
					if math.Hypot(p[0]-e, p[1]-n) <= r {
						return true
					}
				}
			}
		}
		return false
	}
	reaches := func(r [][2]float64) bool {
		for i := range r {
			a, b := r[i], r[(i+1)%len(r)]
			l := math.Hypot(b[0]-a[0], b[1]-a[1])
			for k := 0.0; k <= l; k += 10 {
				f := k / math.Max(l, 1e-9)
				if near(a[0]+f*(b[0]-a[0]), a[1]+f*(b[1]-a[1])) {
					return true
				}
			}
		}
		return false
	}
	var out []exactWater
	t.exact = map[int]bool{}
	for _, id := range t.cover.m.Waters() {
		rings := waterRings(t.cover.m.Edges(id))
		if len(rings) != 1 || len(rings[0]) < 3 || len(rings[0]) > exactMaxPoints {
			continue
		}
		r := rings[0]
		over := false
		for _, p := range r {
			if have[[2]int{int(math.Floor(p[0] / size)), int(math.Floor(p[1] / size))}] {
				over = true
				break
			}
		}
		if !over || !reaches(r) {
			continue
		}
		// Only where ear clipping takes the whole outline, every triangle
		// turning the ring's way: else the cells. (A 1 % tolerance on
		// unsigned areas let a pond through whose triangles left square
		// metres of it uncovered, holes at the water's edge.)
		want, got, flipped := ringArea(r), 0.0, false
		for _, tr := range earClip(r) {
			a := ringArea([][2]float64{r[tr[0]], r[tr[1]], r[tr[2]]})
			if a*want < 0 && math.Abs(a) > 1e-6 {
				flipped = true
			}
			got += a
		}
		if want == 0 || flipped || math.Abs(got-want) > 1e-6*math.Abs(want)+1e-3 {
			continue
		}
		out = append(out, exactWater{id: id, ring: r, key: [2]int{int(math.Floor(r[0][0] / size)), int(math.Floor(r[0][1] / size))}})
		t.exact[id] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// waterMeshes draws an exact water: its surface (material water), and
// its bed and bank (material ground, in vertex colours). land is the land
// as drawn before the cut: where the bank face starts.
func (t *terrain) waterMeshes(x exactWater, water, ground int, land *landSurface) (surface, bed gltf.Primitive) {
	level := func(e, n float64) float64 { return t.cover.level(t, x.id, e, n) }
	surface = waterSheet(level, x.ring, water)
	bed = waterSheet(func(e, n float64) float64 { return level(e, n) - bedM }, x.ring, ground)
	bedColour := landColors[scenery.LandWater]
	bed.UVs = nil
	for range len(bed.Positions) / 3 {
		bed.Colors = append(bed.Colors, bedColour[0], bedColour[1], bedColour[2])
	}
	// The bank: round the outline, from the land's edge (or the level,
	// where no land is drawn) down to the bed, facing the water.
	grass := landColors[scenery.LandNone]
	r := x.ring
	inward := 1.0 // the interior is left of the ring's direction when it runs counter-clockwise
	if ringArea(r) < 0 {
		inward = -1
	}
	var pts [][2]float64
	for i := range r {
		a, b := r[i], r[(i+1)%len(r)]
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		n := max(1, int(math.Ceil(l/bankFaceStepM)))
		for k := range n {
			f := float64(k) / float64(n)
			pts = append(pts, [2]float64{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1])})
		}
	}
	base := uint32(len(bed.Positions) / 3)
	for _, p := range pts {
		lv := level(p[0], p[1])
		top := lv
		if h, ok := land.at(p[0], p[1]); ok {
			top = math.Max(h, lv)
		}
		bed.Positions = append(bed.Positions, float32(p[0]), float32(top+0.02), float32(-p[1]), float32(p[0]), float32(lv-bedM), float32(-p[1]))
		bed.Colors = append(bed.Colors, grass[0], grass[1], grass[2], bedColour[0], bedColour[1], bedColour[2])
		bed.Normals = append(bed.Normals, 0, 0, 0, 0, 0, 0) // set below
	}
	m := len(pts)
	for i := range m {
		j := (i + 1) % m
		a, b := pts[i], pts[j]
		// Horizontal normal into the water: left of a→b for an inward 1.
		de, dn := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(de, dn)
		if l == 0 {
			continue
		}
		ne, nn := -dn/l*inward, de/l*inward
		for _, v := range []int{i, j} {
			for k := range 2 {
				o := 3 * (int(base) + 2*v + k)
				bed.Normals[o], bed.Normals[o+2] = float32(ne), float32(-nn) // z is south
			}
		}
		ta, ba := base+uint32(2*i), base+uint32(2*i)+1
		tb, bb := base+uint32(2*j), base+uint32(2*j)+1
		// Seen from the water (the normal's side) counter-clockwise.
		if inward > 0 {
			bed.Indices = append(bed.Indices, ta, tb, ba, tb, bb, ba)
		} else {
			bed.Indices = append(bed.Indices, ta, ba, tb, tb, ba, bb)
		}
	}
	return surface, bed
}

// waterSheet lays outline r flat at height(e, n), ear-clipped, each
// triangle split by its own longest edge into pieces at most waterCellM
// across; normals up, UVs every 5 m.
func waterSheet(height func(e, n float64) float64, r [][2]float64, material int) gltf.Primitive {
	p := gltf.Primitive{Material: material}
	for _, t := range earClip(r) {
		a, b, c := r[t[0]], r[t[1]], r[t[2]]
		longest := math.Max(math.Hypot(b[0]-a[0], b[1]-a[1]), math.Max(math.Hypot(c[0]-b[0], c[1]-b[1]), math.Hypot(a[0]-c[0], a[1]-c[1])))
		n := min(8, max(1, int(math.Ceil(longest/waterCellM))))
		idx := map[[2]int]uint32{}
		vtx := func(i, j int) uint32 {
			if v, ok := idx[[2]int{i, j}]; ok {
				return v
			}
			fi, fj := float64(i)/float64(n), float64(j)/float64(n)
			e := a[0] + fi*(b[0]-a[0]) + fj*(c[0]-a[0])
			no := a[1] + fi*(b[1]-a[1]) + fj*(c[1]-a[1])
			p.Positions = append(p.Positions, float32(e), float32(height(e, no)), float32(-no))
			p.Normals = append(p.Normals, 0, 1, 0)
			p.UVs = append(p.UVs, float32(e/5), float32(no/5))
			v := uint32(len(p.Positions)/3 - 1)
			idx[[2]int{i, j}] = v
			return v
		}
		for i := range n {
			for j := range n - i {
				tri(&p, vtx(i, j), vtx(i+1, j), vtx(i, j+1), [3]float64{0, 1, 0})
				if i+j+1 < n {
					tri(&p, vtx(i+1, j), vtx(i+1, j+1), vtx(i, j+1), [3]float64{0, 1, 0})
				}
			}
		}
	}
	return p
}
