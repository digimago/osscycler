package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/digimago/osscycler/internal/course"
)

// courseFiles gives clients the GPX of the courses in the courses
// directory, so they can build a course's 3D world where they draw it
// (the core builds none: it may be a Pi by the trainer). Built-in tracks
// and included routes come with every client: none to give.
type courseFiles struct {
	dir     string
	courses []*course.Course
}

func (f courseFiles) File(id string) (string, []byte, error) {
	for _, c := range f.courses {
		if c.ID != id {
			continue
		}
		if c.Builtin || c.Included {
			return "", nil, fmt.Errorf("course %q comes with osscycler: %w", id, os.ErrNotExist)
		}
		// The ID is the file's name (course.LoadDir), so this is the file
		// it was loaded from.
		name := id + ".gpx"
		b, err := os.ReadFile(filepath.Join(f.dir, name))
		return name, b, err
	}
	return "", nil, fmt.Errorf("no course %q: %w", id, os.ErrNotExist)
}
