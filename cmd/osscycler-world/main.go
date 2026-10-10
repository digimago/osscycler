// Command osscycler-world builds the 3D world of courses for renderers:
// terrain from a ground model (the Dutch AHN where the course lies in the
// Netherlands), the road on it, and a manifest, written as
// <out>/<course>/world.glb and world.json.
//
// Ground model tiles are fetched once and cached; -fetch=false builds from
// the cache alone. Courses outside every ground model's area, and the
// built-in test tracks, get terrain shaped from their own profile.
//
// Map data (OpenStreetMap) comes from the core's cache beside the courses,
// the same stretches the core fetches for a ride; missing stretches are
// fetched and cached for both.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/home"
	"github.com/digimago/osscycler/internal/scenery"
	"github.com/digimago/osscycler/internal/world"
	"github.com/digimago/osscycler/internal/worlds"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "osscycler-world:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		courses  = flag.String("courses", home.Courses(), "directory of .gpx courses")
		out      = flag.String("out", home.Worlds(), "directory for the worlds, one subdirectory per course")
		ground   = flag.String("dem", "auto", "ground model: auto (AHN inside the Netherlands), ahn, or none")
		cache    = flag.String("dem-cache", "", "ground model tile cache (default <courses>/.dem)")
		fetch    = flag.Bool("fetch", true, "fetch missing ground model tiles and map data; false uses the caches only")
		osm      = flag.Bool("osm", true, "use OpenStreetMap map data")
		osmCache = flag.String("osm-cache", "", "map data cache (default <courses>/.osm, the core's when it shares the courses directory)")
		osmURL   = flag.String("osm-url", scenery.DefaultOverpassURL, "Overpass API server for missing map data")
		demCell  = flag.Float64("dem-cell", 5, "ground model resolution to fetch, m")
		cell     = flag.Float64("cell", 5, "terrain grid spacing, m")
		chunk    = flag.Float64("chunk", 250, "terrain chunk size, m")
		corridor = flag.Float64("corridor", 300, "terrain reaches this far from the road, m")
		width    = flag.Float64("road-width", 5, "road width, m")
		maxHoles = flag.Float64("max-holes", 10, "fail when the check finds more holes than this within 40 m of the path, m²")
		maxOver  = flag.Float64("max-land-over-road", 1, "fail when the check finds more land over road than this within 40 m of the path, m²")
		maxOff   = flag.Float64("max-off-road", 1, "fail when the check finds more of the riding line than this with no road surface under it, m per km of course")
		check    = flag.String("check", "fail", "after a build: fail past -max-holes, -max-land-over-road or -max-off-road, or warn (the world is kept)")
		progress = flag.Bool("progress", false, "print each phase (\""+worlds.PhasePrefix+"…\") and the world's directory (\""+worlds.WorldPrefix+"…\"), for a program running this one")
		ifStale  = flag.Bool("if-stale", false, "build only when no current world is there (in -out or -prebuilt); a renderer asks this way")
		preview  = flag.Bool("preview", false, "with -if-stale: before building a world, build a preview to ride meanwhile (no ground model or map data, made-up countryside; seconds) and say where (\""+worlds.PreviewPrefix+"…\")")
		prebuilt = flag.String("prebuilt", "", "ready-built worlds to use with -if-stale, a list of directories (default: share/osscycler/worlds beside the program, worlds/ beside it, /usr/share/osscycler/worlds)")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: osscycler-world [flags] course-id...\n\nCourses are the included ones (test tracks, posbank) and the .gpx files in -courses (the file name is the ID).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *check != "fail" && *check != "warn" {
		return fmt.Errorf("-check %q: want fail or warn", *check)
	}
	switch *ground {
	case "auto", "ahn", "none":
	default:
		return fmt.Errorf("-dem %q: want auto, ahn or none", *ground)
	}
	if *osmCache == "" {
		*osmCache = filepath.Join(*courses, ".osm")
	}
	if *cache == "" {
		*cache = filepath.Join(*courses, ".dem")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *progress {
		// Run by a renderer: when it goes (quit, crashed), so does the
		// build, rather than running on for minutes with nobody to load it.
		go leaveWithParent(log)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	byID := map[string]*course.Course{}
	for _, c := range course.Included() {
		byID[c.ID] = c
	}
	cs, err := course.LoadDir(*courses)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn("some courses didn't load", "err", err)
	}
	for _, c := range cs {
		if _, builtin := byID[c.ID]; !builtin {
			byID[c.ID] = c
		}
	}

	generator := "osscycler-world " + version()
	userAgent := "osscycler/" + version() + " (+https://github.com/digimago/osscycler)"
	maps := scenery.NewStore(scenery.Config{
		CacheDir: *osmCache, Fetch: *fetch, Endpoint: *osmURL, UserAgent: userAgent, Log: log,
	}, nil)
	b := &worlds.Builder{
		Ground: *ground, DEMCache: *cache, DEMCellM: *demCell, Fetch: *fetch, UserAgent: userAgent, Log: log,
		Options: world.Options{CellM: *cell, ChunkM: *chunk, CorridorM: *corridor, RoadWidthM: *width, Generator: generator},
		Timing:  &worlds.Timing{File: filepath.Join(*out, ".timing.json")},
	}
	if *progress {
		// How far, and the time left: at most twice a second, and at each
		// change of phase.
		var mu sync.Mutex
		var last time.Time
		lastPhase := ""
		b.Progress = func(p worlds.Progress) {
			mu.Lock()
			defer mu.Unlock()
			if p.Phase == lastPhase && time.Since(last) < 500*time.Millisecond {
				return
			}
			last, lastPhase = time.Now(), p.Phase
			fmt.Println(worlds.FormatProgress(p))
		}
	}
	if *osm {
		b.Maps = maps
	}
	for _, id := range flag.Args() {
		c := byID[id]
		if c == nil {
			return fmt.Errorf("no course %q (included or in %s)", id, *courses)
		}
		if *ifStale {
			exe, _ := os.Executable()
			dirs := worlds.PrebuiltDirs(exe)
			if *prebuilt != "" {
				dirs = filepath.SplitList(*prebuilt)
			}
			if dir := worlds.Find(append([]string{*out}, dirs...), id, worlds.Stamp(c)); dir != "" {
				if *progress {
					fmt.Println(worlds.WorldPrefix + dir)
				}
				continue
			}
		}
		var phase func(string)
		if *progress {
			phase = func(p string) { fmt.Println(worlds.PhasePrefix + p) }
		}
		// The test tracks build in a second or two: no preview for them.
		if *ifStale && *preview && !c.Builtin {
			dir, err := buildPreview(ctx, b, c, *out, phase)
			if err != nil {
				log.Warn("no preview world", "course", id, "err", err)
			} else if *progress {
				fmt.Println(worlds.PreviewPrefix + dir)
			}
		}
		start := time.Now()
		res, err := b.Build(ctx, c, phase)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		w, mapInfo := res.World, res.MapInfo
		dir := filepath.Join(*out, id)
		if err := w.Write(dir); err != nil {
			return err
		}
		size := int64(0)
		if fi, err := os.Stat(filepath.Join(dir, world.ModelFile)); err == nil {
			size = fi.Size()
		}
		fmt.Printf("%s: %d terrain chunks (%s), %s, %d vertices, %d triangles, %d plants, %.1f MB, in %s → %s\n",
			id, w.Manifest.Terrain.Chunks, w.Manifest.Terrain.Source, mapInfo, w.Vertices, w.Triangles, w.Plants,
			float64(size)/1e6, time.Since(start).Round(100*time.Millisecond), dir)
		cs := time.Now()
		if phase != nil {
			phase(worlds.PhaseCheck)
		}
		r := w.Check()
		offPerKm := r.OffRoadM / math.Max(c.Distance/1000, 1e-3)
		fmt.Printf("%s: check within 40 m of the path: holes %.1f m²%s, land over road %.1f m²%s; riding line off road %.0f m (%.1f m/km)%s (%s)\n",
			id, r.HolesM2, worst(r.Holes), r.LandOverRoadM2, worst(r.LandOverRoad), r.OffRoadM, offPerKm, worst(r.OffRoad), time.Since(cs).Round(100*time.Millisecond))
		if r.HolesM2 > *maxHoles || r.LandOverRoadM2 > *maxOver || offPerKm > *maxOff {
			if *check != "warn" {
				return fmt.Errorf("%s: the check found more than -max-holes %g m², -max-land-over-road %g m² or -max-off-road %g m/km", id, *maxHoles, *maxOver, *maxOff)
			}
			log.Warn("the check found more than -max-holes, -max-land-over-road or -max-off-road", "course", id,
				"holes_m2", r.HolesM2, "land_over_road_m2", r.LandOverRoadM2, "off_road_m", r.OffRoadM, "off_road_m_per_km", offPerKm)
		}
		if *progress {
			fmt.Println(worlds.WorldPrefix + dir)
		}
	}
	return nil
}

// buildPreview builds course c's preview world, unless a current one is
// there; its directory.
func buildPreview(ctx context.Context, b *worlds.Builder, c *course.Course, out string, phase func(string)) (string, error) {
	dir := worlds.PreviewDir(out, c.ID)
	if worlds.ReadStamp(dir) == worlds.PreviewStamp(c) {
		return dir, nil
	}
	if phase != nil {
		phase(worlds.PhasePreview)
	}
	start := time.Now()
	pb := *b
	pb.Preview = true
	res, err := pb.Build(ctx, c, nil)
	if err != nil {
		return "", err
	}
	if err := res.World.Write(dir); err != nil {
		return "", err
	}
	fmt.Printf("%s: preview, %s, %d triangles, in %s → %s\n", c.ID, res.MapInfo, res.World.Triangles,
		time.Since(start).Round(100*time.Millisecond), dir)
	debug.FreeOSMemory() // the real build needs it more
	return dir, nil
}

// leaveWithParent exits when the program that started this one has gone
// (the process is handed to another parent then).
func leaveWithParent(log *slog.Logger) {
	parent := os.Getppid()
	for range time.Tick(2 * time.Second) {
		if os.Getppid() != parent {
			log.Info("the program that started the build has gone: stopping")
			os.Exit(1)
		}
	}
}

// worst names the worst few stretches of a check.
func worst(spots []world.CheckSpot) string {
	if len(spots) == 0 {
		return ""
	}
	out := " (worst"
	for i, s := range spots[:min(4, len(spots))] {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(" %.1f km %.1f", s.D/1000, s.Area)
	}
	return out + ")"
}

func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 && (v == "" || v == "(devel)") {
			v = s.Value[:12]
		}
	}
	return v
}
