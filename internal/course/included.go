package course

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// routes are real courses that come with osscycler, built into the
// binaries so every release has them. They follow OpenStreetMap roads
// (each file credits it) with elevation from open data, and are never a
// rider's own recording.
//
//go:embed routes/*.gpx
var routes embed.FS

// Included returns the courses that come with osscycler: the built-in test
// tracks, then the included routes, sorted by ID, each followed by its
// reverse. Unlike the tracks, the
// routes are real places, with map data.
func Included() []*Course {
	var cs []*Course
	for _, t := range Tracks() {
		rev := t.Reversed()
		rev.Forward = t
		cs = append(cs, t, rev)
	}
	names, err := fs.Glob(routes, "routes/*.gpx")
	if err != nil {
		panic(err) // the pattern is fixed
	}
	for _, n := range names {
		b, err := routes.ReadFile(n)
		if err != nil {
			panic(err)
		}
		c, err := ParseGPX(strings.TrimSuffix(path.Base(n), ".gpx"), bytes.NewReader(b))
		if err != nil {
			panic(fmt.Sprintf("course: included route %s: %v", n, err)) // a test reads every one
		}
		c.Included = true
		fwd, rev := withReverse(c)
		cs = append(cs, fwd, rev)
	}
	return cs
}
