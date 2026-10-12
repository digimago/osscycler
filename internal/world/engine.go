package world

import (
	"fmt"
	"sort"

	"github.com/digimago/osscycler/internal/course"
)

// Two world builders live side by side (owner, 2026-10-11: a second
// builder to compare with the first, swapped by a flag, rather than on a
// branch, where switching between them loses track of either):
//
//   - graph (BuildGraph, graph.go), the default since 2026-10-12 (owner:
//     it beats the classic one on the path and keeps up elsewhere): the
//     road network first, as a graph; heights solved on it, the course as
//     a way through it, the surfaces from the junctions out, and the land
//     last, laid round the roads' outline.
//   - classic (Build), the fallback: the terrain first, the roads fitted
//     into it and cut out of it, the rider's path matched point by point
//     and then mended. Built when asked for (-builder classic), when the
//     graph builder fails, or when its world fails the check (worlds).
//
// Both write the same world package (Manifest, glTF, instances, ground
// map), so renderers and the check take either.
const (
	EngineClassic = "classic"
	EngineGraph   = "graph"
	DefaultEngine = EngineGraph
)

// Engines are the builders by name.
var Engines = map[string]func(*course.Course, Options) (*World, error){
	EngineClassic: Build,
	EngineGraph:   BuildGraph,
}

// EngineNames lists the builders' names, sorted.
func EngineNames() []string {
	var out []string
	for k := range Engines {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// BuildWith builds c's world with the named builder ("" the default).
func BuildWith(engine string, c *course.Course, o Options) (*World, error) {
	if engine == "" {
		engine = DefaultEngine
	}
	b, ok := Engines[engine]
	if !ok {
		return nil, fmt.Errorf("world: no builder %q (want one of %v)", engine, EngineNames())
	}
	w, err := b(c, o)
	if w != nil {
		w.Manifest.Builder = engine
	}
	return w, err
}
