package world

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

// Two courses over the same road build the same world there (owner,
// 2026-10-10: a route sharing 7 km with the Posbank Loop had heights up
// to 1.1 m apart and 10 % of its plants where the loop's stood): in one
// frame their grids line up, and heights are the ground model's own, not
// each course's GPS profile's (A reads 10 m, B 25 m on the same road).
func TestCoursesSharingARoadBuildTheSameWorldThere(t *testing.T) {
	const lat0, lon0 = 52.0, 5.0
	kx := 111195.0 * math.Cos(lat0*math.Pi/180)
	at := func(e, n float64) (float64, float64) { return lat0 + n/111195, lon0 + e/kx }
	pt := func(e, n, ele float64) course.Point {
		la, lo := at(e, n)
		return course.Point{Lat: la, Lon: lo, Ele: ele}
	}
	var a, b []course.Point
	for n := 0.0; n <= 1500; n += 20 {
		a = append(a, pt(0, n, 10)) // due north
	}
	for e := 600.0; e > 0; e -= 20 {
		b = append(b, pt(e, 300, 25)) // from the east, onto A's road
	}
	for n := 300.0; n <= 1300; n += 20 {
		b = append(b, pt(0, n, 25))
	}
	ca, err := course.New("a", "A", a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := course.New("b", "B", b)
	if err != nil {
		t.Fatal(err)
	}
	ground := func(lat, lon float64) (float64, bool) {
		e, n := (lon-lon0)*kx, (lat-lat0)*111195
		return 5 + 0.002*n + math.Sin(e/50), true
	}
	ring := func(e0, n0, e1, n1 float64) []scenery.LatLon {
		var r []scenery.LatLon
		for _, p := range [][2]float64{{e0, n0}, {e1, n0}, {e1, n1}, {e0, n1}, {e0, n0}} {
			la, lo := at(p[0], p[1])
			r = append(r, scenery.LatLon{Lat: la, Lon: lo})
		}
		return r
	}
	build := func(c *course.Course) *World {
		c = c.InFrame(lat0, lon0)
		lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
			{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-500, -500, 1000, 2000)}}})
		w, err := Build(c, Options{Generator: "test", Elevation: ground, Land: lm})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	wa, wb := build(ca), build(cb)
	// Where only the shared road is near: 700-1000 m north, 60 m either side.
	in := func(x, z float64) bool { return math.Abs(x) < 60 && -z > 700 && -z < 1000 }
	plants := func(w *World) map[[3]int32]bool {
		out := map[[3]int32]bool{}
		for i := 0; i+20 <= len(w.instances); i += 20 {
			f := func(k int) float64 {
				return float64(math.Float32frombits(binary.LittleEndian.Uint32(w.instances[i+4*k:])))
			}
			if x, y, z := f(0), f(1), f(2); in(x, z) {
				out[[3]int32{int32(math.Round(x * 100)), int32(math.Round(y * 100)), int32(math.Round(z * 100))}] = true
			}
		}
		return out
	}
	pa, pb := plants(wa), plants(wb)
	same := 0
	for k := range pa {
		if pb[k] {
			same++
		}
	}
	if len(pa) < 50 || same < len(pa)*98/100 || len(pb) != len(pa) {
		t.Errorf("plants near the shared road: A %d, B %d, at the same spot and height %d", len(pa), len(pb), same)
	}
	terrain := func(w *World) map[[3]int32]bool {
		out := map[[3]int32]bool{}
		for _, p := range w.prims {
			if w.doc.MaterialName(p.Material) != "terrain" {
				continue
			}
			for i := 0; i+2 < len(p.Positions); i += 3 {
				// The 5 m grid's vertices (the courses' own roads are sampled
				// along each course's distance, so their lines fall differently;
				// map roads are sampled along their ways).
				x, y, z := float64(p.Positions[i]), float64(p.Positions[i+1]), float64(p.Positions[i+2])
				if onGrid := math.Abs(x/5-math.Round(x/5)) < 1e-3 && math.Abs(z/5-math.Round(z/5)) < 1e-3; in(x, z) && onGrid {
					out[[3]int32{int32(math.Round(x * 100)), int32(math.Round(y * 100)), int32(math.Round(z * 100))}] = true
				}
			}
		}
		return out
	}
	ta, tb := terrain(wa), terrain(wb)
	// By place, and how far apart in height.
	byXZ := func(m map[[3]int32]bool) map[[2]int32]int32 {
		out := map[[2]int32]int32{}
		for k := range m {
			out[[2]int32{k[0], k[2]}] = k[1]
		}
		return out
	}
	xa, xb := byXZ(ta), byXZ(tb)
	same, worst := 0, 0.0
	for k, ya := range xa {
		if yb, ok := xb[k]; ok {
			same++
			worst = math.Max(worst, math.Abs(float64(ya-yb))/100)
		}
	}
	if len(xa) < 100 || same < len(xa)*98/100 || worst > 0.05 {
		t.Errorf("terrain vertices near the shared road: A %d, B %d, at the same place %d, heights up to %.2f m apart", len(xa), len(xb), same, worst)
	}
}
