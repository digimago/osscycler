package world

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

func TestCutIsTheSameInAnyOrder(t *testing.T) {
	// Road surfaces overlapping over a piece of land, as junctions over the
	// roads they join. The roads reach the cutter in map order, and the
	// land left over must not depend on it (the Posbank Loop's terrain had
	// differed from build to build).
	r := rand.New(rand.NewSource(1))
	var roads []gltf.Primitive
	for range 30 {
		x, z := r.Float32()*16-8, r.Float32()*16-8
		roads = append(roads, gltf.Primitive{Positions: []float32{x, 0, z,
			x + r.Float32()*6, 0, z + r.Float32()*2, x + r.Float32()*2, 0, z + r.Float32()*6},
			Indices: []uint32{0, 1, 2}})
	}
	land := gltf.Primitive{Positions: []float32{-8, 0, -8, 8, 0, -8, 8, 0, 8, -8, 0, 8},
		Indices: []uint32{0, 1, 2, 0, 2, 3}}
	forward, backward := newCutter(), newCutter()
	for i := range roads {
		forward.add(roads[i])
		backward.add(roads[len(roads)-1-i])
	}
	x, y := forward.clip(land), backward.clip(land)
	if len(x.Indices) == 0 {
		t.Fatal("nothing of the land is left")
	}
	if !reflect.DeepEqual(x, y) {
		t.Error("the cut differs with the order of the roads")
	}
}
