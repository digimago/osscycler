package scenery

import "math"

// The figure 8's village and river (owner, 2026-10-22: the demo track
// should have every kind of thing a world is built from, so a change to
// the builder can be checked on it first). Along the polder's southern
// arc the route rides through a village on mapped roads: Dorpsstraat with
// houses either side (sidewalks guessed from the built-up land), a side
// street to a school (a T-junction), a car park, a small roundabout with a
// street of houses off one side and a polder road to a farm off the other,
// and a bridge over a river that runs north to south through the polder.
// The river crosses the northern arc too, under a short stretch of mapped
// country road, where the course's own way joins and leaves a road. The
// crossing of the two passes and the rest of the loop stay the course's
// own way: without a ground model a mapped road takes its heights from
// the profile around it, which near the crossing would mix the two
// passes' heights and lose the bridge.

const (
	villageFromM = 4000.0 // Dorpsstraat along the course
	villageToM   = 4520.0
	schoolM      = 4100.0 // Schoolstraat's mouth
	parkingM     = 4175.0 // the car park's entrance
	roundaboutM  = 4250.0 // the roundabout's middle
	roundaboutR  = 12.0   // one lane: a flat disc, crossed straight
	villageM     = 65.0   // the village reaches this far from its streets
	plotStepM    = 20.0   // house plots along a street
	riverWidthM  = 16.0
	riverCutM    = 4.5  // the river ends this far from a road's line, under its bridge
	bridgeHalfM  = 15.0 // a bridge reaches this far either side of the river's middle
)

// node is the id of the map node at p: the same for every way through it,
// so ways meet where they share a point.
func (g *trackGen) node(p [2]float64) int64 {
	k := [2]int64{int64(math.Round(p[0] * 100)), int64(math.Round(p[1] * 100))}
	id, ok := g.nodes[k]
	if !ok {
		id = int64(len(g.nodes) + 1)
		g.nodes[k] = id
	}
	return id
}

// way adds a road (or a roundabout's piece) along pts, east and north,
// and keeps its points for near.
func (g *trackGen) way(pts [][2]float64, kv ...string) {
	g.id--
	el := Element{Type: "way", ID: g.id, Tags: tags(kv...)}
	for i, q := range pts {
		lat, lon := g.c.Unproject(q[0], q[1])
		el.Geometry = append(el.Geometry, LatLon{Lat: lat, Lon: lon})
		el.Nodes = append(el.Nodes, g.node(q))
		if i > 0 {
			a := pts[i-1]
			l := math.Hypot(q[0]-a[0], q[1]-a[1])
			for f := 0.0; f < l; f += 2 {
				p := [2]float64{a[0] + (q[0]-a[0])*f/l, a[1] + (q[1]-a[1])*f/l}
				k := [2]int{int(math.Floor(p[0] / 20)), int(math.Floor(p[1] / 20))}
				g.roadGrid[k] = append(g.roadGrid[k], p)
			}
		}
	}
	g.data.Elements = append(g.data.Elements, el)
}

// near is the distance from p to the route or any mapped road, up to 60 m.
func (g *trackGen) near(p [2]float64) float64 {
	best := g.dist(p)
	k := [2]int{int(math.Floor(p[0] / 20)), int(math.Floor(p[1] / 20))}
	for gx := k[0] - 3; gx <= k[0]+3; gx++ {
		for gy := k[1] - 3; gy <= k[1]+3; gy++ {
			for _, q := range g.roadGrid[[2]int{gx, gy}] {
				best = math.Min(best, math.Hypot(q[0]-p[0], q[1]-p[1]))
			}
		}
	}
	return best
}

// inVillage tells whether p lies within villageM of a village street.
func (g *trackGen) inVillage(p [2]float64) bool {
	for _, s := range g.streets {
		for _, q := range s {
			if math.Hypot(q[0]-p[0], q[1]-p[1]) < villageM {
				return true
			}
		}
	}
	return false
}

// along is the course from a to b metres every 5 m, b included.
func (g *trackGen) along(a, b float64) [][2]float64 {
	var out [][2]float64
	for d := a; d < b; d += 5 {
		out = append(out, g.at(d))
	}
	return append(out, g.at(b))
}

// heading is the course's direction at d, in radians from east.
func (g *trackGen) heading(d float64) float64 {
	p, q := g.at(d-2.5), g.at(d+2.5)
	return math.Atan2(q[1]-p[1], q[0]-p[0])
}

// riverAt is where along the course, between a and b, it crosses the
// river's middle at x (the course runs across it once there).
func (g *trackGen) riverAt(x, a, b float64) float64 {
	for range 40 {
		m := (a + b) / 2
		if (g.at(a)[0]-x)*(g.at(m)[0]-x) <= 0 {
			b = m
		} else {
			a = m
		}
	}
	return (a + b) / 2
}

// line is a straight street from p heading h, every 5 m for length.
func line(p [2]float64, h, length float64) [][2]float64 {
	var out [][2]float64
	for s := 0.0; s <= length; s += 5 {
		out = append(out, [2]float64{p[0] + math.Cos(h)*s, p[1] + math.Sin(h)*s})
	}
	return out
}

// villageRoads lays the village's roads and the country road over the
// river on the northern arc, before anything that keeps clear of them.
// river is the river's middle (east).
func (g *trackGen) villageRoads(river float64) {
	snap := func(d float64) float64 { return math.Round(d/5) * 5 }
	// Dorpsstraat, split at the roundabout and at the bridge.
	c := g.at(roundaboutM)
	in := g.at(roundaboutM - roundaboutR)
	a0 := math.Atan2(in[1]-c[1], in[0]-c[0])
	var ring [16][2]float64
	for k := range ring {
		a := a0 + 2*math.Pi*float64(k)/16
		ring[k] = [2]float64{c[0] + roundaboutR*math.Cos(a), c[1] + roundaboutR*math.Sin(a)}
	}
	dorp := []string{"highway", "tertiary", "name", "Dorpsstraat"}
	first := append(g.along(villageFromM, snap(roundaboutM-roundaboutR-2.5)-5), ring[0])
	g.way(first, dorp...)
	dr := g.riverAt(river, roundaboutM, villageToM)
	bFrom, bTo := snap(dr-bridgeHalfM), snap(dr+bridgeHalfM)
	g.way(append([][2]float64{ring[8]}, g.along(snap(roundaboutM+roundaboutR+2.5)+5, bFrom)...), dorp...)
	g.way(g.along(bFrom, bTo), append(dorp, "bridge", "yes", "layer", "1")...)
	g.way(g.along(bTo, villageToM), dorp...)
	g.streets = append(g.streets, g.along(villageFromM, villageToM))
	// The ring in four pieces between its roads (pieces sharing their
	// ends: its middle stays the course's).
	for q := range 4 {
		piece := append([][2]float64(nil), ring[4*q:4*q+4]...)
		g.way(append(piece, ring[(4*q+4)%16]), "highway", "tertiary", "junction", "roundabout")
	}
	// The ring's other two roads: Kerkstraat on the left (north, inside
	// the loop), houses along it; the Polderweg on the right, out over the
	// polder to a farm at its end.
	h := g.heading(roundaboutM)
	kerk := line(ring[12], h+math.Pi/2, 120)
	g.way(kerk, "highway", "residential", "name", "Kerkstraat")
	g.streets = append(g.streets, kerk)
	polder := line(ring[4], h-math.Pi/2, 220)
	g.way(polder, "highway", "unclassified", "name", "Polderweg")
	// Schoolstraat on the left to the school, a T-junction.
	g.school = line(g.at(schoolM), g.heading(schoolM)+math.Pi/2, 110)
	g.way(g.school, "highway", "residential", "name", "Schoolstraat")
	g.streets = append(g.streets, g.school)
	// The car park's entrance on the right.
	g.way(line(g.at(parkingM), g.heading(parkingM)-math.Pi/2, 25), "highway", "service")

	// The country road over the river on the northern arc: the course's
	// own way joins it and leaves it again.
	du := g.riverAt(river, 450, 750)
	from, to := snap(du-50), snap(du+50)
	bFrom, bTo = snap(du-bridgeHalfM), snap(du+bridgeHalfM)
	dijk := []string{"highway", "unclassified", "name", "Rivierdijk"}
	g.way(g.along(from, bFrom), dijk...)
	g.way(g.along(bFrom, bTo), append(dijk, "bridge", "yes", "layer", "1")...)
	g.way(g.along(bTo, to), dijk...)
}

// village lays the land, houses, school, car park and farm along the
// roads villageRoads laid.
func (g *trackGen) village(river float64) {
	// House plots either side of each street, each a residential area of
	// its own (the smallest area there is the land's use, so plots win
	// over the polder's parcels), a house on most; none on the river.
	for _, s := range g.streets {
		g.plots(s, river)
	}
	// The school at Schoolstraat's end, its grounds round it.
	school := g.school
	end, h := school[len(school)-1], math.Atan2(school[1][1]-school[0][1], school[1][0]-school[0][0])
	off := func(p [2]float64, h, along, across float64) [2]float64 {
		return [2]float64{p[0] + math.Cos(h)*along - math.Sin(h)*across, p[1] + math.Sin(h)*along + math.Cos(h)*across}
	}
	g.area(trackRect(off(end, h, 30, 0), h, 50, 70), tags("landuse", "education"))
	g.building(off(end, h, 30, 0), h+math.Pi/2, 34, 14, "building", "school", "building:levels", "2")
	// The car park beyond its entrance, 40 by 30 m.
	ph := g.heading(parkingM) - math.Pi/2
	g.area(trackRect(off(g.at(parkingM), ph, 25+12, 0), ph, 30, 40),
		tags("amenity", "parking", "parking", "surface", "access", "yes", "surface", "asphalt"))
	// A farm at the Polderweg's end.
	rh := g.heading(roundaboutM) - math.Pi/2
	c := g.at(roundaboutM)
	end = off(c, rh, roundaboutR+220, 0)
	g.farmstead(end, rh, 1)
}

// plots lays house plots either side of street s, every plotStepM along
// it, 4 to 55 m from its line, with a house on each that keeps clear of
// every road; none where they would reach the river.
func (g *trackGen) plots(s [][2]float64, river float64) {
	// Points every plotStepM along the street, with its direction there.
	var at [][2]float64
	var dirs []float64
	for i := 0; i < len(s); i += int(plotStepM / 5) {
		j := min(i+1, len(s)-1)
		k := max(j-1, 0)
		at = append(at, s[i])
		dirs = append(dirs, math.Atan2(s[j][1]-s[k][1], s[j][0]-s[k][0]))
	}
	for i := 0; i+1 < len(at); i++ {
		for _, side := range []float64{1, -1} {
			pt := func(k int, out float64) [2]float64 {
				h := dirs[k]
				return [2]float64{at[k][0] - math.Sin(h)*out*side, at[k][1] + math.Cos(h)*out*side}
			}
			ring := [][2]float64{pt(i, 4), pt(i+1, 4), pt(i+1, 55), pt(i, 55)}
			if side < 0 {
				ring = [][2]float64{pt(i, 4), pt(i, 55), pt(i+1, 55), pt(i+1, 4)}
			}
			wet := false
			for _, q := range ring {
				wet = wet || math.Abs(q[0]-river) < riverWidthM/2+6
			}
			if wet || (ring[0][0]-river)*(ring[2][0]-river) < 0 {
				continue
			}
			g.area(ring, tags("landuse", "residential"))
			mid := [2]float64{(ring[0][0] + ring[1][0] + ring[2][0] + ring[3][0]) / 4, (ring[0][1] + ring[1][1] + ring[2][1] + ring[3][1]) / 4}
			h := (dirs[i] + dirs[i+1]) / 2
			if math.Abs(dirs[i]-dirs[i+1]) > math.Pi {
				h += math.Pi
			}
			// The house towards the street, a front garden before it.
			c := [2]float64{mid[0] + math.Sin(h)*side*12, mid[1] - math.Cos(h)*side*12}
			levels := "1"
			if g.rng.Float64() < 0.5 {
				levels = "2"
			}
			g.building(c, h, 9+3*g.rng.Float64(), 9, "building", "house", "building:levels", levels)
		}
	}
}

// farmstead is a farm beside the road at p (heading h), on the left when
// side is 1: a farmyard with a house, a barn and a shed, an orchard and a
// belt of trees.
func (g *trackGen) farmstead(p [2]float64, h, side float64) {
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

// river lays the river along x from s to n (north), in pieces between the
// roads over it, with meadow either side in its column of the polder.
func (g *trackGen) river(x, x0, x1, s, n float64) {
	g.area([][2]float64{{x0, s}, {x1, s}, {x1, n}, {x0, n}}, tags("landuse", "meadow"))
	from := math.NaN()
	for y := s; y <= n; y++ {
		clear := y < n && g.crossClear(x, y)
		switch {
		case clear && math.IsNaN(from):
			from = y
		case !clear && !math.IsNaN(from):
			if y-from >= 10 {
				g.area(trackRect([2]float64{x, (from + y) / 2}, math.Pi/2, y-from, riverWidthM),
					tags("natural", "water", "water", "river"))
			}
			from = math.NaN()
		}
	}
}

// crossClear tells whether the river's width at y keeps riverCutM off
// every road.
func (g *trackGen) crossClear(x, y float64) bool {
	for _, dx := range []float64{-riverWidthM / 2, 0, riverWidthM / 2} {
		if g.near([2]float64{x + dx, y}) < riverCutM {
			return false
		}
	}
	return true
}
