package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func TestCourseFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "home loop.gpx"), []byte("<gpx/>"), 0o644)
	own := &course.Course{ID: "home loop"}
	f := courseFiles{dir: dir, courses: append(course.Included(), own, &course.Course{ID: "home loop-reverse", Forward: own})}
	// A reverse is built from its forward course's file, reversed there.
	for _, id := range []string{"home loop", "home loop-reverse"} {
		if name, b, err := f.File(id); err != nil || name != "home loop.gpx" || string(b) != "<gpx/>" {
			t.Errorf("%s: %q %q %v", id, name, b, err)
		}
	}
	for _, id := range []string{"posbank", "posbank-reverse", "oval-400", "../profile", "nowhere"} {
		if _, _, err := f.File(id); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: %v", id, err)
		}
	}
}
