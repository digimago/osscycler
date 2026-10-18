package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
)

// The distant terrain is laid where the route sees it and nowhere else
// (owner, 2026-10-18: far-away terrain on the Hohneck's climb): a course
// along a valley floor at 10 m, a hill 2 km east in plain view, and 2 km
// west a 200 m ridge with a hollow behind it, out of sight. It keeps off
// the near terrain's chunks, and a flat valley floor far off (under
// farMinDeg from a level gaze) isn't laid.
func TestFarTerrainIsWhatTheRouteSees(t *testing.T) {
	c := northCourse(t) // 1 km north along e = 0
	kx := 111195 * math.Cos(52*math.Pi/180)
	far := func(lat, lon float64) (float64, bool) {
		e := (lon - 5) * kx
		switch {
		case e > 1500 && e < 2500: // the hill in view
			return 10 + 150*math.Cos((e-2000)/500*math.Pi/2), true
		case e < -1000 && e > -1400: // the ridge
			return 210, true
		case e <= -1400 && e > -3000: // the hollow behind it
			return 50, true
		}
		return 10, true
	}
	o := Options{Region: RegionFR, FarElevation: far, FarM: 3000}
	tr := testTerrain(c, o)
	keys := tr.chunks()
	w := &World{doc: gltf.New("test")}
	mat := w.doc.AddMaterial(gltf.Material{Name: "far land"})
	cells, err := w.addFarTerrain(tr, keys, mat)
	if err != nil {
		t.Fatal(err)
	}
	if cells == 0 {
		t.Fatal("no distant terrain")
	}
	near := map[[2]int]bool{}
	for _, k := range keys {
		near[k] = true
	}
	var hill, ridge, hollow, flat, onNear int
	for _, p := range w.prims {
		for i := 0; i+2 < len(p.Indices); i += 3 {
			var e, n float64
			for k := range 3 {
				v := p.Indices[i+k]
				e += float64(p.Positions[3*v]) / 3
				n -= float64(p.Positions[3*v+2]) / 3
			}
			if near[[2]int{int(math.Floor(e / o.ChunkM)), int(math.Floor(n / o.ChunkM))}] {
				onNear++
			}
			switch {
			case e > 1600 && e < 2400:
				hill++
			case e < -1050 && e > -1350:
				ridge++
			case e < -1600 && e > -2800:
				hollow++
			case math.Abs(e) > 2700 && math.Abs(e) < 2950 && n > 0 && n < 1000:
				flat++
			}
		}
	}
	if hill == 0 || ridge == 0 {
		t.Errorf("the hill in view has %d triangles, the ridge %d: want both laid", hill, ridge)
	}
	if hollow > 0 {
		t.Errorf("%d triangles in the hollow behind the ridge, out of sight", hollow)
	}
	if flat > 0 {
		t.Errorf("%d triangles on the flat valley floor ~2.8 km off, too flat to see", flat)
	}
	if onNear > 0 {
		t.Errorf("%d triangles over the near terrain's chunks", onNear)
	}
}
