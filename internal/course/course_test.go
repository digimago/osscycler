package course

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// line builds points due north from the equator, where 1e-5 degrees of
// latitude is 1.11195 m, rising at gradePct.
func line(meters, spacing, gradePct float64) []Point {
	const mPerDeg = 6371000 * math.Pi / 180
	var pts []Point
	for d := 0.0; d <= meters+1e-9; d += spacing {
		pts = append(pts, Point{Lat: d / mPerDeg, Ele: 100 + d*gradePct/100})
	}
	return pts
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestConstantGrade(t *testing.T) {
	c, err := New("c", "climb", line(2000, 7, 5))
	if err != nil {
		t.Fatal(err)
	}
	if !near(c.Distance, 1995, 1) { // last point at 285*7 m
		t.Errorf("distance %.1f, want ~1995", c.Distance)
	}
	if !near(c.Gain, c.Distance*0.05, 0.5) || c.Loss > 0.01 {
		t.Errorf("gain %.2f loss %.2f", c.Gain, c.Loss)
	}
	for _, d := range []float64{0, 5, 500, 1234.5, c.Distance} {
		ele, g := c.At(d)
		if !near(g, 5, 0.01) || !near(ele, 100+d*0.05, 0.05) {
			t.Errorf("At(%v) = %.2f m, %.3f %%", d, ele, g)
		}
	}
	// Clamped beyond the ends.
	if e, _ := c.At(-50); !near(e, 100, 0.01) {
		t.Errorf("At(-50) ele %.2f", e)
	}
}

func TestSmoothingRemovesQuantisation(t *testing.T) {
	pts := line(1000, 1, 3)
	for i := range pts {
		pts[i].Ele = math.Round(pts[i].Ele*5) / 5 // 0.2 m steps, like Garmin exports
	}
	c, err := New("q", "q", pts)
	if err != nil {
		t.Fatal(err)
	}
	// Away from the ends (where the window narrows) the grade is clean.
	for d := 100.0; d < 900; d += 10 {
		if _, g := c.At(d); !near(g, 3, 0.25) {
			t.Fatalf("grade at %v m = %.2f %%, want 3 ± 0.25", d, g)
		}
	}
}

func TestStandstillPointsIgnored(t *testing.T) {
	pts := line(500, 5, 0)
	// Ten seconds standing still halfway, with a stray elevation reading.
	still := pts[50]
	extra := make([]Point, 10)
	for i := range extra {
		extra[i] = still
	}
	pts = append(pts[:51], append(extra, pts[51:]...)...)
	c, err := New("s", "s", pts)
	if err != nil {
		t.Fatal(err)
	}
	if !near(c.Distance, 500, 0.5) {
		t.Errorf("distance %.2f, want 500", c.Distance)
	}
}

func TestProfile(t *testing.T) {
	c, _ := New("p", "p", line(1000, 5, 4))
	ele, grade := c.Profile()
	if len(ele) != len(grade) || len(ele) != 101 {
		t.Fatalf("profile lengths %d, %d, want 101", len(ele), len(grade))
	}
	if !near(ele[100], 140, 0.01) || !near(grade[50], 4, 0.01) {
		t.Errorf("ele[100] %.2f grade[50] %.2f", ele[100], grade[50])
	}
}

func TestParseGPX(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>
<gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1"
     xmlns:ns3="http://www.garmin.com/xmlschemas/TrackPointExtension/v1">
  <metadata><name>meta</name></metadata>
  <trk><name>Test Climb</name><trkseg>`)
	for _, p := range line(300, 10, 6) {
		fmt.Fprintf(&b, `<trkpt lat="%.10f" lon="0"><ele>%.2f</ele><extensions><ns3:hr>120</ns3:hr></extensions></trkpt>`, p.Lat, p.Ele)
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	c, err := ParseGPX("climb", strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "climb" || c.Name != "Test Climb" || !near(c.Gain, 18, 0.1) {
		t.Errorf("course %q %q gain %.2f", c.ID, c.Name, c.Gain)
	}

	if _, err := ParseGPX("x", strings.NewReader(`<gpx><trk><trkseg><trkpt lat="0" lon="0"/><trkpt lat="0.01" lon="0"/></trkseg></trk></gpx>`)); err == nil {
		t.Error("accepted points without elevation")
	}
	if _, err := ParseGPX("x", strings.NewReader(`<gpx><trk><trkseg></trkseg></trk></gpx>`)); err == nil {
		t.Error("accepted an empty track")
	}
}

func TestElevationGlitches(t *testing.T) {
	pts := line(1000, 2, 0)
	pts[0].Ele += 5.6 // altimeter settling at the start, as in a real export
	pts[200].Ele -= 8 // a one-point dropout mid-ride
	pts[300].Ele += 4 // and a two-point one
	pts[301].Ele += 4
	pts[len(pts)-1].Ele -= 6 // and at the very end
	c, err := New("g", "g", pts)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxGrade > 0.01 || c.MinGrade < -0.01 {
		t.Errorf("flat course with glitches: grade %.2f..%.2f %%", c.MinGrade, c.MaxGrade)
	}
	if e, _ := c.At(0); !near(e, 100, 0.01) {
		t.Errorf("start elevation %.2f, want 100", e)
	}
}

func TestDespikeKeepsSlopesAndSteps(t *testing.T) {
	// A straight slope is untouched, ends included.
	v := []float64{1, 2, 3, 4, 5, 6}
	for i, x := range despike(v) {
		if x != v[i] {
			t.Fatalf("slope changed: %v", despike(v))
		}
	}
	// A real step in the road (a sustained change) survives.
	step := []float64{0, 0, 0, 0, 5, 5, 5, 5}
	got := despike(step)
	if got[3] != 0 || got[4] != 5 {
		t.Errorf("sustained step altered: %v", got)
	}
}

func TestPosition(t *testing.T) {
	const mPerDeg = 6371000 * math.Pi / 180
	// An L: 500 m north, then 500 m east, from 52° N 5° E, with track
	// points 37 m apart so resampling has to interpolate.
	var pts []Point
	for d := 0.0; d <= 500; d += 37 {
		pts = append(pts, Point{Lat: 52 + d/mPerDeg, Lon: 5, Ele: 10})
	}
	corner := Point{Lat: 52 + 500/mPerDeg, Lon: 5, Ele: 10}
	pts = append(pts, corner)
	cos := math.Cos(corner.Lat * math.Pi / 180)
	for d := 37.0; d <= 500; d += 37 {
		pts = append(pts, Point{Lat: corner.Lat, Lon: 5 + d/(mPerDeg*cos), Ele: 10})
	}
	c, err := New("l", "l", pts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ d, north, east float64 }{
		{0, 0, 0}, {250, 250, 0}, {500, 500, 0}, {700, 500, 200}, {c.Distance, 500, c.Distance - 500}, {1e6, 500, c.Distance - 500},
	} {
		lat, lon := c.Position(tc.d)
		north, east := (lat-52)*mPerDeg, (lon-5)*mPerDeg*cos
		if !near(north, tc.north, 0.5) || !near(east, tc.east, 0.5) {
			t.Errorf("Position(%.0f) is %.1f m north, %.1f m east; want %.0f, %.0f", tc.d, north, east, tc.north, tc.east)
		}
	}
}

func TestVerticesArePositions(t *testing.T) {
	c := Tracks()[1] // the figure of eight: many points, a loop
	dist, east, north := c.Vertices()
	if len(dist) < 10 || dist[0] != 0 {
		t.Fatalf("%d vertices from %v", len(dist), dist[:1])
	}
	for i := range dist {
		e, n := c.Project(c.Position(dist[i]))
		if math.Abs(e-east[i]) > 1e-6 || math.Abs(n-north[i]) > 1e-6 {
			t.Fatalf("vertex %d at %.2f, %.2f; Position puts it at %.2f, %.2f", i, east[i], north[i], e, n)
		}
	}
}

// InFrame moves only where Project's metres count from: the same point
// maps to the same lat, lon either way, and two courses in one frame put
// a point at the same metres.
func TestInFrame(t *testing.T) {
	mk := func(lat0 float64) *Course {
		var pts []Point
		for i := range 50 {
			pts = append(pts, Point{Lat: lat0 + float64(i)*0.0001, Lon: 5.01, Ele: 10})
		}
		c, err := New("c", "C", pts)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := mk(52.01).InFrame(52, 5), mk(52.02).InFrame(52, 5)
	if lat, lon := a.Origin(); lat != 52 || lon != 5 {
		t.Errorf("origin %v, %v", lat, lon)
	}
	ea, na := a.Project(52.015, 5.012)
	eb, nb := b.Project(52.015, 5.012)
	if math.Abs(ea-eb) > 1e-9 || math.Abs(na-nb) > 1e-9 {
		t.Errorf("one point, two places: %.3f, %.3f and %.3f, %.3f", ea, na, eb, nb)
	}
	if lat, lon := a.Unproject(ea, na); math.Abs(lat-52.015) > 1e-12 || math.Abs(lon-5.012) > 1e-12 {
		t.Errorf("round trip: %v, %v", lat, lon)
	}
	if e, n := mk(52.01).Project(52.01, 5.01); e != 0 || n != 0 {
		t.Errorf("without a frame the start is 0, 0: %v, %v", e, n)
	}
}
