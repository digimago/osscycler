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
// toRoad, when set, is the unit direction (east, north) from a point to
// the nearest road, false with none near.
func buildingMesh(p *gltf.Primitive, s *surface, st *style, f scenery.Footprint, toRoad func(e, n float64) (float64, float64, bool)) {
	pts := f.Outline
	n := len(pts)
	if n < 3 {
		return
	}
	// A gabled house stands on the rectangle its roof is built on: walls
	// and gables share their edges in one plane (owner, 2026-10-16: a seam
	// showed between the floors and the gable where the outline left the
	// rectangle).
	rect, ax, ay := minRect(pts)
	gabled := f.RoofM > 0 && polyArea(pts) >= rectangular*rect.area && n <= 8
	if gabled {
		hl, hw := rect.long/2, rect.short/2
		bx, by := -ay, ax
		c := func(u, v float64) [2]float64 { return [2]float64{rect.cx + ax*u + bx*v, rect.cy + ay*u + by*v} }
		pts = [][2]float64{c(-hl, -hw), c(hl, -hw), c(hl, hw), c(-hl, hw)}
		n = 4
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, q := range pts {
		g := s.at(q[0], q[1])
		lo, hi = math.Min(lo, g), math.Max(hi, g)
	}
	base, eave := lo-plinthM, hi+f.WallM
	if f.FromLowest {
		eave = math.Max(lo+f.WallM, hi+2.2) // a storey at least on the uphill side
	}
	rng := splitmix(uint64(f.ID))
	wall, roof := colours(st, f, rng)

	// The windows' layout, per face (owner, 2026-10-16: windows cut at
	// corners, under the ground, through the roof line), laid out here and
	// drawn by the renderer (surfaces.gdshader, kind 5) from the UVs: u in
	// bays (window i centred at i + 0.5; only 0 ≤ u < count has windows;
	// cladding adds its offset), v the height above the face's storey
	// reference (rows of windows at winFloorM + storeyM·k); UV2 x the
	// count plus the highest a window's top may reach over 100, y the
	// ground over the reference at that point (a window's sill stays
	// winGroundM over it). A face without windows: count 0.
	pitch := winPitchMin + (winPitchMax-winPitchMin)*unit(splitmix(rng+17))
	var lay winLayout
	add := func(c [3]float32, n [3]float64, v ...[3]float64) {
		first := uint32(len(p.Positions) / 3)
		for _, q := range v {
			p.Positions = append(p.Positions, float32(q[0]), float32(q[1]), float32(-q[2]))
			p.Normals = append(p.Normals, float32(n[0]), float32(n[1]), float32(-n[2]))
			p.Colors = append(p.Colors, c[0], c[1], c[2])
			if lay.count > 0 || lay.clad > 0 {
				// Along the face: the projection on its base edge v0 → v1.
				ex, ez := v[1][0]-v[0][0], v[1][2]-v[0][2]
				x, l := 0.0, math.Hypot(ex, ez)
				if l > 0 {
					x = ((q[0]-v[0][0])*ex + (q[2]-v[0][2])*ez) / l
				}
				g := lay.groundA
				if l > 0 {
					g += (lay.groundB - lay.groundA) * x / l
				}
				p.UVs = append(p.UVs, float32((x-lay.x0)/pitch+lay.clad), float32(q[1]-lay.ref))
				p.UV2s = append(p.UV2s, float32(float64(lay.count)+lay.maxTop/100), float32(g-lay.ref))
			} else {
				p.UVs = append(p.UVs, 0, -1)
				p.UV2s = append(p.UV2s, 0, 0)
			}
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
		cnt := int(math.Floor((l - 2*winMarginM) / pitch))
		// The highest ground along the wall for the whole of it: a row of
		// windows is there or not, never cut where the slope crosses its
		// sills (owner, 2026-10-16: a side wall on a 3 m slope had half its
		// lower row, one window cut on the slant).
		gw := math.Max(s.at(a[0], a[1]), s.at(b[0], b[1]))
		for f := 0.25; f < 1; f += 0.25 {
			gw = math.Max(gw, s.at(a[0]+f*dx, a[1]+f*dy))
		}
		lay = winLayout{count: max(0, cnt), x0: (l - float64(max(0, cnt))*pitch) / 2, ref: lo,
			maxTop: eave - lo - winUnderEaveM, groundA: gw, groundB: gw}
		add(wall, out, pt(a, base), pt(b, base), pt(b, eave), pt(a, eave))
		lay = winLayout{}
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
	switch {
	case gabled:
		// A house on a slope in a style that turns it to the view: the
		// ridge down the fall line, a gable facing the valley (owner,
		// 2026-10-16, La Bresse's Rue du Hohneck), when it is square
		// enough for either way.
		if st.gableToView && rect.long < gableSquare*rect.short {
			hl, hw := rect.long/2, rect.short/2
			bx, by := -ay, ax
			along := math.Abs(s.at(rect.cx+ax*hl, rect.cy+ay*hl)-s.at(rect.cx-ax*hl, rect.cy-ay*hl)) / rect.long
			across := math.Abs(s.at(rect.cx+bx*hw, rect.cy+by*hw)-s.at(rect.cx-bx*hw, rect.cy-by*hw)) / rect.short
			if across > along && across > gableSlope {
				rect.long, rect.short = rect.short, rect.long
				ax, ay = bx, by
			}
		}
		// Each gable's cladding (owner, 2026-10-16): timber where it faces
		// the road with its windows; else mostly the walls' render, now
		// and then zinc squares or slate.
		cladOf := func(end float64) (float64, [3]float32) {
			if st.gableWood == 0 {
				return 0, wall
			}
			if toRoad != nil && rect.short >= 2.5 {
				if re, rn, ok := toRoad(rect.cx+ax*end*rect.long/2, rect.cy+ay*end*rect.long/2); ok && re*ax*end+rn*ay*end > 0.5 {
					return timberU, shadeHouse(srgbLinear(st.gableWood), rng)
				}
			}
			switch r := unit(splitmix(rng + 11)); {
			case r < 0.1:
				return zincU, shadeHouse(srgbLinear(0x8c9296), rng)
			case r < 0.2:
				return slateU, shadeHouse(srgbLinear(0x40434a), rng)
			}
			return 0, wall
		}
		gable(add, pt, rect, ax, ay, eave, f.RoofM, roof, func(end float64) [3]float32 {
			off, c := cladOf(end)
			// One row in the gable, its sill winGableSillM over the eave,
			// as many centred bays as fit under the slope with their
			// shutters (half a bay out from the window's middle).
			hw := rect.short / 2
			top := winGableSillM + winHeightM // over the eave
			k := 0
			for try := 1; try <= 6; try++ {
				outer := float64(try) * pitch / 2 // the outermost bay's edge from the middle
				if f.RoofM*(1-outer/hw)-0.2 < top {
					break
				}
				k = try
			}
			lay = winLayout{count: k, x0: hw - float64(k)*pitch/2, clad: off,
				// The row's sill at the storeys' first sill: winFloorM + the sill height.
				ref: eave + winGableSillM - (winFloorM + winSillM), maxTop: winFloorM + winSillM + winHeightM + 0.01,
				groundA: -100, groundB: -100}
			return c
		}, func() { lay = winLayout{} })
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
	r rectangle, ax, ay, eave, roofM float64, roof [3]float32, gableAt func(end float64) [3]float32, done func()) {
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
		c := gableAt(end) // its cladding; windows in it too
		add(c, n, v...)
		done()
	}
}

// A face's windows (buildingMesh's add): count bays from x0 (metres
// along the face), the storeys' reference height, how high a window's
// top may reach over it, the ground at either end of the face, its
// cladding's u offset.
type winLayout struct {
	count                  int
	x0, ref, maxTop        float64
	groundA, groundB, clad float64
}

// Window layout (the renderer's surfaces.gdshader keeps the same
// measures for a window in its bay and its storey).
const (
	winPitchMin, winPitchMax = 2.4, 2.9 // a bay's width, per house
	winMarginM               = 0.75     // the bays keep this far off a wall's ends: shutters a metre or more from a corner (owner, 2026-10-16)
	winFloorM                = 0.25     // the first storey's floor over the lowest ground
	winSillM                 = 0.9      // a sill over its floor
	winHeightM               = 1.3      // a window's height
	winUnderEaveM            = 0.15     // a window's top keeps this far under the eave
	winGableSillM            = 0.6      // the gable row's sill over the eave
)

// Added to a wall's u where it is clad (the renderer draws the cladding;
// surfaces.gdshader's kind 5): timber boards, zinc squares, slate.
const (
	timberU = 10000.0
	zincU   = 20000.0
	slateU  = 30000.0
)

const (
	gableSquare = 1.6  // a house no longer than this times its width may turn its gable to the view
	gableSlope  = 0.06 // on ground falling this steeply across it
)

// shadeHouse is c a little lighter or darker, by the house's seed.
func shadeHouse(c [3]float32, rng uint64) [3]float32 {
	k := float32(0.88 + 0.24*unit(splitmix(rng+3)))
	return [3]float32{c[0] * k, c[1] * k, c[2] * k}
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
func colours(st *style, f scenery.Footprint, rng uint64) (wall, roof [3]float32) {
	walls, roofs := st.walls, st.roofs
	switch f.Kind {
	case scenery.KindFlat:
		walls, roofs = st.flatWalls, flatRoofs
	case scenery.KindBarn:
		walls, roofs = st.barnWalls, st.barnRoofs
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
