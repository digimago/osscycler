package scenery

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/digimago/osscycler/internal/course"
)

// What the road view shows of the human world along a route, sparingly:
// the junctions where the route changes roads or crosses a major road, the
// car parks beside it with their entrances, and the signs at the limits of
// the places it enters. Side streets, paths and crossings for walkers are
// left out on purpose.

// roadClasses are the OpenStreetMap highways the query asks for: roads a
// route rides on or meets, without paths, tracks and footways.
const roadClasses = "^(motorway|trunk|primary|secondary|tertiary|unclassified|residential|living_street|service|road)(_link)?$"

// JunctionKind says why a junction is shown.
type JunctionKind uint8

const (
	// JunctionTurn: the route leaves one road for another.
	JunctionTurn JunctionKind = iota + 1
	// JunctionCrossing: the route crosses a major road.
	JunctionCrossing
	// JunctionEntrance: the way into a car park beside the route.
	JunctionEntrance
)

// Branch is a road leaving a junction, other than the route itself.
type Branch struct {
	BearingDeg float64 // compass bearing from the junction, clockwise from north
	WidthM     float64
	LengthM    float64 // how far it runs before it ends or bends away (at most branchMaxM)
}

// Junction is a place where other roads meet the route.
type Junction struct {
	DistanceM float64
	Kind      JunctionKind
	Branches  []Branch
}

// Parking is a car park beside the route, as a box aligned with the road
// like Building.
type Parking struct {
	DistanceM, OffsetM, LengthM, DepthM float64
	Name                                string
}

// PlaceSign is a place-name sign where the route enters a built-up area
// (in the Netherlands white on blue), on the right of the road.
type PlaceSign struct {
	DistanceM float64
	Name      string
}

const (
	routeStepM   = 5.0  // the route is followed in steps this long
	ownRoadM     = 12.0 // the route rides on a road this close and parallel
	nodeNearM    = 4.0  // roads meet at a point this close to the junction
	branchProbeM = 20.0 // a branch's bearing is taken this far out
	branchMaxM   = 80.0 // branches are drawn this far at most
	sameWayDeg   = 30.0 // a branch this close to the route's own direction is the route
	mergeM       = 25.0 // junctions this close together are one
	turnDeg      = 30.0 // the route turning this much where another road meets it turns at a junction
	parkingNearM = 60.0 // car parks this close to the road are shown
	parkingMinM2 = 1000.0
	signNearM    = 15.0 // signs this close to the route stand by it
)

// road is a highway way in metres around the start.
type road struct {
	id           int64
	x, y         []float64
	nodes        []int64
	name, class  string
	major, layer bool // major road; on a bridge or in a tunnel
	motorway     bool
	widthM       float64
	// next lists the roads that go on from each end (first, last node).
	next [2][]*road
}

// link records which roads go on from which: OpenStreetMap splits a road
// wherever its tags change, often just past a junction.
func link(roads []*road) {
	ends := map[int64][]*road{}
	for _, rd := range roads {
		ends[rd.nodes[0]] = append(ends[rd.nodes[0]], rd)
		ends[rd.nodes[len(rd.nodes)-1]] = append(ends[rd.nodes[len(rd.nodes)-1]], rd)
	}
	for _, rd := range roads {
		for e, n := range []int64{rd.nodes[0], rd.nodes[len(rd.nodes)-1]} {
			for _, o := range ends[n] {
				if o != rd && sameRoad(o, rd) {
					rd.next[e] = append(rd.next[e], o)
				}
			}
		}
	}
}

// routePt is the route every routeStepM.
type routePt struct{ d, x, y float64 }

func roadsOf(c *course.Course, d *Data) []*road {
	var out []*road
	for _, el := range d.Elements {
		hw := el.Tags["highway"]
		if el.Type != "way" || hw == "" || len(el.Geometry) < 2 || len(el.Nodes) != len(el.Geometry) {
			continue
		}
		class := strings.TrimSuffix(hw, "_link")
		switch class {
		case "motorway", "trunk", "primary", "secondary", "tertiary", "unclassified", "residential", "living_street", "service", "road":
		default:
			continue
		}
		r := &road{id: el.ID, nodes: el.Nodes, name: el.Tags["name"], class: class, widthM: roadWidth(class, el.Tags)}
		// Motorways never meet a ride on the level: they are fetched for
		// the 3D world (viaducts, tunnels), never drawn as junctions.
		r.motorway = class == "motorway"
		r.major = class == "trunk" || class == "primary" || class == "secondary" || class == "tertiary"
		r.layer = el.Tags["bridge"] != "" && el.Tags["bridge"] != "no" || el.Tags["tunnel"] != "" && el.Tags["tunnel"] != "no"
		for _, p := range el.Geometry {
			x, y := c.Project(p.Lat, p.Lon)
			r.x, r.y = append(r.x, x), append(r.y, y)
		}
		out = append(out, r)
	}
	link(out)
	return out
}

// roadWidth is the tagged width, else a guess from the lanes and class.
func roadWidth(class string, t map[string]string) float64 {
	if w, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t["width"]), "m")), 64); err == nil && w >= 2 && w < 40 {
		return w
	}
	w := map[string]float64{"motorway": 11, "trunk": 9, "primary": 7.5, "secondary": 7, "tertiary": 6.5, "service": 4}[class]
	if w == 0 {
		w = 5
	}
	if lanes, err := strconv.Atoi(t["lanes"]); err == nil && lanes >= 2 {
		w = math.Max(w, 3.2*float64(lanes))
	}
	return w
}

func sampleRoute(c *course.Course) []routePt {
	n := int(math.Ceil(c.Distance / routeStepM))
	out := make([]routePt, n+1)
	for i := range out {
		d := c.Distance * float64(i) / float64(n)
		x, y := c.Project(c.Position(d))
		out[i] = routePt{d, x, y}
	}
	return out
}

// bearingAt is the route's compass bearing from a to b metres along it.
func bearingAt(r []routePt, a, b float64) float64 {
	pa, pb := pointAt(r, a), pointAt(r, b)
	return bearing(pa.x, pa.y, pb.x, pb.y)
}

func pointAt(r []routePt, d float64) routePt {
	i := int(math.Round(d / (r[len(r)-1].d / float64(len(r)-1))))
	return r[max(0, min(i, len(r)-1))]
}

func bearing(x0, y0, x1, y1 float64) float64 {
	b := math.Atan2(x1-x0, y1-y0) * 180 / math.Pi
	return math.Mod(b+360, 360)
}

func angleDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return math.Min(d, 360-d)
}

// segDist is the distance from p to the segment a-b, and how far along it
// (0..1) the nearest point lies.
func segDist(px, py, ax, ay, bx, by float64) (float64, float64) {
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l2 := dx*dx + dy*dy; l2 > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l2))
	}
	return math.Hypot(px-ax-t*dx, py-ay-t*dy), t
}

// nearestOnRoute is the distance along the route of the point nearest
// (x, y) within d0..d1, and how far off it is.
func nearestOnRoute(r []routePt, x, y, d0, d1 float64) (along, off float64) {
	off = math.Inf(1)
	for i := 0; i+1 < len(r); i++ {
		if r[i+1].d < d0 || r[i].d > d1 {
			continue
		}
		dist, t := segDist(x, y, r[i].x, r[i].y, r[i+1].x, r[i+1].y)
		if dist < off {
			off, along = dist, r[i].d+t*(r[i+1].d-r[i].d)
		}
	}
	return along, off
}

// ownRoads says which road the route rides on at each point, -1 where
// none fits (a path, or a road the map lacks).
func ownRoads(r []routePt, roads []*road) []int {
	own := make([]int, len(r))
	for i := range r {
		a, b := r[max(0, i-2)], r[min(len(r)-1, i+2)]
		dx, dy := b.x-a.x, b.y-a.y
		l := math.Hypot(dx, dy)
		own[i] = -1
		best := math.Inf(1)
		for k, rd := range roads {
			for j := 0; j+1 < len(rd.x); j++ {
				dist, _ := segDist(r[i].x, r[i].y, rd.x[j], rd.y[j], rd.x[j+1], rd.y[j+1])
				if dist > ownRoadM {
					continue
				}
				sx, sy := rd.x[j+1]-rd.x[j], rd.y[j+1]-rd.y[j]
				sl := math.Hypot(sx, sy)
				if l == 0 || sl == 0 || math.Abs(dx*sx+dy*sy)/(l*sl) < math.Cos(35*math.Pi/180) {
					continue
				}
				if rd.class == "service" {
					dist += 3 // driveways run beside many roads
				}
				if dist < best {
					best, own[i] = dist, k
				}
			}
		}
	}
	// A road for a step or two between stretches of another is noise.
	for i := 1; i+1 < len(own); i++ {
		for w := 1; w <= 3 && i+w < len(own); w++ {
			if own[i-1] != own[i] && own[i-1] == own[i+w] {
				for k := i; k < i+w; k++ {
					own[k] = own[i-1]
				}
				break
			}
		}
	}
	return own
}

// sameRoad tells whether two ways are one road to a rider: the same name,
// or both unnamed and of one class.
func sameRoad(a, b *road) bool {
	if a.name != "" || b.name != "" {
		return a.name == b.name
	}
	return a.class == b.class
}

// branchesAt lists the roads leaving the point (x, y), other than the
// route's own way in (from inBearing) and out (outBearing).
func branchesAt(roads []*road, x, y, inBearing, outBearing float64, keep func(*road) bool) []Branch {
	var out []Branch
	for _, rd := range roads {
		if !keep(rd) || rd.layer || rd.motorway {
			continue
		}
		k, kd := -1, nodeNearM
		for j := range rd.x {
			if dd := math.Hypot(rd.x[j]-x, rd.y[j]-y); dd < kd {
				k, kd = j, dd
			}
		}
		if k < 0 {
			continue
		}
		for _, dir := range []int{1, -1} {
			b, length, ok := walk(rd, k, dir)
			if !ok || angleDiff(b, inBearing+180) < sameWayDeg || angleDiff(b, outBearing) < sameWayDeg {
				continue
			}
			dup := false
			for _, o := range out {
				dup = dup || angleDiff(o.BearingDeg, b) < 15
			}
			if !dup {
				out = append(out, Branch{BearingDeg: b, WidthM: rd.widthM, LengthM: length})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BearingDeg < out[j].BearingDeg })
	return out
}

// walk follows rd from vertex k in direction dir, on into the pieces of
// the same road beyond its end: the bearing to the point branchProbeM out,
// and how far the road runs (up to branchMaxM) before ending or turning
// more than 30° away from that bearing.
func walk(rd *road, k, dir int) (b, length float64, ok bool) {
	x0, y0 := rd.x[k], rd.y[k]
	pts := trace(rd, k, dir, branchMaxM+20)
	if len(pts) < 2 {
		return 0, 0, false
	}
	var px, py float64
	for _, p := range pts[1:] {
		px, py = p[0], p[1]
		if math.Hypot(px-x0, py-y0) >= branchProbeM {
			break
		}
	}
	if math.Hypot(px-x0, py-y0) < 5 {
		return 0, 0, false
	}
	b = bearing(x0, y0, px, py)
	for _, p := range pts[1:] {
		dd := math.Hypot(p[0]-x0, p[1]-y0)
		if dd > 8 && angleDiff(bearing(x0, y0, p[0], p[1]), b) > 30 {
			break
		}
		length = dd
		if length >= branchMaxM {
			return b, branchMaxM, true
		}
	}
	return b, length, true
}

// trace lists the points of rd from vertex k in direction dir, going on
// into the next piece of the road at its end, until limit metres out.
func trace(rd *road, k, dir int, limit float64) [][2]float64 {
	x0, y0 := rd.x[k], rd.y[k]
	pts := [][2]float64{{x0, y0}}
	seen := map[*road]bool{rd: true}
	for {
		j := k
		for j += dir; j >= 0 && j < len(rd.x); j += dir {
			pts = append(pts, [2]float64{rd.x[j], rd.y[j]})
			if math.Hypot(rd.x[j]-x0, rd.y[j]-y0) > limit {
				return pts
			}
		}
		end := 0
		if dir > 0 {
			end = 1
		}
		var nxt *road
		for _, o := range rd.next[end] {
			if !seen[o] {
				nxt = o
				break
			}
		}
		if nxt == nil {
			return pts
		}
		seen[nxt] = true
		// Go on from the shared node, away from it.
		if nxt.nodes[0] == rd.nodes[(len(rd.nodes)-1)*end] {
			rd, k, dir = nxt, 0, 1
		} else {
			rd, k, dir = nxt, len(nxt.nodes)-1, -1
		}
	}
}

// junctions finds where the route changes roads and where it crosses a
// major road on the level.
func junctions(r []routePt, roads []*road, own []int) []Junction {
	var out []Junction
	add := func(j Junction) { out = addJunction(out, j) }
	notService := func(rd *road) bool { return rd.class != "service" }
	stepD := r[len(r)-1].d / float64(len(r)-1)

	// Changes of road: from one stretch to the next, skipping gaps.
	prev := -1
	for i := range own {
		cur := own[i]
		if cur < 0 {
			continue
		}
		if prev >= 0 && cur != prev && !sameRoad(roads[prev], roads[cur]) {
			x, y, ok := meetingPoint(roads[prev], roads[cur], r[i])
			if ok {
				d, off := nearestOnRoute(r, x, y, r[i].d-60, r[i].d+60)
				if off < ownRoadM {
					in, outB := bearingAt(r, d-15, d), bearingAt(r, d, d+15)
					add(Junction{DistanceM: d, Kind: JunctionTurn, Branches: branchesAt(roads, x, y, in, outB, notService)})
				}
			}
		}
		prev = cur
	}

	// Turns where the route keeps its road's name: it bends sharply where
	// another road meets it, as at a T-junction whose through road goes on
	// under another name (the Beekhuizenseweg at the Posbank turns right
	// off the line that goes on as the Schietbergseweg). A turn as much as
	// a change of road is.
	for k, rd := range roads {
		if rd.class == "service" || rd.layer || rd.motorway {
			continue
		}
		for j := range rd.x {
			d, off := nearestOnRoute(r, rd.x[j], rd.y[j], 0, math.Inf(1))
			if off > nodeNearM {
				continue
			}
			i := int(math.Round(d / stepD))
			if o := own[max(0, min(i, len(own)-1))]; o == k || o >= 0 && sameRoad(roads[o], rd) {
				continue // riding on it
			}
			in, outB := bearingAt(r, d-15, d), bearingAt(r, d, d+15)
			if angleDiff(in, outB) < turnDeg {
				continue // a side road along a straight: not shown
			}
			add(Junction{DistanceM: d, Kind: JunctionTurn, Branches: branchesAt(roads, rd.x[j], rd.y[j], in, outB, notService)})
		}
	}

	// Major roads crossed on the level: they share a node with the route's
	// own road where the route meets them.
	for k, rd := range roads {
		if !rd.major || rd.layer {
			continue
		}
		for j := range rd.x {
			d, off := nearestOnRoute(r, rd.x[j], rd.y[j], 0, math.Inf(1))
			if off > nodeNearM {
				continue
			}
			i := int(math.Round(d / stepD))
			if o := own[max(0, min(i, len(own)-1))]; o == k || o >= 0 && sameRoad(roads[o], rd) {
				continue // riding on it
			}
			in, outB := bearingAt(r, d-15, d), bearingAt(r, d, d+15)
			bs := branchesAt(roads, rd.x[j], rd.y[j], in, outB, func(o *road) bool { return o.major })
			left, right := false, false
			for _, b := range bs {
				rel := math.Mod(b.BearingDeg-outB+360, 360)
				left, right = left || rel > 180, right || rel < 180
			}
			if left && right { // across, not a road ending at ours
				add(Junction{DistanceM: d, Kind: JunctionCrossing, Branches: bs})
			}
		}
	}
	sortJunctions(out)
	return out
}

// addJunction adds j to js, merged into one already within mergeM.
func addJunction(js []Junction, j Junction) []Junction {
	if len(j.Branches) == 0 {
		return js
	}
	for i := range js {
		if math.Abs(js[i].DistanceM-j.DistanceM) < mergeM {
			for _, b := range j.Branches {
				dup := false
				for _, o := range js[i].Branches {
					dup = dup || angleDiff(o.BearingDeg, b.BearingDeg) < 15
				}
				if !dup {
					js[i].Branches = append(js[i].Branches, b)
				}
			}
			return js
		}
	}
	return append(js, j)
}

func sortJunctions(js []Junction) {
	sort.Slice(js, func(i, j int) bool { return js[i].DistanceM < js[j].DistanceM })
}

// meetingPoint is where two consecutive roads of the route meet: a node
// they share near p, else p itself.
func meetingPoint(a, b *road, p routePt) (x, y float64, ok bool) {
	best := math.Inf(1)
	for i, na := range a.nodes {
		for _, nb := range b.nodes {
			if na != nb {
				continue
			}
			if dd := math.Hypot(a.x[i]-p.x, a.y[i]-p.y); dd < best && dd < 80 {
				best, x, y = dd, a.x[i], a.y[i]
			}
		}
	}
	if math.IsInf(best, 1) {
		return p.x, p.y, true
	}
	return x, y, true
}

// parkings finds the public car parks beside the route, with the
// entrance from the route where a road leads into one.
func parkings(c *course.Course, d *Data, r []routePt, roads []*road, east, north []float64) ([]Parking, []Junction) {
	var lots []Parking
	var entrances []Junction
	for _, el := range d.Elements {
		t := el.Tags
		if t["amenity"] != "parking" {
			continue
		}
		switch t["parking"] {
		case "street_side", "lane", "on_street", "layby", "underground", "multi-storey", "rooftop", "sheds", "carports", "garage_boxes":
			continue
		}
		switch t["access"] {
		case "private", "no", "customers", "permit", "delivery", "residents":
			continue
		}
		ring := el.Geometry
		if el.Type == "relation" {
			ring = nil
			for _, m := range el.Members {
				if m.Role == "outer" && len(m.Geometry) > len(ring) {
					ring = m.Geometry
				}
			}
		}
		if len(ring) < 4 {
			continue
		}
		xs, ys := make([]float64, len(ring)), make([]float64, len(ring))
		var area, cx, cy float64
		for i, p := range ring {
			xs[i], ys[i] = c.Project(p.Lat, p.Lon)
		}
		for i := range xs {
			j := (i + 1) % len(xs)
			area += xs[i]*ys[j] - xs[j]*ys[i]
			cx, cy = cx+xs[i]/float64(len(xs)), cy+ys[i]/float64(len(xs))
		}
		area = math.Abs(area) / 2
		if area < parkingMinM2 {
			continue
		}
		nearest := math.Inf(1)
		for i := range xs {
			_, off := nearestOnRoute(r, xs[i], ys[i], 0, math.Inf(1))
			nearest = math.Min(nearest, off)
		}
		if nearest > parkingNearM {
			continue
		}
		// A box along the road, as for buildings.
		along, _ := nearestOnRoute(r, cx, cy, 0, math.Inf(1))
		i := max(0, min(int(math.Round(along/c.Spacing)), len(east)-1))
		dx, dy := direction(east, north, i)
		rx, ry := dy, -dx
		a0, a1, o0, o1 := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
		for k := range xs {
			px, py := xs[k]-east[i], ys[k]-north[i]
			a, o := px*dx+py*dy, px*rx+py*ry
			a0, a1, o0, o1 = math.Min(a0, a), math.Max(a1, a), math.Min(o0, o), math.Max(o1, o)
		}
		off, depth := (o0+o1)/2, o1-o0
		if clear := math.Abs(off) - depth/2; clear < buildingClearM {
			off = math.Copysign(buildingClearM+depth/2, off)
		}
		lot := Parking{DistanceM: float64(i)*c.Spacing + (a0+a1)/2, OffsetM: off, LengthM: a1 - a0, DepthM: depth, Name: t["name"]}
		lots = append(lots, lot)
		if j, ok := entrance(r, roads, xs, ys); ok {
			entrances = append(entrances, j)
		}
	}
	sort.Slice(lots, func(i, j int) bool { return lots[i].DistanceM < lots[j].DistanceM })
	return lots, entrances
}

// entrance is where a road from the route leads into the car park with
// the outline xs, ys: the road's point nearest the route, its branch
// pointing into the car park.
func entrance(r []routePt, roads []*road, xs, ys []float64) (Junction, bool) {
	inside := func(x, y float64) bool {
		in := false
		for i := range xs {
			j := (i + len(xs) - 1) % len(xs)
			if (ys[i] > y) != (ys[j] > y) && x < xs[i]+(y-ys[i])*(xs[j]-xs[i])/(ys[j]-ys[i]) {
				in = !in
			}
		}
		return in
	}
	best, found := Junction{}, false
	bestOff := math.Inf(1)
	for _, rd := range roads {
		if rd.class != "service" && rd.class != "unclassified" && rd.class != "residential" {
			continue
		}
		into := -1
		for j := range rd.x {
			if inside(rd.x[j], rd.y[j]) || outlineDist(xs, ys, rd.x[j], rd.y[j]) < 3 {
				into = j
				break
			}
		}
		if into < 0 {
			continue
		}
		for j := range rd.x {
			d, off := nearestOnRoute(r, rd.x[j], rd.y[j], 0, math.Inf(1))
			if off > nodeNearM+2 || off >= bestOff {
				continue
			}
			dir := 1
			if into < j {
				dir = -1
			}
			b, length, ok := walk(rd, j, dir)
			if !ok {
				continue
			}
			bestOff, found = off, true
			best = Junction{DistanceM: d, Kind: JunctionEntrance, Branches: []Branch{{BearingDeg: b, WidthM: rd.widthM, LengthM: length}}}
		}
	}
	return best, found
}

func outlineDist(xs, ys []float64, x, y float64) float64 {
	m := math.Inf(1)
	for i := range xs {
		j := (i + 1) % len(xs)
		d, _ := segDist(x, y, xs[i], ys[i], xs[j], ys[j])
		m = math.Min(m, d)
	}
	return m
}

// placeSigns finds the place-name signs where the route enters a place:
// the route nears the place after passing them.
func placeSigns(c *course.Course, d *Data, r []routePt) []PlaceSign {
	places := map[string][][2]float64{}
	for _, el := range d.Elements {
		if el.Type == "node" && el.Tags["place"] != "" && el.Tags["name"] != "" {
			x, y := c.Project(el.Lat, el.Lon)
			places[el.Tags["name"]] = append(places[el.Tags["name"]], [2]float64{x, y})
		}
	}
	var out []PlaceSign
	for _, el := range d.Elements {
		name := el.Tags["name"]
		if el.Type != "node" || !strings.Contains(el.Tags["traffic_sign"], "city_limit") || name == "" {
			continue
		}
		x, y := c.Project(el.Lat, el.Lon)
		along, off := nearestOnRoute(r, x, y, 0, math.Inf(1))
		if off > signNearM {
			continue
		}
		ps := places[name]
		if len(ps) == 0 {
			continue
		}
		dist := func(dd float64) float64 {
			p := pointAt(r, dd)
			m := math.Inf(1)
			for _, q := range ps {
				m = math.Min(m, math.Hypot(p.x-q[0], p.y-q[1]))
			}
			return m
		}
		if dist(along+150) >= dist(along-150) {
			continue // leaving, or passing by
		}
		dup := false
		for _, o := range out {
			dup = dup || o.Name == name && math.Abs(o.DistanceM-along) < 100
		}
		if !dup {
			out = append(out, PlaceSign{DistanceM: along, Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceM < out[j].DistanceM })
	return out
}
