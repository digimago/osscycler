package scenery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

// Bridges as structures (owner, 2026-11-17: the Erasmusbrug wanted its
// pylon and stays, not houses on its deck): the map's outline of a bridge
// (man_made=bridge) says what kind it is (bridge:structure) and covers all
// of it, carriageways, tram tracks and cycle paths between; its supports
// (bridge:support) say where it stands. Both by tile of bridgeTileDeg,
// asked for once when a course comes within bridgeReachM of a tile and
// shared by every course, as the coastline is: kept apart from the map
// data along the route, so adding them left every course's cache valid.
const (
	bridgeTileDeg = 0.05
	bridgeReachM  = 300.0
	bridgeMaxAge  = 365 * 24 * time.Hour
)

// Bridge is a bridge's outline as the map draws it, in metres east and
// north of the course's start, counter-clockwise.
type Bridge struct {
	ID        int64
	Name      string
	Structure string  // bridge:structure: beam, arch, cable-stayed, suspension, ...; "" not said
	HeightM   float64 // height (its pylons' tops, above the ground); 0 not said
	Outline   [][2]float64
}

// bridgeTile is a cached tile: the elements as the server gave them.
type bridgeTile struct {
	Fetched  time.Time `json:"fetched"`
	Elements []Element `json:"elements"`
}

// BridgeData is the map's bridge outlines and supports within bridgeReachM
// of course c: from the cache, tiles missing or old fetched when fetching
// is on and fetch is true. A tile that can't be had is left out (an old
// copy is used if there is one), and the error says which.
func (s *Store) BridgeData(ctx context.Context, c *course.Course, fetch bool) (*Data, error) {
	var tiles []tile
	seen := map[tile]bool{}
	for d := 0.0; ; d += 100 {
		lat, lon := c.Position(math.Min(d, c.Distance))
		for _, t := range tilesNear(lat, lon, bridgeReachM, bridgeTileDeg, bridgeTileDeg) {
			if !seen[t] {
				seen[t] = true
				tiles = append(tiles, t)
			}
		}
		if d >= c.Distance {
			break
		}
	}
	slices.SortFunc(tiles, func(a, b tile) int {
		if a.j != b.j {
			return a.j - b.j
		}
		return a.i - b.i
	})
	out := &Data{}
	ids := map[string]bool{}
	var errs []error
	for _, t := range tiles {
		bt, err := s.bridgeTile(ctx, t, fetch)
		if err != nil {
			errs = append(errs, fmt.Errorf("bridge tile %d,%d: %w", t.j, t.i, err))
		}
		if bt == nil {
			continue
		}
		for _, el := range bt.Elements {
			if k := el.Type + strconv.FormatInt(el.ID, 10); !ids[k] { // a bridge across a tile's edge comes with both
				ids[k] = true
				out.Elements = append(out.Elements, el)
			}
		}
	}
	return out, errors.Join(errs...)
}

func (s *Store) bridgePath(t tile) string {
	return filepath.Join(s.cfg.CacheDir, fmt.Sprintf("bridges-%d_%d.json", t.j, t.i))
}

// bridgeTile is tile t of bridgeTileDeg.
func (s *Store) bridgeTile(ctx context.Context, t tile, fetch bool) (*bridgeTile, error) {
	var cached *bridgeTile
	if raw, err := os.ReadFile(s.bridgePath(t)); err == nil {
		var bt bridgeTile
		if json.Unmarshal(raw, &bt) == nil {
			cached = &bt
		}
	}
	if cached != nil && time.Since(cached.Fetched) < bridgeMaxAge {
		return cached, nil
	}
	if !fetch || !s.cfg.Fetch {
		if cached == nil {
			return nil, errors.New("not cached, and fetching is off")
		}
		return cached, nil
	}
	box := fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", float64(t.j)*bridgeTileDeg, float64(t.i)*bridgeTileDeg, float64(t.j+1)*bridgeTileDeg, float64(t.i+1)*bridgeTileDeg)
	query := "[out:json][timeout:90][maxsize:67108864];\n(\n" +
		fmt.Sprintf("  way[\"man_made\"=\"bridge\"](%s);\n  relation[\"man_made\"=\"bridge\"](%s);\n  way[\"bridge:support\"](%s);\n", box, box, box) +
		");\nout geom;\n"
	raw, err := s.fetch(ctx, query)
	if err != nil {
		return cached, err
	}
	d, err := Parse(raw)
	if err != nil {
		return cached, err
	}
	bt := &bridgeTile{Fetched: time.Now().UTC(), Elements: d.Elements}
	if out, err := json.Marshal(bt); err == nil {
		if err := os.MkdirAll(s.cfg.CacheDir, 0o755); err == nil {
			tmp := s.bridgePath(t) + ".tmp"
			if os.WriteFile(tmp, out, 0o644) == nil {
				_ = os.Rename(tmp, s.bridgePath(t))
			}
		}
	}
	return bt, nil
}

// Bridges are the bridge outlines near the route.
func Bridges(c *course.Course, d *Data) []Bridge {
	near := nearRoute(c, footprintRangeM)
	seen := map[int64]bool{}
	var out []Bridge
	for _, el := range d.Elements {
		t := el.Tags
		if t["man_made"] != "bridge" || seen[el.ID] {
			continue
		}
		pts, _, _, _ := outlineOf(c, el)
		if pts == nil {
			continue
		}
		close := false
		for _, q := range pts {
			if near(q[0], q[1]) {
				close = true
				break
			}
		}
		if !close {
			continue
		}
		seen[el.ID] = true
		b := Bridge{ID: el.ID, Name: t["name"], Structure: t["bridge:structure"], Outline: pts}
		if h, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(t["height"]), " m"), 64); err == nil && h > 0 {
			b.HeightM = h
		}
		out = append(out, b)
	}
	return out
}

// Merge is the elements of a and b, each once (by type and ID).
func Merge(a, b *Data) *Data {
	out := &Data{}
	ids := map[string]bool{}
	for _, d := range []*Data{a, b} {
		if d == nil {
			continue
		}
		for _, el := range d.Elements {
			if k := el.Type + strconv.FormatInt(el.ID, 10); !ids[k] {
				ids[k] = true
				out.Elements = append(out.Elements, el)
			}
		}
	}
	return out
}
