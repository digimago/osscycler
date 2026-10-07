package tui

import (
	"context"
	"fmt"
	"math"

	tea "charm.land/bubbletea/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// Manual trainer control from the dashboard: w sets ERG watts, g a grade,
// l a brake level. While it is on, + and - step the target and x returns
// to free riding. The core clamps targets and refuses manual control
// during a course ride or workout.

func (m Model) control() *pb.TrainerControl { return m.st.GetControl() }

func (m Model) controlActive() bool {
	switch m.control().GetMode() {
	case pb.ControlMode_CONTROL_MODE_POWER, pb.ControlMode_CONTROL_MODE_GRADE, pb.ControlMode_CONTROL_MODE_LEVEL:
		return true
	}
	return false
}

// controlStep is what one + or - press changes.
var controlStep = map[pb.ControlMode]float64{
	pb.ControlMode_CONTROL_MODE_POWER: 5,
	pb.ControlMode_CONTROL_MODE_GRADE: 0.5,
	pb.ControlMode_CONTROL_MODE_LEVEL: 5,
}

// controlText is the target as the footer shows it.
func controlText(c *pb.TrainerControl) string {
	switch c.GetMode() {
	case pb.ControlMode_CONTROL_MODE_POWER:
		return fmt.Sprintf("ERG %.0f W", c.GetTarget())
	case pb.ControlMode_CONTROL_MODE_GRADE:
		return fmt.Sprintf("GRADE %.1f %%", c.GetTarget())
	case pb.ControlMode_CONTROL_MODE_LEVEL:
		return fmt.Sprintf("LEVEL %.0f %%", c.GetTarget())
	}
	return ""
}

func (m Model) setControl(mode pb.ControlMode, v float64) tea.Cmd {
	cmds := m.cmds
	return m.command("trainer control", func(ctx context.Context) error {
		_, err := cmds.SetTrainerControl(ctx, mode, v)
		return err
	})
}

// controlKey handles w, g and l on the dashboard, and + - x while manual
// control is on. It reports whether it used the key.
func (m Model) controlKey(key string) (Model, tea.Cmd, bool) {
	if m.picking || m.rideActive() || m.workoutActive() || m.calibrationActive() || m.countdown > 0 {
		return m, nil, false
	}
	c := m.control()
	prompt := map[string]struct {
		mode   pb.ControlMode
		prompt string
		start  float64
	}{
		"w": {pb.ControlMode_CONTROL_MODE_POWER, "ERG target in watts: ", 150},
		"g": {pb.ControlMode_CONTROL_MODE_GRADE, "Grade in %: ", 3},
		"l": {pb.ControlMode_CONTROL_MODE_LEVEL, "Brake level in % of maximum: ", 30},
	}
	if p, ok := prompt[key]; ok {
		v := p.start
		if c.GetMode() == p.mode {
			v = c.GetTarget()
		}
		m.input = &input{prompt: p.prompt, value: num(v), control: p.mode}
		return m, nil, true
	}
	if !m.controlActive() {
		return m, nil, false
	}
	switch key {
	case "+", "=", "-", "_":
		step := controlStep[c.GetMode()]
		if key == "-" || key == "_" {
			step = -step
		}
		// Land on multiples of the step, as difficulty does.
		want := (math.Floor(c.GetTarget()/math.Abs(step)+1e-9) + 1) * math.Abs(step)
		if step < 0 {
			want = (math.Ceil(c.GetTarget()/math.Abs(step)-1e-9) - 1) * math.Abs(step)
		}
		return m, m.setControl(c.GetMode(), want), true
	case "x", "0":
		cmds := m.cmds
		return m, m.command("release trainer", cmds.ReleaseTrainerControl), true
	}
	return m, nil, false
}

// num writes a target without needless decimals.
func num(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
