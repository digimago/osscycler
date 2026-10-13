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
	"strings"
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
		maxStep  = flag.Float64("max-path-step", 3, "fail when the rider's height changes more than this between two points of the path (5 m apart), m")
		check    = flag.String("check", "fail", "after a build: fail past -max-holes, -max-land-over-road, -max-off-road or -max-path-step, or warn (the world is kept)")
		progress = flag.Bool("progress", false, "print each phase (\""+worlds.PhasePrefix+"…\") and the world's directory (\""+worlds.WorldPrefix+"…\"), for a program running this one")
		ifCached = flag.Bool("if-cached", false, "build only from what the caches hold (implies -fetch=false): a course whose ground model, map data or coastline isn't all cached is skipped, saying so (\""+worlds.SkippedPrefix+"…\"); a renderer building worlds ahead of rides asks this way")
		ifStale  = flag.Bool("if-stale", false, "build only when no current world is there (in -out or -prebuilt); a renderer asks this way")
		preview  = flag.Bool("preview", false, "with -if-stale: before building a world, build a preview to ride meanwhile (no ground model or map data, made-up countryside; seconds) and say where (\""+worlds.PreviewPrefix+"…\")")
		engine   = flag.String("builder", envOr("OSSCYCLER_WORLD_BUILDER", world.DefaultEngine), "world builder: graph (the road network first; the default), classic (the terrain first; the fallback, built instead when the graph builder fails or its world fails the check; worlds in <out>/.classic/), or ab to build both and compare them ($OSSCYCLER_WORLD_BUILDER sets the default)")
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
	if _, ok := world.Engines[*engine]; !ok && *engine != "ab" {
		return fmt.Errorf("-builder %q: want one of %v, or ab", *engine, world.EngineNames())
	}
	if *check != "fail" && *check != "warn" {
		return fmt.Errorf("-check %q: want fail or warn", *check)
	}
	switch *ground {
	case "auto", "ahn", "none":
	default:
		return fmt.Errorf("-dem %q: want auto, ahn or none", *ground)
	}
	if *ifCached {
		*fetch = false
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
		Options:  world.Options{CellM: *cell, ChunkM: *chunk, CorridorM: *corridor, RoadWidthM: *width, Generator: generator},
		Timing:   &worlds.Timing{File: filepath.Join(*out, ".timing.json")},
		Complete: *ifCached,
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
	engines := []string{*engine}
	if *engine == "ab" {
		engines = []string{world.EngineClassic, world.EngineGraph}
	}
	for _, id := range flag.Args() {
		c := byID[id]
		if c == nil {
			return fmt.Errorf("no course %q (included or in %s)", id, *courses)
		}
		var phase func(string)
		if *progress {
			phase = func(p string) { fmt.Println(worlds.PhasePrefix + p) }
		}
		var reports []report
		for k, eng := range engines {
			if *ifStale {
				dirs := []string{filepath.Dir(worlds.Dir(*out, id, eng))}
				if eng == world.DefaultEngine {
					// Ready-built worlds are the default builder's (a
					// stamp tells an older release's apart).
					exe, _ := os.Executable()
					pre := worlds.PrebuiltDirs(exe)
					if *prebuilt != "" {
						pre = filepath.SplitList(*prebuilt)
					}
					dirs = append(dirs, pre...)
				}
				if dir := worlds.Find(dirs, id, worlds.EngineStamp(c, eng)); dir != "" {
					if *progress {
						fmt.Println(worlds.WorldPrefix + dir)
					}
					continue
				}
			}
			// The test tracks build in a second or two: no preview for them.
			if k == 0 && *ifStale && *preview && !c.Builtin {
				dir, err := buildPreview(ctx, b, c, *out, phase)
				if err != nil {
					log.Warn("no preview world", "course", id, "err", err)
				} else if *progress {
					fmt.Println(worlds.PreviewPrefix + dir)
				}
			}
			eb := *b
			eb.Engine = eng
			r, err := buildOne(ctx, &eb, c, worlds.Dir(*out, id, eng), phase)
			if errors.Is(err, worlds.ErrIncomplete) {
				// Built when it is ridden, fetching what it lacks.
				fmt.Println(worlds.SkippedPrefix + strings.ReplaceAll(err.Error(), "\n", "; "))
				continue
			}
			if err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			failed := func(chk world.CheckResult) bool {
				offPerKm := chk.OffRoadM / math.Max(c.Distance/1000, 1e-3)
				return chk.HolesM2 > *maxHoles || chk.LandOverRoadM2 > *maxOver || offPerKm > *maxOff || chk.StepM > *maxStep
			}
			if failed(r.check) && *check != "warn" && *engine != "ab" && eng != world.EngineClassic {
				// The classic builder as the fallback: its world in the
				// place of the one asked for, with that one's stamp.
				log.Warn("the check found more than the limits; building with the classic builder instead", "course", id, "builder", eng,
					"holes_m2", r.check.HolesM2, "land_over_road_m2", r.check.LandOverRoadM2, "off_road_m", r.check.OffRoadM, "path_step_m", r.check.StepM)
				eb.Engine, eb.Stamp = world.EngineClassic, worlds.EngineStamp(c, eng)
				if r, err = buildOne(ctx, &eb, c, worlds.Dir(*out, id, eng), phase); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
			}
			reports = append(reports, r)
			chk := r.check
			if failed(chk) {
				if *check != "warn" && *engine != "ab" {
					return fmt.Errorf("%s: the check found more than -max-holes %g m², -max-land-over-road %g m², -max-off-road %g m/km or -max-path-step %g m", id, *maxHoles, *maxOver, *maxOff, *maxStep)
				}
				log.Warn("the check found more than -max-holes, -max-land-over-road, -max-off-road or -max-path-step", "course", id, "builder", r.engine,
					"holes_m2", chk.HolesM2, "land_over_road_m2", chk.LandOverRoadM2, "off_road_m", chk.OffRoadM, "off_road_m_per_km", chk.OffRoadM/math.Max(c.Distance/1000, 1e-3), "path_step_m", chk.StepM)
			}
			if *progress {
				fmt.Println(worlds.WorldPrefix + r.dir)
			}
		}
		if len(reports) == 2 {
			printAB(c, reports[0], reports[1])
		}
	}
	return nil
}

// report is what one build made: where, how big, how long, and its check.
type report struct {
	engine, dir string
	w           *world.World
	sizeMB      float64
	build       time.Duration
	check       world.CheckResult
}

// buildOne builds course c's world with b into dir, writes it, prints
// what it made and checks it.
func buildOne(ctx context.Context, b *worlds.Builder, c *course.Course, dir string, phase func(string)) (report, error) {
	start := time.Now()
	res, err := b.Build(ctx, c, phase)
	if err != nil {
		return report{}, err
	}
	w := res.World
	if err := w.Write(dir); err != nil {
		return report{}, err
	}
	r := report{engine: b.Engine, dir: dir, w: w, build: time.Since(start)}
	if r.engine == "" {
		r.engine = world.DefaultEngine
	}
	if res.FellBack {
		r.engine = world.EngineClassic + " (fallback)"
	}
	if fi, err := os.Stat(filepath.Join(dir, world.ModelFile)); err == nil {
		r.sizeMB = float64(fi.Size()) / 1e6
	}
	fmt.Printf("%s: [%s] %d terrain chunks (%s), %s, %d vertices, %d triangles, %d plants, %.1f MB, in %s → %s\n",
		c.ID, r.engine, w.Manifest.Terrain.Chunks, w.Manifest.Terrain.Source, res.MapInfo, w.Vertices, w.Triangles, w.Plants,
		r.sizeMB, r.build.Round(100*time.Millisecond), dir)
	cs := time.Now()
	if phase != nil {
		phase(worlds.PhaseCheck)
	}
	r.check = w.Check()
	chk := r.check
	offPerKm := chk.OffRoadM / math.Max(c.Distance/1000, 1e-3)
	fmt.Printf("%s: [%s] check within 40 m of the path: holes %.1f m²%s, land over road %.1f m²%s; riding line off road %.0f m (%.1f m/km)%s; path height steps at most %.2f m%s; sharpest turn %.0f° per step%s, pace at most %.2f× (%s)\n",
		c.ID, r.engine, chk.HolesM2, worst(chk.Holes), chk.LandOverRoadM2, worst(chk.LandOverRoad), chk.OffRoadM, offPerKm, worst(chk.OffRoad),
		chk.StepM, worst(chk.Steps), chk.TurnDeg, worst(chk.Turns), chk.PaceMax, time.Since(cs).Round(100*time.Millisecond))
	return r, nil
}

// printAB prints two builds of one course side by side.
func printAB(c *course.Course, a, b report) {
	km := math.Max(c.Distance/1000, 1e-3)
	row := func(name, unit string, x, y float64) {
		fmt.Printf("  %-22s %12.2f %12.2f  %s\n", name, x, y, unit)
	}
	fmt.Printf("%s: %-22s %12s %12s\n", c.ID, "A/B", a.engine, b.engine)
	row("build", "s", a.build.Seconds(), b.build.Seconds())
	row("size", "MB", a.sizeMB, b.sizeMB)
	row("triangles", "M", float64(a.w.Triangles)/1e6, float64(b.w.Triangles)/1e6)
	row("plants", "k", float64(a.w.Plants)/1e3, float64(b.w.Plants)/1e3)
	row("holes", "m²", a.check.HolesM2, b.check.HolesM2)
	row("land over road", "m²", a.check.LandOverRoadM2, b.check.LandOverRoadM2)
	row("off road", "m/km", a.check.OffRoadM/km, b.check.OffRoadM/km)
	row("path height step", "m", a.check.StepM, b.check.StepM)
	row("sharpest turn", "° per 5 m", a.check.TurnDeg, b.check.TurnDeg)
	row("turns over 30°", "", float64(len(a.check.Turns)), float64(len(b.check.Turns)))
	row("pace at most", "× even", a.check.PaceMax, b.check.PaceMax)
	fmt.Printf("  worlds: %s, %s\n", a.dir, b.dir)
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
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
