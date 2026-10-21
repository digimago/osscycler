package scenery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

// BD TOPO: France's buildings from IGN (owner, 2026-10-15: build French
// worlds from French sources), with what OpenStreetMap's buildings there
// lack: a height measured by lidar for nearly every one (La Bresse: 99 %),
// the ridge's height, and for about half the walls' and roof's materials.
// Fetched from the Géoplateforme's WFS by tile, cached as GeoJSON.
const (
	// BDTopoURL is the Géoplateforme's WFS.
	BDTopoURL = "https://data.geopf.fr/wfs/ows"
	// BDTopoAttribution credits it (Licence Ouverte 2.0: the source named).
	BDTopoAttribution = "buildings: IGN BD TOPO, Licence Ouverte 2.0 (Etalab)"

	bdTopoLayer = "BDTOPO_V3:batiment"
	bdTopoTileD = 0.02 // tiles of this many degrees square (about 2.2 × 1.5 km)
	bdTopoPage  = 5000
)

// ErrNoBDTopo: a tile isn't cached and fetching is off.
var ErrNoBDTopo = errors.New("scenery: BD TOPO tile not cached and fetching is off")

// BDTopo fetches and caches BD TOPO's buildings.
type BDTopo struct {
	CacheDir  string
	Fetch     bool
	Endpoint  string // default BDTopoURL
	UserAgent string
	Client    *http.Client
	Log       *slog.Logger
}

type bdFeature struct {
	ID       string `json:"id"`
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
	Props struct {
		Hauteur *float64 `json:"hauteur"`
		SolMin  *float64 `json:"altitude_minimale_sol"`
		ToitMax *float64 `json:"altitude_maximale_toit"`
		Etages  *int     `json:"nombre_d_etages"`
		Murs    *string  `json:"materiaux_des_murs"`
		Toiture *string  `json:"materiaux_de_la_toiture"`
		Usage   *string  `json:"usage_1"`
		Nature  *string  `json:"nature"`
		Legere  *bool    `json:"construction_legere"`
		Etat    *string  `json:"etat_de_l_objet"`
		Cleabs  string   `json:"cleabs"`
	} `json:"properties"`
}

type bdCollection struct {
	Features []bdFeature `json:"features"`
}

// Footprints are BD TOPO's buildings within footprintRangeM of course c's
// route, as Footprints gives OpenStreetMap's.
func (b *BDTopo) Footprints(ctx context.Context, c *course.Course) ([]Footprint, error) {
	tiles := map[[2]int]bool{}
	for d := 0.0; d <= c.Distance; d += 20 {
		lat, lon := c.Position(d)
		for _, dl := range []float64{-0.002, 0.002} {
			for _, dk := range []float64{-0.003, 0.003} {
				tiles[[2]int{int(math.Floor((lat + dl) / bdTopoTileD)), int(math.Floor((lon + dk) / bdTopoTileD))}] = true
			}
		}
	}
	keys := make([][2]int, 0, len(tiles))
	for k := range tiles {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	near := nearRoute(c, footprintRangeM)
	seen := map[string]bool{}
	var out []Footprint
	fetched := 0
	for _, k := range keys {
		fs, got, err := b.tile(ctx, k)
		if err != nil {
			return nil, err
		}
		if got {
			fetched++
		}
		for _, f := range fs {
			if seen[f.ID] {
				continue // a building across two tiles
			}
			seen[f.ID] = true
			if fp, ok := bdFootprint(c, f, near); ok {
				out = append(out, fp)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	b.log().Info("BD TOPO buildings", "tiles", len(keys), "fetched", fetched, "buildings", len(out))
	return out, nil
}

// tile reads tile k (latitude, longitude in bdTopoTileD steps) from the
// cache, fetching it when allowed.
func (b *BDTopo) tile(ctx context.Context, k [2]int) ([]bdFeature, bool, error) {
	name := filepath.Join(b.CacheDir, fmt.Sprintf("bdtopo-bati-%d-%d.json", k[0], k[1]))
	raw, err := os.ReadFile(name)
	got := false
	if errors.Is(err, os.ErrNotExist) {
		if !b.Fetch {
			return nil, false, fmt.Errorf("%w: %d,%d", ErrNoBDTopo, k[0], k[1])
		}
		var all []bdFeature
		for start := 0; ; start += bdTopoPage {
			page, err := b.fetch(ctx, k, start)
			if err != nil {
				return nil, false, err
			}
			all = append(all, page...)
			if len(page) < bdTopoPage {
				break
			}
		}
		if raw, err = json.Marshal(bdCollection{Features: all}); err != nil {
			return nil, false, err
		}
		if err := os.MkdirAll(b.CacheDir, 0o755); err != nil {
			return nil, false, err
		}
		tmp := name + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			return nil, false, err
		}
		if err := os.Rename(tmp, name); err != nil {
			return nil, false, err
		}
		got = true
	} else if err != nil {
		return nil, false, err
	}
	var col bdCollection
	if err := json.Unmarshal(raw, &col); err != nil {
		return nil, false, fmt.Errorf("scenery: BD TOPO tile %d,%d: %w", k[0], k[1], err)
	}
	return col.Features, got, nil
}

func (b *BDTopo) fetch(ctx context.Context, k [2]int, start int) ([]bdFeature, error) {
	endpoint := b.Endpoint
	if endpoint == "" {
		endpoint = BDTopoURL
	}
	lat0, lon0 := float64(k[0])*bdTopoTileD, float64(k[1])*bdTopoTileD
	q := url.Values{
		"SERVICE":      {"WFS"},
		"VERSION":      {"2.0.0"},
		"REQUEST":      {"GetFeature"},
		"TYPENAMES":    {bdTopoLayer},
		"OUTPUTFORMAT": {"application/json"},
		"COUNT":        {fmt.Sprint(bdTopoPage)},
		"STARTINDEX":   {fmt.Sprint(start)},
		"SORTBY":       {"cleabs"},
		"BBOX":         {fmt.Sprintf("%.4f,%.4f,%.4f,%.4f,urn:ogc:def:crs:EPSG::4326", lat0, lon0, lat0+bdTopoTileD, lon0+bdTopoTileD)},
		"PROPERTYNAME": {"cleabs,geometrie,hauteur,altitude_minimale_sol,altitude_maximale_toit,nombre_d_etages,materiaux_des_murs,materiaux_de_la_toiture,usage_1,nature,construction_legere,etat_de_l_objet"},
	}
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 3 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if b.UserAgent != "" {
			req.Header.Set("User-Agent", b.UserAgent)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("scenery: BD TOPO %d,%d: %s %.200q", k[0], k[1], resp.Status, body)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				break
			}
			continue
		}
		var col bdCollection
		if err := json.Unmarshal(body, &col); err != nil {
			lastErr = fmt.Errorf("scenery: BD TOPO %d,%d: %w", k[0], k[1], err)
			continue
		}
		return col.Features, nil
	}
	return nil, lastErr
}

func (b *BDTopo) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.New(slog.DiscardHandler)
}

// bdFootprint is feature f as a footprint, if it is a standing building
// near the route.
func bdFootprint(c *course.Course, f bdFeature, near func(x, y float64) bool) (Footprint, bool) {
	p := f.Props
	if p.Etat != nil && *p.Etat != "" && !strings.HasPrefix(*p.Etat, "En service") {
		return Footprint{}, false // planned, under construction, ruined
	}
	var ring [][]float64
	switch f.Geometry.Type {
	case "MultiPolygon":
		var mp [][][][]float64
		if json.Unmarshal(f.Geometry.Coordinates, &mp) != nil {
			return Footprint{}, false
		}
		for _, poly := range mp { // the largest polygon's outer ring
			if len(poly) > 0 && len(poly[0]) > len(ring) {
				ring = poly[0]
			}
		}
	case "Polygon":
		var poly [][][]float64
		if json.Unmarshal(f.Geometry.Coordinates, &poly) != nil || len(poly) == 0 {
			return Footprint{}, false
		}
		ring = poly[0]
	}
	if len(ring) < 4 {
		return Footprint{}, false
	}
	var pts [][2]float64
	for k, q := range ring {
		if len(q) < 2 || k == len(ring)-1 && q[0] == ring[0][0] && q[1] == ring[0][1] {
			continue
		}
		x, y := c.Project(q[1], q[0])
		pts = append(pts, [2]float64{x, y})
	}
	if len(pts) < 3 {
		return Footprint{}, false
	}
	area, per, cx, cy := 0.0, 0.0, 0.0, 0.0
	for i := range pts {
		j := (i + 1) % len(pts)
		area += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
		per += math.Hypot(pts[j][0]-pts[i][0], pts[j][1]-pts[i][1])
		cx, cy = cx+pts[i][0]/float64(len(pts)), cy+pts[i][1]/float64(len(pts))
	}
	if area < 0 {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
		area = -area
	}
	area /= 2
	if area < footprintMinM2 || !near(cx, cy) {
		return Footprint{}, false
	}
	h := fnv.New64a()
	h.Write([]byte(p.Cleabs))
	fp := Footprint{ID: int64(h.Sum64() >> 1), Outline: pts, FromLowest: true}

	usage, nature := deref(p.Usage), deref(p.Nature)
	switch {
	case usage == "Annexe" || strings.Contains(nature, "agricole") || p.Legere != nil && *p.Legere:
		fp.Kind = KindBarn
	case usage == "Industriel" || usage == "Commercial et services" && area > 400 || p.Etages != nil && *p.Etages >= 4:
		fp.Kind = KindFlat
	default:
		fp.Kind = KindHouse
	}
	// Heights: the eave measured (hauteur), the ridge measured (the roof's
	// highest point over the lowest ground). The lidar's roof spans come
	// out low for pitched roofs (0.4-1.7 m on La Bresse's houses), so a
	// pitched roof is at least as high as a 40° pitch over the house's
	// narrow side would make it, under the measured ridge (the eave comes
	// down for it).
	eave := 0.0
	if p.Hauteur != nil {
		eave = *p.Hauteur
	}
	total := eave
	if p.SolMin != nil && p.ToitMax != nil && *p.ToitMax-*p.SolMin > total {
		total = *p.ToitMax - *p.SolMin
	}
	if eave < 2 {
		// No measure: from the storeys, else as OpenStreetMap's.
		_, guess := buildingShape(map[string]string{}, area)
		eave = guess * 0.7
		if p.Etages != nil && *p.Etages > 0 {
			eave = 3*float64(*p.Etages) - 0.5
		}
		total = math.Max(total, eave*1.4)
	}
	// The narrow side of the footprint's rectangle (P/2 = w + l, A = w l).
	half := per / 2
	narrow := (half - math.Sqrt(math.Max(0, half*half-4*area))) / 2
	// The lidar sees trees over small buildings (a shed's roof at 26 m, a
	// one-storey house's eave at 14.9 m in La Bresse): heights are capped
	// by what the building can be, about 3 m a storey, sheds 5 m, small
	// houses four storeys, a roof no steeper than 50° over the narrow side.
	switch {
	case p.Etages != nil && *p.Etages > 0:
		eave = math.Min(eave, 3*float64(*p.Etages)+1.5)
	case fp.Kind == KindBarn:
		eave = math.Min(eave, 5)
	case area < 300:
		eave = math.Min(eave, 12)
	}
	maxRoof := math.Min(narrow/2*math.Tan(50*math.Pi/180), 8)
	total = math.Min(total, eave+maxRoof)
	pitched := fp.Kind != KindFlat || total-eave > 1.5
	if pitched {
		roof := math.Max(total-eave, math.Min(narrow/2*math.Tan(40*math.Pi/180), 7))
		roof = math.Max(1.5, roof)
		fp.RoofM = roof
		fp.WallM = math.Max(2.2, total-roof) // the ridge as measured, the eave lower for the pitch
	} else {
		fp.WallM = math.Max(2.5, total)
	}
	fp.WallColour, fp.RoofColour = bdColours(deref(p.Murs), deref(p.Toiture), uint64(fp.ID))
	return fp, true
}

// bdColours are walls and roof from BD TOPO's material codes (the land
// register's: a digit per material, the main one first; 0 unknown): only
// where the material decides the colour (stone, brick, wood; slate, zinc),
// "" otherwise, for the style's palette (render, tiles).
func bdColours(walls, roof string, seed uint64) (string, string) {
	first := func(code string) byte {
		for i := 0; i < len(code); i++ {
			if code[i] != '0' {
				return code[i]
			}
		}
		return '0'
	}
	pick := func(cs ...string) string {
		return cs[seed%uint64(len(cs))]
	}
	var w, r string
	switch first(walls) {
	case '1', '2': // stone, millstone: the Vosges' grey granite, pink sandstone
		w = pick("#a69e92", "#b49a86", "#9c8f80", "#b8a48e")
	case '4': // brick
		w = pick("#9a5a44", "#8a4e3c")
	case '6': // wood: timber cladding
		w = pick("#6e5236", "#7a5c3c", "#5e4630")
	}
	switch first(roof) {
	case '2': // slate
		r = pick("#4a4c52", "#3e4046")
	case '3': // zinc, aluminium, steel sheet
		r = pick("#8a9096", "#7a8086", "#5a6066")
	case '4': // concrete
		r = "#6e6c68"
	}
	return w, r
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
