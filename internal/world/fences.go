package world

import (
	"fmt"
	"math"
	"sort"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Fences: wooden posts every fencePostM with three rails, and steel
// five-bar gates hung between heavier posts; at a field entrance, the
// strip of bare earth from the road to the gate (scenery.Fence.Entry).
const (
	fencePostM = 2.0
	fenceH     = 1.25
)

// addFences adds the fences, a mesh per terrain chunk, standing on the
// ground.
func (w *World) addFences(t *terrain, fences []scenery.Fence) error {
	if len(fences) == 0 {
		return nil
	}
	mat := w.doc.AddMaterial(gltf.Material{Name: "fence", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.8})
	s := &surface{t: t, lists: map[[2]int]chunkLists{}}
	wood, post, steel := srgbLinear(0x8a7a62), srgbLinear(0x6e5f4a), srgbLinear(0xb4bcc2)
	byChunk := map[[2]int]*gltf.Primitive{}
	for _, f := range fences {
		if len(f.Line) < 2 {
			continue
		}
		a, b := f.Line[0], f.Line[len(f.Line)-1]
		k := [2]int{int(math.Floor((a[0] + b[0]) / 2 / t.o.ChunkM)), int(math.Floor((a[1] + b[1]) / 2 / t.o.ChunkM))}
		p := byChunk[k]
		if p == nil {
			p = &gltf.Primitive{Material: mat}
			byChunk[k] = p
		}
		if len(f.Entry) == 4 {
			entryStrip(p, s, f.Entry)
		}
		if f.Wire {
			wireFence(p, s, f, styleFor(t.o.Region).pasture, steel)
			continue
		}
		l := math.Hypot(b[0]-a[0], b[1]-a[1])
		ux, uy := (b[0]-a[0])/l, (b[1]-a[1])/l
		at := func(d, y float64) [3]float64 {
			e, n := a[0]+ux*d, a[1]+uy*d
			return [3]float64{e, s.at(e, n) + y, -n}
		}
		// Wooden stretches, either side of the gate.
		for _, part := range [][2]float64{{0, f.GateFromM}, {f.GateToM, l}} {
			if part[1]-part[0] < 0.5 {
				continue
			}
			steps := max(1, int(math.Ceil((part[1]-part[0])/fencePostM)))
			for i := 0; i <= steps; i++ {
				d := part[0] + (part[1]-part[0])*float64(i)/float64(steps)
				beam(p, at(d, -0.3), at(d, fenceH), 0.11, 0.11, post)
			}
			for _, y := range []float64{0.35, 0.7, 1.05} {
				beam(p, at(part[0], y), at(part[1], y), 0.03, 0.12, wood)
			}
		}
		// The gate: heavy posts, five bars and two braces.
		if f.GateToM > f.GateFromM {
			g0, g1 := f.GateFromM+0.1, f.GateToM-0.1
			beam(p, at(f.GateFromM, -0.3), at(f.GateFromM, fenceH+0.1), 0.16, 0.16, post)
			beam(p, at(f.GateToM, -0.3), at(f.GateToM, fenceH+0.1), 0.16, 0.16, post)
			for i := range 5 {
				y := 0.2 + 0.22*float64(i)
				beam(p, at(g0, y), at(g1, y), 0.04, 0.04, steel)
			}
			for _, d := range []float64{g0, g1} {
				beam(p, at(d, 0.15), at(d, 1.13), 0.05, 0.05, steel)
			}
			mid := (g0 + g1) / 2
			beam(p, at(g0, 0.2), at(mid, 1.08), 0.035, 0.035, steel)
			beam(p, at(g1, 0.2), at(mid, 1.08), 0.035, 0.035, steel)
		}
	}
	for _, k := range chunkOrder(byChunk) {
		p := byChunk[k]
		if err := w.add(fmt.Sprintf("fences %d %d", k[0], k[1]), map[string]any{"kind": "fences", "chunk": k}, *p); err != nil {
			return err
		}
	}
	return nil
}

// A pasture's wire fence (the style's pastureFence): wooden posts, strands
// of plain galvanised wire (barbed wire's barbs are too small to see from
// the road), a five-bar gate in its gap.
type pastureFence struct {
	postM, postH, postR float64 // posts this far apart, this tall and thick (half width)
	leanDeg             float64 // each leans up to this much, its own way (0: kept straight)
	strands             []float64
	colour              uint32
}

// A Dutch farmer's: posts every 3.5 m, three strands.
var keptFence = pastureFence{postM: 3.5, postH: 1.15, postR: 0.09, strands: []float64{0.45, 0.75, 1.05}, colour: 0x6e5f4a}

// wireFence draws fence f, posts and wire along its (bending) line.
func wireFence(p *gltf.Primitive, s *surface, f scenery.Fence, pf pastureFence, steel [3]float32) {
	wire, post := srgbLinear(0x7c8084), srgbLinear(pf.colour)
	l := polyLength(f.Line)
	at := func(d, y float64) [3]float64 {
		q := polyAt(f.Line, d)
		return [3]float64{q[0], s.at(q[0], q[1]) + y, -q[1]}
	}
	gate := f.GateToM > f.GateFromM
	inGate := func(d float64) bool { return gate && d > f.GateFromM-0.01 && d < f.GateToM+0.01 }
	// The posts: along the line, and at the gate's sides.
	var posts []float64
	steps := max(1, int(math.Ceil(l/pf.postM)))
	for i := 0; i <= steps; i++ {
		if d := l * float64(i) / float64(steps); !inGate(d) {
			posts = append(posts, d)
		}
	}
	if gate {
		posts = append(posts, f.GateFromM, f.GateToM)
		sort.Float64s(posts)
	}
	// A post's point y up it: leaning by its own seeded lean (none in a
	// kept fence), so the wire hangs from where the post is.
	tilt := func(d float64) (float64, float64) {
		if pf.leanDeg == 0 {
			return 0, 0
		}
		q := polyAt(f.Line, d)
		h := splitmix(uint64(int64(math.Round(q[0]*10)))<<32 ^ uint64(int64(math.Round(q[1]*10))) ^ 0x1ea7)
		lean := math.Tan(pf.leanDeg * math.Pi / 180 * unit(h))
		az := 2 * math.Pi * unit(h>>20)
		return lean * math.Cos(az), lean * math.Sin(az)
	}
	on := func(d, y float64) [3]float64 {
		q := at(d, y)
		tx, tz := tilt(d)
		return [3]float64{q[0] + tx*math.Max(0, y), q[1], q[2] + tz*math.Max(0, y)}
	}
	for _, d := range posts {
		beam(p, on(d, -0.3), on(d, pf.postH), pf.postR, pf.postR, post)
	}
	// The wire, either side of the gate: one piece as long as the posts it
	// passes stay within 10 cm of it (a straight, level stretch is one).
	span := func(from, to float64) {
		var ds []float64
		for _, d := range posts {
			if d >= from-0.01 && d <= to+0.01 {
				ds = append(ds, d)
			}
		}
		for i := 0; i+1 < len(ds); {
			j := i + 1
			for pf.leanDeg == 0 && j+1 < len(ds) && straight(at(ds[i], 0), at(ds[j+1], 0), ds[i:j+2], at) {
				j++
			}
			for _, y := range pf.strands {
				beam(p, on(ds[i], y), on(ds[j], y), 0.012, 0.012, wire)
			}
			i = j
		}
	}
	if gate {
		span(0, f.GateFromM)
		span(f.GateToM, l)
	} else {
		span(0, l)
	}
	if gate {
		g0, g1 := f.GateFromM+0.1, f.GateToM-0.1
		for i := range 5 {
			y := 0.2 + 0.22*float64(i)
			beam(p, at(g0, y), at(g1, y), 0.04, 0.04, steel)
		}
		for _, d := range []float64{g0, g1} {
			beam(p, at(d, 0.15), at(d, 1.13), 0.05, 0.05, steel)
		}
		mid := (g0 + g1) / 2
		beam(p, at(g0, 0.2), at(mid, 1.08), 0.035, 0.035, steel)
		beam(p, at(g1, 0.2), at(mid, 1.08), 0.035, 0.035, steel)
	}
}

// straight tells whether the posts at ds lie within 10 cm of the line
// from a to b (their foot, as at gives it).
func straight(a, b [3]float64, ds []float64, at func(d, y float64) [3]float64) bool {
	ab := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	l2 := dot3(ab, ab)
	for _, d := range ds {
		q := at(d, 0)
		aq := [3]float64{q[0] - a[0], q[1] - a[1], q[2] - a[2]}
		f := 0.0
		if l2 > 0 {
			f = math.Max(0, math.Min(1, dot3(aq, ab)/l2))
		}
		dx, dy, dz := aq[0]-ab[0]*f, aq[1]-ab[1]*f, aq[2]-ab[2]*f
		if dx*dx+dy*dy+dz*dz > 0.01 {
			return false
		}
	}
	return true
}

// entryLiftM: the strip lies this far over the land, as car parks did on
// the terrain mesh.
const entryLiftM = 0.04

// entryStrip lays a field entrance's dirt strip on the land: outline is
// its four corners (the road's end first, then the far end, as
// scenery.Fence.Entry), draped every metre. Across it, as on a farm track:
// two wheel ruts of darker earth with grass between them.
func entryStrip(p *gltf.Primitive, s *surface, outline [][2]float64) {
	near0, near1, far0 := outline[0], outline[1], outline[3]
	width := math.Hypot(near1[0]-near0[0], near1[1]-near0[1])
	length := math.Hypot(far0[0]-near0[0], far0[1]-near0[1])
	if width <= 0 || length <= 0 {
		return
	}
	dirt, rut, grass := srgbLinear(0x8f7a5a), srgbLinear(0x6e5d45), srgbLinear(0x75804a)
	// Bands across, in metres from the middle: dirt, rut, grass, rut, dirt.
	h := width / 2
	bands := []struct {
		from, to float64
		c        [3]float32
	}{{-h, -1.25, dirt}, {-1.25, -0.7, rut}, {-0.7, 0.7, grass}, {0.7, 1.25, rut}, {1.25, h, dirt}}
	// at is the point across x from the middle, along y from the road's
	// end, on the land.
	at := func(x, y float64) [3]float64 {
		fx, fy := (x+h)/width, y/length
		e := near0[0] + (near1[0]-near0[0])*fx + (far0[0]-near0[0])*fy
		n := near0[1] + (near1[1]-near0[1])*fx + (far0[1]-near0[1])*fy
		return [3]float64{e, s.at(e, n) + entryLiftM, -n}
	}
	up := [3]float64{0, 1, 0}
	steps := max(1, int(math.Ceil(length)))
	for _, b := range bands {
		if b.to-b.from < 0.05 {
			continue
		}
		for i := range steps {
			y0, y1 := length*float64(i)/float64(steps), length*float64(i+1)/float64(steps)
			quad(p, at(b.from, y0), at(b.to, y0), at(b.to, y1), at(b.from, y1), up, b.c)
		}
	}
}

// beam is a box from a to b (glTF axes), w wide and h deep across it.
func beam(p *gltf.Primitive, a, b [3]float64, w, h float64, c [3]float32) {
	dx, dy, dz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
	l := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if l == 0 {
		return
	}
	d := [3]float64{dx / l, dy / l, dz / l}
	// Two directions across: one horizontal, one up-ish.
	up := [3]float64{0, 1, 0}
	if math.Abs(d[1]) > 0.9 {
		up = [3]float64{1, 0, 0}
	}
	u := cross(d, up)
	u = scale3(u, 1/math.Sqrt(dot3(u, u)))
	v := cross(u, d)
	corners := [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	// Four sides, each its own quad (flat normals).
	for i := range 4 {
		c0, c1 := corners[i], corners[(i+1)%4]
		pt := func(end [3]float64, cc [2]float64) [3]float64 {
			return [3]float64{end[0] + u[0]*cc[0]*w/2 + v[0]*cc[1]*h/2, end[1] + u[1]*cc[0]*w/2 + v[1]*cc[1]*h/2, end[2] + u[2]*cc[0]*w/2 + v[2]*cc[1]*h/2}
		}
		mx, my := (c0[0]+c1[0])/2, (c0[1]+c1[1])/2
		n := [3]float64{u[0]*mx + v[0]*my, u[1]*mx + v[1]*my, u[2]*mx + v[2]*my}
		quad(p, pt(a, c0), pt(a, c1), pt(b, c1), pt(b, c0), n, c)
	}
	// The far end (a post's top).
	var q [4][3]float64
	for i, cc := range corners {
		q[i] = [3]float64{b[0] + u[0]*cc[0]*w/2 + v[0]*cc[1]*h/2, b[1] + u[1]*cc[0]*w/2 + v[1]*cc[1]*h/2, b[2] + u[2]*cc[0]*w/2 + v[2]*cc[1]*h/2}
	}
	quad(p, q[0], q[1], q[2], q[3], d, c)
}

// quad adds the quad a b c d facing n, in colour c.
func quad(p *gltf.Primitive, a, b, c, d, n [3]float64, col [3]float32) {
	base := uint32(len(p.Positions) / 3)
	for _, q := range [][3]float64{a, b, c, d} {
		p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(q[2]))
		p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(n[2]))
		p.Colors = append(p.Colors, col[0], col[1], col[2])
	}
	tri(p, base, base+1, base+2, n)
	tri(p, base, base+2, base+3, n)
}

func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func dot3(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func scale3(a [3]float64, k float64) [3]float64 { return [3]float64{a[0] * k, a[1] * k, a[2] * k} }

// fenceWallM: a pasture's fence keeps this far from a retaining wall.
const fenceWallM = 0.6

// fencesClearOfWalls is fences with the pastures' wire fences cut where
// they come within fenceWallM of a retaining wall's footprint (owner,
// 2026-10-18, 20.00 km of the Hohneck loop: placed 1.2 m off the road's
// edge, a fence ran through the wall standing 1 m off it, its wire along
// the masonry). A fence that is cut loses its gate.
func fencesClearOfWalls(fences []scenery.Fence, walls [][][2]float64) []scenery.Fence {
	if len(walls) == 0 {
		return fences
	}
	type box struct{ lo, hi [2]float64 }
	boxes := make([]box, len(walls))
	for i, w := range walls {
		b := box{[2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}}
		for _, q := range w {
			b.lo[0], b.lo[1] = math.Min(b.lo[0], q[0]-fenceWallM), math.Min(b.lo[1], q[1]-fenceWallM)
			b.hi[0], b.hi[1] = math.Max(b.hi[0], q[0]+fenceWallM), math.Max(b.hi[1], q[1]+fenceWallM)
		}
		boxes[i] = b
	}
	nearWall := func(q [2]float64) bool {
		for i, w := range walls {
			b := boxes[i]
			if q[0] < b.lo[0] || q[0] > b.hi[0] || q[1] < b.lo[1] || q[1] > b.hi[1] {
				continue
			}
			if inPoly(w, q[0], q[1]) || polyEdgeDist(w, q[0], q[1]) < fenceWallM {
				return true
			}
		}
		return false
	}
	var out []scenery.Fence
	for _, f := range fences {
		if !f.Wire {
			out = append(out, f)
			continue
		}
		// Points every metre along the line, so a short wall cuts it too.
		var pts [][2]float64
		for i := 0; i+1 < len(f.Line); i++ {
			a, b := f.Line[i], f.Line[i+1]
			n := max(1, int(math.Ceil(math.Hypot(b[0]-a[0], b[1]-a[1]))))
			for j := 0; j < n; j++ {
				u := float64(j) / float64(n)
				pts = append(pts, [2]float64{a[0] + u*(b[0]-a[0]), a[1] + u*(b[1]-a[1])})
			}
		}
		pts = append(pts, f.Line[len(f.Line)-1])
		cut := false
		var piece [][2]float64
		flush := func() {
			if len(piece) >= 2 {
				out = append(out, scenery.Fence{Line: piece, Wire: true})
			}
			piece = nil
		}
		for _, q := range pts {
			if nearWall(q) {
				cut = true
				flush()
				continue
			}
			piece = append(piece, q)
		}
		if !cut {
			out = append(out, f)
			continue
		}
		flush()
	}
	return out
}
