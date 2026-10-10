package scenery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

// The coastline as a basemap (owner, 2026-10-10: woods by the sea are low,
// wind keeps them so; the builder needs to know how far the sea is). It is
// shared by every course rather than fetched for each: OpenStreetMap's
// natural=coastline by whole-degree tile, asked for once when a course
// first comes within CoastReachM of a tile (an inland tile is cached as
// empty), again only when the cached tile is older than coastMaxAge, and
// kept simplified to coastSimplifyM, which is plenty for how far the sea
// is. The shoreline itself, where the 3D world needs it, comes with the
// map data along the route.
const (
	CoastReachM    = 10000.0                  // how far from the sea it matters
	coastMaxAge    = 3 * 365 * 24 * time.Hour // coasts move a little (sand nourishment, harbours)
	coastSimplifyM = 30.0                     // the lines kept this close to OSM's
	coastCellDeg   = 0.02                     // the distance index's cells
)

// Coast is the coastline near a course, for how far the sea is.
type Coast struct {
	cells map[[2]int][][4]float64 // segments by cell: lat0 lon0 lat1 lon1
}

// coastTile is a cached tile.
type coastTile struct {
	Fetched time.Time      `json:"fetched"`
	Lines   [][][2]float64 `json:"lines"` // lat, lon
}

// Coast gives the coastline within CoastReachM of course c: from the cache,
// tiles missing or old fetched when fetching is on and fetch is true. A
// tile that can't be had is left out (an old copy is used if there is
// one), and the error says which.
func (s *Store) Coast(ctx context.Context, c *course.Course, fetch bool) (*Coast, error) {
	s0, w0, n0, e0 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for d := 0.0; d <= c.Distance; d += 100 {
		lat, lon := c.Position(d)
		s0, w0, n0, e0 = math.Min(s0, lat), math.Min(w0, lon), math.Max(n0, lat), math.Max(e0, lon)
	}
	dLat := CoastReachM / 111195
	dLon := CoastReachM / (111195 * math.Cos((s0+n0)/2*math.Pi/180))
	co := &Coast{cells: map[[2]int][][4]float64{}}
	var errs []error
	for la := int(math.Floor(s0 - dLat)); la <= int(math.Floor(n0+dLat)); la++ {
		for lo := int(math.Floor(w0 - dLon)); lo <= int(math.Floor(e0+dLon)); lo++ {
			t, err := s.coastTile(ctx, la, lo, fetch)
			if err != nil {
				errs = append(errs, fmt.Errorf("coast tile %d,%d: %w", la, lo, err))
			}
			if t != nil {
				for _, l := range t.Lines {
					co.add(l)
				}
			}
		}
	}
	return co, errors.Join(errs...)
}

func (s *Store) coastPath(lat, lon int) string {
	return filepath.Join(s.cfg.CacheDir, fmt.Sprintf("coast-%d_%d.json", lat, lon))
}

// coastTile is the tile whose south-west corner is lat, lon.
func (s *Store) coastTile(ctx context.Context, lat, lon int, fetch bool) (*coastTile, error) {
	var cached *coastTile
	if raw, err := os.ReadFile(s.coastPath(lat, lon)); err == nil {
		var t coastTile
		if json.Unmarshal(raw, &t) == nil {
			cached = &t
		}
	}
	if cached != nil && time.Since(cached.Fetched) < coastMaxAge {
		return cached, nil
	}
	if !fetch || !s.cfg.Fetch {
		if cached == nil {
			return nil, errors.New("not cached, and fetching is off")
		}
		return cached, nil
	}
	query := fmt.Sprintf(`[out:json][timeout:90][maxsize:67108864];way["natural"="coastline"](%d,%d,%d,%d);out geom;`, lat, lon, lat+1, lon+1)
	raw, err := s.fetch(ctx, query)
	if err != nil {
		return cached, err
	}
	d, err := Parse(raw)
	if err != nil {
		return cached, err
	}
	t := &coastTile{Fetched: time.Now().UTC()}
	for _, el := range d.Elements {
		if el.Type != "way" || len(el.Geometry) < 2 {
			continue
		}
		var l [][2]float64
		for _, p := range el.Geometry {
			l = append(l, [2]float64{p.Lat, p.Lon})
		}
		t.Lines = append(t.Lines, simplifyLatLon(l, coastSimplifyM))
	}
	if out, err := json.Marshal(t); err == nil {
		if err := os.MkdirAll(s.cfg.CacheDir, 0o755); err == nil {
			tmp := s.coastPath(lat, lon) + ".tmp"
			if os.WriteFile(tmp, out, 0o644) == nil {
				_ = os.Rename(tmp, s.coastPath(lat, lon))
			}
		}
	}
	return t, nil
}

func (co *Coast) add(l [][2]float64) {
	for i := 0; i+1 < len(l); i++ {
		a, b := l[i], l[i+1]
		seg := [4]float64{a[0], a[1], b[0], b[1]}
		for la := int(math.Floor(math.Min(a[0], b[0]) / coastCellDeg)); la <= int(math.Floor(math.Max(a[0], b[0])/coastCellDeg)); la++ {
			for lo := int(math.Floor(math.Min(a[1], b[1]) / coastCellDeg)); lo <= int(math.Floor(math.Max(a[1], b[1])/coastCellDeg)); lo++ {
				k := [2]int{la, lo}
				co.cells[k] = append(co.cells[k], seg)
			}
		}
	}
}

// Distance is how far lat, lon is from the coastline, in metres; +Inf when
// it is CoastReachM or more (or there is no coast near).
func (co *Coast) Distance(lat, lon float64) float64 {
	if co == nil || len(co.cells) == 0 {
		return math.Inf(1)
	}
	kx := 111195 * math.Cos(lat*math.Pi/180)
	best := math.Inf(1)
	rLat := int(math.Ceil(CoastReachM / 111195 / coastCellDeg))
	rLon := int(math.Ceil(CoastReachM / kx / coastCellDeg))
	c0 := [2]int{int(math.Floor(lat / coastCellDeg)), int(math.Floor(lon / coastCellDeg))}
	for la := c0[0] - rLat; la <= c0[0]+rLat; la++ {
		for lo := c0[1] - rLon; lo <= c0[1]+rLon; lo++ {
			for _, s := range co.cells[[2]int{la, lo}] {
				ax, ay := (s[1]-lon)*kx, (s[0]-lat)*111195
				bx, by := (s[3]-lon)*kx, (s[2]-lat)*111195
				dx, dy := bx-ax, by-ay
				t := 0.0
				if l2 := dx*dx + dy*dy; l2 > 0 {
					t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l2))
				}
				best = math.Min(best, math.Hypot(ax+t*dx, ay+t*dy))
			}
		}
	}
	if best >= CoastReachM {
		return math.Inf(1)
	}
	return best
}

// simplifyLatLon thins a line of lat, lon points to within tol metres
// (Douglas-Peucker), its ends kept.
func simplifyLatLon(l [][2]float64, tol float64) [][2]float64 {
	if len(l) < 3 {
		return l
	}
	kx := 111195 * math.Cos(l[0][0]*math.Pi/180)
	keep := make([]bool, len(l))
	keep[0], keep[len(l)-1] = true, true
	var dp func(i, j int)
	dp = func(i, j int) {
		ax, ay := l[i][1]*kx, l[i][0]*111195
		bx, by := l[j][1]*kx, l[j][0]*111195
		dx, dy := bx-ax, by-ay
		ln := math.Hypot(dx, dy)
		far, at := 0.0, -1
		for k := i + 1; k < j; k++ {
			px, py := l[k][1]*kx-ax, l[k][0]*111195-ay
			var d float64
			if ln == 0 {
				d = math.Hypot(px, py)
			} else {
				d = math.Abs(px*dy-py*dx) / ln
			}
			if d > far {
				far, at = d, k
			}
		}
		if at >= 0 && far > tol {
			keep[at] = true
			dp(i, at)
			dp(at, j)
		}
	}
	dp(0, len(l)-1)
	var out [][2]float64
	for k, ok := range keep {
		if ok {
			out = append(out, l[k])
		}
	}
	return out
}
