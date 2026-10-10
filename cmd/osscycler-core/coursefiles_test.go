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
	f := courseFiles{dir: dir, courses: append(course.Included(), &course.Course{ID: "home loop"})}
	if name, b, err := f.File("home loop"); err != nil || name != "home loop.gpx" || string(b) != "<gpx/>" {
		t.Errorf("home loop: %q %q %v", name, b, err)
	}
	for _, id := range []string{"posbank", "oval-400", "../profile", "nowhere"} {
		if _, _, err := f.File(id); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: %v", id, err)
		}
	}
}
