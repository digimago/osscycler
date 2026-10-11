package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// Without a trainer after a few seconds the TUI says what is missing and
// offers a simulated rider or carrying on without; s stops the simulated
// rider later.
func TestStartingWithoutTheHardware(t *testing.T) {
	cmds := &stubCommands{}
	clock := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	m0 := New("x:1", cmds)
	m0.now = func() time.Time { return clock }
	next, _ := m0.Update(tea.WindowSizeMsg{Width: 110, Height: 36})
	st := sample()
	st.Profile = &pb.RiderProfile{Complete: true, WeightKg: 80, FtpW: 220}
	st.Trainer.Sensor = &pb.Sensor{Status: pb.SensorStatus_SENSOR_STATUS_SEARCHING}
	st.Radio = &pb.Radio{Present: false, Error: "no stick"}
	next, _ = next.(Model).Update(StateMsg{State: st})
	m := next.(Model)
	if strings.Contains(plain(m.render()), "NO TRAINER YET") {
		t.Fatal("asked at once; the trainer gets a few seconds")
	}
	clock = clock.Add(9 * time.Second)
	out := plain(m.render())
	for _, want := range []string{"NO TRAINER YET", "✕ ANT+ stick: not found", "✕ trainer: not found", "simulated rider", "carry on without"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel lacks %q:\n%s", want, out)
		}
	}
	m, cmd := press(m, "s")
	m = run(m, cmd)
	if len(cmds.simulated) != 1 || !cmds.simulated[0] || m.askHardware() {
		t.Fatalf("s: simulated %v, still asking %v", cmds.simulated, m.askHardware())
	}
	// The core says it's simulating: the footer says so; s stops it.
	st.Simulated = true
	st.Trainer.Sensor = &pb.Sensor{Status: pb.SensorStatus_SENSOR_STATUS_CONNECTED}
	next, _ = m.Update(StateMsg{State: st})
	m = next.(Model)
	if !strings.Contains(plain(m.render()), "simulated rider: made-up numbers, nothing recorded") {
		t.Errorf("footer:\n%s", plain(m.render()))
	}
	m, cmd = press(m, "s")
	run(m, cmd)
	if len(cmds.simulated) != 2 || cmds.simulated[1] {
		t.Errorf("s again: %v", cmds.simulated)
	}

	// esc carries on without, and the panel stays away.
	m2 := New("x:1", &stubCommands{})
	m2.now = func() time.Time { return clock }
	next, _ = m2.Update(tea.WindowSizeMsg{Width: 110, Height: 36})
	st.Simulated = false
	st.Trainer.Sensor = &pb.Sensor{Status: pb.SensorStatus_SENSOR_STATUS_SEARCHING}
	next, _ = next.(Model).Update(StateMsg{State: st})
	m2 = next.(Model)
	clock = clock.Add(9 * time.Second)
	m2 = pressAll(m2, "esc")
	if m2.askHardware() || !strings.Contains(m2.notice, "carrying on without") {
		t.Errorf("esc: asking %v, notice %q", m2.askHardware(), m2.notice)
	}
}
