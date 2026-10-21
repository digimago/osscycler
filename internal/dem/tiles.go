package dem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const tileM = 1000 // tiles are 1 km squares on the source's own grid

// ErrOutside reports that the area has no data in this source.
var ErrOutside = errors.New("dem: area outside the elevation source")

// ErrNotCached: a tile isn't in the cache, and fetching is off.
var ErrNotCached = errors.New("not in the cache and fetching is off")

// Source is how a ground model's tiles are got and kept: 1 km tiles,
// coarsened by the server to CellM, every tile kept in a cache directory,
// so tiles are shared by courses through the same area and fetched once.
// AHN and RGEAlti are Sources.
type Source struct {
	CacheDir  string
	Fetch     bool    // allow asking the server; without it only the cache is used
	CellM     float64 // cell size, default 2 m
	Endpoint  string  // default the source's own service
	UserAgent string
	Client    *http.Client
	Log       *slog.Logger
	// Progress, when set, hears each tile done (cached or fetched) of
	// total, from the loading goroutines.
	Progress func(done, total int)
}

// LatLon is a point in WGS84 degrees.
type LatLon struct{ Lat, Lon float64 }

// tileset is what differs between sources: the grid and the service.
type tileset struct {
	name        string  // for logs and errors
	native      float64 // the source's finest cell, m
	endpoint    string  // the default service
	contentType string  // of a good answer
	project     func(lat, lon float64) (x, y float64)
	// inside tells whether the source may have data in tile tx, ty.
	inside func(tx, ty int) bool
	file   func(tx, ty int, cell float64) string // the cache file's name
	url    func(endpoint string, tx, ty int, cell float64) string
}

// load reads the tiles within radius metres of any of pts; ErrOutside
// when none of pts has a height in them.
func (s *Source) load(ctx context.Context, ts *tileset, pts []LatLon, radius float64) (*Mosaic, error) {
	cell := s.CellM
	if cell == 0 {
		cell = 2
	}
	if cell < ts.native || cell > 50 {
		return nil, fmt.Errorf("dem: cell size %v m out of range", cell)
	}
	keys := map[[2]int]bool{}
	for _, p := range pts {
		x, y := ts.project(p.Lat, p.Lon)
		for tx := int(math.Floor((x - radius) / tileM)); tx <= int(math.Floor((x+radius)/tileM)); tx++ {
			for ty := int(math.Floor((y - radius) / tileM)); ty <= int(math.Floor((y+radius)/tileM)); ty++ {
				if ts.inside(tx, ty) {
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

	m := &Mosaic{tiles: map[[2]int]*Grid{}, project: ts.project}
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
				g, got, err := s.tile(ctx, ts, k[0], k[1], cell)
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
				if s.Progress != nil {
					s.Progress(done, len(order))
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
	s.log().Info(ts.name+" tiles", "tiles", len(order), "fetched", fetched, "cell_m", cell)
	if firstErr != nil {
		return nil, firstErr
	}
	// A source's extent is a box round its country; beyond the border its
	// tiles are all nodata.
	for _, p := range pts {
		if _, ok := m.Elevation(p.Lat, p.Lon); ok {
			return m, nil
		}
	}
	return nil, ErrOutside
}

// tile returns tile tx, ty from the cache, fetching it when allowed; got
// tells whether it came from the server.
func (s *Source) tile(ctx context.Context, ts *tileset, tx, ty int, cell float64) (*Grid, bool, error) {
	name := filepath.Join(s.CacheDir, ts.file(tx, ty, cell))
	b, err := os.ReadFile(name)
	got := false
	if errors.Is(err, os.ErrNotExist) {
		if !s.Fetch {
			return nil, false, fmt.Errorf("dem: %s tile %d,%d: %w", ts.name, tx, ty, ErrNotCached)
		}
		if b, err = s.fetch(ctx, ts, tx, ty, cell); err != nil {
			return nil, false, err
		}
		got = true
	} else if err != nil {
		return nil, false, err
	}
	g, err := DecodeGeoTIFF(b)
	if err != nil {
		return nil, false, fmt.Errorf("dem: %s tile %d,%d: %w", ts.name, tx, ty, err)
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

func (s *Source) fetch(ctx context.Context, ts *tileset, tx, ty int, cell float64) ([]byte, error) {
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = ts.endpoint
	}
	u := ts.url(endpoint, tx, ty, cell)
	client := s.Client
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
		if s.UserAgent != "" {
			req.Header.Set("User-Agent", s.UserAgent)
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
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ts.contentType {
			lastErr = fmt.Errorf("dem: %s tile %d,%d: %s %.200q", ts.name, tx, ty, resp.Status, b)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				break
			}
			continue
		}
		return b, nil
	}
	return nil, lastErr
}

func (s *Source) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
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

// Mosaic is a set of a source's tiles.
type Mosaic struct {
	tiles   map[[2]int]*Grid
	project func(lat, lon float64) (x, y float64)
}

// Elevation returns the ground height above the source's datum (NAP, NGF)
// at a WGS84 point; false where no tile covers it or the data has a hole.
func (m *Mosaic) Elevation(lat, lon float64) (float64, bool) {
	x, y := m.project(lat, lon)
	g := m.tiles[[2]int{int(math.Floor(x / tileM)), int(math.Floor(y / tileM))}]
	if g == nil {
		return 0, false
	}
	return g.At(x, y)
}
