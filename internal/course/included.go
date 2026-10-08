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
// tracks, then the included routes, sorted by ID. Unlike the tracks, the
// routes are real places, with map data.
func Included() []*Course {
	cs := Tracks()
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
		cs = append(cs, c)
	}
	return cs
}
