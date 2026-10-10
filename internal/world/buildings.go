package world

import (
	"math"
	"strconv"
	"strings"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Buildings from their footprints on the map: walls from just below the
// lowest ground under them up to the eaves (a plinth shows on a slope),
// and a roof: gabled along the long side where the footprint is nearly a
// rectangle, hipped to its middle where it is otherwise convex, flat for
// flats and for shapes a pitched roof doesn't fit. Colours per building
// from its map tags, else picked from a palette by its ID, so the same
// house looks the same in every build.
const (
	plinthM      = 0.3  // walls start this far below the lowest ground
	overhangM    = 0.3  // a pitched roof reaches this far past the walls
	rectangular  = 0.85 // a footprint this close to its rectangle gets a gable
	maxHipPoints = 12   // more corners than this: a flat roof
)

var (
	brickWalls = []uint32{0x8e4a35, 0x8e4a35, 0x6e3a2c, 0x7a5a44, 0x9c5a3c, 0xb8a074, 0xd8d4c8}
	flatWalls  = []uint32{0x9a9890, 0xb4ad9c, 0x8e4a35, 0x7f7a70, 0xc8c2b4}
	barnWalls  = []uint32{0x4a4038, 0x3c463e, 0x6f6a60, 0x5a4a3a}
	tileRoofs  = []uint32{0x3c3c3e, 0x3c3c3e, 0x8a3e2c, 0x26282a, 0xa0522d, 0x5a3a30}
	flatRoofs  = []uint32{0x4a4a4c, 0x5a5856, 0x3e3e40}
	barnRoofs  = []uint32{0x5a5c5a, 0x3a3c3a, 0x6a5a4a}
	namedColor = map[string]uint32{
		"red": 0x8e3a2c, "brown": 0x6e4a34, "white": 0xe0dcd0, "grey": 0x8c8c8c, "gray": 0x8c8c8c,
		"black": 0x2a2a2a, "yellow": 0xc8b070, "beige": 0xc8b896, "orange": 0xb05a2c, "green": 0x3c5a3c, "blue": 0x3a4a6a,
	}
)

// buildingMesh adds one building to p (vertex colours on a white
// material).
func buildingMesh(p *gltf.Primitive, s *surface, f scenery.Footprint) {
	pts := f.Outline
	n := len(pts)
	if n < 3 {
		return
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, q := range pts {
		g := s.at(q[0], q[1])
		lo, hi = math.Min(lo, g), math.Max(hi, g)
	}
	base, eave := lo-plinthM, hi+f.WallM
	rng := splitmix(uint64(f.ID))
	wall, roof := colours(f, rng)

	add := func(c [3]float32, n [3]float64, v ...[3]float64) {
		first := uint32(len(p.Positions) / 3)
		for _, q := range v {
			p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(-q[2]))
			p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(-n[2]))
			p.Colors = append(p.Colors, c[0], c[1], c[2])
		}
		g := [3]float64{n[0], n[1], -n[2]}
		for k := 1; k+1 < len(v); k++ {
			tri(p, first, first+uint32(k), first+uint32(k+1), g)
		}
	}
	// Positions as east, up, north; add turns them into glTF's axes.
	pt := func(q [2]float64, y float64) [3]float64 { return [3]float64{q[0], y, q[1]} }

	// Walls, facing out (the outline runs counter-clockwise).
	for i := range n {
		a, b := pts[i], pts[(i+1)%n]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		if l < 0.01 {
			continue
		}
		out := [3]float64{dy / l, 0, -dx / l}
		add(wall, out, pt(a, base), pt(b, base), pt(b, eave), pt(a, eave))
	}

	up := [3]float64{0, 1, 0}
	flat := func() {
		for _, t := range earClip(pts) {
			add(roof, up, pt(pts[t[0]], eave), pt(pts[t[1]], eave), pt(pts[t[2]], eave))
		}
	}
	if f.RoofM <= 0 {
		flat()
		return
	}
	area := polyArea(pts)
	rect, ax, ay := minRect(pts)
	switch {
	case area >= rectangular*rect.area && n <= 8:
		gable(add, pt, rect, ax, ay, eave, f.RoofM, wall, roof)
	case convex(pts) && n <= maxHipPoints:
		var cx, cy float64
		for _, q := range pts {
			cx, cy = cx+q[0]/float64(n), cy+q[1]/float64(n)
		}
		apex := [3]float64{cx, eave + f.RoofM*0.8, cy}
		for i := range n {
			a, b := pts[i], pts[(i+1)%n]
			add(roof, faceNormal(pt(a, eave), pt(b, eave), apex), pt(a, eave), pt(b, eave), apex)
		}
	default:
		flat()
	}
}

// gable roofs the rectangle r (axis ax, ay along its long side) with a
// ridge along its middle, the gable ends filled.
func gable(add func([3]float32, [3]float64, ...[3]float64), pt func([2]float64, float64) [3]float64,
	r rectangle, ax, ay, eave, roofM float64, wall, roof [3]float32) {
	bx, by := -ay, ax // across
	o := overhangM
	corner := func(u, v float64) [2]float64 {
		return [2]float64{r.cx + ax*u + bx*v, r.cy + ay*u + by*v}
	}
	hl, hw := r.long/2, r.short/2
	ridge := eave + roofM
	// The two slopes, out past the walls.
	for _, side := range []float64{-1, 1} {
		e0, e1 := corner(-hl-o, side*(hw+o)), corner(hl+o, side*(hw+o))
		r0, r1 := corner(-hl-o, 0), corner(hl+o, 0)
		drop := roofM * o / hw // the overhang continues the slope
		v := []([3]float64){pt(e0, eave-drop), pt(e1, eave-drop), pt(r1, ridge), pt(r0, ridge)}
		add(roof, faceNormal(v[0], v[1], v[2]), v...)
	}
	// The gable ends: triangles of wall up to the ridge.
	for _, end := range []float64{-1, 1} {
		a, b, top := corner(end*hl, -hw), corner(end*hl, hw), corner(end*hl, 0)
		v := []([3]float64){pt(a, eave), pt(b, eave), pt(top, ridge)}
		n := faceNormal(v[0], v[1], v[2])
		if n[0]*ax*end+n[2]*ay*end < 0 { // face out of the house
			v[0], v[1] = v[1], v[0]
			n = faceNormal(v[0], v[1], v[2])
		}
		add(wall, n, v...)
	}
}

// faceNormal is the upward-or-outward normal of a triangle given as east,
// up, north points, counter-clockwise seen from outside.
func faceNormal(a, b, c [3]float64) [3]float64 {
	// In east, up, north the handedness flips: cross b-a by c-a, negated.
	u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
	v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
	n := [3]float64{-(u[1]*v[2] - u[2]*v[1]), -(u[2]*v[0] - u[0]*v[2]), -(u[0]*v[1] - u[1]*v[0])}
	if n[1] < 0 {
		n = [3]float64{-n[0], -n[1], -n[2]}
	}
	l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
	if l == 0 {
		return [3]float64{0, 1, 0}
	}
	return [3]float64{n[0] / l, n[1] / l, n[2] / l}
}

type rectangle struct{ cx, cy, long, short, area float64 }

// minRect is the smallest rectangle around pts with a side along one of
// their edges, and the direction of its long side.
func minRect(pts [][2]float64) (rectangle, float64, float64) {
	best, bax, bay := rectangle{area: math.Inf(1)}, 1.0, 0.0
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		if l < 0.01 {
			continue
		}
		ux, uy := dx/l, dy/l
		u0, u1, v0, v1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for _, q := range pts {
			u, v := q[0]*ux+q[1]*uy, -q[0]*uy+q[1]*ux
			u0, u1, v0, v1 = math.Min(u0, u), math.Max(u1, u), math.Min(v0, v), math.Max(v1, v)
		}
		if area := (u1 - u0) * (v1 - v0); area < best.area {
			cu, cv := (u0+u1)/2, (v0+v1)/2
			r := rectangle{cx: cu*ux - cv*uy, cy: cu*uy + cv*ux, long: u1 - u0, short: v1 - v0, area: area}
			ax, ay := ux, uy
			if r.short > r.long {
				r.long, r.short = r.short, r.long
				ax, ay = -uy, ux
			}
			best, bax, bay = r, ax, ay
		}
	}
	return best, bax, bay
}

func polyArea(pts [][2]float64) float64 {
	a := 0.0
	for i := range pts {
		j := (i + 1) % len(pts)
		a += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
	}
	return math.Abs(a) / 2
}

func convex(pts [][2]float64) bool {
	sign := 0.0
	for i := range pts {
		a, b, c := pts[i], pts[(i+1)%len(pts)], pts[(i+2)%len(pts)]
		cr := (b[0]-a[0])*(c[1]-b[1]) - (b[1]-a[1])*(c[0]-b[0])
		if math.Abs(cr) < 1e-9 {
			continue
		}
		if sign == 0 {
			sign = cr
		} else if sign*cr < 0 {
			return false
		}
	}
	return true
}

// colours are a building's walls and roof (linear RGB): from the map's
// tags, else from the palette for its kind, with a little variation.
func colours(f scenery.Footprint, rng uint64) (wall, roof [3]float32) {
	walls, roofs := brickWalls, tileRoofs
	switch f.Kind {
	case scenery.KindFlat:
		walls, roofs = flatWalls, flatRoofs
	case scenery.KindBarn:
		walls, roofs = barnWalls, barnRoofs
	}
	if f.RoofM <= 0 {
		roofs = flatRoofs
	}
	pick := func(tag string, from []uint32, salt uint64) [3]float32 {
		if c, ok := parseColour(tag); ok {
			return srgbLinear(c)
		}
		h := splitmix(rng + salt)
		c := srgbLinear(from[h%uint64(len(from))])
		k := float32(0.9 + 0.2*float64(h>>32%1000)/1000) // ±10 % in brightness
		return [3]float32{c[0] * k, c[1] * k, c[2] * k}
	}
	return pick(f.WallColour, walls, 1), pick(f.RoofColour, roofs, 2)
}

// parseColour reads an OpenStreetMap colour: #rrggbb, #rgb or a name.
func parseColour(s string) (uint32, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := namedColor[s]; ok {
		return c, true
	}
	if h, ok := strings.CutPrefix(s, "#"); ok {
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) == 6 {
			if v, err := strconv.ParseUint(h, 16, 32); err == nil {
				return uint32(v), true
			}
		}
	}
	return 0, false
}

// splitmix is a small hash: the same input, the same output, every build.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ x>>30) * 0xbf58476d1ce4e5b9
	x = (x ^ x>>27) * 0x94d049bb133111eb
	return x ^ x>>31
}
