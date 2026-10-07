package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/digimago/osscycler/api"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/workout"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

func TestBig(t *testing.T) {
	got := Big("1.5")
	lines := strings.Split(got, "\n")
	if len(lines) != BigHeight {
		t.Fatalf("%d lines, want %d", len(lines), BigHeight)
	}
	for i, l := range lines {
		if len([]rune(l)) != len([]rune(lines[0])) {
			t.Errorf("line %d width %d differs from line 0 width %d", i, len([]rune(l)), len([]rune(lines[0])))
		}
	}
	// "1" is 3 pixels, "." 1, "5" 3, plus two gaps: (3+1+3)*2 + 2*2 = 18 cells.
	if w := len([]rune(lines[0])); w != 18 {
		t.Errorf("width %d, want 18", w)
	}
}

func u32(v uint32) *uint32   { return &v }
func f64(v float64) *float64 { return &v }

func sample() *pb.State {
	return &pb.State{
		Trainer: &pb.Trainer{
			Sensor:     &pb.Sensor{Status: pb.SensorStatus_SENSOR_STATUS_CONNECTED, DeviceNumber: 47508},
			State:      pb.TrainerState_TRAINER_STATE_IN_USE,
			PowerW:     u32(251),
			CadenceRpm: u32(88),
			SpeedMps:   f64(8.333),
			DistanceM:  12345,
			ElapsedS:   3725,
			// Shown as a warning in the footer.
			ResistanceCalibrationRequired: true,
		},
		HeartRate: &pb.HeartRate{
			Sensor: &pb.Sensor{Status: pb.SensorStatus_SENSOR_STATUS_SEARCHING},
		},
	}
}

func sized(w, h int) Model {
	m, _ := New("127.0.0.1:7420", nil).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(Model)
}

func TestRenderBig(t *testing.T) {
	m, _ := sized(100, 30).Update(StateMsg{State: sample()})
	out := plain(m.(Model).render())
	for _, want := range []string{"POWER", "HEART RATE", "CADENCE", "SPEED", "1:02:05", "12.35 km", "riding",
		"trainer #47508", "hrm searching", "spin-down calibration recommended"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if !strings.Contains(out, "██") {
		t.Error("expected big digits at 100x30")
	}
	if n := strings.Count(out, "\n") + 1; n > 30 {
		t.Errorf("view is %d lines, taller than the 30-line terminal", n)
	}
}

func TestRenderCompactWhenSmall(t *testing.T) {
	m, _ := sized(60, 12).Update(StateMsg{State: sample()})
	out := plain(m.(Model).render())
	if strings.Contains(out, "██") {
		t.Error("big digits on a 60x12 terminal")
	}
	for _, want := range []string{"251", "88", "30.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact view lacks %q", want)
		}
	}
}

func TestDisconnectHidesNumbers(t *testing.T) {
	m, _ := sized(60, 12).Update(StateMsg{State: sample()})
	m, _ = m.Update(ConnMsg{Err: errors.New("core not reachable, retrying")})
	out := plain(m.(Model).render())
	if strings.Contains(out, "251") {
		t.Error("stale power shown after disconnect")
	}
	if !strings.Contains(out, "core not reachable") {
		t.Error("disconnect reason not shown")
	}
}

type stubCommands struct {
	started, cancelled int
	courses            []*pb.Course
	rides              []string
	races              []string
	stops              int
	difficulties       []float64
	workouts           []*pb.WorkoutDef
	saved              []*pb.WorkoutDef
	startedWorkouts    []string
	workoutStops       int
	skips              int
	intensities        []float64
	ftps               []float64
	ends               []bool // discard flags
	results            []*pb.RideResult
	profile            *pb.RiderProfile
	activities         []*pb.Activity
	controls           []string
	exports            []string
	fitBytes           []byte
	profileCalls       [][2]*float64
}

func (s *stubCommands) SetProfile(_ context.Context, w, f *float64) (*pb.RiderProfile, error) {
	p := s.profile
	if p == nil {
		p = &pb.RiderProfile{}
	}
	if w != nil {
		p.WeightKg, p.SuggestedFtpW = *w, float64(int(*w*2.5/5+0.5)*5)
	}
	if f != nil {
		p.FtpW = *f
	}
	s.profile = p
	s.profileCalls = append(s.profileCalls, [2]*float64{w, f})
	return p, nil
}

func (s *stubCommands) SetTrainerControl(_ context.Context, mode pb.ControlMode, v float64) (float64, error) {
	s.controls = append(s.controls, fmt.Sprintf("%v %g", mode, v))
	return v, nil
}

func (s *stubCommands) ReleaseTrainerControl(context.Context) error {
	s.controls = append(s.controls, "release")
	return nil
}

func (s *stubCommands) ListActivities(context.Context) ([]*pb.Activity, error) {
	return s.activities, nil
}

func (s *stubCommands) ExportActivity(_ context.Context, name string, w io.Writer) (int64, error) {
	s.exports = append(s.exports, name)
	n, err := w.Write(s.fitBytes)
	return int64(n), err
}

func (s *stubCommands) ListResults(context.Context) ([]*pb.RideResult, error) {
	return s.results, nil
}

func (s *stubCommands) EndActivity(_ context.Context, discard bool) (string, error) {
	s.ends = append(s.ends, discard)
	if discard {
		return "", nil
	}
	return "2026-10-07-183010.fit", nil
}

func (s *stubCommands) StartCalibration(context.Context) error  { s.started++; return nil }
func (s *stubCommands) CancelCalibration(context.Context) error { s.cancelled++; return nil }
func (s *stubCommands) ListCourses(context.Context) ([]*pb.Course, error) {
	return s.courses, nil
}
func (s *stubCommands) StartRide(_ context.Context, id string) error {
	s.rides = append(s.rides, id)
	return nil
}
func (s *stubCommands) StartRideAgainst(_ context.Context, id string, finished int64) error {
	s.races = append(s.races, fmt.Sprintf("%s@%d", id, finished))
	return nil
}
func (s *stubCommands) StopRide(context.Context) error { s.stops++; return nil }
func (s *stubCommands) ListWorkouts(context.Context) ([]*pb.WorkoutDef, error) {
	return s.workouts, nil
}
func (s *stubCommands) SaveWorkout(_ context.Context, id string, w *pb.WorkoutDef) (string, error) {
	s.saved = append(s.saved, w)
	if id == "" {
		id = "new-id"
	}
	return id, nil
}
func (s *stubCommands) StartWorkout(_ context.Context, id string) error {
	s.startedWorkouts = append(s.startedWorkouts, id)
	return nil
}
func (s *stubCommands) StopWorkout(context.Context) error { s.workoutStops++; return nil }
func (s *stubCommands) SkipSegment(context.Context) error { s.skips++; return nil }
func (s *stubCommands) SetIntensity(_ context.Context, p float64) (float64, error) {
	s.intensities = append(s.intensities, p)
	return p, nil
}
func (s *stubCommands) SetFTP(_ context.Context, w float64) (float64, error) {
	s.ftps = append(s.ftps, w)
	return w, nil
}
func (s *stubCommands) SetDifficulty(_ context.Context, p float64) (float64, error) {
	s.difficulties = append(s.difficulties, p)
	return p, nil
}

// calModel is a connected model whose trainer asks for a spin-down.
func calModel(cmds Commands) Model {
	st := sample()
	m, _ := New("127.0.0.1:7420", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(StateMsg{State: st})
	return m.(Model)
}

func press(m Model, key string) (Model, tea.Cmd) {
	var k tea.KeyPressMsg
	named := map[string]rune{"esc": tea.KeyEscape, "enter": tea.KeyEnter, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "space": tea.KeySpace, "f1": tea.KeyF1}
	switch code, ok := named[key]; {
	case ok:
		k = tea.KeyPressMsg{Code: code}
	default:
		k = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

// run executes a command and feeds its messages back, like the runtime
// does, following batches and the commands that updates return.
func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = run(m, c)
		}
		return m
	}
	next, follow := m.Update(msg)
	return run(next.(Model), follow)
}

func TestCalibrationKeyOnlyWhenRequired(t *testing.T) {
	st := sample()
	st.Trainer.ResistanceCalibrationRequired = false
	m, _ := New("x:1", &stubCommands{}).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(StateMsg{State: st})
	m2, cmd := press(m.(Model), "c")
	if m2.countdown != 0 || cmd != nil {
		t.Fatalf("countdown %d after c without calibration required", m2.countdown)
	}
	// Read-only display: c does nothing either.
	if m3, _ := press(calModel(nil), "c"); m3.countdown != 0 {
		t.Fatal("read-only model started a countdown")
	}
}

func TestCountdownStartsCalibration(t *testing.T) {
	cmds := &stubCommands{}
	m, cmd := press(calModel(cmds), "c")
	if m.countdown != CountdownSeconds || cmd == nil {
		t.Fatalf("countdown = %d, want %d", m.countdown, CountdownSeconds)
	}
	out := plain(m.render())
	for _, want := range []string{"SPIN-DOWN CALIBRATION", "starting in 10 s", "esc to abort"} {
		if !strings.Contains(out, want) {
			t.Errorf("countdown view lacks %q", want)
		}
	}
	// Feed ticks directly instead of waiting ten real seconds.
	var next tea.Model = m
	for i := 0; i < CountdownSeconds; i++ {
		next, cmd = next.Update(countdownMsg{id: m.countdownID})
	}
	m = next.(Model)
	if m.countdown != 0 || cmds.started != 0 {
		t.Fatalf("countdown %d, started %d before running the final command", m.countdown, cmds.started)
	}
	m = run(m, cmd)
	if cmds.started != 1 {
		t.Fatalf("StartCalibration called %d times, want 1", cmds.started)
	}
}

func TestCountdownAbort(t *testing.T) {
	cmds := &stubCommands{}
	m, _ := press(calModel(cmds), "c")
	id := m.countdownID
	m, _ = press(m, "esc")
	if m.countdown != 0 || !strings.Contains(plain(m.render()), "calibration aborted") {
		t.Fatalf("esc did not abort: countdown %d", m.countdown)
	}
	// A tick from the aborted countdown must not resurrect it.
	next, cmd := m.Update(countdownMsg{id: id})
	if next.(Model).countdown != 0 || cmd != nil {
		t.Fatal("stale tick restarted the countdown")
	}
	if cmds.started != 0 {
		t.Fatal("aborted countdown started a calibration")
	}
}

func TestCalibrationPanels(t *testing.T) {
	cmds := &stubCommands{}
	m := calModel(cmds)
	st := sample()
	st.CoreTimeNs = int64(20 * time.Second)
	cal := &pb.Calibration{
		Phase:          pb.CalibrationPhase_CALIBRATION_PHASE_IN_PROGRESS,
		SpeedCondition: pb.CalibrationCondition_CALIBRATION_CONDITION_TOO_LOW,
		TargetSpeedMps: f64(9.7),
	}
	st.Trainer.Calibration = cal
	next, _ := m.Update(StateMsg{State: st})
	m = next.(Model)
	if out := plain(m.render()); !strings.Contains(out, "PEDAL UP to 35 km/h") || !strings.Contains(out, "esc to cancel") {
		t.Errorf("speed-too-low panel:\n%s", out)
	}

	m, cmd := press(m, "esc")
	run(m, cmd)
	if cmds.cancelled != 1 {
		t.Errorf("esc during calibration: cancel called %d times", cmds.cancelled)
	}

	cal.SpeedCondition = pb.CalibrationCondition_CALIBRATION_CONDITION_OK
	next, _ = m.Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); !strings.Contains(out, "STOP PEDALLING") {
		t.Errorf("speed-ok panel lacks STOP PEDALLING:\n%s", out)
	}

	cal.Phase = pb.CalibrationPhase_CALIBRATION_PHASE_SUCCEEDED
	cal.SpinDownMs = u32(2980)
	cal.PhaseChangedNs = int64(15 * time.Second)
	st.Trainer.ResistanceCalibrationRequired = false
	next, _ = m.Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); !strings.Contains(out, "CALIBRATED") || !strings.Contains(out, "2980 ms") {
		t.Errorf("result panel:\n%s", out)
	}

	// After resultShownFor the tiles come back.
	st.CoreTimeNs = int64(30 * time.Second)
	next, _ = m.Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); strings.Contains(out, "CALIBRATED") || !strings.Contains(out, "POWER") {
		t.Errorf("result still shown after %v", resultShownFor)
	}
}

func TestCommandErrorShown(t *testing.T) {
	m := calModel(&stubCommands{})
	next, _ := m.Update(commandMsg{what: "start calibration", err: errors.New("trainer not connected")})
	if out := plain(next.(Model).render()); !strings.Contains(out, "start calibration failed: trainer not connected") {
		t.Errorf("error not shown:\n%s", out)
	}
}

// hillCourse is 3 km: 1 km flat, 1 km at 8 %, 1 km at -5 %, every 10 m.
func hillCourse() *pb.Course {
	c := &pb.Course{Id: "hill", Name: "Hill Test", DistanceM: 3000, GainM: 80, MaxGradePct: 8, ProfileStepM: 10}
	ele := float32(100)
	for i := 0; i <= 300; i++ {
		g := float32(0)
		switch {
		case i >= 200:
			g = -5
		case i >= 100:
			g = 8
		}
		c.ProfileGradePct = append(c.ProfileGradePct, g)
		c.ProfileElevationM = append(c.ProfileElevationM, ele)
		ele += g / 10
	}
	return c
}

func TestGradeColor(t *testing.T) {
	hex := func(c interface{ RGBA() (r, g, b, a uint32) }) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	for _, tc := range []struct {
		grade float64
		want  string
	}{
		{0, "#2c7bb6"}, {16, "#7f0000"}, {25, "#7f0000"}, {-3, "#6c7a89"},
	} {
		if got := hex(gradeColor(tc.grade)); got != tc.want {
			t.Errorf("gradeColor(%v) = %s, want %s", tc.grade, got, tc.want)
		}
	}
	// Past the greens, steeper is never bluer.
	prevB := 300.0
	for g := 5.0; g <= 16; g += 0.5 {
		_, _, b, _ := gradeColor(g).RGBA()
		if float64(b>>8) > prevB+1 && g > 7 {
			t.Errorf("blue rises at %v %%", g)
		}
		prevB = float64(b >> 8)
	}
}

func TestGradeStrip(t *testing.T) {
	c := hillCourse()
	out := plain(gradeStrip(c, 900, -1, 100))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("strip has %d lines, want 3:\n%s", len(lines), out)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 100 {
			t.Errorf("line %d width %d, want 100", i, w)
		}
	}
	// From 900 m: 100 m flat, then the 8 % climb.
	if !strings.HasPrefix(lines[0], "0%") || !strings.Contains(lines[0], "8%") {
		t.Errorf("labels: %q", lines[0])
	}
	for _, tick := range []string{"now", "50", "100", "200"} {
		if !strings.Contains(lines[2], tick) {
			t.Errorf("ruler lacks %q: %q", tick, lines[2])
		}
	}
	if near := plain(gradeStrip(c, 2850, -1, 100)); !strings.Contains(near, "FINISH") {
		t.Errorf("no FINISH 150 m from the line:\n%s", near)
	}
	if gradeStrip(c, 0, -1, 10) != "" {
		t.Error("strip drawn in 10 columns")
	}
}

func rideState(phase pb.RidePhase, dist float64) *pb.State {
	st := sample()
	st.CoreTimeNs = int64(100 * time.Second)
	st.Ride = &pb.Ride{
		Phase: phase, CourseId: "hill", CourseName: "Hill Test", CourseDistanceM: 3000, CourseGainM: 80,
		DistanceM: dist, GradePct: 8, SpeedMps: 3.5, ElapsedS: 754, ClimbedM: 40, AvgPowerW: 245,
		PhaseChangedNs: int64(95 * time.Second),
	}
	return st
}

func TestPickAndStartCourse(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{{Id: "a", Name: "Alpha"}, hillCourse()}}
	m, cmd := press(calModel(cmds), "r")
	m = run(m, cmd)
	if !m.picking {
		t.Fatal("r did not open the course list")
	}
	out := plain(m.render())
	if !strings.Contains(out, "COURSES") || !strings.Contains(out, "Hill Test") || !strings.Contains(out, "3.00 km") {
		t.Errorf("picker:\n%s", out)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd = press(next.(Model), "enter")
	run(m, cmd)
	if len(cmds.rides) != 1 || cmds.rides[0] != "hill" {
		t.Fatalf("StartRide calls: %v", cmds.rides)
	}
	if m.picking {
		t.Error("picker still open after starting")
	}
}

func TestRideScreen(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	m := calModel(cmds)
	next, cmd := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1200)})
	if cmd == nil {
		t.Fatal("no course fetch for an unknown ride course")
	}
	next, _ = next.Update(cmd())
	m = next.(Model)
	out := plain(m.render())
	for _, want := range []string{"POWER", "GRADE", "TIME", "TO GO", "climbed", "now", "8%", "x x abort ride"} {
		if !strings.Contains(out, want) {
			t.Errorf("ride screen lacks %q", want)
		}
	}
	if strings.Contains(out, "c calibrate") {
		t.Error("calibration offered during a ride")
	}
	if n := strings.Count(out, "\n") + 1; n > 30 {
		t.Errorf("ride screen is %d lines on a 30-line terminal", n)
	}
	// The strip is the last thing on screen.
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[len(lines)-1], "now") {
		t.Errorf("last line is not the strip ruler: %q", lines[len(lines)-1])
	}

	// x once warns, twice aborts.
	m, cmd = press(m, "x")
	if cmd != nil || !strings.Contains(plain(m.render()), "press x again") {
		t.Fatal("first x did not ask for confirmation")
	}
	m, cmd = press(m, "x")
	run(m, cmd)
	if cmds.stops != 1 {
		t.Fatalf("StopRide calls %d after x x", cmds.stops)
	}
}

func TestArmedAndFinished(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	m := calModel(cmds)
	next, cmd := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_ARMED, 0)})
	next, _ = next.Update(cmd())
	if out := plain(next.(Model).render()); !strings.Contains(out, "start pedalling to start the clock") {
		t.Errorf("armed screen:\n%s", out)
	}

	next, _ = next.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_FINISHED, 3000)})
	m = next.(Model)
	out := plain(m.render())
	for _, want := range []string{"FINISHED", "Hill Test", "245 W average", "x or enter to close"} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %q:\n%s", want, out)
		}
	}
	m, cmd = press(m, "enter")
	run(m, cmd)
	if cmds.stops != 1 {
		t.Errorf("enter on the result: StopRide calls %d", cmds.stops)
	}
}

func TestClock(t *testing.T) {
	for s, want := range map[float64]string{0: "0:00", 59.9: "0:59", 754: "12:34", 3725: "1:02:05"} {
		if got := clock(s); got != want {
			t.Errorf("clock(%v) = %q, want %q", s, got, want)
		}
	}
}

func TestForcedCalibration(t *testing.T) {
	st := sample()
	st.Trainer.ResistanceCalibrationRequired = false
	cmds := &stubCommands{}
	m, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(StateMsg{State: st})
	if out := plain(m.(Model).render()); !strings.Contains(out, "C recalibrate") || strings.Contains(out, "c calibrate") {
		t.Errorf("hints without calibration required:\n%s", out)
	}
	if m2, _ := press(m.(Model), "c"); m2.countdown != 0 {
		t.Fatal("c started a countdown though the trainer doesn't ask for one")
	}
	m2, cmd := press(m.(Model), "C")
	if m2.countdown != CountdownSeconds || cmd == nil {
		t.Fatalf("C: countdown %d", m2.countdown)
	}

	// Not during a ride.
	rs := rideState(pb.RidePhase_RIDE_PHASE_RIDING, 500)
	rs.Trainer.ResistanceCalibrationRequired = false
	r, _ := m.Update(StateMsg{State: rs})
	if m3, _ := press(r.(Model), "C"); m3.countdown != 0 {
		t.Fatal("C started a calibration during a ride")
	}
}

func TestDifficultyKeys(t *testing.T) {
	cmds := &stubCommands{}
	m := calModel(cmds)
	st := rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1200)
	st.Ride.DifficultyPct = 55
	next, _ := m.Update(StateMsg{State: st})
	m = next.(Model)
	if out := plain(m.render()); !strings.Contains(out, "at 55% difficulty") || !strings.Contains(out, "+/- difficulty 55%") {
		t.Errorf("difficulty not shown:\n%s", out)
	}
	for _, key := range []string{"+", "=", "-", "_"} {
		m2, cmd := press(m, key)
		run(m2, cmd)
	}
	// From 55, up snaps to 60 and down to 50.
	want := []float64{60, 60, 50, 50}
	if fmt.Sprint(cmds.difficulties) != fmt.Sprint(want) {
		t.Errorf("SetDifficulty calls %v, want %v", cmds.difficulties, want)
	}

	// From a step value it moves a whole step.
	st.Ride.DifficultyPct = 50
	next, _ = m.Update(StateMsg{State: st})
	for _, key := range []string{"+", "-"} {
		m2, cmd := press(next.(Model), key)
		run(m2, cmd)
	}
	if got := cmds.difficulties[4:]; got[0] != 60 || got[1] != 40 {
		t.Errorf("from 50: %v, want [60 40]", got)
	}

	// Clamped at the ends.
	st.Ride.DifficultyPct = 100
	next, _ = m.Update(StateMsg{State: st})
	m2, cmd := press(next.(Model), "+")
	run(m2, cmd)
	st.Ride.DifficultyPct = 0
	next, _ = m.Update(StateMsg{State: st})
	m2, cmd = press(next.(Model), "-")
	run(m2, cmd)
	if got := cmds.difficulties[len(cmds.difficulties)-2:]; got[0] != 100 || got[1] != 0 {
		t.Errorf("clamping: %v", got)
	}
}

func testWorkoutDef(t *testing.T) *pb.WorkoutDef {
	t.Helper()
	w, err := workout.ParseText(strings.NewReader("name Sweet Spot\nwarmup 5m 40-70%\n2x 5m 90% / 2m 50% @90rpm\n> 10s Smooth now\nfree 2m\n"))
	if err != nil {
		t.Fatal(err)
	}
	return api.WorkoutToProto("sweet-spot", w)
}

func workoutState(phase pb.WorkoutPhase, elapsed float64) *pb.State {
	st := sample()
	st.CoreTimeNs = int64(100 * time.Second)
	st.Workout = &pb.WorkoutProgress{
		Phase: phase, Id: "sweet-spot", Name: "Sweet Spot", DurationS: 1140, ElapsedS: elapsed,
		Segment: 1, SegmentLabel: "Interval 1/2 on", SegmentRemainingS: 241, TargetW: 225, TargetCadence: 90,
		NextLabel: "Interval 1/2 off", NextTargetW: 125, Message: "Smooth now",
		FtpW: 250, IntensityPct: 100, AvgPowerW: 190, PhaseChangedNs: int64(90 * time.Second),
	}
	return st
}

func TestWorkoutPicker(t *testing.T) {
	broken := &pb.WorkoutDef{Id: "broken", Name: "broken", Error: "parse .zwo: EOF"}
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}, workouts: []*pb.WorkoutDef{testWorkoutDef(t), broken}}
	m, cmd := press(calModel(cmds), "w")
	m = run(m, cmd)
	out := plain(m.render())
	for _, want := range []string{"WORKOUTS", "Fixed power (ERG)", "Sweet Spot", "21:00", "broken", "parse .zwo", "n new", "e edit", "f FTP"} {
		if !strings.Contains(out, want) {
			t.Errorf("workout picker lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "COURSES") {
		t.Errorf("workout picker shows the ride tabs:\n%s", out)
	}
	// The first row asks for a fixed power instead.
	if m2, _ := press(m, "enter"); m2.input == nil || m2.picking || !strings.Contains(plain(m2.render()), "ERG target in watts: 150") {
		t.Errorf("fixed power row: input %+v", m2.input)
	}
	m = pressAll(m, "down")
	m2, cmd := press(m, "enter")
	run(m2, cmd)
	if len(cmds.startedWorkouts) != 1 || cmds.startedWorkouts[0] != "sweet-spot" {
		t.Fatalf("StartWorkout calls %v", cmds.startedWorkouts)
	}
	// A broken entry explains itself instead of starting.
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m3, _ := press(next.(Model), "enter")
	if len(cmds.startedWorkouts) != 1 || !strings.Contains(plain(m3.render()), "broken: parse .zwo") {
		t.Errorf("broken workout: starts %v", cmds.startedWorkouts)
	}
}

func TestWorkoutScreen(t *testing.T) {
	cmds := &stubCommands{workouts: []*pb.WorkoutDef{testWorkoutDef(t)}}
	m := calModel(cmds)
	next, cmd := m.Update(StateMsg{State: workoutState(pb.WorkoutPhase_WORKOUT_PHASE_RUNNING, 400)})
	m = run(next.(Model), cmd)
	out := plain(m.render())
	for _, want := range []string{"TARGET", "INTERVAL 1/2 ON", "left", "next: Interval 1/2 off @ 125 W",
		"target 90", "» Smooth now", "FTP 250 W", "+/- intensity 100%", "n skip"} {
		if !strings.Contains(out, want) {
			t.Errorf("workout screen lacks %q", want)
		}
	}
	if strings.Contains(out, "c calibrate") || strings.Contains(out, "r ride") {
		t.Error("other actions offered during a workout")
	}
	// The workout profile is the bottom of the screen.
	lines := strings.Split(out, "\n")
	if !strings.ContainsAny(lines[len(lines)-1], "█▁▂▃▄▅▆▇") {
		t.Errorf("last line is not the workout profile: %q", lines[len(lines)-1])
	}

	for _, key := range []string{"+", "-"} {
		m2, cmd := press(m, key)
		run(m2, cmd)
	}
	if fmt.Sprint(cmds.intensities) != "[101 99]" {
		t.Errorf("intensity calls %v, want [101 99]", cmds.intensities)
	}
	if len(cmds.difficulties) != 0 {
		t.Error("+/- changed difficulty during a workout")
	}
	m2, cmd := press(m, "n")
	run(m2, cmd)
	if cmds.skips != 1 {
		t.Errorf("skips %d", cmds.skips)
	}
	m2, _ = press(m, "x")
	m2, cmd = press(m2, "x")
	run(m2, cmd)
	if cmds.workoutStops != 1 {
		t.Errorf("x x: stops %d", cmds.workoutStops)
	}

	paused, _ := m.Update(StateMsg{State: workoutState(pb.WorkoutPhase_WORKOUT_PHASE_PAUSED, 400)})
	if !strings.Contains(plain(paused.(Model).render()), "paused: pedal to resume") {
		t.Error("paused banner missing")
	}
	done, _ := m.Update(StateMsg{State: workoutState(pb.WorkoutPhase_WORKOUT_PHASE_FINISHED, 1140)})
	if out := plain(done.(Model).render()); !strings.Contains(out, "WORKOUT DONE") || !strings.Contains(out, "190 W average") {
		t.Errorf("result:\n%s", out)
	}
}

func TestFTPInput(t *testing.T) {
	cmds := &stubCommands{}
	st := sample()
	st.Workout = &pb.WorkoutProgress{FtpW: 200, IntensityPct: 100}
	m0, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m0, _ = m0.Update(StateMsg{State: st})
	m, _ := press(m0.(Model), "f")
	if !strings.Contains(plain(m.render()), "FTP in watts: 200") {
		t.Fatalf("prompt:\n%s", plain(m.render()))
	}
	for range 3 {
		next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = next.(Model)
	}
	for _, k := range []string{"2", "6", "5"} {
		m, _ = press(m, k)
	}
	entered, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(entered.(Model), cmd)
	if fmt.Sprint(cmds.ftps) != "[265]" {
		t.Errorf("SetFTP calls %v", cmds.ftps)
	}
}

// pressAll presses keys in order; a multi-character string that isn't a
// named key is typed letter by letter.
func pressAll(m Model, keys ...string) Model {
	for _, k := range keys {
		if len(k) > 1 && !slices.Contains([]string{"esc", "enter", "tab", "backspace", "up", "down", "left", "right", "space", "f1"}, k) {
			for _, r := range k {
				m, _ = press(m, string(r))
			}
			continue
		}
		m, _ = press(m, k)
	}
	return m
}

func formText(m Model) string { return workout.FormatText(tidy(m.draft.w)) }

func TestFormEditor(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	m, cmd := press(calModel(cmds), "w")
	m = run(m, cmd)
	m = pressAll(m, "n")
	if m.draft == nil || m.draft.row != headerRows {
		t.Fatalf("n: draft %+v", m.draft)
	}
	out := plain(m.render())
	for _, want := range []string{"New workout", "My workout", "Warmup", "10m", "40%", "→ 75%", "3×", "95%", "Cooldown", "s save"} {
		if !strings.Contains(out, want) {
			t.Errorf("form lacks %q:\n%s", want, out)
		}
	}

	// Rename: q and s are letters here, not quit and save.
	m = pressAll(m, "up", "up", "up", "enter")
	for range len("My workout") {
		m, _ = press(m, "backspace")
	}
	m = pressAll(m, "Sq", "space", "set", "enter")
	if m.draft.w.Name != "Sq set" || m.draft.buf != nil {
		t.Fatalf("name %q, typing %v", m.draft.w.Name, m.draft.buf != nil)
	}

	// Warmup: type a duration, nudge the end power, then a bad value.
	m = pressAll(m, "down", "down", "down", "right", "8m", "enter", "right", "right", "+", "+")
	m = pressAll(m, "left", "left", "5", "enter")
	if !strings.Contains(m.notice, "add a unit") {
		t.Errorf("bare number accepted for a duration: notice %q", m.notice)
	}
	m = pressAll(m, "esc")
	// Intervals: 3x → 4x via +, retype the cooldown to a steady block.
	m = pressAll(m, "down", "left", "left", "left", "right", "+")
	m = pressAll(m, "down", "t", "T", "T", "T", "T")
	want := "name        Sq set\n\nwarmup   8m 40-77%\n4x 5m 95% / 3m 55%\nramp     5m 40-65%\n"
	if got := formText(m); got != want {
		t.Errorf("after edits:\n%s\nwant:\n%s", got, want)
	}

	// Add, copy, move and delete blocks.
	m = pressAll(m, "a", "c", "K", "d")
	if n := len(m.draft.w.Blocks); n != 4 || m.draft.w.Blocks[3].Type != workout.SteadyState {
		t.Errorf("blocks after a c K d: %s", formText(m))
	}

	// esc with changes asks first; s saves the tidied workout.
	if m2 := pressAll(m, "esc"); m2.draft == nil || !strings.Contains(m2.notice, "esc again") {
		t.Errorf("first esc: draft %v notice %q", m2.draft, m2.notice)
	} else if m3 := pressAll(m2, "esc"); m3.draft != nil {
		t.Error("second esc kept the draft")
	}
	m, cmd = press(m, "s")
	m = run(m, cmd)
	if len(cmds.saved) != 1 || cmds.saved[0].GetName() != "Sq set" || cmds.saved[0].GetBlocks()[2].GetType() != "Ramp" {
		t.Fatalf("saved %v", cmds.saved)
	}
	if m.draft != nil || !m.picking || !strings.Contains(m.notice, "saved new-id") {
		t.Errorf("after save: draft %v picking %v notice %q", m.draft, m.picking, m.notice)
	}
}

func TestFormInvalid(t *testing.T) {
	m := calModel(&stubCommands{})
	m.draft = newDraft("", &workout.Workout{Name: "Empty"})
	if out := plain(m.render()); !strings.Contains(out, "no blocks yet") || !strings.Contains(out, "no blocks") {
		t.Errorf("empty form:\n%s", out)
	}
	m, cmd := press(m, "s")
	if cmd != nil || !strings.Contains(m.notice, "can't save") {
		t.Errorf("saved an empty workout: notice %q", m.notice)
	}
}

func TestRetypeKeepsValues(t *testing.T) {
	b := workout.Block{Type: workout.SteadyState, Duration: 7 * time.Minute, Power: 0.8}
	for range blockTypes {
		b = retype(b, blockTypes[(slices.Index(blockTypes, b.Type)+1)%len(blockTypes)])
		w := tidy(&workout.Workout{Blocks: []workout.Block{b}})
		if err := w.Validate(); err != nil {
			t.Errorf("%s: %v", b.Type, err)
		}
	}
	if b.Type != workout.SteadyState || b.Duration != 7*time.Minute || b.Power != 0.8 {
		t.Errorf("full cycle changed the block: %+v", b)
	}
	c := retype(workout.Block{Type: workout.SteadyState, Duration: time.Minute, Power: 0.7}, workout.Cooldown)
	if c.PowerLow <= c.PowerHigh {
		t.Errorf("cooldown not written high to low: %+v", c)
	}
}

func TestDurationStep(t *testing.T) {
	b := workout.Block{Duration: time.Minute}
	step(&b, fDuration, false)
	if b.Duration != 55*time.Second {
		t.Errorf("1m down = %v", b.Duration)
	}
	b.Duration = 10*time.Minute + 20*time.Second
	step(&b, fDuration, true)
	if b.Duration != 11*time.Minute {
		t.Errorf("10m20s up = %v, want 11m (snapped)", b.Duration)
	}
	b.OffDuration = 5 * time.Second
	step(&b, fOff, false)
	if b.OffDuration != 0 {
		t.Errorf("off can't reach 0: %v", b.OffDuration)
	}
}

func TestEditAsText(t *testing.T) {
	m := calModel(&stubCommands{})
	m.draft = newDraft("", newWorkout())

	// A broken edit keeps the form and the text, and says where it broke.
	next, _ := m.Update(editedMsg{text: "# help\n\nname X\nsteady 5m\n"})
	m = next.(Model)
	if out := plain(m.render()); !strings.Contains(out, "line 4") || !strings.Contains(out, "E to fix it") {
		t.Errorf("error:\n%s", out)
	}
	if m.draft.text != "name X\nsteady 5m\n" || m.draft.w.Name != "My workout" {
		t.Errorf("draft text %q name %q", m.draft.text, m.draft.w.Name)
	}

	next, _ = m.Update(editedMsg{text: "name Threshold\nsteady 10m 100%\n"})
	m = next.(Model)
	if m.draft.err != "" || m.draft.w.Name != "Threshold" || !m.draft.dirty {
		t.Errorf("good edit: %+v", m.draft)
	}
	if out := plain(m.render()); !strings.Contains(out, "Steady") || !strings.Contains(out, "100%") {
		t.Errorf("form after edit:\n%s", out)
	}
}

func TestStripHelp(t *testing.T) {
	in := "# name <text>\n# more help\n\nname A\nsteady 5m 80%\n"
	if got := stripHelp(in); got != "name A\nsteady 5m 80%\n" {
		t.Errorf("stripHelp = %q", got)
	}
}

func TestTourRunsAndStops(t *testing.T) {
	tour := &Tour{steps: []tourStep{
		{"first", []string{"r"}, 10 * time.Millisecond},
		{"second", []string{"tab", "enter"}, 10 * time.Millisecond},
	}}
	var (
		mu  sync.Mutex
		got []string
	)
	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		switch m := msg.(type) {
		case tourCaptionMsg:
			got = append(got, "caption:"+string(m))
		case tourKeyMsg:
			got = append(got, "key:"+string(m))
		}
	}
	tour.Run(context.Background(), send)
	want := "caption:DEMO 1/2  first key:r caption:DEMO 2/2  second key:tab key:enter caption:Tour done: the keys are yours (r ride · c calibrate · q quit)"
	if strings.Join(got, " ") != want {
		t.Errorf("tour sent:\n%v\nwant:\n%v", strings.Join(got, " "), want)
	}
	if tour.Stop() {
		t.Error("Stop after the end reported a running tour")
	}

	// Stopped before it starts: nothing is sent.
	got = nil
	stopped := NewTour()
	stopped.Stop()
	stopped.Run(context.Background(), send)
	if len(got) != 0 {
		t.Errorf("stopped tour sent %v", got)
	}
}

func TestTourInModel(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	tour := NewTour()
	m := calModel(cmds).WithTour(tour)
	next, _ := m.Update(tourCaptionMsg("DEMO 1/9  hello"))
	out := plain(next.(Model).render())
	if !strings.HasPrefix(out, " DEMO 1/9  hello") {
		t.Errorf("banner not on the first line:\n%s", out)
	}
	if n := strings.Count(out, "\n") + 1; n > 30 {
		t.Errorf("screen with banner is %d lines, terminal has 30", n)
	}
	// Tour keys act like real ones but don't stop the tour.
	next, cmd := next.Update(tourKeyMsg("r"))
	m = run(next.(Model), cmd)
	if !m.picking || tour.stopped.Load() {
		t.Fatalf("tour key: picking %v, stopped %v", m.picking, tour.stopped.Load())
	}
	// A real key stops it, and still counts.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if !tour.stopped.Load() || m.picking || !strings.Contains(plain(m.render()), "Tour stopped") {
		t.Errorf("real key: stopped %v, picking %v", tour.stopped.Load(), m.picking)
	}
}

func TestRecordingFooter(t *testing.T) {
	m := calModel(&stubCommands{})
	for _, c := range []struct {
		rec  *pb.Recording
		want string
	}{
		{&pb.Recording{Active: true, TimerS: 754}, "● rec 12:34"},
		{&pb.Recording{Active: true, Paused: true, TimerS: 754}, "⏸ rec 12:34"},
		{&pb.Recording{LastSaved: "2026-10-07-183010.fit"}, "✓ saved 2026-10-07-183010.fit"},
		{&pb.Recording{Error: "disk full"}, "recording failed: disk full"},
	} {
		st := sample()
		st.Recording = c.rec
		next, _ := m.Update(StateMsg{State: st})
		if out := plain(next.(Model).render()); !strings.Contains(out, c.want) {
			t.Errorf("footer lacks %q:\n%s", c.want, out)
		}
	}
}

func TestEndRide(t *testing.T) {
	cmds := &stubCommands{}
	m := calModel(cmds)
	if m2, _ := press(m, "e"); m2.ending != nil {
		t.Fatal("e offered to end a ride that isn't recorded")
	}
	st := sample()
	st.Recording = &pb.Recording{Active: true, TimerS: 3723, DistanceM: 34500}
	st.Trainer.ResistanceCalibrationRequired = false // its warning would hide the hints
	next, _ := m.Update(StateMsg{State: st})
	m = next.(Model)
	if !strings.Contains(plain(m.render()), "e end ride") {
		t.Error("hint missing")
	}
	m, _ = press(m, "e")
	if out := plain(m.render()); !strings.Contains(out, "END RIDE?") || !strings.Contains(out, "1:02:03 riding · 34.50 km") {
		t.Errorf("panel:\n%s", out)
	}
	if m2, _ := press(m, "esc"); m2.ending != nil {
		t.Error("esc didn't keep riding")
	}

	// enter saves, the default.
	m2, cmd := press(m, "enter")
	m2 = run(m2, cmd)
	if fmt.Sprint(cmds.ends) != "[false]" || !strings.Contains(m2.notice, "ride saved: 2026-10-07-183010.fit") {
		t.Errorf("save: ends %v notice %q", cmds.ends, m2.notice)
	}

	// One d only asks; the second discards.
	m3, cmd := press(m, "d")
	if cmd != nil || m3.ending == nil || !strings.Contains(m3.notice, "press d again") {
		t.Fatalf("first d: notice %q", m3.notice)
	}
	m3, cmd = press(m3, "d")
	m3 = run(m3, cmd)
	if fmt.Sprint(cmds.ends) != "[false true]" || m3.notice != "ride discarded" {
		t.Errorf("discard: ends %v notice %q", cmds.ends, m3.notice)
	}

	// Not during a course ride.
	st.Ride = &pb.Ride{Phase: pb.RidePhase_RIDE_PHASE_RIDING}
	next, _ = calModel(cmds).Update(StateMsg{State: st})
	if m4, _ := press(next.(Model), "e"); m4.ending != nil {
		t.Error("e ended a course ride")
	}
}

func TestHistoryTab(t *testing.T) {
	at := time.Date(2026, 10, 7, 18, 30, 0, 0, time.Local).UnixMilli()
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}, results: []*pb.RideResult{
		{FinishedUnixMs: at, CourseId: hillCourse().GetId(), CourseName: "Hill", ElapsedS: 754, AvgPowerW: 231, DistanceM: 3000, PersonalBest: true},
		{FinishedUnixMs: at - 86400000, CourseId: hillCourse().GetId(), CourseName: "Hill", StartM: 1480, ElapsedS: 300, AvgPowerW: 250, DistanceM: 1520, PersonalBest: true},
		{FinishedUnixMs: at - 2*86400000, CourseId: hillCourse().GetId(), CourseName: "Hill", ElapsedS: 800, AvgPowerW: 220, DistanceM: 3000},
	}}
	m, cmd := press(calModel(cmds), "r")
	m = run(m, cmd)
	// The course list shows the full-course PB, not the one from 1.48 km.
	if out := plain(m.render()); !strings.Contains(out, "PB 12:34") || strings.Contains(out, "PB 5:00") {
		t.Errorf("course PB:\n%s", out)
	}
	m = pressAll(m, "tab")
	out := plain(m.render())
	for _, want := range []string{"HISTORY", "2026-10-07 18:30", "12:34", "231 W", "Hill from 1.48 km", "★ PB", "13:20"} {
		if !strings.Contains(out, want) {
			t.Errorf("history lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "★ PB"); n != 2 {
		t.Errorf("%d PB marks, want 2 (one per stretch)", n)
	}
	// Tabs go round both ways.
	if m2 := pressAll(m, "tab"); m2.tab != tabActivities {
		t.Errorf("tab from history: %d", m2.tab)
	}
	if m2 := pressAll(m, "tab", "tab"); m2.tab != tabCourses {
		t.Errorf("tabs don't go round: %d", m2.tab)
	}
	if m2 := pressAll(m, "left"); m2.tab != tabCourses {
		t.Errorf("left from history: %d", m2.tab)
	}
	// enter races the ride under the cursor (the one from 1.48 km).
	m2, cmd := press(pressAll(m, "down"), "enter")
	run(m2, cmd)
	if want := fmt.Sprintf("hill@%d", at-86400000); fmt.Sprint(cmds.races) != "["+want+"]" || m2.picking {
		t.Errorf("races %v, want [%s]", cmds.races, want)
	}
}

func TestRaceFromActivity(t *testing.T) {
	at := time.Date(2026, 10, 7, 18, 30, 0, 0, time.Local).UnixMilli()
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()},
		results: []*pb.RideResult{{FinishedUnixMs: at, CourseId: hillCourse().GetId(), CourseName: "Hill", ElapsedS: 754, DistanceM: 3000, File: "2026-10-07-180000.fit"}},
		activities: []*pb.Activity{
			{Name: "2026-10-07-180000.fit", StartUnixMs: at - 900000, ElapsedS: 900, TimerS: 880, DistanceM: 3000, Virtual: true, Laps: 1},
			{Name: "2026-10-06-180000.fit", StartUnixMs: at - 86400000, ElapsedS: 1800, TimerS: 1800, DistanceM: 9000, Laps: 1},
		}}
	m, cmd := press(calModel(cmds), "r")
	m = run(m, cmd)
	m = pressAll(m, "tab", "tab")
	m2, cmd := press(m, "enter")
	run(m2, cmd)
	if want := fmt.Sprintf("[hill@%d]", at); fmt.Sprint(cmds.races) != want {
		t.Errorf("races %v, want %s", cmds.races, want)
	}
	// A free ride has nothing to race: enter saves it as before.
	if _, cmd := press(pressAll(m, "down"), "enter"); cmd == nil || len(cmds.races) != 1 {
		t.Errorf("free ride: races %v, export %v", cmds.races, cmd != nil)
	}
}

func TestGhostOnRideScreen(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	m := run(press(calModel(cmds), "r")) // load the course list
	m.picking = false
	show := func(phase pb.RidePhase, gap float64) string {
		st := rideState(phase, 1200)
		st.Ride.Ghost = &pb.RideGhost{Label: "PB 7 Oct", DistanceM: 1300, GapS: gap, TimeS: 744}
		next, _ := m.Update(StateMsg{State: st})
		return plain(next.(Model).render())
	}

	out := show(pb.RidePhase_RIDE_PHASE_RIDING, 4.24)
	for _, want := range []string{"TIME vs PB 7 Oct", "+4.2 s behind", "◆PB"} {
		if !strings.Contains(out, want) {
			t.Errorf("behind the ghost: lacks %q\n%s", want, out)
		}
	}
	if out := show(pb.RidePhase_RIDE_PHASE_RIDING, -65); !strings.Contains(out, "-1:05 ahead") {
		t.Errorf("ahead by over a minute:\n%s", out)
	}
	if out := show(pb.RidePhase_RIDE_PHASE_ARMED, 0); !strings.Contains(out, "racing your PB 7 Oct (12:24)") {
		t.Errorf("armed:\n%s", out)
	}
	if out := show(pb.RidePhase_RIDE_PHASE_FINISHED, -3.2); !strings.Contains(out, "★ NEW PB  3.2 s faster than 12:24") {
		t.Errorf("new PB:\n%s", out)
	}
	if out := show(pb.RidePhase_RIDE_PHASE_FINISHED, 5); !strings.Contains(out, "5.0 s off your PB 7 Oct (12:24)") {
		t.Errorf("slower:\n%s", out)
	}

	// No ghost: a first ride on the stretch.
	next, _ := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_FINISHED, 3000)})
	if out := plain(next.(Model).render()); !strings.Contains(out, "first ride on this stretch") {
		t.Errorf("first ride:\n%s", out)
	}
}

func TestOnboarding(t *testing.T) {
	cmds := &stubCommands{}
	m0, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	st := sample()
	st.Profile = &pb.RiderProfile{Missing: []string{"weight_kg", "ftp_w"}, SuggestedFtpW: 200, Path: "/home/r/.config/osscycler/profile.json"}
	next, _ := m0.Update(StateMsg{State: st})
	m := next.(Model)
	out := plain(m.render())
	for _, want := range []string{"WELCOME TO OSSCYCLER", "1/2  Your weight:", "kg", "climbs feel", "profile.json", "esc later"} {
		if !strings.Contains(out, want) {
			t.Errorf("onboarding lacks %q:\n%s", want, out)
		}
	}

	// Out of range is caught before it goes anywhere.
	m = pressAll(m, "5", "enter")
	if len(cmds.profileCalls) != 0 || !strings.Contains(m.notice, "30 to 200") {
		t.Fatalf("weight 5: calls %d, notice %q", len(cmds.profileCalls), m.notice)
	}
	// Weight goes to the core at once; its suggestion fills the FTP step.
	m = pressAll(m, "backspace", "87")
	m, cmd := press(m, "enter")
	m = run(m, cmd)
	if len(cmds.profileCalls) != 1 || *cmds.profileCalls[0][0] != 87 || cmds.profileCalls[0][1] != nil {
		t.Fatalf("calls %v", cmds.profileCalls)
	}
	if out := plain(m.render()); !strings.Contains(out, "2/2  Your FTP: 220") || !strings.Contains(out, "2.5 W/kg") {
		t.Errorf("FTP step:\n%s", out)
	}
	m, cmd = press(m, "enter")
	m = run(m, cmd)
	if len(cmds.profileCalls) != 2 || *cmds.profileCalls[1][1] != 220 || m.onboarding != nil || m.notice != "profile saved" {
		t.Errorf("after FTP: calls %d, onboarding %v, notice %q", len(cmds.profileCalls), m.onboarding, m.notice)
	}

	// The core says it's complete: nothing more to ask; p edits.
	st.Profile = &pb.RiderProfile{Complete: true, WeightKg: 87, FtpW: 220}
	st.Trainer.ResistanceCalibrationRequired = false
	next, _ = m.Update(StateMsg{State: st})
	m = next.(Model)
	m.notice = "" // a notice takes the hints' place
	if m.onboarding != nil || !strings.Contains(plain(m.render()), "p profile") {
		t.Errorf("complete profile:\n%s", plain(m.render()))
	}
	m = pressAll(m, "p")
	if out := plain(m.render()); !strings.Contains(out, "RIDER PROFILE") || !strings.Contains(out, "Your weight: 87") {
		t.Errorf("edit:\n%s", out)
	}
	if m2 := pressAll(m, "esc"); m2.onboarding != nil || m2.onboardLater {
		t.Error("esc while editing should just close")
	}
}

func TestOnboardingLater(t *testing.T) {
	m0, _ := New("x:1", &stubCommands{}).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	st := sample()
	st.Profile = &pb.RiderProfile{WeightKg: 87, Missing: []string{"ftp_w"}, SuggestedFtpW: 220}
	st.Trainer.ResistanceCalibrationRequired = false
	next, _ := m0.Update(StateMsg{State: st})
	m := next.(Model)
	// Only the FTP is missing: straight to it, with the suggestion.
	if out := plain(m.render()); !strings.Contains(out, "Your FTP: 220") {
		t.Fatalf("FTP only:\n%s", out)
	}
	m = pressAll(m, "esc")
	next, _ = m.Update(StateMsg{State: st})
	m = next.(Model)
	if m.onboarding != nil || !strings.Contains(m.notice, "press p") {
		t.Fatalf("put off: onboarding %v notice %q", m.onboarding, m.notice)
	}
	m.notice = ""
	if !strings.Contains(plain(m.render()), "p add your FTP") {
		t.Errorf("hint:\n%s", plain(m.render()))
	}
}

func TestActivitiesExport(t *testing.T) {
	at := time.Date(2026, 10, 7, 18, 30, 0, 0, time.Local).UnixMilli()
	cmds := &stubCommands{fitBytes: []byte("FIT bytes"), activities: []*pb.Activity{
		{Name: "2026-10-07-183000.fit", StartUnixMs: at, TimerS: 3725, DistanceM: 34500, AvgPowerW: 213, Virtual: true, Laps: 3},
		{Name: "2026-10-06-090000.fit", Error: "fit: CRC mismatch"},
	}}
	dir := t.TempDir()
	m0, _ := New("x:1", cmds).WithExportDir(dir).Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	m0, _ = m0.Update(StateMsg{State: sample()})
	m, cmd := press(m0.(Model), "r")
	m = run(m, cmd)
	m = pressAll(m, "left") // ACTIVITIES is the last tab
	out := plain(m.render())
	for _, want := range []string{"ACTIVITIES", "2026-10-07 18:30", "1:02:05", "34.50 km", "213 W", "virtual", "3 laps", "CRC mismatch", "s save the FIT file to " + dir} {
		if !strings.Contains(out, want) {
			t.Errorf("activities lack %q:\n%s", want, out)
		}
	}

	m, cmd = press(m, "s")
	m = run(m, cmd)
	saved := filepath.Join(dir, "2026-10-07-183000.fit")
	if b, err := os.ReadFile(saved); err != nil || string(b) != "FIT bytes" || m.notice != "saved "+saved {
		t.Fatalf("export: %q, %v, notice %q", b, err, m.notice)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".osscycler-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	// Again: the file is there, so nothing is fetched or overwritten.
	m, cmd = press(m, "s")
	m = run(m, cmd)
	if len(cmds.exports) != 1 || !strings.Contains(m.notice, "already saved") {
		t.Errorf("second export: %v, notice %q", cmds.exports, m.notice)
	}
	// A recording that doesn't decode isn't offered.
	m = pressAll(m, "down")
	if _, cmd := press(m, "s"); cmd != nil {
		t.Error("export offered for a broken recording")
	}
}

func TestManualControl(t *testing.T) {
	cmds := &stubCommands{}
	m := calModel(cmds)
	m = pressAll(m, "w", "enter") // the fixed power row
	if out := plain(m.render()); !strings.Contains(out, "ERG target in watts: 150") {
		t.Fatalf("prompt:\n%s", out)
	}
	m = pressAll(m, "backspace", "backspace", "backspace", "200")
	m, cmd := press(m, "enter")
	run(m, cmd)
	// A descent: grades take a minus sign and a decimal.
	m = pressAll(m, "g", "backspace", "-", "2.5")
	m, cmd = press(m, "enter")
	run(m, cmd)

	st := sample()
	st.Trainer.ResistanceCalibrationRequired = false
	st.Control = &pb.TrainerControl{Mode: pb.ControlMode_CONTROL_MODE_POWER, Target: 200}
	next, _ := m.Update(StateMsg{State: st})
	m = next.(Model)
	out := plain(m.render())
	for _, want := range []string{"ERG 200 W", "+/- 5 · w ERG or workout · g grade · l level · x free ride"} {
		if !strings.Contains(out, want) {
			t.Errorf("control on: lacks %q\n%s", want, out)
		}
	}
	for _, key := range []string{"+", "-", "x"} {
		m2, cmd := press(m, key)
		run(m2, cmd)
	}
	want := "[CONTROL_MODE_POWER 200 CONTROL_MODE_GRADE -2.5 CONTROL_MODE_POWER 205 CONTROL_MODE_POWER 195 release]"
	if got := fmt.Sprint(cmds.controls); got != want {
		t.Errorf("calls %s\nwant  %s", got, want)
	}
	if len(cmds.difficulties) != 0 {
		t.Error("+/- changed the difficulty while the trainer was under manual control")
	}

	// Not during a course ride.
	next, _ = calModel(cmds).Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 100)})
	if m2, _ := press(next.(Model), "w"); m2.input != nil {
		t.Error("manual control offered during a course ride")
	}
}

func TestStickMissingFooter(t *testing.T) {
	m := calModel(&stubCommands{})
	st := sample()
	st.Radio = &pb.Radio{Error: "open /dev/ttyANT: no such file or directory"}
	next, _ := m.Update(StateMsg{State: st})
	out := plain(next.(Model).render())
	if !strings.Contains(out, "ANT+ stick not found: plug it in") || strings.Contains(out, "trainer #47508") {
		t.Errorf("missing stick:\n%s", out)
	}
	st.Radio = &pb.Radio{Present: true}
	next, _ = m.Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); strings.Contains(out, "stick not found") || !strings.Contains(out, "trainer #47508") {
		t.Errorf("stick present:\n%s", out)
	}
}

func TestHelp(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	for _, c := range []struct {
		name  string
		setup func(Model) Model
		want  []string
	}{
		{"dashboard", func(m Model) Model { return m }, []string{"KEYS: DASHBOARD", "workouts, or a fixed power", "spin-down"}},
		{"rides", func(m Model) Model { return run(press(m, "r")) }, []string{"KEYS: RIDES", "COURSES, HISTORY, ACTIVITIES"}},
		{"workouts", func(m Model) Model { return run(press(m, "w")) }, []string{"KEYS: WORKOUTS", "Fixed power", "write a new workout"}},
		{"ride", func(m Model) Model {
			next, _ := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 100)})
			return next.(Model)
		}, []string{"KEYS: COURSE RIDE", "road view or big numbers", "only a finished ride counts"}},
	} {
		for _, key := range []string{"?", "h", "f1"} {
			m := c.setup(calModel(cmds))
			m = pressAll(m, key)
			out := plain(m.render())
			for _, want := range c.want {
				if !strings.Contains(out, want) {
					t.Errorf("%s, %s: help lacks %q:\n%s", c.name, key, want, out)
				}
			}
			// Any key closes it and does nothing else.
			m2, cmd := press(m, "r")
			if m2.help || cmd != nil || m2.picking != m.picking {
				t.Errorf("%s: closing key acted: help %v, cmd %v", c.name, m2.help, cmd != nil)
			}
		}
	}

	// In the workout form, h is a letter while typing; F1 still helps.
	m := pressAll(run(press(calModel(cmds), "w")), "n")
	if m.draft == nil {
		t.Fatal("no form")
	}
	typed := ""
	d := *m.draft
	d.buf = &typed // as after enter on a name or value
	m.draft = &d
	if m2 := pressAll(m, "h"); m2.help {
		t.Error("h opened the help while typing")
	}
	if m2 := pressAll(m, "f1"); !m2.help {
		t.Error("F1 didn't open the help while typing")
	}
}

func TestCadenceAverage(t *testing.T) {
	clock := time.Unix(1000, 0)
	m := calModel(&stubCommands{})
	m.now = func() time.Time { return clock }
	m.cadence = nil
	feed := func(rpm int, valid bool, after time.Duration) {
		clock = clock.Add(after)
		st := sample()
		st.Trainer.CadenceRpm = nil
		if valid {
			c := uint32(rpm)
			st.Trainer.CadenceRpm = &c
		}
		next, _ := m.Update(StateMsg{State: st})
		m = next.(Model)
	}
	if _, ok := m.cadenceAvg(); ok {
		t.Error("an average without readings")
	}
	// 80 rpm for 4 s, then 100 rpm for the last second: 84.
	feed(80, true, 0)
	feed(80, true, 2*time.Second)
	feed(100, true, 2*time.Second)
	clock = clock.Add(time.Second)
	if got, ok := m.cadenceAvg(); !ok || math.Abs(got-84) > 0.01 {
		t.Errorf("average %.2f, %v; want 84", got, ok)
	}
	// Older readings fall out of the window; invalid ones don't count.
	feed(0, false, 10*time.Second)
	clock = clock.Add(time.Second)
	if got, ok := m.cadenceAvg(); !ok || got != 100 {
		t.Errorf("after a gap: %.2f, %v; want the 100 that held into the window", got, ok)
	}
	if len(m.cadence) > 3 {
		t.Errorf("%d readings kept", len(m.cadence))
	}
}

func TestCadenceTiles(t *testing.T) {
	cmds := &stubCommands{courses: []*pb.Course{hillCourse()}}
	m := run(press(calModel(cmds), "r"))
	m.picking = false
	next, _ := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 100)})
	m = next.(Model)
	for _, tiles := range []bool{false, true} { // road view and big numbers
		m.tiles = tiles
		if out := plain(m.render()); !strings.Contains(out, "CADENCE 5s") {
			t.Errorf("ride (tiles %v) lacks the cadence:\n%s", tiles, out)
		}
	}
	st := workoutState(pb.WorkoutPhase_WORKOUT_PHASE_RUNNING, 300)
	next, _ = calModel(&stubCommands{workouts: []*pb.WorkoutDef{testWorkoutDef(t)}}).Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); !strings.Contains(out, "CADENCE 5s") || !strings.Contains(out, "aim 90") {
		t.Errorf("workout lacks the cadence and its aim:\n%s", out)
	}
}
