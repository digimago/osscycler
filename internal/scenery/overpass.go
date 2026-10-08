// Package scenery describes what lies along a course (fields, forest,
// water, buildings) from OpenStreetMap, for renderers to draw.
//
// The map data comes from an Overpass API server, per 5 km stretch of a
// course, fetched in the background for the courses being ridden and
// cached; nothing waits for it. OpenStreetMap data
// is © OpenStreetMap contributors, available under the ODbL: renderers
// show Attribution with it.
package scenery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

const (
	// DefaultOverpassURL is the main public Overpass server.
	DefaultOverpassURL = "https://overpass-api.de/api/interpreter"
	// Attribution is the credit the ODbL requires with the data.
	Attribution = "map data © OpenStreetMap contributors"

	// placeMarginM reaches the places whose limits the route crosses.
	placeMarginM = 2000.0
	// maxResponse guards against a runaway answer.
	maxResponse = 256 << 20
)

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
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		if msg := firstLine(body); msg != "" && !strings.HasPrefix(msg, "<") {
			return nil, fmt.Errorf("overpass: %s: %s", resp.Status, msg)
		}
		return nil, fmt.Errorf("overpass: %s", resp.Status)
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
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// ErrRateLimited is the server's answer while all of this client's slots
// are taken: a query that just finished keeps its slot for a while.
var ErrRateLimited = errors.New("overpass: too many requests")

// slotWaitDefault is the wait for a slot when the server doesn't say.
const slotWaitDefault = 30 * time.Second

// slotWait asks the server how long until one of this client's slots is
// free (its /api/status, beside the interpreter), at most maxWait.
func slotWait(ctx context.Context, client *http.Client, endpoint, userAgent string, maxWait time.Duration) time.Duration {
	wait := slotWaitDefault
	status, ok := strings.CutSuffix(endpoint, "/interpreter")
	if !ok {
		return min(wait, maxWait)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, status+"/status", nil)
	if err != nil {
		return min(wait, maxWait)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return min(wait, maxWait)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == http.StatusOK {
		if w, ok := parseSlotWait(string(body)); ok {
			wait = w
		}
	}
	return min(wait, maxWait)
}

var (
	slotsFree = regexp.MustCompile(`(?m)^[1-9][0-9]* slots? available now`)
	slotIn    = regexp.MustCompile(`(?m)^Slot available after: \S+, in (-?[0-9]+) seconds?\.`)
)

// parseSlotWait reads the wait for the first free slot from an Overpass
// status page.
func parseSlotWait(status string) (time.Duration, bool) {
	if slotsFree.MatchString(status) {
		return 0, true
	}
	best := -1
	for _, m := range slotIn.FindAllStringSubmatch(status, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && (best < 0 || n < best) {
			best = max(n, 0)
		}
	}
	if best < 0 {
		return 0, false
	}
	return time.Duration(best+1) * time.Second, true
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

// merge joins the data of several stretches. A way or relation that
// reaches into more than one comes back with each: it is kept once.
func merge(parts []*Data) *Data {
	type key struct {
		kind string
		id   int64
	}
	seen := map[key]bool{}
	d := &Data{}
	for _, p := range parts {
		for _, e := range p.Elements {
			k := key{e.Type, e.ID}
			if !seen[k] {
				seen[k] = true
				d.Elements = append(d.Elements, e)
			}
		}
	}
	return d
}

// Parse reads an Overpass JSON answer.
func Parse(raw []byte) (*Data, error) {
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("scenery: parse map data: %w", err)
	}
	return &d, nil
}
