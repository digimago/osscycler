package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The start menu: what the rider can do, shown when the TUI starts and
// again with m from the dashboard. Each row stands for the key that does
// the same from the dashboard, so the menu is a way in, not a second set
// of rules.

type menuItem struct {
	key   string // shortcut in the menu
	label string
	does  string
}

var menuItems = []menuItem{
	{"f", "Free ride", "just ride: the dashboard, no course or target"},
	{"r", "Ride a course", "a GPX course, racing your best time on it"},
	{"w", "Workout", "a structured workout, or a fixed power (ERG)"},
	{"a", "Activities", "your recorded rides: save FIT files, race past rides"},
	{"p", "Profile", "weight, height and FTP"},
	{"c", "Calibrate", "spin-down calibration of the trainer"},
	{"q", "Quit", "close the screen (the core keeps running)"},
}

// WithMenu opens the start menu.
func (m Model) WithMenu() Model {
	m.menu = &menuState{}
	return m
}

type menuState struct{ sel int }

// busy reports whether something is already going on that the menu would
// be in the way of: a ride, a workout, manual control or a calibration.
func (m Model) busy() bool {
	return m.rideActive() || m.workoutActive() || m.controlActive() || m.calibrationActive() || m.countdown > 0
}

// canOpenMenu is when m opens the menu: on the plain dashboard.
func (m Model) canOpenMenu() bool {
	return !m.busy() && !m.showRide() && !m.showWorkout() && !m.picking &&
		m.draft == nil && m.input == nil && m.ending == nil && m.onboarding == nil && m.arranging == nil
}

// menuKey handles the menu, and m to open it. It reports whether it used
// the key.
func (m Model) menuKey(key string) (Model, tea.Cmd, bool) {
	if m.onboarding != nil {
		return m, nil, false // the profile form has the keys; the menu waits
	}
	if m.menu == nil {
		if key == "m" && m.canOpenMenu() {
			m.menu, m.notice = &menuState{}, ""
			return m, nil, true
		}
		return m, nil, false
	}
	s := *m.menu
	m.menu = &s
	switch key {
	case "up", "k":
		s.sel = (s.sel + len(menuItems) - 1) % len(menuItems)
	case "down", "j", "tab":
		s.sel = (s.sel + 1) % len(menuItems)
	case "esc", "m":
		m.menu = nil
	case "enter", "space", " ":
		return m.choose(menuItems[s.sel].key)
	default:
		for i, it := range menuItems {
			if key == it.key || key == fmt.Sprint(i+1) {
				return m.choose(it.key)
			}
		}
	}
	return m, nil, true
}

// choose closes the menu and does what the item stands for.
func (m Model) choose(item string) (Model, tea.Cmd, bool) {
	m.menu = nil
	switch item {
	case "q":
		return m, tea.Quit, true
	case "r", "w", "a":
		key := map[string]string{"a": "r"}[item]
		if key == "" {
			key = item
		}
		next, cmd, _ := m.rideKey(key)
		if item == "a" && next.picking {
			next.tab = tabActivities
		}
		return next, cmd, true
	case "p":
		if m.profile() == nil {
			m.notice = "the profile comes from the core: waiting for it"
			return m, nil, true
		}
		return m.startOnboarding(true), nil, true
	case "c":
		if !m.mayCalibrate() {
			m.notice = "calibration needs the trainer connected"
			return m, nil, true
		}
		next, cmd, _ := m.calibrationKey("C")
		return next, cmd, true
	}
	return m, nil, true // f: the dashboard is free riding
}

var (
	menuSelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("220"))
	menuKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
)

func (m Model) menuPanel(width, height int) string {
	labelW := 0
	for _, it := range menuItems {
		labelW = max(labelW, lipgloss.Width(it.label))
	}
	lines := []string{titleStyle.Render("OSSCYCLER"), dimStyle.Render("what would you like to do?"), ""}
	for i, it := range menuItems {
		label := fmt.Sprintf(" %s  %-*s ", it.key, labelW, it.label)
		if i == m.menu.sel {
			label = menuSelStyle.Render(label)
		} else {
			label = " " + menuKeyStyle.Render(it.key) + "  " + fmt.Sprintf("%-*s ", labelW, it.label)
		}
		does, extra := it.does, ""
		if it.key == "c" {
			switch {
			case !m.mayCalibrate():
				does += " (needs the trainer)"
			case m.st.GetTrainer().GetResistanceCalibrationRequired():
				extra = warnStyle.Render(" · recommended")
			}
		}
		lines = append(lines, label+"  "+dimStyle.Render(truncate(does, max(10, width-labelW-12)))+extra)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// menuHelp lists the menu's keys for the help.
func menuHelp() []helpEntry {
	keys := make([]string, len(menuItems))
	for i, it := range menuItems {
		keys[i] = it.key
	}
	return []helpEntry{
		{"↑ ↓  k j", "choose"},
		{"enter", "do it"},
		{strings.Join(keys, " "), "do that one directly (or its number)"},
		{"esc  m", "close: free ride on the dashboard"},
	}
}
