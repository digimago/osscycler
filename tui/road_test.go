package tui

import (
	"math"
	"strings"
	"testing"
	"time"

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

// The core moves the rider 4 times a second while other state (trainer,
// heart rate) arrives in between: the drawn position must advance
// steadily, never back.
func TestRidePosSteady(t *testing.T) {
	const speed = 10.0 // m/s
	clock := time.Unix(0, 0)
	m := New("test", nil)
	m.now = func() time.Time { return clock }
	state := func(d float64) StateMsg {
		return StateMsg{State: &pb.State{Ride: &pb.Ride{
			Phase: pb.RidePhase_RIDE_PHASE_RIDING, CourseId: "c", CourseDistanceM: 10000,
			DistanceM: d, SpeedMps: speed,
		}}}
	}
	reported := 0.0
	last := -1.0
	for step := 0; step <= 200; step++ { // 10 s in 50 ms frames
		now := float64(step) * 0.05
		clock = time.Unix(0, 0).Add(time.Duration(now * float64(time.Second)))
		switch {
		case step%5 == 0: // a ride tick every 250 ms, a little behind (jitter)
			reported = speed*now - 0.3*float64(step%3)
			next, _ := m.Update(state(reported))
			m = next.(Model)
		case step%2 == 0: // other state, same ride distance
			next, _ := m.Update(state(reported))
			m = next.(Model)
		}
		pos := m.ridePos()
		if last >= 0 && step > 5 {
			if d := pos - last; d < 0.2 || d > 0.8 {
				t.Fatalf("at %.2f s the position moved %.2f m in a frame, want about %.2f", now, d, speed*0.05)
			}
		}
		last = pos
	}
}

func TestSpanClipsToScreen(t *testing.T) {
	for _, c := range []struct {
		lo, hi float64
		a, b   int
	}{
		{2.5, 7.2, 2, 8},      // as the loop i := floor(lo); i < hi
		{-1e9, 1e9, 0, 100},   // a sprite beside the rider: only the screen
		{120, 130, 0, 0},      // off screen
		{math.NaN(), 5, 0, 0}, // degenerate projection
	} {
		if a, b := span(c.lo, c.hi, 100); a != c.a || b != c.b {
			t.Errorf("span(%v, %v) = %d, %d; want %d, %d", c.lo, c.hi, a, b, c.a, c.b)
		}
	}
}

// A building right beside the rider projects to millions of pixels; the
// frame must only draw the ones on screen.
func TestRoadBuildingBesideRider(t *testing.T) {
	c := testCourse(200, flat, flat)
	c.Buildings = []*pb.Building{{DistanceM: 105, OffsetM: 4.2, LengthM: 30, DepthM: 1, HeightM: 9, Kind: pb.BuildingKind_BUILDING_KIND_HOUSE}}
	sc := newRoadScene(c)
	start := time.Now()
	for pos := 90.0; pos < 110; pos += 0.25 {
		sc.render(pos, -1, 200, 45)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("80 frames passing a building took %v", el)
	}
}
