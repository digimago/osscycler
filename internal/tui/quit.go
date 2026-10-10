package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Quitting (owner, 2026-10-10): q never closes the screen from a ride; it
// opens the menu (pausing what is under way, as esc does), and the screen
// is closed from there (Quit, or q). With a ride, a workout or manual
// control under way and no other screen connected, Quit asks first:
// end it and save the ride, or leave it paused (the core pauses a ride no
// screen watches after 30 s and ends it after 30 minutes anyway).

// quitKey: q off the menu opens it; the question once Quit is chosen.
// Whether it used the key.
func (m Model) quitKey(key string) (Model, tea.Cmd, bool) {
	if m.quitting {
		switch key {
		case "enter", "q":
			m.quitting = false
			return m, m.endAndQuit(), true
		case "l":
			m.quitting = false
			return m, m.then(tea.QuitMsg{}, func(ctx context.Context) error {
				_, err := m.cmds.SetPaused(ctx, true)
				return err
			}), true
		case "esc":
			m.quitting = false
			m.menu = &menuState{sel: len(menuItems) - 1} // back on Quit
		}
		return m, nil, true
	}
	if key != "q" || m.menu != nil || !m.keysFree() {
		return m, nil, false // in the menu, q is its Quit
	}
	m.menu, m.notice = &menuState{}, ""
	if m.underWay() && !m.paused() {
		m.menuPaused = true
		return m, m.setPaused(true), true
	}
	return m, nil, true
}

// askQuit is the menu's Quit: straight away, unless something is under
// way that no other screen is watching.
func (m Model) askQuit() (Model, tea.Cmd) {
	if m.underWay() && m.st.GetHeads() <= 1 {
		m.quitting = true
		return m, nil
	}
	return m, tea.Quit
}

// endAndQuit ends what is under way, saves the activity, then quits.
func (m Model) endAndQuit() tea.Cmd {
	var calls []func(context.Context) error
	if m.rideActive() {
		calls = append(calls, m.cmds.StopRide)
	}
	if m.workoutActive() {
		calls = append(calls, m.cmds.StopWorkout)
	}
	if m.controlActive() {
		calls = append(calls, m.cmds.ReleaseTrainerControl)
	}
	if m.st.GetRecording().GetActive() {
		calls = append(calls, func(ctx context.Context) error {
			_, err := m.cmds.EndActivity(ctx, false)
			return err
		})
	}
	return m.then(tea.QuitMsg{}, calls...)
}

// then makes calls to the core in order, then sends msg (quitting: a call
// that fails, with the core gone say, is no reason to stay).
func (m Model) then(msg tea.Msg, calls ...func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		for _, call := range calls {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			_ = call(ctx) // best effort on the way out
			cancel()
		}
		return msg
	}
}

func (m Model) quitPanel(width, height int) string {
	what := "manual control"
	switch {
	case m.workoutActive():
		what = "the workout " + m.wk().GetName()
	case m.rideActive():
		what = "the ride on " + m.ride().GetCourseName()
	}
	lines := []string{
		titleStyle.Render("QUIT?"), "",
		fmt.Sprintf("%s is under way, and no other screen is connected.", what), "",
		okStyle.Render("enter end it, save the ride and quit"),
		dimStyle.Render("l  quit and leave it paused (ended and saved after 30 minutes without a screen)"),
		dimStyle.Render("esc  back to the menu"),
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, lines...))
}
