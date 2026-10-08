// Package scenery describes what lies along a course (fields, forest,
// water, buildings) from OpenStreetMap, for renderers to draw.
//
// The map data comes from an Overpass API server, fetched once per course
// in the background and cached; nothing waits for it. OpenStreetMap data
// is © OpenStreetMap contributors, available under the ODbL: renderers
// show Attribution with it.
package scenery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/digimago/osscycler/internal/course"
)

const (
	// DefaultOverpassURL is the main public Overpass server.
	DefaultOverpassURL = "https://overpass-api.de/api/interpreter"
	// Attribution is the credit the ODbL requires with the data.
	Attribution = "map data © OpenStreetMap contributors"

	// chunkM splits the route into stretches with a box each, so the query
	// covers a corridor rather than the whole box around a long route.
	chunkM = 1000.0
	// The margins widen each stretch's box: land use, buildings, roads the
	// route meets, car parks beside it, signs along it, and the places
	// whose limits it crosses.
	landMarginM     = 300.0
	buildingMarginM = 120.0
	roadMarginM     = 30.0
	parkingMarginM  = 80.0
	signMarginM     = 25.0
	placeMarginM    = 2000.0
	// maxResponse guards against a runaway answer.
	maxResponse = 256 << 20
)

// Query is the Overpass QL for what lies along c: land use, buildings,
// the roads it meets, car parks, town limit signs and places.
func Query(c *course.Course) string {
	var b strings.Builder
	b.WriteString("[out:json][timeout:180];\n(\n")
	for start := 0.0; start < c.Distance; start += chunkM {
		land := bbox(c, start, math.Min(start+chunkM, c.Distance), landMarginM)
		build := bbox(c, start, math.Min(start+chunkM, c.Distance), buildingMarginM)
		for _, kind := range []string{"way", "relation"} {
			fmt.Fprintf(&b, "  %s[\"landuse\"](%s);\n", kind, land)
			fmt.Fprintf(&b, "  %s[\"natural\"~\"^(wood|water|scrub|heath|grassland|wetland|sand|beach|fell)$\"](%s);\n", kind, land)
			fmt.Fprintf(&b, "  %s[\"leisure\"~\"^(park|garden|golf_course)$\"](%s);\n", kind, land)
		}
		fmt.Fprintf(&b, "  way[\"building\"](%s);\n", build)
		d1 := math.Min(start+chunkM, c.Distance)
		fmt.Fprintf(&b, "  way[\"highway\"~\"%s\"](%s);\n", roadClasses, bbox(c, start, d1, roadMarginM))
		fmt.Fprintf(&b, "  nwr[\"amenity\"=\"parking\"](%s);\n", bbox(c, start, d1, parkingMarginM))
		fmt.Fprintf(&b, "  node[\"traffic_sign\"~\"city_limit\"](%s);\n", bbox(c, start, d1, signMarginM))
		fmt.Fprintf(&b, "  node[\"place\"~\"^(city|town|village|hamlet|suburb)$\"](%s);\n", bbox(c, start, d1, placeMarginM))
	}
	b.WriteString(");\nout geom;\n")
	return b.String()
}

// bbox is the Overpass box (south,west,north,east) around the course from
// d0 to d1, widened by margin metres.
func bbox(c *course.Course, d0, d1, margin float64) string {
	s, w, n, e := 90.0, 180.0, -90.0, -180.0
	for d := d0; ; d += 10 {
		lat, lon := c.Position(math.Min(d, d1))
		s, w, n, e = math.Min(s, lat), math.Min(w, lon), math.Max(n, lat), math.Max(e, lon)
		if d >= d1 {
			break
		}
	}
	dLat := margin / 111195
	dLon := margin / (111195 * math.Cos((s+n)/2*math.Pi/180))
	return fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", s-dLat, w-dLon, n+dLat, e+dLon)
}

// Fetch runs query on an Overpass server and returns the raw JSON.
func Fetch(ctx context.Context, client *http.Client, endpoint, userAgent, query string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(url.Values{"data": {query}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass: %s: %s", resp.Status, strings.TrimSpace(firstLine(body)))
	}
	// Overpass reports a timeout or overload as "remark" in a 200 answer,
	// with whatever it had so far: not something to cache.
	var probe struct {
		Remark string `json:"remark"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("overpass: answer isn't JSON: %w", err)
	}
	if probe.Remark != "" {
		return nil, fmt.Errorf("overpass: %s", probe.Remark)
	}
	return body, nil
}

func firstLine(b []byte) string {
	s := string(b[:min(len(b), 200)])
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Data is the parsed map data.
type Data struct {
	Elements []Element `json:"elements"`
}

// Element is a node, or a way or relation with its geometry (Overpass
// "out geom").
type Element struct {
	Type     string            `json:"type"`
	ID       int64             `json:"id"`
	Tags     map[string]string `json:"tags"`
	Lat      float64           `json:"lat"`      // nodes
	Lon      float64           `json:"lon"`      // nodes
	Nodes    []int64           `json:"nodes"`    // ways: node IDs, one per geometry point
	Geometry []LatLon          `json:"geometry"` // ways
	Members  []Member          `json:"members"`  // relations
}

// Member is part of a relation; for multipolygons, an outer or inner ring
// or a piece of one.
type Member struct {
	Type     string   `json:"type"`
	Role     string   `json:"role"`
	Geometry []LatLon `json:"geometry"`
}

type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Parse reads an Overpass JSON answer.
func Parse(raw []byte) (*Data, error) {
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("scenery: parse map data: %w", err)
	}
	return &d, nil
}
