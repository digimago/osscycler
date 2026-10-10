package scenery

import (
	"math"
	"strconv"
	"strings"
)

// Surface is what a road is made of, as far as it shows.
type Surface uint8

const (
	SurfaceAsphalt Surface = iota // also paved roads that don't say
	SurfaceConcrete
	SurfacePaving  // bricks, setts, paving stones: Dutch klinkers
	SurfaceUnpaved // gravel, compacted, dirt, sand
)

// surfaceOf reads OpenStreetMap's surface tag; untagged, the road's class
// decides (every class fetched is paved unless it says otherwise).
func surfaceOf(surface, _ string) Surface {
	switch surface {
	case "concrete", "concrete:plates", "concrete:lanes":
		return SurfaceConcrete
	case "paving_stones", "sett", "cobblestone", "unhewn_cobblestone", "bricks", "brick", "paving_stones:30", "grass_paver":
		return SurfacePaving
	case "unpaved", "gravel", "fine_gravel", "compacted", "dirt", "earth", "ground", "sand", "pebblestone", "grass", "mud", "woodchips", "rock":
		return SurfaceUnpaved
	}
	return SurfaceAsphalt
}

// RoadStretch is a stretch of the route on one road, as the 3D world
// builds it.
type RoadStretch struct {
	FromM, ToM float64
	// Class is the OpenStreetMap highway class; "" where the route rides
	// on no road of the map (a cycle path, a road the map lacks).
	Name, Class string
	WidthM      float64 // the carriageway
	Surface     Surface
	// Sidewalk and CycleLane by side of the route as ridden: [0] left,
	// [1] right.
	Sidewalk, CycleLane [2]bool
	// The road on an embankment or in a cutting; on a bridge or in a
	// tunnel.
	Embankment, Cutting, Bridge, Tunnel bool
}

const (
	pathWidthM = 3.5 // a road the map lacks: most likely a cycle path
	// sideSmooth: the land use that gives an untagged street sidewalks is
	// taken by majority over this many samples each side, so a garden
	// between two houses doesn't break a pavement.
	sideSmooth = 3
	// cornerSamples: a gap this short between roads is a corner.
	cornerSamples = 3
)

// routeRoads describes what the route rides on, stretch by stretch, from
// the road it is on at each point (own). Sidewalks the map doesn't tag
// come from the land use: a street through a built-up area has them.
// builtAt says whether the land on a side (0 left, 1 right) is built up at
// a distance along the course.
func routeRoads(r []routePt, roads []*road, own []int, builtAt func(d float64, side int) bool) []RoadStretch {
	n := len(r)
	if n < 2 {
		return nil
	}
	own = bridgeCorners(own)

	// The land use on each side, smoothed.
	built := make([][2]bool, n)
	for side := range 2 {
		raw := make([]bool, n)
		for i := range r {
			raw[i] = builtAt(r[i].d, side)
		}
		for i := range r {
			votes, all := 0, 0
			for k := max(0, i-sideSmooth); k <= min(n-1, i+sideSmooth); k++ {
				all++
				if raw[k] {
					votes++
				}
			}
			built[i][side] = 2*votes > all
		}
	}

	at := func(i int) RoadStretch {
		k := own[i]
		if k < 0 {
			return RoadStretch{WidthM: pathWidthM}
		}
		rd := roads[k]
		t := rd.tags
		s := RoadStretch{
			Name: rd.name, Class: rd.class, WidthM: rd.widthM, Surface: surfaceOf(t["surface"], rd.class),
			Embankment: yes(t["embankment"]), Cutting: yes(t["cutting"]),
			Bridge: t["bridge"] != "" && t["bridge"] != "no", Tunnel: t["tunnel"] != "" && t["tunnel"] != "no",
		}
		walkL, walkR, tagged := sides(t, "sidewalk", "yes", "separate", "both")
		if !tagged && sidewalkClass(rd.class) && walkersOn(t) {
			walkL, walkR = built[i][0], built[i][1]
		}
		laneL, laneR, _ := sides(t, "cycleway", "lane", "opposite_lane")
		// OpenStreetMap's left and right follow the way's direction.
		if !forward(r, i, rd) {
			walkL, walkR, laneL, laneR = walkR, walkL, laneR, laneL
		}
		s.Sidewalk, s.CycleLane = [2]bool{walkL, walkR}, [2]bool{laneL, laneR}
		return s
	}

	var out []RoadStretch
	for i := range r {
		s := at(i)
		if len(out) > 0 && sameStretch(out[len(out)-1], s) {
			out[len(out)-1].ToM = r[i].d
			continue
		}
		s.FromM, s.ToM = r[i].d, r[i].d
		if len(out) > 0 {
			s.FromM = out[len(out)-1].ToM // stretches meet
		}
		out = append(out, s)
	}
	out[len(out)-1].ToM = r[n-1].d
	return out
}

func sameStretch(a, b RoadStretch) bool {
	a.FromM, a.ToM, b.FromM, b.ToM = 0, 0, 0, 0
	return a == b
}

func yes(v string) bool { return v != "" && v != "no" }

// walkersOn tells whether pedestrians may walk along a road: not with
// foot=no, nor use_sidepath (their own path beside it, mapped apart).
func walkersOn(t map[string]string) bool {
	return t["foot"] != "no" && t["foot"] != "use_sidepath"
}

// sidewalkClass: streets of these classes have sidewalks in built-up
// areas unless tagged otherwise; service roads, woonerven (living
// streets) and motorways don't.
func sidewalkClass(class string) bool {
	switch class {
	case "residential", "unclassified", "tertiary", "secondary", "primary", "trunk", "road":
		return true
	}
	return false
}

// sides reads OpenStreetMap's side tags for key (sidewalk, cycleway): the
// key itself (both, left, right, no, or a value for both sides), then
// key:both, key:left and key:right. A side counts when its value is one
// of on. tagged is false when the road says nothing.
func sides(t map[string]string, key string, on ...string) (left, right, tagged bool) {
	is := func(v string) bool {
		for _, o := range on {
			if v == o {
				return true
			}
		}
		return false
	}
	switch v := t[key]; v {
	case "":
	case "both":
		left, right, tagged = true, true, true
	case "left":
		left, tagged = true, true
	case "right":
		right, tagged = true, true
	default:
		left, right, tagged = is(v), is(v), true
	}
	if v, ok := t[key+":both"]; ok {
		left, right, tagged = is(v), is(v), true
	}
	if v, ok := t[key+":left"]; ok {
		left, tagged = is(v), true
	}
	if v, ok := t[key+":right"]; ok {
		right, tagged = is(v), true
	}
	return left, right, tagged
}

// forward tells whether the route at r[i] runs the way rd is drawn.
func forward(r []routePt, i int, rd *road) bool {
	a, b := r[max(0, i-1)], r[min(len(r)-1, i+1)]
	dx, dy := b.x-a.x, b.y-a.y
	best, dot := math.Inf(1), 1.0
	for j := 0; j+1 < len(rd.x); j++ {
		dist, _ := segDist(r[i].x, r[i].y, rd.x[j], rd.y[j], rd.x[j+1], rd.y[j+1])
		if dist < best {
			best, dot = dist, dx*(rd.x[j+1]-rd.x[j])+dy*(rd.y[j+1]-rd.y[j])
		}
	}
	return dot >= 0
}

// bridgeCorners: a few metres on no road between roads is a corner, where
// the route cuts across: it stays on the road before it.
func bridgeCorners(own []int) []int {
	own = append([]int(nil), own...)
	n := len(own)
	for i := 1; i < n; i++ {
		if own[i] >= 0 || own[i-1] < 0 {
			continue
		}
		j := i
		for j < n && own[j] < 0 {
			j++
		}
		if j < n && j-i <= cornerSamples {
			for k := i; k < j; k++ {
				own[k] = own[i-1]
			}
		}
	}
	return own
}

// Way is a road of the map near the route, for the 3D world to draw: its
// line and what it is like, by its own direction (Sidewalk and CycleLane
// [0] left, [1] right of the way as drawn).
type Way struct {
	ID int64
	// Line in metres east and north of the course's start; Nodes are the
	// map's node IDs of its points (ways meet where they share one).
	Line                                [][2]float64
	Nodes                               []int64
	Name, Class                         string
	WidthM                              float64
	Surface                             Surface
	Sidewalk, CycleLane                 [2]bool
	Bridge, Tunnel, Embankment, Cutting bool
	Motorway                            bool
	// Oneway: 1 traffic goes the way's own direction only, -1 against it
	// only, 0 both ways (roundabouts and motorways are one-way).
	Oneway int
	// Roundabout: part of a roundabout's ring; Lanes, its lanes if tagged.
	Roundabout bool
	Lanes      int
	// Sidepath: cyclists must take a path beside it (bicycle=no or
	// use_sidepath), so a route on a cycle path nearby rides its sidepath.
	Sidepath bool
	// Driveway: a service road to a house or yard (not drawn).
	Driveway bool
}

// ways are the roads as Ways (in the same order, so RouteOn indexes
// them). Untagged streets get sidewalks on a side where the land beside
// them is mostly built up, unless pedestrians may not walk on the road
// (foot=no or use_sidepath: a main road whose walkers have their own path)
// or another road runs alongside on that side (a divide, not a pavement:
// the Arnhemsestraatweg in Rheden and its service roads).
func ways(roads []*road, built func(x, y float64) bool) []Way {
	near := newSegGrid(roads, 20)
	out := make([]Way, len(roads))
	for k, rd := range roads {
		t := rd.tags
		w := Way{
			ID: rd.id, Nodes: rd.nodes, Name: rd.name, Class: rd.class, WidthM: rd.widthM,
			Surface: surfaceOf(t["surface"], rd.class), Motorway: rd.motorway,
			Bridge: t["bridge"] != "" && t["bridge"] != "no", Tunnel: t["tunnel"] != "" && t["tunnel"] != "no",
			Embankment: yes(t["embankment"]), Cutting: yes(t["cutting"]),
			Driveway:   t["service"] == "driveway",
			Oneway:     oneway(t, rd.motorway),
			Roundabout: t["junction"] == "roundabout" || t["junction"] == "circular",
			Sidepath:   t["bicycle"] == "no" || t["bicycle"] == "use_sidepath",
			Lanes:      atoi(t["lanes"]),
		}
		for i := range rd.x {
			w.Line = append(w.Line, [2]float64{rd.x[i], rd.y[i]})
		}
		walkL, walkR, tagged := sides(t, "sidewalk", "yes", "separate", "both")
		if !tagged && sidewalkClass(rd.class) && walkersOn(t) {
			walkL, walkR = builtBeside(rd, func(x, y float64) bool {
				return built(x, y) && !near.onRoad(x, y, rd)
			})
		}
		laneL, laneR, _ := sides(t, "cycleway", "lane", "opposite_lane")
		w.Sidewalk, w.CycleLane = [2]bool{walkL, walkR}, [2]bool{laneL, laneR}
		out[k] = w
	}
	return out
}

// builtBeside says whether the land on each side of a road (left, right
// of its direction) is mostly built up, from points every 10 m along it.
func builtBeside(rd *road, built func(x, y float64) bool) (left, right bool) {
	var votes, all [2]int
	for j := 0; j+1 < len(rd.x); j++ {
		dx, dy := rd.x[j+1]-rd.x[j], rd.y[j+1]-rd.y[j]
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		rx, ry := dy/l, -dx/l
		off := rd.widthM/2 + 4
		for f := 0.0; f < l; f += 10 {
			x, y := rd.x[j]+dx*f/l, rd.y[j]+dy*f/l
			for side, sign := range []float64{-1, 1} {
				all[side]++
				if built(x+sign*rx*off, y+sign*ry*off) {
					votes[side]++
				}
			}
		}
	}
	return 2*votes[0] > all[0], 2*votes[1] > all[1]
}

// segGrid finds the roads' segments near a point quickly.
type segGrid struct {
	cell  float64
	cells map[[2]int][]segRef
}

type segRef struct {
	rd *road
	j  int // segment j to j+1
}

func newSegGrid(roads []*road, cell float64) *segGrid {
	g := &segGrid{cell: cell, cells: map[[2]int][]segRef{}}
	for _, rd := range roads {
		for j := 0; j+1 < len(rd.x); j++ {
			x0, x1 := math.Min(rd.x[j], rd.x[j+1]), math.Max(rd.x[j], rd.x[j+1])
			y0, y1 := math.Min(rd.y[j], rd.y[j+1]), math.Max(rd.y[j], rd.y[j+1])
			for gx := int(math.Floor(x0 / cell)); gx <= int(math.Floor(x1/cell)); gx++ {
				for gy := int(math.Floor(y0 / cell)); gy <= int(math.Floor(y1/cell)); gy++ {
					g.cells[[2]int{gx, gy}] = append(g.cells[[2]int{gx, gy}], segRef{rd, j})
				}
			}
		}
	}
	return g
}

// onRoad tells whether x, y lies on another road than self: within half
// its width and a metre of its line.
func (g *segGrid) onRoad(x, y float64, self *road) bool {
	k := [2]int{int(math.Floor(x / g.cell)), int(math.Floor(y / g.cell))}
	for gx := k[0] - 1; gx <= k[0]+1; gx++ {
		for gy := k[1] - 1; gy <= k[1]+1; gy++ {
			for _, s := range g.cells[[2]int{gx, gy}] {
				if s.rd == self || s.rd.motorway {
					continue
				}
				ax, ay, bx, by := s.rd.x[s.j], s.rd.y[s.j], s.rd.x[s.j+1], s.rd.y[s.j+1]
				dx, dy := bx-ax, by-ay
				f := 0.0
				if l2 := dx*dx + dy*dy; l2 > 0 {
					f = math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/l2))
				}
				if math.Hypot(x-ax-f*dx, y-ay-f*dy) < s.rd.widthM/2+1 {
					return true
				}
			}
		}
	}
	return false
}

// oneway is a road's one-way direction from its tags: 1 its own way, -1
// against it, 0 both ways. Roundabouts and motorways are one-way unless
// tagged otherwise.
func oneway(t map[string]string, motorway bool) int {
	switch t["oneway"] {
	case "yes", "true", "1":
		return 1
	case "-1", "reverse":
		return -1
	case "no", "false", "0":
		return 0
	}
	if j := t["junction"]; j == "roundabout" || j == "circular" || motorway {
		return 1
	}
	return 0
}

// atoi is s as a whole number, 0 if it isn't one.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
