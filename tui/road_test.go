package tui

import (
	"math"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// testCourse runs north for 500 m, then follows heading(d) (radians,
// clockwise from north) and elevation ele(d), sampled every 10 m.
func testCourse(n int, heading, ele func(d float64) float64) *pb.Course {
	c := &pb.Course{Id: "t", ProfileStepM: 10, DistanceM: float64(n-1) * 10}
	var x, y float64
	for i := range n {
		d := float64(i) * 10
		c.ProfileEastM = append(c.ProfileEastM, float32(x))
		c.ProfileNorthM = append(c.ProfileNorthM, float32(y))
		c.ProfileElevationM = append(c.ProfileElevationM, float32(ele(d)))
		h := heading(d)
		x, y = x+10*math.Sin(h), y+10*math.Cos(h)
	}
	return c
}

func flat(float64) float64 { return 0 }

func TestRoadSceneNeedsTrack(t *testing.T) {
	if newRoadScene(&pb.Course{ProfileStepM: 10, ProfileElevationM: []float32{1, 2, 3}}) != nil {
		t.Error("a course without a track got a road view")
	}
}

func TestRoadProjection(t *testing.T) {
	const w, h = 160, 60
	straight := newRoadScene(testCourse(200, flat, flat))
	cam := straight.camera(100, w, h)
	sx, sy, depth, ok := cam.project(straight.at(200))
	if !ok || math.Abs(sx-w/2) > 0.5 || math.Abs(depth-100) > 1 {
		t.Errorf("100 m ahead on a straight road: x %.1f depth %.1f ok %v, want centred at 100 m", sx, depth, ok)
	}
	if sy <= cam.horizon() {
		t.Errorf("flat road ahead at row %.1f, not below the horizon %.1f", sy, cam.horizon())
	}

	// A right-hand bend from 150 m on: the road ahead swings right.
	right := newRoadScene(testCourse(200, func(d float64) float64 {
		return math.Min(math.Max(0, d-150)/100, 1) * math.Pi / 2
	}, flat))
	cam = right.camera(100, w, h)
	if sx, _, _, ok := cam.project(right.at(260)); !ok || sx <= w/2+5 {
		t.Errorf("road after a right bend at column %.1f, want right of %d", sx, w/2)
	}

	// A climb from 200 m: the road ahead rises above the horizon.
	climb := newRoadScene(testCourse(200, flat, func(d float64) float64 { return math.Max(0, d-200) * 0.1 }))
	cam = climb.camera(100, w, h)
	if _, sy, _, _ := cam.project(climb.at(350)); sy >= cam.horizon() {
		t.Errorf("climb 250 m ahead at row %.1f, want above the horizon %.1f", sy, cam.horizon())
	}
}

func TestRoadRender(t *testing.T) {
	sc := newRoadScene(testCourse(100, func(d float64) float64 { return d / 2000 }, func(d float64) float64 { return 20 * math.Sin(d/200) }))
	for _, pos := range []float64{0, 500, 980} { // start, middle, past the last sample
		out := sc.render(pos, pos+30, 80, 20)
		lines := strings.Split(out, "\n")
		if len(lines) != 20 {
			t.Fatalf("at %.0f m: %d lines, want 20", pos, len(lines))
		}
		for i, l := range lines {
			if got := lipgloss.Width(l); got != 80 {
				t.Fatalf("at %.0f m: line %d is %d wide, want 80", pos, i, got)
			}
		}
	}
}

func TestRoadGhostVisible(t *testing.T) {
	sc := newRoadScene(testCourse(200, flat, flat))
	const w, h = 160, 60
	without := sc.pixels(100, -1, w, h)
	with := sc.pixels(100, 120, w, h)
	diff := 0
	for i := range with {
		if with[i] != without[i] {
			diff++
		}
	}
	if diff < 20 {
		t.Errorf("a ghost 20 m ahead changed %d pixels, want a visible rider", diff)
	}
}
