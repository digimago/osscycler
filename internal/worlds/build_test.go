package worlds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
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
		name string
		o    world.Options
	}{
		{"no ground model", world.Options{Generator: "test"}},
		{"ground model", world.Options{Generator: "test", Elevation: ground}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Builder{Ground: "none", Options: tc.o}
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
			if !bytes.Contains(files[0][world.ManifestFile], []byte(`"stamp":"`+Stamp(c)+`"`)) {
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

// A world's stamp is the graph builder's as it was while there were two
// builders (owner, 2026-10-21: the classic one retired), so the worlds it
// built stay current; a preview's is its own; worlds go in out/id.
func TestStampsAndDirs(t *testing.T) {
	c := track(t, "oval-400")
	if s := Stamp(c); !strings.HasPrefix(s, fmt.Sprintf("g%d-", BuilderVersion)) || PreviewStamp(c) == s {
		t.Errorf("stamp %q, preview %q", s, PreviewStamp(c))
	}
	if got := Dir("/w", "oval-400"); got != filepath.Join("/w", "oval-400") {
		t.Errorf("dir %s", got)
	}
}

// A build from the caches only (Complete, Fetch off) never makes a world
// with grass where data is missing: it says the data isn't all cached, so
// the course is built when it is ridden and fetches what it lacks (owner,
// 2026-10-13: worlds built ahead of rides from the caches only).
func TestCompleteBuildNeedsEverythingCached(t *testing.T) {
	var c *course.Course
	for _, ic := range course.Included() {
		if ic.ID == "posbank" {
			c = ic
		}
	}
	if c == nil {
		t.Fatal("no included posbank course")
	}
	maps := scenery.NewStore(scenery.Config{CacheDir: t.TempDir(), Fetch: false}, nil)
	for _, ground := range []string{"ahn", "none"} {
		b := &Builder{Ground: ground, DEMCache: t.TempDir(), Maps: maps, Complete: true, Options: world.Options{Generator: "test"}}
		if _, err := b.Build(context.Background(), c, nil); !errors.Is(err, ErrIncomplete) {
			t.Errorf("ground %s, empty caches: %v, want ErrIncomplete", ground, err)
		}
	}
}
