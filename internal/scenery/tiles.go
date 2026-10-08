package scenery

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/digimago/osscycler/internal/course"
)

// The map data is asked for in tiles on a fixed grid of degrees, the
// tiles within each layer's reach of the route: a corridor fits them
// better than a box per stretch of road, and a place the route passes
// twice (a loop's start and finish, an out-and-back) is asked for once.
const (
	tileLat = 1.0 / 400 // ≈ 278 m
	tileLon = 1.0 / 250 // ≈ 274 m at 52° N
	// sampleM is how often along the route the tiles in reach are found.
	sampleM = 10.0
	// queryHeader keeps the limits each query declares modest: the server
	// admits small requests more readily (a stretch's answer is a few MB).
	queryHeader = "[out:json][timeout:90][maxsize:134217728];\n"
)

// layer is what is asked for within marginM of the route, in tiles split
// sub × sub (narrow corridors fit finer tiles). Each statement has %s for
// the box.
type layer struct {
	marginM    float64
	sub        int
	statements []string
}

var layers = []layer{
	{300, 1, []string{ // land use
		`way["landuse"](%s);`,
		`relation["landuse"](%s);`,
		`way["natural"~"^(wood|water|scrub|heath|grassland|wetland|sand|beach|fell)$"](%s);`,
		`relation["natural"~"^(wood|water|scrub|heath|grassland|wetland|sand|beach|fell)$"](%s);`,
		`way["leisure"~"^(park|garden|golf_course)$"](%s);`,
		`relation["leisure"~"^(park|garden|golf_course)$"](%s);`,
	}},
	// Footprints only near the road, where a rider sees them as they are
	// (Build keeps them within buildingRangeM); further out land use says
	// enough.
	{100, 4, []string{`way["building"](%s);`}},
	{30, 4, []string{`way["highway"~"` + roadClasses + `"](%s);`}},
	{80, 2, []string{`nwr["amenity"="parking"](%s);`}},
	{25, 4, []string{`node["traffic_sign"~"city_limit"](%s);`}},
}

type tile struct{ i, j int }

// queries is the Overpass QL for each stretch of c, the stretches split
// at bounds (distances along it, from 0 to its end). A stretch asks for
// the tiles its part of the route first reaches; the places around it
// come in one box per stretch (nodes only: cheap).
func queries(c *course.Course, bounds []float64) []string {
	n := len(bounds) - 1
	parts := make([][]string, n)
	for _, l := range layers {
		dLat, dLon := tileLat/float64(l.sub), tileLon/float64(l.sub)
		taken := map[tile]bool{}
		for k := range n {
			var mine []tile
			for d := bounds[k]; ; d += sampleM {
				lat, lon := c.Position(math.Min(d, bounds[k+1]))
				for _, t := range tilesNear(lat, lon, l.marginM, dLat, dLon) {
					if !taken[t] {
						taken[t] = true
						mine = append(mine, t)
					}
				}
				if d >= bounds[k+1] {
					break
				}
			}
			for _, r := range blocks(mine) {
				box := fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",
					float64(r.j0)*dLat, float64(r.i0)*dLon, float64(r.j1+1)*dLat, float64(r.i1+1)*dLon)
				for _, st := range l.statements {
					parts[k] = append(parts[k], fmt.Sprintf(st, box))
				}
			}
		}
	}
	qs := make([]string, n)
	for k := range n {
		var b strings.Builder
		b.WriteString(queryHeader + "(\n")
		for _, st := range parts[k] {
			b.WriteString("  " + st + "\n")
		}
		fmt.Fprintf(&b, "  node[\"place\"~\"^(city|town|village|hamlet|suburb)$\"](%s);\n", bbox(c, bounds[k], bounds[k+1], placeMarginM))
		b.WriteString(");\nout geom;\n")
		qs[k] = b.String()
	}
	return qs
}

// tilesNear lists the tiles of a grid (dLat × dLon degrees) within margin
// metres of a point.
func tilesNear(lat, lon, margin, dLat, dLon float64) []tile {
	mLat := 111195.0
	mLon := mLat * math.Cos(lat*math.Pi/180)
	var ts []tile
	for j := int(math.Floor((lat - margin/mLat) / dLat)); j <= int(math.Floor((lat+margin/mLat)/dLat)); j++ {
		for i := int(math.Floor((lon - margin/mLon) / dLon)); i <= int(math.Floor((lon+margin/mLon)/dLon)); i++ {
			// The tile's nearest point to the route point.
			y := (math.Max(float64(j)*dLat, math.Min(lat, float64(j+1)*dLat)) - lat) * mLat
			x := (math.Max(float64(i)*dLon, math.Min(lon, float64(i+1)*dLon)) - lon) * mLon
			if math.Hypot(x, y) <= margin {
				ts = append(ts, tile{i, j})
			}
		}
	}
	return ts
}

// block is a rectangle of tiles, i0..i1 by j0..j1 inclusive.
type block struct{ i0, j0, i1, j1 int }

// blocks merges tiles into few rectangles, so a query has few boxes: runs
// along each row, then runs with the same columns in the rows above.
func blocks(ts []tile) []block {
	ts = slices.Clone(ts)
	slices.SortFunc(ts, func(a, b tile) int {
		if a.j != b.j {
			return a.j - b.j
		}
		return a.i - b.i
	})
	var runs []block
	for k, t := range ts {
		if k > 0 && t.j == ts[k-1].j && t.i == ts[k-1].i+1 {
			runs[len(runs)-1].i1 = t.i
			continue
		}
		runs = append(runs, block{t.i, t.j, t.i, t.j})
	}
	// open: the rectangles that may still grow, by their column span.
	var out []block
	open := map[[2]int]int{} // i0,i1 → index in out
	for _, r := range runs {
		key := [2]int{r.i0, r.i1}
		if k, ok := open[key]; ok && out[k].j1 == r.j0-1 {
			out[k].j1 = r.j0
			continue
		}
		open[key] = len(out)
		out = append(out, r)
	}
	return out
}
