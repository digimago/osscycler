package world

import (
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// The road's cross-section, from the map where it has the road: the
// carriageway (its width and surface), cycle lanes painted along its
// edges, and sidewalks on a kerb.
const (
	laneM      = 1.5  // a cycle lane's width, inside the carriageway's edge
	walkM      = 1.8  // a sidewalk's width, outside it
	kerbM      = 0.12 // the kerb's height
	walkFootM  = 0.25 // a sidewalk's outer face goes this far into the ground
	skirtM     = 0.3  // a road's edge without a sidewalk goes this far into the ground
	taperSteps = 5    // width changes are spread over this many fine steps each way
)

// roadMaterials are the materials of the road's parts, by the part.
type roadMaterials struct {
	surface    [5]int // by scenery.Surface
	lane, walk int
	kerb       int
	ground     int // the terrain's
	island     int // a roundabout's island: grass in its vertices, not land (the cut leaves it)
	paving     int // a roundabout\'s paved centre circle
}

func addRoadMaterials(d *gltf.Doc) roadMaterials {
	mat := func(name string, hex uint32, rough float32) int {
		c := srgbLinear(hex)
		return d.AddMaterial(gltf.Material{Name: name, Color: [4]float32{c[0], c[1], c[2], 1}, Roughness: rough})
	}
	var m roadMaterials
	// One grey for every carriageway until roads get textures (owner,
	// 2026-10-09): the surface (asphalt, concrete, klinkers, unpaved) is
	// known per road and junction, for the texture to show then.
	road := mat("road", 0x45464a, 0.85)
	for i := range m.surface {
		m.surface[i] = road
	}
	m.surface[scenery.SurfaceTrack] = mat("running track", 0xa5503e, 0.9) // the test oval's
	m.lane = mat("cycle lane", 0xa04a3c, 0.85)                            // red, as Dutch fietsstroken
	m.walk = mat("sidewalk", 0x96928a, 0.9)
	m.kerb = mat("kerb", 0xc8c6c0, 0.8)
	m.island = d.AddMaterial(gltf.Material{Name: "island", Color: [4]float32{1, 1, 1, 1}, Roughness: 1})
	m.paving = mat("roundabout centre", 0x8c5a48, 0.9) // red klinkers
	return m
}

// applyRoads gives each sample its road from the stretches (by distance):
// half width tapered where it changes, sidewalks, cycle lanes, surface;
// and where the ground lies flat under it. Without stretches the road is
// width wide, asphalt, without sidewalks.
func applyRoads(fine []sample, roads []scenery.RoadStretch, width, cell float64) {
	raw := make([]float64, len(fine))
	for i := range fine {
		fine[i].hw, fine[i].surface = width/2, scenery.SurfaceAsphalt
		if len(roads) == 0 {
			raw[i] = width / 2
			continue
		}
		k := sort.Search(len(roads), func(k int) bool { return roads[k].ToM >= fine[i].d })
		r := roads[min(k, len(roads)-1)]
		raw[i] = r.WidthM / 2
		fine[i].surface, fine[i].walk, fine[i].lane = r.Surface, r.Sidewalk, r.CycleLane
		// A lane needs room for traffic beside it.
		for side := range 2 {
			if r.WidthM < 2*laneM+2.5 {
				fine[i].lane[side] = false
			}
		}
	}
	for i := range fine {
		var sum float64
		n := 0
		for k := max(0, i-taperSteps); k <= min(len(fine)-1, i+taperSteps); k++ {
			sum += raw[k]
			n++
		}
		fine[i].hw = sum / float64(n)
		fine[i].edge = fine[i].hw
		if fine[i].walk[0] || fine[i].walk[1] {
			fine[i].edge += walkM
		}
		fine[i].flat = fine[i].edge + flatPast(cell)
	}
}

// part is a strip of the road's cross-section at one sample: from offset
// a to b (metres to the right of the centre line) at heights ya and yb
// above the road, facing up, or sideways when it is a wall.
type part struct {
	on             bool
	a, b, ya, yb   float64
	material       int
	wall, wallLeft bool // a vertical face, looking left (else right)
}

// roadMesh is the road along seg, which starts at all[from]: a strip per
// part of the cross-section, the parts sharing a material in one
// primitive. Cross-sections stand at the samples, and in between where the
// smooth line bends more than a few degrees; in a bend tighter than the
// road and its sidewalks are wide, the inner edge holds back rather than
// fold over. UVs run across each strip (0 to 1) and along it (one per
// 5 m).
func roadMesh(seg, all []sample, from int, line *smoothLine, loop bool, m roadMaterials, v *verger) []gltf.Primitive {
	parts := func(s sample) [11]part {
		if s.again {
			return [11]part{} // an earlier pass draws this road
		}
		a, b := -s.hw-s.wide[0], s.hw+s.wide[1]
		if s.lane[0] {
			a += laneM
		}
		if s.lane[1] {
			b -= laneM
		}
		return [11]part{
			{on: true, a: a, b: b, material: m.surface[s.surface]},
			{on: s.lane[0], a: -s.hw, b: -s.hw + laneM, material: m.lane},
			{on: s.lane[1], a: s.hw - laneM, b: s.hw, material: m.lane},
			{on: s.walk[0], a: -s.hw - walkM, b: -s.hw, ya: kerbM, yb: kerbM, material: m.walk},
			{on: s.walk[1], a: s.hw, b: s.hw + walkM, ya: kerbM, yb: kerbM, material: m.walk},
			{on: s.walk[0], a: -s.hw, b: -s.hw, ya: 0, yb: kerbM, material: m.kerb, wall: true},
			{on: s.walk[1], a: s.hw, b: s.hw, ya: 0, yb: kerbM, material: m.kerb, wall: true, wallLeft: true},
			{on: s.walk[0], a: -s.hw - walkM, b: -s.hw - walkM, ya: -walkFootM, yb: kerbM, material: m.kerb, wall: true, wallLeft: true},
			{on: s.walk[1], a: s.hw + walkM, b: s.hw + walkM, ya: -walkFootM, yb: kerbM, material: m.kerb, wall: true},
			// Without a sidewalk, the edge goes on into the ground: the road
			// sits in the landscape, not on it.
			{on: !s.walk[0], a: -s.hw - s.wide[0], b: -s.hw - s.wide[0], ya: -skirtM, yb: 0, material: m.surface[s.surface], wall: true, wallLeft: true},
			{on: !s.walk[1], a: s.hw + s.wide[1], b: s.hw + s.wide[1], ya: -skirtM, yb: 0, material: m.surface[s.surface], wall: true},
		}
	}
	type frame struct {
		x, y, z    float64 // the centre line, glTF axes
		rx, rz     float64 // to the right, horizontal
		nx, ny, nz float64 // the road's normal
		v          float64 // along, for UVs
		parts      [11]part
		onPatch    bool // within a junction's span
		underPatch bool
		verge      [2][]vpt // left and right (nil: none)
	}
	// slope is the road's grade at sample g, from its neighbours.
	slope := func(g int) float64 {
		prev, next := g-1, g+1
		if prev < 0 {
			prev = 0
			if loop {
				prev = len(all) - 2 // the last sample is the first again
			}
		}
		if next >= len(all) {
			next = len(all) - 1
			if loop {
				next = 1
			}
		}
		a, b := all[prev], all[next]
		if l := math.Hypot(b.e-a.e, b.n-a.n); l > 0 {
			return (b.ele - a.ele) / l
		}
		return 0
	}
	mk := func(s sample, d, ele, grade float64) frame {
		x, y := line.at(d)
		de, dn, k := line.heading(d)
		// Right of the direction of travel is (dn, -de) east/north; glTF z
		// is south.
		rx, rz := dn, de
		// The normal: right × along (up on a level road).
		tx, ty, tz := de, grade, -dn
		nx, ny, nz := -rz*ty, rz*tx-rx*tz, rx*ty
		nl := math.Sqrt(nx*nx + ny*ny + nz*nz)
		// In a bend tighter than the road and its sidewalks are wide, the
		// inner sidewalk and cycle lane stop (squeezed, they would fan into
		// spikes), and the carriageway's inner edge holds back.
		if inner := 0.9 / math.Abs(k); k != 0 && inner < s.hw+walkM+0.5 {
			side := 1
			if k > 0 { // turning left: the left side is inside
				side = 0
			}
			s.walk[side], s.lane[side] = false, false
		}
		ps := parts(s)
		if inner := 0.9 / math.Abs(k); k != 0 && inner < s.hw {
			for i := range ps {
				if k > 0 {
					ps[i].a, ps[i].b = math.Max(ps[i].a, -inner), math.Max(ps[i].b, -inner)
				} else {
					ps[i].a, ps[i].b = math.Min(ps[i].a, inner), math.Min(ps[i].b, inner)
				}
			}
		}
		f := frame{x: x, y: ele, z: -y, rx: rx, rz: rz, nx: nx / nl, ny: ny / nl, nz: nz / nl, v: d / 5, parts: ps, onPatch: s.onPatch, underPatch: s.underPatch}
		// Verges, from the edge on each side (the sidewalk's outer edge
		// where there is one), as far as a tight bend's inside allows.
		if v != nil && !s.aloft && !s.onPatch && !s.underPatch && !s.again {
			for side, sign := range []float64{-1, 1} {
				base, y0 := s.hw+s.wide[side], ele
				if s.walk[side] {
					base, y0 = s.hw+walkM, ele+kerbM
				}
				limit := math.Inf(1)
				if k != 0 && (k > 0) == (side == 0) {
					limit = 0.9 / math.Abs(k)
				}
				if base >= limit {
					continue
				}
				f.verge[side] = v.across(f.x, f.z, sign*rx, sign*rz, base, limit, y0, s.line)
			}
		}
		return f
	}
	var frames []frame
	for k, s := range seg {
		g := from + k
		frames = append(frames, mk(s, s.d, s.ele, slope(g)))
		if k+1 == len(seg) {
			break
		}
		// Through a bend: more cross-sections, every few degrees.
		nxt := seg[k+1]
		h0x, h0y, _ := line.heading(s.d)
		h1x, h1y, _ := line.heading(nxt.d)
		turn := math.Abs(math.Atan2(h0x*h1y-h0y*h1x, h0x*h1x+h0y*h1y))
		extra := int(turn / maxArcRad)
		for q := 1; q <= extra; q++ {
			f := float64(q) / float64(extra+1)
			mid := s
			mid.hw = s.hw + f*(nxt.hw-s.hw)
			mid.onPatch = s.onPatch || nxt.onPatch
			frames = append(frames, mk(mid, s.d+f*(nxt.d-s.d), s.ele+f*(nxt.ele-s.ele), slope(g)+f*(slope(g+1)-slope(g))))
		}
	}

	prims := map[int]*gltf.Primitive{}
	var order []int
	vert := func(p *gltf.Primitive, f frame, off, y, u float64, n [3]float64) uint32 {
		p.Positions = append(p.Positions, float32(f.x+f.rx*off), float32(f.y+y), float32(f.z+f.rz*off))
		p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		p.UVs = append(p.UVs, float32(u), float32(f.v))
		return uint32(len(p.Positions)/3 - 1)
	}
	// A strip's quads share their vertices with the next quad's.
	type row struct {
		frame int
		a, b  uint32
		mat   int
	}
	last := map[int]row{} // by part
	for k := 0; k+1 < len(frames); k++ {
		f0, f1 := frames[k], frames[k+1]
		if f0.onPatch || f1.onPatch || f0.underPatch && f1.underPatch {
			continue // the junction draws this surface: the road ends at its edge
		}
		for i := range f0.parts {
			p0, p1 := f0.parts[i], f1.parts[i]
			if !p0.on || !p1.on {
				continue
			}
			pr := prims[p0.material]
			if pr == nil {
				pr = &gltf.Primitive{Material: p0.material}
				prims[p0.material] = pr
				order = append(order, p0.material)
			}
			normal := func(f frame, p part) [3]float64 {
				switch {
				case !p.wall:
					return [3]float64{f.nx, f.ny, f.nz}
				case p.wallLeft:
					return [3]float64{-f.rx, 0, -f.rz}
				}
				return [3]float64{f.rx, 0, f.rz}
			}
			var a0, b0 uint32
			if r, ok := last[i]; ok && r.frame == k && r.mat == p0.material {
				a0, b0 = r.a, r.b
			} else {
				a0 = vert(pr, f0, p0.a, p0.ya, 0, normal(f0, p0))
				b0 = vert(pr, f0, p0.b, p0.yb, 1, normal(f0, p0))
			}
			a1 := vert(pr, f1, p1.a, p1.ya, 0, normal(f1, p1))
			b1 := vert(pr, f1, p1.b, p1.yb, 1, normal(f1, p1))
			last[i] = row{k + 1, a1, b1, p0.material}
			tri(pr, a0, b0, a1, normal(f0, p0))
			tri(pr, b0, b1, a1, normal(f0, p0))
		}
	}
	out := make([]gltf.Primitive, 0, len(order)+1)
	for _, mat := range order {
		out = append(out, *prims[mat])
	}
	if v != nil {
		vp := gltf.Primitive{Material: v.mat}
		for side := range 2 {
			var run [][]vpt
			for k := range frames {
				if a := frames[k].verge[side]; a != nil {
					run = append(run, a)
					continue
				}
				vergeRun(&vp, run)
				run = nil
			}
			vergeRun(&vp, run)
		}
		if len(vp.Indices) > 0 {
			smoothNormals(&vp)
			out = append(out, vp)
		}
	}
	return out
}

// tri adds a triangle facing n: glTF's front faces wind counter-clockwise.
func tri(p *gltf.Primitive, a, b, c uint32, n [3]float64) {
	pos := func(i uint32) [3]float64 {
		return [3]float64{float64(p.Positions[3*i]), float64(p.Positions[3*i+1]), float64(p.Positions[3*i+2])}
	}
	pa, pb, pc := pos(a), pos(b), pos(c)
	u := [3]float64{pb[0] - pa[0], pb[1] - pa[1], pb[2] - pa[2]}
	v := [3]float64{pc[0] - pa[0], pc[1] - pa[1], pc[2] - pa[2]}
	cr := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
	if cr[0]*n[0]+cr[1]*n[1]+cr[2]*n[2] < 0 {
		b, c = c, b
	}
	p.Indices = append(p.Indices, a, b, c)
}

// samePassM: a later pass of the route this close to an earlier one, and
// parallel to it (either way), rides the same road.
const samePassM = 1.5

// markPassesAgain marks where the route rides a road a second time (a loop
// back through a village, out and home on the same street): the road is
// drawn once, by the first pass, so two slightly different copies of it
// don't fight over the same place. ls are the route's lines, in order;
// a loop's end meets its start, the same pass.
func markPassesAgain(ls []*roadLine) {
	const cellM = 4.0
	type ref struct{ l, i int }
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	dir := func(ss []sample, i int) (float64, float64) {
		a, b := ss[max(0, i-1)], ss[min(len(ss)-1, i+1)]
		l := math.Hypot(b.e-a.e, b.n-a.n)
		if l == 0 {
			return 0, 0
		}
		return (b.e - a.e) / l, (b.n - a.n) / l
	}
	for li, l := range ls {
		for i := range l.samples {
			s := &l.samples[i]
			k := key(s.e, s.n)
			de, dn := dir(l.samples, i)
			for gx := k[0] - 1; gx <= k[0]+1 && !s.again; gx++ {
				for gy := k[1] - 1; gy <= k[1]+1 && !s.again; gy++ {
					for _, r := range grid[[2]int{gx, gy}] {
						os := ls[r.l].samples
						o := os[r.i]
						sep := s.d - o.d
						if l.loop && r.l == li {
							sep = min(sep, l.samples[len(l.samples)-1].d-sep) // round the loop: its end is its start
						}
						if sep < otherPassM || math.Hypot(o.e-s.e, o.n-s.n) > samePassM {
							continue
						}
						oe, on := dir(os, r.i)
						if math.Abs(de*oe+dn*on) > math.Cos(30*math.Pi/180) {
							s.again = true
							break
						}
					}
				}
			}
			grid[k] = append(grid[k], ref{li, i})
		}
	}
}
