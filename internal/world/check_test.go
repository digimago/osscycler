package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
)

func TestCheck(t *testing.T) {
	// 1 km north over rolling ground, with a ground model: roads cut out of
	// the terrain, verges beside them.
	var pts []course.Point
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 10})
	}
	c, err := course.New("check", "Check", pts)
	if err != nil {
		t.Fatal(err)
	}
	ground := func(lat, lon float64) (float64, bool) {
		e, n := c.Project(lat, lon)
		return 10 + 2*math.Sin(e/40) + math.Sin(n/70), true
	}
	w, err := Build(c, Options{Generator: "test", Elevation: ground})
	if err != nil {
		t.Fatal(err)
	}
	r := w.Check()
	if r.HolesM2 > 0.5 || r.LandOverRoadM2 > 0.1 || r.OffRoadM > 0 || r.StepM > checkStepM || len(r.Steps) > 0 {
		t.Errorf("holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v, path steps %.2f m %v",
			r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad, r.StepM, r.Steps)
	}
	// The path dropping 7.5 m at 805 m (onto a road below, as once at
	// Zandvoort) and back up 5 m on.
	xyz := w.Manifest.Path.XYZ
	saved := append([]float64(nil), xyz...)
	xyz[3*161+1] -= 7.5
	if r := w.Check(); math.Abs(r.StepM-7.5) > 0.5 || len(r.Steps) != 1 || r.Steps[0].D != 800 {
		t.Errorf("path dropped 7.5 m: steps at most %.2f m %v, want 7.5 m at 800 m", r.StepM, r.Steps)
	}
	copy(xyz, saved)
	// The road taken out from 600 to 610 m: the terrain (verges) is whole,
	// so no holes, but the riding line is off road for 10 m.
	all := w.prims
	var cut []gltf.Primitive
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "road" {
			cut = append(cut, p)
			continue
		}
		q := p
		q.Indices = nil
		for i := 0; i+2 < len(p.Indices); i += 3 {
			z := (p.Positions[3*p.Indices[i]+2] + p.Positions[3*p.Indices[i+1]+2] + p.Positions[3*p.Indices[i+2]+2]) / 3
			if z > -600 || z < -610 {
				q.Indices = append(q.Indices, p.Indices[i:i+3]...)
			}
		}
		cut = append(cut, q)
	}
	w.prims = cut
	if r := w.Check(); math.Abs(r.OffRoadM-10) > 2 || len(r.OffRoad) != 1 || r.OffRoad[0].D != 600 {
		t.Errorf("road taken out: off road %.0f m %v, want 10 m at 600 m", r.OffRoadM, r.OffRoad)
	}
	w.prims = all

	// Land over the road, 400-410 m along: grass through the asphalt.
	land := w.doc.AddMaterial(gltf.Material{Name: "terrain"})
	y := float32(20)
	w.prims = append(w.prims, gltf.Primitive{Material: land,
		Positions: []float32{-1, y, -400, 1, y, -400, 1, y, -410, -1, y, -410},
		Indices:   []uint32{0, 1, 2, 0, 2, 3}})
	r = w.Check()
	if math.Abs(r.LandOverRoadM2-20) > 1 || len(r.LandOverRoad) != 1 || r.LandOverRoad[0].D != 400 {
		t.Errorf("land over road %.2f m² %v, want 20 m² at 400 m", r.LandOverRoadM2, r.LandOverRoad)
	}
	// Without the terrain: holes everywhere beside the road.
	var kept []gltf.Primitive
	for _, p := range w.prims {
		if w.doc.MaterialName(p.Material) != "terrain" {
			kept = append(kept, p)
		}
	}
	w.prims = kept
	if r := w.Check(); r.HolesM2 < 1000*60 {
		t.Errorf("without the terrain, holes %.0f m², want most of 1000 × 80", r.HolesM2)
	}
}
