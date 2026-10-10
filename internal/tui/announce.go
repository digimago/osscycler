package tui

import (
	"time"

	"charm.land/lipgloss/v2"
)

// Announcements (Announce): a message for every screen for a while, from a
// script running a session or a coach on another screen (owner,
// 2026-10-10: a hands-off trainer test whose steps showed only in its own
// terminal). Shown on top, until it expires.

var announceStyle = lipgloss.NewStyle().Background(lipgloss.Color("#ffd700")).Foreground(lipgloss.Color("#000000")).Bold(true)

// announcement is the text to show now, "" for none.
func (m Model) announcement() string {
	a := m.st.GetAnnouncement()
	if a.GetText() == "" || time.Now().UnixMilli() >= a.GetUntilUnixMs() {
		return ""
	}
	return a.GetText()
}

func (m Model) announceBanner(text string) string {
	return announceStyle.Width(m.width).Render(" " + truncate(text, m.width-2))
}
