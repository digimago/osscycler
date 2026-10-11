package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// Starting without the hardware (owner's list, item 12): when no trainer
// has come within hardwareGrace of the TUI's first state, a panel says
// what is missing (the ANT+ stick, the trainer and with it power and
// cadence, the heart rate strap) and offers the ways on: wait (it closes
// once the trainer comes), s a simulated rider in its place (the core's
// SetSimulated: nothing is recorded meanwhile), esc carry on without (the
// numbers stay at zero). Not for a core without a stick (-fake). On the
// dashboard s starts or stops the simulated rider later.

const hardwareGrace = 8 * time.Second

// trainerMissing: the core has a stick to look through (or looks for
// one) and no trainer on it, and no simulated rider stands in.
func (m Model) trainerMissing() bool {
	return m.st != nil && m.st.GetRadio() != nil && !m.st.GetSimulated() &&
		m.st.GetTrainer().GetSensor().GetStatus() != pb.SensorStatus_SENSOR_STATUS_CONNECTED
}

// askHardware reports whether the panel is up.
func (m Model) askHardware() bool {
	return m.cmds != nil && !m.hardwareLater && !m.firstState.IsZero() && m.now().Sub(m.firstState) >= hardwareGrace &&
		m.trainerMissing() && m.onboarding == nil && !m.busy()
}

func (m Model) setSimulated(on bool) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		what := "the simulated rider"
		if !on {
			what = "stopping the simulated rider"
		}
		return commandMsg{what, cmds.SetSimulated(ctx, on)}
	}
}

// hardwareKey: s or esc on the panel; s on the dashboard too.
func (m Model) hardwareKey(key string) (Model, tea.Cmd, bool) {
	if m.askHardware() {
		switch key {
		case "s":
			m.hardwareLater = true
			return m, m.setSimulated(true), true
		case "esc":
			m.hardwareLater = true
			m.notice = "carrying on without a trainer: s rides a simulated rider"
			return m, nil, true
		}
		return m, nil, key != "ctrl+c" && key != "q"
	}
	if key == "s" && m.menu == nil && !m.picking && m.draft == nil && m.input == nil && m.ending == nil && m.onboarding == nil {
		switch {
		case m.st.GetSimulated():
			return m, m.setSimulated(false), true
		case m.trainerMissing():
			return m, m.setSimulated(true), true
		}
	}
	return m, nil, false
}

// hardwarePanel says what is missing and how to go on.
func (m Model) hardwarePanel(width, height int) string {
	ok := func(b bool, yes, no string) string {
		if b {
			return okStyle.Render("● " + yes)
		}
		return badStyle.Render("✕ " + no)
	}
	r, tr, hr := m.st.GetRadio(), m.st.GetTrainer().GetSensor(), m.st.GetHeartRate().GetSensor()
	connected := func(s *pb.Sensor) bool { return s.GetStatus() == pb.SensorStatus_SENSOR_STATUS_CONNECTED }
	lines := []string{titleStyle.Render("NO TRAINER YET"), "",
		ok(r.GetPresent(), "ANT+ stick", "ANT+ stick: not found; plug it in"),
		ok(connected(tr), "trainer", "trainer: not found; pedal to wake it (power, cadence and speed come from it)"),
		ok(connected(hr), "heart rate strap", "heart rate strap: not found; moisten it and put it on"),
		"",
		dimStyle.Render("This closes by itself when the trainer is found."),
		"",
		okStyle.Render("s") + "    ride a simulated rider in its place (made-up numbers; nothing is recorded)",
		okStyle.Render("esc") + "  carry on without (the numbers stay at zero)",
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}
