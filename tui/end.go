package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The end-ride screen, as Zwift's: e asks, enter saves (the default),
// d d discards the recording, esc keeps riding.

// ending is the end-ride question on screen.
type ending struct {
	discardUntil time.Time // a second d before this discards
}

type endedMsg struct {
	file    string
	discard bool
	err     error
}

// canEnd reports whether e offers to end the ride: the core is
// recording and no course ride or workout is under way.
func (m Model) canEnd() bool {
	return m.st.GetRecording().GetActive() && !m.rideActive() && !m.workoutActive() &&
		!m.picking && m.draft == nil && m.input == nil
}

func (m Model) endKey(key string) (Model, tea.Cmd, bool) {
	if m.ending == nil {
		if key == "e" && m.canEnd() {
			m.ending, m.notice = &ending{}, ""
			return m, nil, true
		}
		return m, nil, false
	}
	switch key {
	case "enter", "s":
		m.ending = nil
		return m, m.endActivity(false), true
	case "d":
		if m.now().Before(m.ending.discardUntil) {
			m.ending, m.notice = nil, ""
			return m, m.endActivity(true), true
		}
		m.ending = &ending{discardUntil: m.now().Add(abortConfirm)}
		m.notice = "press d again to discard: the ride will be deleted"
	case "esc":
		m.ending, m.notice = nil, ""
	}
	return m, nil, true
}

func (m Model) endActivity(discard bool) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		file, err := cmds.EndActivity(ctx, discard)
		return endedMsg{file: file, discard: discard, err: err}
	}
}

func (m Model) onEnded(msg endedMsg) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.notice = "ending the ride failed: " + friendlyErr(msg.err)
	case msg.discard:
		m.notice = "ride discarded"
	default:
		m.notice = "ride saved: " + msg.file
	}
	return m, nil
}

func (m Model) endPanel(width, height int) string {
	r := m.st.GetRecording()
	lines := []string{
		titleStyle.Render("END RIDE?"), "",
		fmt.Sprintf("%s riding · %.2f km", clock(r.GetTimerS()), r.GetDistanceM()/1000), "",
		okStyle.Render("enter save") + dimStyle.Render("  ·  d discard  ·  esc keep riding"),
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}
