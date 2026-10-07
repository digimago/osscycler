package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// menuModel is a TUI as it starts: the menu open, a state from the core
// with the profile complete.
func menuModel(t *testing.T, st *pb.State) Model {
	t.Helper()
	if st.Profile == nil {
		st.Profile = &pb.RiderProfile{WeightKg: 80, FtpW: 200}
	}
	m, _ := New("x:1", &stubCommands{}).WithMenu().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(StateMsg{State: st})
	return m.(Model)
}

func TestMenuAtStart(t *testing.T) {
	m := menuModel(t, sample())
	out := plain(m.render())
	for _, want := range []string{"Free ride", "Ride a course", "Workout", "Activities", "Profile", "Calibrate", "Quit", "recommended", "esc free ride"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "POWER") {
		t.Error("dashboard tiles under the menu")
	}

	// The trainer not asking: no recommendation.
	st := sample()
	st.Trainer.ResistanceCalibrationRequired = false
	if out := plain(menuModel(t, st).render()); strings.Contains(out, "recommended") {
		t.Error("calibration recommended though the trainer doesn't ask")
	}
}

func TestMenuFreeRide(t *testing.T) {
	for _, keys := range [][]string{{"enter"}, {"f"}, {"1"}, {"esc"}, {"up", "up", "up", "up", "up", "up", "up", "enter"}} {
		m := pressAll(menuModel(t, sample()), keys...)
		if m.menu != nil {
			t.Fatalf("%v: menu still open", keys)
		}
		if out := plain(m.render()); !strings.Contains(out, "POWER") || !strings.Contains(out, "m menu") {
			t.Errorf("%v: not the dashboard:\n%s", keys, out)
		}
	}
	// m opens it again from the dashboard.
	m := pressAll(menuModel(t, sample()), "esc", "m")
	if m.menu == nil {
		t.Fatal("m didn't open the menu")
	}
}

func TestMenuChoices(t *testing.T) {
	cases := []struct {
		keys  []string
		check func(Model) bool
	}{
		{[]string{"r"}, func(m Model) bool { return m.picking && m.tab == tabCourses }},
		{[]string{"down", "enter"}, func(m Model) bool { return m.picking && m.tab == tabCourses }},
		{[]string{"w"}, func(m Model) bool { return m.picking && m.tab == tabWorkouts }},
		{[]string{"a"}, func(m Model) bool { return m.picking && m.tab == tabActivities }},
		{[]string{"p"}, func(m Model) bool { return m.onboarding != nil }},
		{[]string{"c"}, func(m Model) bool { return m.countdown == CountdownSeconds }},
	}
	for _, c := range cases {
		m := pressAll(menuModel(t, sample()), c.keys...)
		if m.menu != nil || !c.check(m) {
			t.Errorf("%v: menu %v, picking %v tab %d, onboarding %v, countdown %d",
				c.keys, m.menu != nil, m.picking, m.tab, m.onboarding != nil, m.countdown)
		}
	}

	if _, cmd := press(menuModel(t, sample()), "q"); cmd == nil {
		t.Error("q in the menu doesn't quit")
	}

	// Calibrating needs the trainer.
	st := sample()
	st.Trainer.Sensor.Status = pb.SensorStatus_SENSOR_STATUS_SEARCHING
	m := pressAll(menuModel(t, st), "c")
	if m.countdown != 0 || !strings.Contains(m.notice, "needs the trainer") {
		t.Errorf("calibrating without the trainer: countdown %d, notice %q", m.countdown, m.notice)
	}
}

func TestMenuStaysOutOfTheWay(t *testing.T) {
	// Joining a ride already running shows the ride, not the menu.
	m := menuModel(t, rideState(pb.RidePhase_RIDE_PHASE_RIDING, 500))
	if m.menu != nil {
		t.Error("menu open over a running ride")
	}
	if m2, _ := press(m, "m"); m2.menu != nil {
		t.Error("m opened the menu during a ride")
	}

	// Onboarding comes first; the menu is there after it.
	st := sample()
	st.Profile = &pb.RiderProfile{Missing: []string{"weight_kg"}}
	m = menuModel(t, st)
	if m.onboarding == nil || !strings.Contains(plain(m.render()), "weight") {
		t.Fatal("onboarding not shown first")
	}
	m, _ = press(m, "esc")
	if m.onboarding != nil || m.menu == nil {
		t.Errorf("after putting onboarding off: onboarding %v, menu %v", m.onboarding != nil, m.menu != nil)
	}

	// o and z don't act under the menu.
	m = pressAll(menuModel(t, sample()), "o", "z")
	if m.arranging != nil || m.mediumDigits {
		t.Error("tile keys acted while the menu was open")
	}
}
