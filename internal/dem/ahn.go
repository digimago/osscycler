package dem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// AHNURL is PDOK's WCS for the AHN.
	AHNURL = "https://service.pdok.nl/rws/ahn/wcs/v1_0"
	// AHNAttribution credits the source. AHN is CC0, so this is courtesy,
	// not a condition.
	AHNAttribution = "elevation: AHN (Actueel Hoogtebestand Nederland), CC0"

	ahnCoverage = "dtm_05m" // bare ground, 0.5 m cells
	ahnNative   = 0.5       // m
	tileM       = 1000      // tiles are 1 km squares on the RD grid
)

// AHN's extent in RD New, from the WCS's coverage description.
var ahnMinX, ahnMinY, ahnMaxX, ahnMaxY = 10000.0, 306250.0, 280000.0, 618750.0

// ErrOutside reports that the area has no data in this source.
var ErrOutside = errors.New("dem: area outside the elevation source")

// AHN fetches the AHN ground model in 1 km tiles, coarsened by the server
// to CellM, and keeps every tile in a cache directory: tiles are shared by
// courses through the same area, and fetched once.
type AHN struct {
	CacheDir  string
	Fetch     bool    // allow asking the server; without it only the cache is used
	CellM     float64 // cell size, default 2 m
	Endpoint  string  // default AHNURL
	UserAgent string
	Client    *http.Client
	Log       *slog.Logger
	// Progress, when set, hears each tile done (cached or fetched) of
	// total, from the loading goroutines.
	Progress func(done, total int)
}

// LatLon is a point in WGS84 degrees.
type LatLon struct{ Lat, Lon float64 }

// Load reads the tiles within radius metres of any of pts.
func (a *AHN) Load(ctx context.Context, pts []LatLon, radius float64) (*Mosaic, error) {
	cell := a.CellM
	if cell == 0 {
		cell = 2
	}
	if cell < ahnNative || cell > 50 {
		return nil, fmt.Errorf("dem: cell size %v m out of range", cell)
	}
	keys := map[[2]int]bool{}
	for _, p := range pts {
		x, y := ToRD(p.Lat, p.Lon)
		for tx := int(math.Floor((x - radius) / tileM)); tx <= int(math.Floor((x+radius)/tileM)); tx++ {
			for ty := int(math.Floor((y - radius) / tileM)); ty <= int(math.Floor((y+radius)/tileM)); ty++ {
				if inAHN(tx, ty) {
					keys[[2]int{tx, ty}] = true
				}
			}
		}
	}
	if len(keys) == 0 {
		return nil, ErrOutside
	}
	order := make([][2]int, 0, len(keys))
	for k := range keys {
		order = append(order, k)
	}
	sort.Slice(order, func(i, j int) bool {
		return order[i][1] < order[j][1] || order[i][1] == order[j][1] && order[i][0] < order[j][0]
	})

	m := &Mosaic{tiles: map[[2]int]*Grid{}}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		fetched  int
		done     int
	)
	work := make(chan [2]int)
	for range 4 { // a few at a time, to be kind to the server
		wg.Go(func() {
			for k := range work {
				g, got, err := a.tile(ctx, k[0], k[1], cell)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				if g != nil {
					m.tiles[k] = g
				}
				if got {
					fetched++
				}
				done++
				if a.Progress != nil {
					a.Progress(done, len(order))
				}
				mu.Unlock()
			}
		})
	}
	for _, k := range order {
		work <- k
	}
	close(work)
	wg.Wait()
	a.log().Info("AHN tiles", "tiles", len(order), "fetched", fetched, "cell_m", cell)
	if firstErr != nil {
		return nil, firstErr
	}
	return m, nil
}

func inAHN(tx, ty int) bool {
	x, y := float64(tx*tileM), float64(ty*tileM)
	return x+tileM > ahnMinX && x < ahnMaxX && y+tileM > ahnMinY && y < ahnMaxY
}

// tile returns tile tx, ty from the cache, fetching it when allowed; got
// tells whether it came from the server.
func (a *AHN) tile(ctx context.Context, tx, ty int, cell float64) (*Grid, bool, error) {
	name := filepath.Join(a.CacheDir, fmt.Sprintf("ahn-%s-%gm-%d-%d.tif", ahnCoverage, cell, tx, ty))
	b, err := os.ReadFile(name)
	got := false
	if errors.Is(err, os.ErrNotExist) {
		if !a.Fetch {
			return nil, false, fmt.Errorf("dem: AHN tile %d,%d not in the cache and fetching is off", tx, ty)
		}
		if b, err = a.fetch(ctx, tx, ty, cell); err != nil {
			return nil, false, err
		}
		got = true
	} else if err != nil {
		return nil, false, err
	}
	g, err := DecodeGeoTIFF(b)
	if err != nil {
		return nil, false, fmt.Errorf("dem: AHN tile %d,%d: %w", tx, ty, err)
	}
	if got {
		// Only tiles that decode are kept.
		if err := writeAtomic(name, b); err != nil {
			return nil, false, err
		}
	}
	// Holes are buildings and water. A ring per cell covers houses and
	// canals; anything wider stays a hole and the caller falls back.
	g.Fill(int(40 / cell))
	return g, got, nil
}

func (a *AHN) fetch(ctx context.Context, tx, ty int, cell float64) ([]byte, error) {
	endpoint := a.Endpoint
	if endpoint == "" {
		endpoint = AHNURL
	}
	q := url.Values{
		"service":     {"WCS"},
		"version":     {"2.0.1"},
		"request":     {"GetCoverage"},
		"CoverageId":  {ahnCoverage},
		"format":      {"image/tiff"},
		"scalefactor": {fmt.Sprint(ahnNative / cell)},
	}
	u := endpoint + "?" + q.Encode() +
		fmt.Sprintf("&subset=x(%d,%d)&subset=y(%d,%d)", tx*tileM, (tx+1)*tileM, ty*tileM, (ty+1)*tileM)
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if a.UserAgent != "" {
			req.Header.Set("User-Agent", a.UserAgent)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/tiff" {
			lastErr = fmt.Errorf("dem: AHN tile %d,%d: %s %.200q", tx, ty, resp.Status, b)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				break
			}
			continue
		}
		return b, nil
	}
	return nil, lastErr
}

func (a *AHN) log() *slog.Logger {
	if a.Log != nil {
		return a.Log
	}
	return slog.New(slog.DiscardHandler)
}

func writeAtomic(name string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".tile-*")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), name)
}

// Mosaic is a set of AHN tiles.
type Mosaic struct {
	tiles map[[2]int]*Grid
}

// Elevation returns the ground height above NAP at a WGS84 point; false
// where no tile covers it or the data has a hole.
func (m *Mosaic) Elevation(lat, lon float64) (float64, bool) {
	x, y := ToRD(lat, lon)
	g := m.tiles[[2]int{int(math.Floor(x / tileM)), int(math.Floor(y / tileM))}]
	if g == nil {
		return 0, false
	}
	return g.At(x, y)
}
