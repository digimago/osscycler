package scenery

import (
	"math"
	"strconv"
	"strings"

	"github.com/digimago/osscycler/internal/course"
)

// Footprint is a building as the map draws it, for the 3D world: its
// outline and a guess at its walls and roof (Dutch map data has neither:
// the building register gives outlines only).
type Footprint struct {
	ID int64
	// Outline in metres east and north of the course's start
	// (course.Project), counter-clockwise, the first point not repeated.
	Outline [][2]float64
	Kind    BuildingKind
	WallM   float64 // from the ground to the eaves (or the flat roof)
	RoofM   float64 // from the eaves to the ridge; 0 for a flat roof
	// Colours from the map (building:colour, roof:colour), "" when it
	// doesn't say.
	WallColour, RoofColour string
	// FromLowest: WallM is measured from the lowest ground under it (a
	// survey's height, BD TOPO), not the highest (a guess): on a slope the
	// downhill wall is the taller one.
	FromLowest bool
}

const (
	footprintRangeM = 110.0 // footprints this close to the route (the query reaches 100 m)
	footprintMinM2  = 12.0  // sheds and kiosks smaller than this don't show
)

// nearRoute tells whether a point (metres east and north of the
// course's start) lies within r of the route (r at most the grid's cell).
func nearRoute(c *course.Course, r float64) func(x, y float64) bool {
	rt := sampleRoute(c)
	// The route by grid cell, to tell near from far quickly.
	cells := map[[2]int][]int{}
	for i, p := range rt {
		k := [2]int{cell(p.x), cell(p.y)}
		cells[k] = append(cells[k], i)
	}
	return func(x, y float64) bool {
		for gx := cell(x) - 1; gx <= cell(x)+1; gx++ {
			for gy := cell(y) - 1; gy <= cell(y)+1; gy++ {
				for _, i := range cells[[2]int{gx, gy}] {
					if math.Hypot(rt[i].x-x, rt[i].y-y) < r {
						return true
					}
				}
			}
		}
		return false
	}
}

// Footprints are the buildings near the route with their outlines. A
// bridge's support (bridge:support) is no building, though the Dutch
// register (BAG) gives the Erasmusbrug's piers in the Maas building=yes:
// drawn as houses they stood through its deck (owner, 2026-11-17); they
// are Supports. Nor is one underground (location=underground: a car park
// under a square).
func Footprints(c *course.Course, d *Data) []Footprint {
	near := nearRoute(c, footprintRangeM)
	var out []Footprint
	for _, el := range d.Elements {
		t := el.Tags
		b := t["building"]
		if b == "" || b == "no" || b == "roof" { // a roof on posts isn't a building to draw as one
			continue
		}
		if sp := t["bridge:support"]; sp != "" && sp != "no" || t["location"] == "underground" {
			continue
		}
		pts, area, cx, cy := outlineOf(c, el)
		if pts == nil || area < footprintMinM2 || !near(cx, cy) {
			continue
		}
		kind, height := buildingShape(t, area)
		f := Footprint{ID: el.ID, Outline: pts, Kind: kind, WallColour: t["building:colour"], RoofColour: t["roof:colour"]}
		switch {
		case kind == KindFlat || t["roof:shape"] == "flat":
			f.WallM = height
		default:
			// A pitched roof: tagged, else about a third of the height.
			f.RoofM = math.Max(1.5, math.Min(height*0.4, 6))
			if h, err := strconv.ParseFloat(strings.TrimSpace(t["roof:height"]), 64); err == nil && h > 0 && h < height {
				f.RoofM = h
			}
			f.WallM = math.Max(2.2, height-f.RoofM)
		}
		out = append(out, f)
	}
	return out
}

// outlineOf is a closed way's (or a multipolygon's largest outer ring's)
// outline in metres east and north of the course's start (course.Project),
// counter-clockwise, the first point not repeated, with its area and the
// mean of its points; nil when it has fewer than three.
func outlineOf(c *course.Course, el Element) ([][2]float64, float64, float64, float64) {
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
		return nil, 0, 0, 0
	}
	var pts [][2]float64
	for k, p := range ring {
		if k == len(ring)-1 && p == ring[0] {
			break
		}
		x, y := c.Project(p.Lat, p.Lon)
		pts = append(pts, [2]float64{x, y})
	}
	if len(pts) < 3 {
		return nil, 0, 0, 0
	}
	area, cx, cy := 0.0, 0.0, 0.0
	for i := range pts {
		j := (i + 1) % len(pts)
		area += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
		cx, cy = cx+pts[i][0]/float64(len(pts)), cy+pts[i][1]/float64(len(pts))
	}
	if area < 0 { // clockwise: turn it round
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
		area = -area
	}
	return pts, area / 2, cx, cy
}

// Support is a bridge's support as the map draws it (bridge:support: a
// pier, an abutment, a pylon's foot), an outline in metres east and north
// of the course's start, counter-clockwise.
type Support struct {
	ID      int64
	Kind    string // the tag's value: pier, abutment, pylon, ...
	Outline [][2]float64
}

// Supports are the bridges' supports near the route.
func Supports(c *course.Course, d *Data) []Support {
	near := nearRoute(c, footprintRangeM)
	seen := map[int64]bool{}
	var out []Support
	for _, el := range d.Elements {
		k := el.Tags["bridge:support"]
		if k == "" || k == "no" || seen[el.ID] {
			continue
		}
		pts, _, cx, cy := outlineOf(c, el)
		if pts == nil || !near(cx, cy) {
			continue
		}
		seen[el.ID] = true
		out = append(out, Support{ID: el.ID, Kind: k, Outline: pts})
	}
	return out
}
