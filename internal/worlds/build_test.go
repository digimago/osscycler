package worlds

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/world"
)

func track(t *testing.T, id string) *course.Course {
	t.Helper()
	for _, c := range course.Tracks() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no track %s", id)
	return nil
}

// A world is the same every build (the world-assets rule: seeded, never
// random), so a stamp names its contents and a change shows only what it
// changed. The figure 8 has everything added per chunk: terrain, roads,
// a bridge, fences; with a ground model its roads are cut out of the
// terrain too. Too few of its roads overlap to show the order faults the
// Posbank Loop had (2026-10-09, about 200 arrays from run to run):
// TestCutIsTheSameInAnyOrder covers the cut.
func TestBuildIsTheSameEveryTime(t *testing.T) {
	c := track(t, "figure-8")
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		return 20 + 3*math.Sin(e/60) + 2*math.Sin(n/90), true
	}
	for _, tc := range []struct {
		name, engine string
		o            world.Options
	}{
		{"no ground model", world.EngineClassic, world.Options{Generator: "test"}},
		{"ground model", world.EngineClassic, world.Options{Generator: "test", Elevation: ground}},
		{"graph, no ground model", world.EngineGraph, world.Options{Generator: "test"}},
		{"graph, ground model", world.EngineGraph, world.Options{Generator: "test", Elevation: ground}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Builder{Ground: "none", Options: tc.o, Engine: tc.engine}
			var files [2]map[string][]byte
			for i := range files {
				res, err := b.Build(context.Background(), c, nil)
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				if err := res.World.Write(dir); err != nil {
					t.Fatal(err)
				}
				files[i] = map[string][]byte{}
				for _, f := range []string{world.ModelFile, world.ManifestFile, world.InstancesFile, world.GroundFile} {
					if files[i][f], err = os.ReadFile(filepath.Join(dir, f)); err != nil {
						t.Fatal(err)
					}
				}
			}
			for f, a := range files[0] {
				if !bytes.Equal(a, files[1][f]) {
					t.Errorf("%s differs between two builds", f)
				}
			}
			if !bytes.Contains(files[0][world.ManifestFile], []byte(`"stamp":"`+EngineStamp(c, tc.engine)+`"`)) {
				t.Error("the manifest lacks the course's stamp")
			}
		})
	}
}

func TestStampFollowsTheCourse(t *testing.T) {
	a, b := track(t, "figure-8"), track(t, "oval-400")
	again := *a
	if Stamp(a) != Stamp(&again) || Stamp(a) == Stamp(b) {
		t.Errorf("stamps %s %s", Stamp(a), Stamp(b))
	}
	moved := *a
	moved.Name = "Figure 8, renamed"
	if Stamp(&moved) == Stamp(a) {
		t.Error("a renamed course kept its stamp")
	}
}

func TestPreview(t *testing.T) {
	// A preview of a real course takes neither ground model nor map data
	// (none to be had here: no caches, no fetching), has made-up
	// countryside, and a stamp of its own, never taken for the world's.
	var c *course.Course
	for _, x := range course.Included() {
		if !x.Builtin {
			c = x
		}
	}
	b := &Builder{Ground: "auto", DEMCache: t.TempDir(), Preview: true, Options: world.Options{Generator: "test"}}
	res, err := b.Build(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.World.Manifest
	if m.Terrain.Source != "course profile" || m.Stamp != PreviewStamp(c) || m.Stamp == Stamp(c) {
		t.Errorf("terrain %q, stamp %q (world's %q)", m.Terrain.Source, m.Stamp, Stamp(c))
	}
	if res.World.Plants == 0 || !strings.Contains(res.MapInfo, "made-up") {
		t.Errorf("%d plants, %s", res.World.Plants, res.MapInfo)
	}
	dir := t.TempDir()
	if err := res.World.Write(PreviewDir(dir, c.ID)); err != nil {
		t.Fatal(err)
	}
	if Find([]string{dir}, c.ID, Stamp(c)) != "" {
		t.Error("the preview was taken for the world")
	}
}

// Each builder's worlds have their own stamp and directory, so both can
// be kept and neither is taken for the other's; the classic builder's are
// what they always were.
func TestEngineStampsAndDirs(t *testing.T) {
	c := track(t, "oval-400")
	classic, graph := EngineStamp(c, world.EngineClassic), EngineStamp(c, world.EngineGraph)
	if classic != Stamp(c) {
		t.Errorf("the classic builder's stamp %q, want the course's %q", classic, Stamp(c))
	}
	if graph == classic || !strings.HasPrefix(graph, "g") || EngineStamp(c, "") != graph {
		t.Errorf("the graph builder's stamp %q (classic %q, default %q)", graph, classic, EngineStamp(c, ""))
	}
	if got := Dir("/w", "oval-400", world.EngineClassic); got != filepath.Join("/w", ".classic", "oval-400") {
		t.Errorf("classic dir %s", got)
	}
	if got := Dir("/w", "oval-400", world.EngineGraph); got != filepath.Join("/w", "oval-400") || Dir("/w", "oval-400", "") != got {
		t.Errorf("graph dir %s (default %s)", got, Dir("/w", "oval-400", ""))
	}
}

// When the graph builder fails, the classic one builds the world in its
// place, under the graph builder's stamp (so it isn't tried again and
// again), and the result says so.
func TestBuilderFallsBackToClassic(t *testing.T) {
	c := track(t, "oval-400")
	was := world.Engines[world.EngineGraph]
	world.Engines[world.EngineGraph] = func(*course.Course, world.Options) (*world.World, error) {
		return nil, errors.New("no network")
	}
	defer func() { world.Engines[world.EngineGraph] = was }()
	b := &Builder{Ground: "none"}
	r, err := b.Build(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !r.FellBack || r.World.Manifest.Builder != world.EngineClassic {
		t.Errorf("fell back %v, builder %q: want the classic builder in the graph builder's place", r.FellBack, r.World.Manifest.Builder)
	}
	if r.World.Manifest.Stamp != EngineStamp(c, world.EngineGraph) {
		t.Errorf("stamp %q, want the graph builder's %q", r.World.Manifest.Stamp, EngineStamp(c, world.EngineGraph))
	}
}
