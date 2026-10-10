package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Pausing (owner, 2026-10-10: park the core for a coffee): P or space
// pauses and carries on; esc during a ride, a workout or manual control
// opens the menu with the core paused, and leaving the menu with esc
// carries on. The core holds everything while paused (SetPaused); the TUI
// only asks and shows it.

// keysFree is whether nothing that takes keys is open (menu, picker,
// prompt, editors, onboarding, the end-ride question, a countdown).
func (m Model) keysFree() bool {
	return m.menu == nil && !m.picking && m.draft == nil && m.input == nil && m.ending == nil &&
		m.onboarding == nil && m.arranging == nil && m.countdown == 0
}

// underWay is whether something drives the trainer: a ride, a workout or
// manual control.
func (m Model) underWay() bool { return m.rideActive() || m.workoutActive() || m.controlActive() }

func (m Model) paused() bool { return m.st.GetPaused() }

// pauseKey handles P, space and esc as above; whether it used the key.
func (m Model) pauseKey(key string) (Model, tea.Cmd, bool) {
	if !m.keysFree() {
		return m, nil, false
	}
	switch key {
	case "P", "space", " ":
		return m, m.setPaused(!m.paused()), true
	case "esc":
		if m.underWay() && !m.paused() {
			m.menu, m.menuPaused, m.notice = &menuState{}, true, ""
			return m, m.setPaused(true), true
		}
	}
	return m, nil, false
}

func (m Model) setPaused(on bool) tea.Cmd {
	what := "pause"
	if !on {
		what = "carry on"
	}
	return m.command(what, func(ctx context.Context) error {
		_, err := m.cmds.SetPaused(ctx, on)
		return err
	})
}

var pauseStyle = lipgloss.NewStyle().Background(lipgloss.Color("#3f7fff")).Foreground(lipgloss.Color("#ffffff")).Bold(true)

// pauseBanner is the line shown on top while the core is paused.
func (m Model) pauseBanner() string {
	text := " ⏸ PAUSED   the trainer is flat, the clocks stand   P or space carries on"
	if ms := m.st.GetPausedSinceUnixMs(); ms > 0 {
		if d := time.Since(time.UnixMilli(ms)).Round(time.Second); d > 0 {
			text = " ⏸ PAUSED " + d.String() + "   the trainer is flat, the clocks stand   P or space carries on"
		}
	}
	return pauseStyle.Width(m.width).Render(truncate(text, m.width))
}
