package course

import (
	"math"
	"testing"
)

func TestTrackInMetres(t *testing.T) {
	// 1 km due north, then 1 km due east, on flat ground.
	const lat0, lon0 = 52.0, 5.0
	dLat := 1000 / metresPerDegree
	dLon := 1000 / (metresPerDegree * math.Cos(lat0*math.Pi/180))
	var pts []Point
	for i := 0; i <= 100; i++ {
		pts = append(pts, Point{Lat: lat0 + dLat*float64(i)/100, Lon: lon0, Ele: 10})
	}
	for i := 1; i <= 100; i++ {
		pts = append(pts, Point{Lat: lat0 + dLat, Lon: lon0 + dLon*float64(i)/100, Ele: 10})
	}
	c, err := New("t", "t", pts)
	if err != nil {
		t.Fatal(err)
	}
	east, north := c.Track()
	ele, _ := c.Profile()
	if len(east) != len(ele) || len(north) != len(ele) {
		t.Fatalf("track has %d/%d samples, profile %d", len(east), len(north), len(ele))
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 5 }
	if !near(east[0], 0) || !near(north[0], 0) {
		t.Errorf("start at %.1f E %.1f N, want 0 0", east[0], north[0])
	}
	mid := len(east) / 2
	if !near(east[mid], 0) || !near(north[mid], 1000) {
		t.Errorf("corner at %.1f E %.1f N, want 0 E 1000 N", east[mid], north[mid])
	}
	last := len(east) - 1
	if !near(east[last], 1000) || !near(north[last], 1000) {
		t.Errorf("finish at %.1f E %.1f N, want 1000 E 1000 N", east[last], north[last])
	}
}

func TestUnprojectInvertsProject(t *testing.T) {
	c := Tracks()[1]
	for _, p := range [][2]float64{{0, 0}, {1234.5, -678.9}, {-2500, 2500}} {
		lat, lon := c.Unproject(p[0], p[1])
		e, n := c.Project(lat, lon)
		if math.Abs(e-p[0]) > 1e-6 || math.Abs(n-p[1]) > 1e-6 {
			t.Errorf("Project(Unproject(%v)) = %.6f %.6f", p, e, n)
		}
	}
}
