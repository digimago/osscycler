// Package worlds builds courses' 3D worlds and keeps them (plan step 10):
// the build shared by osscycler-world and the core, and the core's store
// of built worlds, which builds a course's world when it is first ridden
// or asked for, and hands it to renderers over the API.
package worlds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/dem"
	"github.com/digimago/osscycler/internal/scenery"
	"github.com/digimago/osscycler/internal/world"
)

// BuilderVersion goes up when a change to the builder should rebuild the
// worlds riders have (a fix they'd ride into, not every commit).
const BuilderVersion = 5

// Builder builds worlds.
type Builder struct {
	// Ground is "auto" (AHN inside the Netherlands, else the profile),
	// "ahn" or "none".
	Ground   string
	DEMCache string  // AHN tile cache
	DEMCellM float64 // AHN resolution to fetch, m (default 5)
	Fetch    bool    // fetch what the caches lack
	// Maps is the map data (OpenStreetMap); nil builds without.
	Maps *scenery.Store
	// Options are the world's settings (cell, chunk, corridor, road width,
	// generator); the rest is filled in per course.
	Options   world.Options
	UserAgent string
	Log       *slog.Logger
	// Preview builds a world to ride while the real one is built: no
	// ground model, no map data, made-up countryside along the route
	// (scenery.Countryside), in seconds. Its stamp is PreviewStamp, never
	// a world's own, so it is never taken for the real one.
	Preview bool
	// Progress, when set, hears how far a build is with an estimate of the
	// time left (Progress); Timing keeps this machine's build rate for it.
	Progress func(Progress)
	Timing   *Timing
}

// Phases a build goes through, for its status.
const (
	PhaseGround = "ground model"
	PhaseMaps   = "map data"
	PhaseBuild  = "building"
	PhaseCheck  = "checking"
	// PhasePreview: building a preview world first (osscycler-world
	// -preview).
	PhasePreview = "preview"
)

// Result is a built world with what went into it.
type Result struct {
	World   *world.World
	MapInfo string // "1234 map elements", "no map data", ...
}

// Build builds course c's world; phase reports progress.
func (b *Builder) Build(ctx context.Context, c *course.Course, phase func(string)) (Result, error) {
	if phase == nil {
		phase = func(string) {}
	}
	log := b.Log
	if log == nil {
		log = slog.Default()
	}
	o := b.Options
	o.Attribution = append([]string(nil), o.Attribution...)
	o.Stamp = Stamp(c)
	if b.Preview {
		o.Stamp = PreviewStamp(c)
	}
	// Real places are built in a regional frame (Anchor), so routes in one
	// area lay their grids (terrain, chunks, plants) alike and build the same
	// world where they meet. The test tracks keep their own: their
	// landscapes are made up and never shared.
	if !c.Builtin {
		c = c.InFrame(Anchor(c.Position(0)))
	}
	tr := &tracker{report: b.Progress, km: c.Distance / 1000, buildSPerKm: b.Timing.perKm(), fetchS: stretchFetchS}
	if b.Maps != nil && !c.Builtin && !b.Preview && b.Fetch && b.Progress != nil {
		tr.missing = b.Maps.Missing(c)
	}
	enter := func(p string) {
		phase(p)
		tr.start(p)
	}
	if b.Ground != "none" && !c.Builtin && !b.Preview {
		enter(PhaseGround)
		cell := b.DEMCellM
		if cell <= 0 {
			cell = 5
		}
		a := &dem.AHN{CacheDir: b.DEMCache, Fetch: b.Fetch, CellM: cell, Log: log, UserAgent: b.UserAgent,
			Progress: func(done, total int) { tr.step(done, total, float64(done)/float64(max(1, total))) }}
		var pts []dem.LatLon
		for d := 0.0; d <= c.Distance; d += 20 {
			lat, lon := c.Position(d)
			pts = append(pts, dem.LatLon{Lat: lat, Lon: lon})
		}
		corridor, chunk := o.CorridorM, o.ChunkM
		if corridor <= 0 {
			corridor = 300
		}
		if chunk <= 0 {
			chunk = 250
		}
		// The corridor plus a chunk: every terrain vertex is inside.
		m, err := a.Load(ctx, pts, corridor+1.5*chunk)
		switch {
		case errors.Is(err, dem.ErrOutside) && b.Ground == "auto":
			log.Info("no ground model for this area: terrain from the course profile", "course", c.ID)
		case err != nil:
			return Result{}, err
		default:
			o.Elevation = m.Elevation
			o.TerrainSource = fmt.Sprintf("AHN DTM, %g m", cell)
			o.Attribution = append(o.Attribution, dem.AHNAttribution)
		}
	}
	mapInfo := "no map data"
	if b.Maps != nil && !c.Builtin && !b.Preview {
		enter(PhaseMaps)
		// A stretch missing is no reason to stop; the world shows grass
		// there.
		mapsAt, fetched := time.Now(), 0
		d, err := b.Maps.LoadProgress(ctx, c, func(done, total, toFetch int) {
			tr.mu.Lock()
			if toFetch < tr.missing { // one more fetched: the measured time per stretch
				fetched += tr.missing - toFetch
				tr.fetchS = time.Since(mapsAt).Seconds() / float64(fetched)
			}
			tr.missing = toFetch
			tr.mu.Unlock()
			tr.step(done, total, float64(done)/float64(max(1, total)))
		})
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			log.Warn("map data incomplete", "course", c.ID, "err", err)
		}
		if len(d.Elements) > 0 {
			o.Land = scenery.NewLandMap(c, d)
			o.Scenery = scenery.Build(c, d)
			o.Buildings = scenery.Footprints(c, d)
			o.Attribution = append(o.Attribution, scenery.Attribution+" (ODbL)")
		}
		mapInfo = fmt.Sprintf("%d map elements", len(d.Elements))
	}
	if b.Preview && !c.Builtin {
		t := scenery.Countryside(c)
		o.Land = scenery.NewLandMap(c, t.Data)
		o.Scenery = scenery.Build(c, t.Data)
		o.Buildings = scenery.Footprints(c, t.Data)
		mapInfo = fmt.Sprintf("%d made-up map elements (preview)", len(t.Data.Elements))
	}
	// How far the sea is (trees grow lower towards it): the coastline
	// basemap, fetched by tile once and shared by every course; a preview
	// takes only what is cached, so it stays quick.
	if b.Maps != nil && !c.Builtin {
		co, err := b.Maps.Coast(ctx, c, !b.Preview)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			log.Warn("coastline incomplete", "course", c.ID, "err", err)
		}
		o.SeaDistance = co.Distance
	}
	if t := scenery.TrackScenery(c); t != nil {
		// A test track's own made-up surroundings.
		o.Land = scenery.NewLandMap(c, t.Data)
		o.Scenery = scenery.Build(c, t.Data)
		if t.Roads != nil {
			o.Scenery.Roads = t.Roads
		}
		o.Buildings = scenery.Footprints(c, t.Data)
		o.Lawns = t.Lawns
		o.Fences = t.Fences
		mapInfo = fmt.Sprintf("%d made-up map elements", len(t.Data.Elements))
	}
	if c.Credit != "" {
		o.Attribution = append(o.Attribution, "route: "+c.Credit)
	}
	enter(PhaseBuild)
	o.Progress = func(f float64) { tr.step(0, 0, f) }
	buildAt := time.Now()
	w, err := world.Build(c, o)
	if err == nil && !b.Preview && c.Distance > 1000 {
		b.Timing.learn(time.Since(buildAt).Seconds() / (c.Distance / 1000))
	}
	if err != nil {
		return Result{}, err
	}
	return Result{World: w, MapInfo: mapInfo}, nil
}

// Anchor is the regional frame's origin for a course starting at lat, lon:
// the nearest whole degree of each (owner, 2026-10-10: routes sharing roads
// build the same world there; a route sharing 7 km with the Posbank Loop
// had 10 % of its plants where the loop's stood, every grid being laid
// from the course's own start). Courses starting within the same degree
// square round it share one; positions stay within about 70 km of it for
// rides of a day, as float32 needs.
func Anchor(lat, lon float64) (float64, float64) {
	return math.Round(lat), math.Round(lon)
}

// PreviewStamp is the stamp of course c's preview world.
func PreviewStamp(c *course.Course) string { return "preview-" + Stamp(c) }

// Stamp identifies what course c's world is built from: the course as
// loaded (its line every 10 m, its profile, its name and length) and the
// builder's version. A GPX edited or a builder that builds differently
// gives another stamp.
func Stamp(c *course.Course) string {
	h := sha256.New()
	put := func(v float64) { h.Write(strconv.AppendFloat(nil, math.Round(v*1e6)/1e6, 'g', -1, 64)) }
	fmt.Fprintf(h, "osscycler world %d\x00%s\x00%s\x00%t\x00", BuilderVersion, c.ID, c.Name, c.Loop)
	put(c.Distance)
	for d := 0.0; d <= c.Distance; d += 10 {
		lat, lon := c.Position(d)
		put(lat)
		put(lon)
		ele, _ := c.At(d)
		put(ele)
	}
	return fmt.Sprintf("%d-%s", BuilderVersion, hex.EncodeToString(h.Sum(nil))[:16])
}
