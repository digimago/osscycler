package scenery

import (
	"math"
	"math/rand/v2"

	"github.com/digimago/osscycler/internal/course"
)

// Landscapes for osscycler's own test tracks (owner, 2026-10-09): they lie
// in open ocean, where the map has nothing, so their surroundings are made
// up here, as map data, and go through the same land use, buildings and
// road code as a real course's. The figure 8 has polder at its low end
// (fields in long parcels between ditches, farmsteads, rows of trees) and
// heath and forest at its high end, a hamlet where its roads cross; the
// oval is a stadium (a running track round a pitch, grandstands along
// the straights, car parks behind the bends). Seeded, so a track looks the
// same every time.

// Track is a test track's made-up surroundings.
type Track struct {
	Data *Data
	// Roads overrides the road the route rides on (nil: the default).
	Roads []RoadStretch
	// Fences are the polder's fences (dam fences where a ditch meets the
	// road).
	Fences []Fence
	// Lawns are mown grass, nothing grows on them (a pitch), in metres
	// east and north of the course's start (course.Project).
	Lawns [][][2]float64
}

// Fence is a fence along Line (east and north of the course's start):
// wooden posts and rails, with a gate from GateFromM to GateToM along it.
type Fence struct {
	Line               [][2]float64
	GateFromM, GateToM float64
}

// SurfaceTrack is a running track's synthetic surface.
const SurfaceTrack Surface = SurfaceUnpaved + 1

// TrackScenery is the made-up surroundings of a built-in test track, nil
// for any other course.
func TrackScenery(c *course.Course) *Track {
	if !c.Builtin {
		return nil
	}
	g := newTrackGen(c)
	switch c.ID {
	case "oval-400":
		return g.stadium()
	case "figure-8":
		return g.figureEight()
	}
	return nil
}

type trackGen struct {
	c      *course.Course
	rng    *rand.Rand
	id     int64
	data   *Data
	route  [][2]float64 // every 5 m, east and north of the start
	grid   map[[2]int][]int
	lo, hi [2]float64 // the route's bounds
	fences []Fence
	clearM float64 // buildings keep this far from the route's line
}

func newTrackGen(c *course.Course) *trackGen {
	g := &trackGen{c: c, rng: rand.New(rand.NewPCG(uint64(len(c.ID)), 2026)), data: &Data{}, grid: map[[2]int][]int{}, clearM: 12}
	g.lo, g.hi = [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for d := 0.0; d <= c.Distance; d += 5 {
		lat, lon := c.Position(d)
		e, n := c.Project(lat, lon)
		g.grid[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}] = append(g.grid[[2]int{int(math.Floor(e / 20)), int(math.Floor(n / 20))}], len(g.route))
		g.route = append(g.route, [2]float64{e, n})
		g.lo = [2]float64{math.Min(g.lo[0], e), math.Min(g.lo[1], n)}
		g.hi = [2]float64{math.Max(g.hi[0], e), math.Max(g.hi[1], n)}
	}
	return g
}

// at is the route's point at d, east and north.
func (g *trackGen) at(d float64) [2]float64 {
	lat, lon := g.c.Position(d)
	e, n := g.c.Project(lat, lon)
	return [2]float64{e, n}
}

// dist is the distance from p to the route, up to 60 m (further counts as
// 60).
func (g *trackGen) dist(p [2]float64) float64 {
	best := 60.0
	k := [2]int{int(math.Floor(p[0] / 20)), int(math.Floor(p[1] / 20))}
	for gx := k[0] - 3; gx <= k[0]+3; gx++ {
		for gy := k[1] - 3; gy <= k[1]+3; gy++ {
			for _, i := range g.grid[[2]int{gx, gy}] {
				best = math.Min(best, math.Hypot(g.route[i][0]-p[0], g.route[i][1]-p[1]))
			}
		}
	}
	return best
}

// area adds a closed way with tags round ring (east, north).
func (g *trackGen) area(ring [][2]float64, tags map[string]string) {
	g.id--
	el := Element{Type: "way", ID: g.id, Tags: tags}
	for _, q := range append(ring, ring[0]) {
		lat, lon := g.c.Unproject(q[0], q[1])
		el.Geometry = append(el.Geometry, LatLon{Lat: lat, Lon: lon})
		el.Nodes = append(el.Nodes, g.id*1000-int64(len(el.Nodes)))
	}
	el.Nodes[len(el.Nodes)-1] = el.Nodes[0]
	g.data.Elements = append(g.data.Elements, el)
}

// trackRect is a rectangle round centre c, length along heading h (radians
// from east) and width across.
func trackRect(c [2]float64, h, length, width float64) [][2]float64 {
	ux, uy := math.Cos(h)*length/2, math.Sin(h)*length/2
	vx, vy := -math.Sin(h)*width/2, math.Cos(h)*width/2
	return [][2]float64{
		{c[0] - ux - vx, c[1] - uy - vy}, {c[0] + ux - vx, c[1] + uy - vy},
		{c[0] + ux + vx, c[1] + uy + vy}, {c[0] - ux + vx, c[1] - uy + vy},
	}
}

// ellipse is an ellipse round c with radii rx, ry, wobbled a little.
func (g *trackGen) ellipse(c [2]float64, rx, ry float64) [][2]float64 {
	var out [][2]float64
	const n = 28
	for k := range n {
		a := 2 * math.Pi * float64(k) / n
		f := 0.9 + 0.2*g.rng.Float64()
		out = append(out, [2]float64{c[0] + rx*f*math.Cos(a), c[1] + ry*f*math.Sin(a)})
	}
	return out
}

func tags(kv ...string) map[string]string {
	t := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		t[kv[i]] = kv[i+1]
	}
	return t
}

// building adds a building, unless it would stand within clearM of the
// route.
func (g *trackGen) building(c [2]float64, h, length, width float64, kv ...string) bool {
	r := trackRect(c, h, length, width)
	for _, q := range append(r, c) {
		if g.dist(q) < g.clearM {
			return false
		}
	}
	g.area(r, tags(kv...))
	return true
}

// culvertM: a ditch stops this far from the road's line, where it passes
// under it (further, its two ends read as ponds beside the road).
const culvertM = 7.0

// figureEight: polder at the low east end, heath and forest at the high
// west end, a hamlet by the crossing.
func (g *trackGen) figureEight() *Track {
	const reach = 340.0 // past the route: the world's corridor and a bit
	mid := (g.lo[0] + g.hi[0]) / 2
	s, n := g.lo[1]-reach, g.hi[1]+reach

	// Polder: parcels 50 m wide and 250 m long, north to south, fields and
	// meadows, a ditch along each one's east side (broken where the road
	// crosses: culverts), now and then a row of trees.
	const pw, pl, ditch = 50.0, 250.0, 2.5
	for x := mid + 80; x < g.hi[0]+reach; x += pw {
		for y := s; y < n; y += pl {
			c := [2]float64{x + pw/2, y + pl/2}
			use := tags("landuse", "farmland")
			if g.rng.Float64() < 0.45 {
				use = tags("landuse", "meadow")
			}
			g.area(trackRect(c, math.Pi/2, pl-ditch, pw-ditch), use)
			// One water per run between the road's crossings: pieces each
			// got a bank at their seams, which showed as bars across.
			from := math.NaN()
			for yy := y; yy <= y+pl; yy += 5 {
				clear := yy < y+pl && g.dist([2]float64{x + pw - ditch/2, yy + 2.5}) > culvertM
				switch {
				case clear && math.IsNaN(from):
					from = yy
					if yy > y {
						g.damFence([2]float64{x + pw - ditch/2, yy})
					}
				case !clear && !math.IsNaN(from):
					if yy < y+pl {
						g.damFence([2]float64{x + pw - ditch/2, yy})
					}
					if yy-from >= 10 {
						g.area(trackRect([2]float64{x + pw - ditch/2, (from + yy) / 2}, math.Pi/2, yy-from, ditch),
							tags("natural", "water", "water", "ditch"))
					}
					from = math.NaN()
				}
			}
			if g.rng.Float64() < 0.12 {
				g.area(trackRect([2]float64{x + 4, y + pl/2}, math.Pi/2, pl-20, 6), tags("landuse", "forest", "leaf_type", "broadleaved"))
			}
		}
	}
	// Farmsteads beside the road on the polder: a house, a barn, a shed,
	// an orchard and trees round the yard.
	for _, d := range []float64{350, 750, 4550} {
		p := g.at(d)
		q := g.at(d + 5)
		h := math.Atan2(q[1]-p[1], q[0]-p[0])
		side := 1.0
		if d == 750 {
			side = -1
		}
		nx, ny := -math.Sin(h)*side, math.Cos(h)*side
		off := func(across, along float64) [2]float64 {
			return [2]float64{p[0] + nx*across + math.Cos(h)*along, p[1] + ny*across + math.Sin(h)*along}
		}
		g.area(trackRect(off(45, 0), h, 70, 50), tags("landuse", "farmyard"))
		g.building(off(25, -12), h, 13, 9, "building", "farmhouse", "building:levels", "1")
		g.building(off(45, 8), h, 32, 16, "building", "barn")
		g.building(off(30, 22), h, 10, 6, "building", "shed")
		g.area(trackRect(off(45, -45), h, 40, 40), tags("landuse", "orchard"))
		g.area(trackRect(off(80, 0), h, 70, 12), tags("landuse", "forest", "leaf_type", "broadleaved"))
	}

	// Heath and forest: forest everywhere west of the middle, in blocks of
	// pine, of oak and beech, and mixed; heath round the top of the climb
	// with a pond (a ven) and a few groups of pines on it.
	leaf := []string{"needleleaved", "broadleaved", "mixed", "needleleaved"}
	for k, y := 0, s; y < n; k, y = k+1, y+(n-s)/4 {
		g.area([][2]float64{{g.lo[0] - reach, y}, {mid - 80, y}, {mid - 80, y + (n-s)/4}, {g.lo[0] - reach, y + (n-s)/4}},
			tags("landuse", "forest", "leaf_type", leaf[k%len(leaf)]))
	}
	top := g.at(3050)
	g.area(g.ellipse(top, 330, 240), tags("natural", "heath"))
	// The ven beside the road, where a rider sees it, on the side away
	// from the loop's middle.
	p, q := g.at(2950), g.at(2955)
	h := math.Atan2(q[1]-p[1], q[0]-p[0])
	ven := [2]float64{p[0] - math.Sin(h)*55, p[1] + math.Cos(h)*55}
	if g.dist(ven) < 50 {
		ven = [2]float64{p[0] + math.Sin(h)*55, p[1] - math.Cos(h)*55}
	}
	g.area(g.ellipse(ven, 45, 30), tags("natural", "water"))
	for range 6 {
		p := [2]float64{top[0] + (g.rng.Float64()-0.5)*450, top[1] + (g.rng.Float64()-0.5)*320}
		if g.dist(p) > 25 {
			g.area(g.ellipse(p, 18, 14), tags("landuse", "forest", "leaf_type", "needleleaved"))
		}
	}

	// Between them, by the crossing: meadows and a hamlet.
	g.area([][2]float64{{mid - 80, s}, {mid + 80, s}, {mid + 80, n}, {mid - 80, n}}, tags("landuse", "meadow"))
	cross := [2]float64{mid, (g.lo[1] + g.hi[1]) / 2}
	for _, dy := range []float64{-1, 1} {
		c := [2]float64{cross[0], cross[1] + dy*110}
		g.area(trackRect(c, 0, 150, 90), tags("landuse", "residential"))
		for k := range 6 {
			p := [2]float64{c[0] - 60 + float64(k)*24, c[1] - dy*15}
			g.building(p, 0, 9+3*g.rng.Float64(), 8, "building", "house", "building:levels", "2")
			g.building([2]float64{p[0], p[1] + dy*30}, 0, 9+3*g.rng.Float64(), 8, "building", "house", "building:levels", "1")
		}
	}
	return &Track{Data: g.data, Fences: g.fences}
}

// damFence closes off a ditch's end by the road (owner, 2026-10-09: as on
// Dutch polder roads, a wooden fence over the ditch and a steel gate onto
// the meadow beside it), along the road.
func (g *trackGen) damFence(end [2]float64) {
	best, k := math.Inf(1), 0
	for i, q := range g.route {
		if d := math.Hypot(q[0]-end[0], q[1]-end[1]); d < best {
			best, k = d, i
		}
	}
	a, b := g.route[max(0, k-1)], g.route[min(len(g.route)-1, k+1)]
	l := math.Hypot(b[0]-a[0], b[1]-a[1])
	ux, uy := (b[0]-a[0])/l, (b[1]-a[1])/l
	side := 1.0
	if g.rng.Float64() < 0.5 {
		side = -1
	}
	at := func(f float64) [2]float64 { return [2]float64{end[0] + ux*f*side, end[1] + uy*f*side} }
	g.fences = append(g.fences, Fence{Line: [][2]float64{at(-3), at(7)}, GateFromM: 4, GateToM: 8})
}

// stadium: the oval as an athletics stadium.
func (g *trackGen) stadium() *Track {
	const width = 9.76 // eight lanes of 1.22 m
	g.clearM = width/2 + 3
	c := [2]float64{(g.lo[0] + g.hi[0]) / 2, (g.lo[1] + g.hi[1]) / 2}
	straight := g.hi[0] - g.lo[0] - (g.hi[1] - g.lo[1]) // the straights' length
	r := (g.hi[1] - g.lo[1]) / 2                        // the bends' radius
	t := &Track{Roads: []RoadStretch{{FromM: 0, ToM: g.c.Distance, Class: "track", WidthM: width, Surface: SurfaceTrack}}}
	// The pitch in the middle.
	pitch := trackRect(c, 0, math.Min(100, straight+2*(r-width/2)-8), math.Min(64, 2*(r-width/2)-4))
	// Mown grass inside the track and round it out to the stands and past
	// the bends: no trees or long grass in the bowl.
	t.Lawns = append(t.Lawns, trackRect(c, 0, straight+2*r+40, 2*(r+width/2+4)))
	g.area(pitch, tags("leisure", "pitch", "sport", "soccer"))
	// Grandstands along both straights, a covered main stand on the home
	// straight's side (south), an open one opposite; a clubhouse.
	for _, side := range []float64{-1, 1} {
		depth, height := 16.0, "12"
		if side > 0 {
			depth, height = 11, "7"
		}
		g.building([2]float64{c[0], c[1] + side*(r+width/2+4+depth/2)}, 0, straight+10, depth,
			"building", "grandstand", "height", height, "roof:shape", "flat", "building:colour", "#b9b7b0")
	}
	g.building([2]float64{c[0] + straight/2 + r + 30, c[1] - r - 10}, 0, 24, 12, "building", "sports_hall", "height", "6")
	// Car parks behind the bends, grass round the stadium, trees beyond.
	for _, side := range []float64{-1, 1} {
		pc := [2]float64{c[0] + side*(straight/2+r+45), c[1] + 20}
		g.area(trackRect(pc, math.Pi/2, 70, 50), tags("amenity", "parking", "access", "yes", "parking", "surface"))
	}
	g.area(trackRect(c, 0, straight+2*r+220, 2*r+190), tags("landuse", "recreation_ground"))
	for k := range 14 {
		a := 2 * math.Pi * float64(k) / 14
		p := [2]float64{c[0] + (straight/2+r+130)*math.Cos(a), c[1] + (r+110)*math.Sin(a)}
		g.area(g.ellipse(p, 30, 22), tags("landuse", "forest", "leaf_type", "broadleaved"))
	}
	t.Data = g.data
	return t
}
