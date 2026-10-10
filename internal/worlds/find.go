package worlds

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/digimago/osscycler/internal/world"
)

// Worlds are built where they are drawn (owner, 2026-10-09: the core may
// be a Pi by the trainer; building a world is the client's): ready-built
// ones come with the program, the rest are built on demand into the
// client's worlds directory. Find picks the current one.

// Lines osscycler-world prints for a program running it (-progress).
const (
	PhasePrefix = "phase: " // a build's phase (PhaseGround, PhaseMaps, ...)
	// ProgressPrefix: how far the build is, as JSON (FormatProgress).
	ProgressPrefix = "progress: "
	WorldPrefix    = "world: " // the directory of the world to load
	// PreviewPrefix: a preview world's directory, to ride until the
	// world's own comes (-preview).
	PreviewPrefix = "preview: "
)

// PreviewDir is where course id's preview world goes in the worlds
// directory out: apart from the worlds, which Find looks for.
func PreviewDir(out, id string) string { return filepath.Join(out, ".preview", id) }

// Files are a world's files, the manifest last (a reader that has it has
// the rest).
var Files = []string{world.ModelFile, world.InstancesFile, world.GroundFile, world.ManifestFile}

// ReadStamp is the stamp in the manifest of the world in dir ("" if
// there is none). The stamp comes before the path, but the manifest is
// read whole: it is a few megabytes at most.
func ReadStamp(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, world.ManifestFile))
	if err != nil {
		return ""
	}
	var m struct {
		Stamp string `json:"stamp"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m.Stamp
}

// Find looks in dirs, in order, for course id's world built from stamp;
// its directory, or "".
func Find(dirs []string, id, stamp string) string {
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if dir := filepath.Join(d, id); ReadStamp(dir) == stamp {
			return dir
		}
	}
	return ""
}

// PrebuiltDirs are where ready-built worlds come with the program at exe:
// share/osscycler/worlds beside its bin/ (an archive or package),
// worlds/ beside it, and where packages put them.
func PrebuiltDirs(exe string) []string {
	var dirs []string
	if exe != "" {
		d := filepath.Dir(exe)
		dirs = append(dirs, filepath.Join(d, "..", "share", "osscycler", "worlds"), filepath.Join(d, "worlds"))
	}
	return append(dirs, "/usr/share/osscycler/worlds")
}
