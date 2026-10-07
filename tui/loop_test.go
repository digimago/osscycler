package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/digimago/osscycler/api"
	"github.com/digimago/osscycler/course"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

func trackCourses() []*pb.Course {
	var out []*pb.Course
	for _, c := range course.Tracks() {
		out = append(out, api.CourseToProto(c, nil))
	}
	return out
}

var lapClock = time.Date(2026, 10, 7, 19, 0, 0, 0, time.Local)

// loopState is lap n of the oval under way, 120 m in, laps before it done.
func loopState(n int) *pb.State {
	st := sample()
	st.Trainer.ResistanceCalibrationRequired = false
	st.Profile = &pb.RiderProfile{WeightKg: 80, FtpW: 200}
	st.Ride = &pb.Ride{Phase: pb.RidePhase_RIDE_PHASE_RIDING, CourseId: "oval-400", CourseName: "Oval 400 m",
		CourseDistanceM: 400, DistanceM: 120, SpeedMps: 10, ElapsedS: float64(40 * n), Loop: true, Lap: uint32(n), LapElapsedS: 12.3,
		Ghost: &pb.RideGhost{Label: "PB lap 2", DistanceM: 130, GapS: 0.4, TimeS: 39.4}}
	if n > 1 {
		lap := func(k int, s float64) *pb.RideLap {
			return &pb.RideLap{Number: uint32(k), TimeS: s, AvgPowerW: 250, FinishedUnixMs: lapClock.Add(time.Duration(40*k) * time.Second).UnixMilli()}
		}
		times := map[int]float64{1: 52.1, 2: 39.4, 3: 40.2, 4: 39.9}
		st.Ride.LastLap = lap(n-1, times[n-1])
		st.Ride.BestLap = lap(min(2, n-1), times[min(2, n-1)])
	}
	return st
}

func loopModel(t *testing.T, cmds *stubCommands) Model {
	t.Helper()
	m, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m, _ = m.Update(coursesMsg{courses: trackCourses()})
	mm := m.(Model)
	mm.now = func() time.Time { return lapClock }
	return mm
}

// update applies msg without running what it asks for: the road view's
// frames would tick for ever.
func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestLoopRideScreen(t *testing.T) {
	m := update(loopModel(t, &stubCommands{}), StateMsg{State: loopState(3)})
	out := plain(m.render())
	for _, want := range []string{"LAP 3", "+0.4 s behind", "LAP TO GO", "best lap 0:39.4 (lap 2)", "lap 3", "x x end ride", "w workout"} {
		if !strings.Contains(out, want) {
			t.Errorf("loop ride screen lacks %q:\n%s", want, out)
		}
	}
	// The strip ahead runs over the line into the next lap.
	st := loopState(3)
	st.Ride.DistanceM = 330
	m = update(m, StateMsg{State: st})
	if strip := plain(gradeStrip(m.courses["oval-400"], 330, -1, 120)); !strings.Contains(strip, "LAP") || strings.Contains(strip, "FINISH") {
		t.Errorf("strip near the line:\n%s", strip)
	}
}

func TestLapPopIn(t *testing.T) {
	cmds := &stubCommands{results: []*pb.RideResult{{CourseId: "oval-400", ElapsedS: 38.8, PersonalBest: true,
		FinishedUnixMs: time.Date(2026, 10, 5, 18, 31, 0, 0, time.Local).UnixMilli()}}}
	m := loopModel(t, cmds)
	m.results = cmds.results                     // as fetched after a lap
	m = update(m, StateMsg{State: loopState(2)}) // joined: no pop-in for a lap done before
	if m.lapsPanel(30) != "" {
		t.Fatal("pop-in shown on joining")
	}
	m = update(m, StateMsg{State: loopState(3)})
	panel := m.lapsPanel(30)
	out := plain(panel)
	for _, want := range []string{"LAPS · Oval 400 m", "★ PB", "0:38.8", "5 Oct 18:31", "lap 1", "lap 2", "0:39.4", "7 Oct 19:01", "lap 3", "0:12.3", "now"} {
		if !strings.Contains(out, want) {
			t.Errorf("pop-in lacks %q:\n%s", want, out)
		}
	}
	// Fixed width, on the left of the screen.
	if w := lipgloss.Width(panel); w != lapsBoxCols+4 {
		t.Errorf("pop-in %d wide", w)
	}
	if !strings.Contains(plain(m.render()), "LAPS · Oval") {
		t.Error("pop-in not on screen")
	}
	// The fastest is bold, the lap under way italic.
	if !strings.Contains(panel, lapFastest.Render(fmtLapRow("★ PB", 38.8, "5 Oct 18:31"))) {
		t.Errorf("fastest not bold:\n%q", panel)
	}
	if !strings.Contains(panel, lapRunning.Render(fmtLapRow("lap 3", 12.3, "now"))) {
		t.Errorf("running lap not italic:\n%q", panel)
	}
	// Gone 15 s after the lap.
	m.now = func() time.Time { return lapClock.Add(lapsShownFor + time.Second) }
	if m.lapsPanel(30) != "" || strings.Contains(plain(m.render()), "LAPS · Oval") {
		t.Error("pop-in still shown after 15 s")
	}
}

func fmtLapRow(name string, s float64, when string) string {
	if when == "now" {
		return strings.TrimRight(padLap(name, s)+"  now", " ")
	}
	return padLap(name, s) + "  " + when
}

func padLap(name string, s float64) string {
	return strings.Replace(strings.Replace("NAME    TIME", "NAME   ", pad(name, 7), 1), "TIME", pad(lapTime(s), -7), 1)
}

// pad pads s to n columns, on the right; a negative n pads on the left.
func pad(s string, n int) string {
	if n < 0 {
		return strings.Repeat(" ", -n-len([]rune(s))) + s
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func TestFreeRideOnATrack(t *testing.T) {
	cmds := &stubCommands{}
	m := loopModel(t, cmds).WithMenu()
	m = update(m, StateMsg{State: func() *pb.State { st := sample(); st.Profile = &pb.RiderProfile{WeightKg: 80, FtpW: 200}; return st }()})
	m = pressAll(m, "f")
	if m.menu == nil || !m.menu.tracks {
		t.Fatal("free ride didn't offer the tracks")
	}
	out := plain(m.render())
	for _, want := range []string{"FREE RIDE", "Just the numbers", "↻ Oval 400 m", "0.40 km round, flat", "↻ Figure 8 · 5 km", "42 m up and down"} {
		if !strings.Contains(out, want) {
			t.Errorf("track choice lacks %q:\n%s", want, out)
		}
	}
	// esc goes back to the menu; 1 is the numbers alone.
	if back := pressAll(m, "esc"); back.menu == nil || back.menu.tracks {
		t.Error("esc didn't go back to the menu")
	}
	if dash := pressAll(m, "1"); dash.menu != nil || len(cmds.rides) != 0 {
		t.Error("1 didn't go to the dashboard")
	}
	next, cmd := press(m, "3")
	run(next, cmd)
	if next.menu != nil || len(cmds.rides) != 1 || cmds.rides[0] != "figure-8" {
		t.Errorf("rides %v", cmds.rides)
	}
}

func TestKeysOnALoop(t *testing.T) {
	m := update(loopModel(t, &stubCommands{}), StateMsg{State: loopState(3)})
	if w, _ := press(m, "w"); !w.picking || w.tab != tabWorkouts {
		t.Error("w didn't open the workouts on a loop")
	}
	if g, _ := press(m, "g"); g.input == nil {
		t.Error("g didn't ask for a grade on a loop")
	}
	if r, _ := press(m, "r"); r.picking {
		t.Error("r opened the rides during a ride")
	}
	if x, _ := press(m, "x"); !strings.Contains(x.asking, "end the ride") {
		t.Errorf("x asks %q", x.asking)
	}
}

func TestLoopRoad(t *testing.T) {
	c := trackCourses()[1]
	sc := newRoadScene(c)
	l := c.GetDistanceM()
	for _, s := range []float64{0, 1234.5, l - 3} {
		x0, y0, z0 := sc.at(s)
		x1, y1, z1 := sc.at(s + l)
		if math.Abs(x0-x1) > 1e-6 || math.Abs(y0-y1) > 1e-6 || math.Abs(z0-z1) > 1e-6 {
			t.Errorf("at(%v) differs a lap on", s)
		}
	}
	// Trees stand in the same places every lap.
	a := sc.treeSpots(l-100, l+100)
	b := sc.treeSpots(2*l-100, 2*l+100)
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("tree spots %d and %d", len(a), len(b))
	}
	for i := range a {
		if a[i].k != b[i].k || math.Abs(a[i].s+l-b[i].s) > 1e-6 {
			t.Errorf("spot %d: %+v and %+v", i, a[i], b[i])
		}
	}
	// Drawing over the line works, ghost and all.
	if out := sc.render(l-20, l+15, 80, 20); out == "" {
		t.Error("no road over the line")
	}
	// The ghost just over the line is drawn ahead, not a lap back.
	m := update(loopModel(t, &stubCommands{}), StateMsg{State: loopState(3)})
	st := loopState(3)
	st.Ride.DistanceM, st.Ride.Ghost.DistanceM = 395, 10
	m = update(m, StateMsg{State: st})
	if g := m.loopGhost(395); g != 410 {
		t.Errorf("ghost at %v, want 410", g)
	}
}

func TestGhostCarriedForward(t *testing.T) {
	m := loopModel(t, &stubCommands{})
	at := lapClock
	m.now = func() time.Time { return at }
	report := func(ghost float64) {
		st := loopState(3)
		st.Ride.Ghost.DistanceM = ghost
		m = update(m, StateMsg{State: st})
	}
	report(100)
	at = at.Add(250 * time.Millisecond)
	report(102.5)
	at = at.Add(100 * time.Millisecond)
	if g := m.ghostMoving(); math.Abs(g-103.25) > 0.3 { // half the 10 m/s at first: smoothed
		t.Errorf("ghost drawn at %.2f m", g)
	}
	// Steady reports converge on its speed; over the line it keeps it.
	for _, d := range []float64{105, 107.5, 110, 112.5} {
		at = at.Add(250 * time.Millisecond)
		report(d)
	}
	if math.Abs(m.ghostV-10) > 1 {
		t.Errorf("ghost speed %.1f m/s", m.ghostV)
	}
	for _, d := range []float64{395, 397.5, 0.0} { // a jump first: not speed
		at = at.Add(250 * time.Millisecond)
		report(d)
	}
	if math.Abs(m.ghostV-10) > 1.5 {
		t.Errorf("over the line: %.1f m/s", m.ghostV)
	}
	// A ghost done with its lap waits at the line.
	at = at.Add(250 * time.Millisecond)
	report(400)
	at = at.Add(200 * time.Millisecond)
	if g := m.ghostMoving(); g != 400 {
		t.Errorf("finished ghost drawn at %.2f", g)
	}
}
