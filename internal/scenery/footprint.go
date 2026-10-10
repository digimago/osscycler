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
}

const (
	footprintRangeM = 110.0 // footprints this close to the route (the query reaches 100 m)
	footprintMinM2  = 12.0  // sheds and kiosks smaller than this don't show
)

// Footprints are the buildings near the route with their outlines.
func Footprints(c *course.Course, d *Data) []Footprint {
	r := sampleRoute(c)
	// The route by grid cell, to tell near from far quickly.
	cells := map[[2]int][]int{}
	for i, p := range r {
		k := [2]int{cell(p.x), cell(p.y)}
		cells[k] = append(cells[k], i)
	}
	near := func(x, y float64) bool {
		for gx := cell(x) - 1; gx <= cell(x)+1; gx++ {
			for gy := cell(y) - 1; gy <= cell(y)+1; gy++ {
				for _, i := range cells[[2]int{gx, gy}] {
					if math.Hypot(r[i].x-x, r[i].y-y) < footprintRangeM {
						return true
					}
				}
			}
		}
		return false
	}
	var out []Footprint
	for _, el := range d.Elements {
		t := el.Tags
		b := t["building"]
		if b == "" || b == "no" || b == "roof" { // a roof on posts isn't a building to draw as one
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
		var pts [][2]float64
		for k, p := range ring {
			if k == len(ring)-1 && p == ring[0] {
				break
			}
			x, y := c.Project(p.Lat, p.Lon)
			pts = append(pts, [2]float64{x, y})
		}
		if len(pts) < 3 {
			continue
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
		area /= 2
		if area < footprintMinM2 || !near(cx, cy) {
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
