package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The rider arranges the tiles of the dashboard, the course ride and the
// workout screen (o), and picks the digit size (z). Both are kept in a
// file on the machine the TUI runs on: they're about this screen, not the
// core.

// Screens with arrangeable tiles.
const (
	screenDashboard = "dashboard"
	screenRide      = "ride"
	screenWorkout   = "workout"
)

var screenNames = map[string]string{screenDashboard: "DASHBOARD", screenRide: "COURSE RIDE", screenWorkout: "WORKOUT"}

// tileChoices are the tiles each screen offers, defaults first.
var tileChoices = map[string][]string{
	screenDashboard: {"power", "heart_rate", "cadence", "speed", "cadence_5s", "distance"},
	screenRide:      {"power", "cadence_5s", "grade", "time", "to_go", "heart_rate", "cadence", "speed", "climbed", "avg_power"},
	screenWorkout:   {"target", "power", "cadence_5s", "interval", "heart_rate", "cadence", "workout_left", "avg_power", "speed"},
}

// tileDefaults are shown until the rider arranges them.
var tileDefaults = map[string][]string{
	screenDashboard: {"power", "heart_rate", "cadence", "speed"},
	screenRide:      {"power", "cadence_5s", "grade", "time", "to_go"},
	screenWorkout:   {"target", "power", "cadence_5s", "interval", "heart_rate"},
}

// maxTiles keeps tiles big enough to read: two rows of four.
const maxTiles = 8

// Layout is the rider's arrangement, as saved.
type Layout struct {
	// Tiles lists the shown tiles per screen, in order.
	Tiles map[string][]string `json:"tiles,omitempty"`
	// Digits is "medium" to keep the numbers medium-sized; large otherwise.
	Digits string `json:"digits,omitempty"`
}

// LoadLayout reads the arrangement; a missing file is the defaults.
// Unknown tiles are dropped, so an older or newer TUI copes.
func LoadLayout(path string) (Layout, error) {
	var l Layout
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return Layout{}, fmt.Errorf("%s: %w", path, err)
	}
	for screen, ids := range l.Tiles {
		var keep []string
		for _, id := range ids {
			if slices.Contains(tileChoices[screen], id) && !slices.Contains(keep, id) && len(keep) < maxTiles {
				keep = append(keep, id)
			}
		}
		l.Tiles[screen] = keep
	}
	return l, nil
}

// SaveLayout writes the arrangement atomically.
func SaveLayout(path string, l Layout) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tui-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// WithLayout uses an arrangement, saving changes to path ("" keeps them
// for this session only).
func (m Model) WithLayout(l Layout, path string) Model {
	m.layout, m.layoutPath = l, path
	m.mediumDigits = l.Digits == "medium"
	return m
}

// WithNotice shows a message in the footer until the next one.
func (m Model) WithNotice(s string) Model {
	m.notice = s
	return m
}

type layoutSavedMsg struct{ err error }

func (m Model) saveLayout() tea.Cmd {
	if m.layoutPath == "" {
		return nil
	}
	path, l := m.layoutPath, m.layout
	return func() tea.Msg { return layoutSavedMsg{SaveLayout(path, l)} }
}

// shownTiles is the screen's tiles in the rider's order.
func (m Model) shownTiles(screen string) []string {
	if ids := m.layout.Tiles[screen]; len(ids) > 0 {
		return ids
	}
	return tileDefaults[screen]
}

// screenTiles builds the screen's tiles.
func (m Model) screenTiles(screen string) []metric {
	var ms []metric
	for _, id := range m.shownTiles(screen) {
		ms = append(ms, m.tile(id))
	}
	return ms
}

// tile builds one tile from the current state.
func (m Model) tile(id string) metric {
	base := m.metrics() // power, heart rate, cadence, speed
	r, p := m.ride(), m.wk()
	switch id {
	case "power":
		return base[0]
	case "heart_rate":
		return base[1]
	case "cadence":
		return base[2]
	case "cadence_5s":
		var aim uint32
		if m.workoutActive() {
			aim = p.GetTargetCadence()
		}
		return m.cadenceTile(aim)
	case "speed":
		if m.rideActive() {
			return metric{"SPEED", fmt.Sprintf("%.1f", r.GetSpeedMps()*3.6), "km/h", speedStyle, nil}
		}
		return base[3]
	case "distance":
		return metric{"DISTANCE", fmt.Sprintf("%.2f", m.st.GetTrainer().GetDistanceM()/1000), "km", speedStyle, nil}
	case "grade":
		g := r.GetGradePct()
		return metric{"GRADE", fmt.Sprintf("%.1f", g), "%", lipgloss.NewStyle().Foreground(gradeColor(g)), nil}
	case "time":
		return m.timeTile()
	case "to_go":
		return metric{"TO GO", fmt.Sprintf("%.2f", math.Max(0, r.GetCourseDistanceM()-r.GetDistanceM())/1000), "km", speedStyle, nil}
	case "climbed":
		return metric{"CLIMBED", fmt.Sprintf("%.0f", r.GetClimbedM()), fmt.Sprintf("of %.0f m", r.GetCourseGainM()), lipgloss.NewStyle(), nil}
	case "avg_power":
		avg := r.GetAvgPowerW()
		if m.workoutActive() {
			avg = p.GetAvgPowerW()
		}
		return metric{"AVG POWER", fmt.Sprintf("%.0f", avg), "W", powerStyle, nil}
	case "target":
		if p.GetFree() {
			label := "FREE"
			if strings.HasPrefix(p.GetSegmentLabel(), "Max") {
				label = "MAX"
			}
			return metric{"TARGET", "--", label, dimStyle, nil} // the block font has no letters
		}
		return metric{"TARGET", fmt.Sprintf("%.0f", p.GetTargetW()), "W", lipgloss.NewStyle().Foreground(zoneColor(p.GetTargetW() / p.GetFtpW())), nil}
	case "interval":
		return metric{strings.ToUpper(p.GetSegmentLabel()), clock(p.GetSegmentRemainingS()), "left", lipgloss.NewStyle(), nil}
	case "workout_left":
		return metric{"WORKOUT LEFT", clock(math.Max(0, p.GetDurationS()-p.GetElapsedS())), "", lipgloss.NewStyle(), nil}
	}
	return metric{strings.ToUpper(id), "--", "", dimStyle, nil}
}

// tileLabels name the tiles in the editor.
var tileLabels = map[string]string{
	"power": "Power", "heart_rate": "Heart rate", "cadence": "Cadence", "cadence_5s": "Cadence, 5 s average",
	"speed": "Speed", "distance": "Distance", "grade": "Grade", "time": "Time (and the gap to your ghost)",
	"to_go": "Distance to go", "climbed": "Climbed", "avg_power": "Average power", "target": "Target power",
	"interval": "Time left in this part", "workout_left": "Time left in the workout",
}

// tileScreen is the screen whose tiles o arranges now; "" where there are
// none.
func (m Model) tileScreen() string {
	switch {
	case m.picking || m.draft != nil || m.input != nil || m.ending != nil || m.onboarding != nil:
		return ""
	case m.showWorkout() && m.workoutActive():
		return screenWorkout
	case m.showRide() && m.rideActive():
		return screenRide
	case m.showWorkout() || m.showRide() || m.countdown > 0 || m.calibrationActive() || m.showResult():
		return ""
	}
	return screenDashboard
}

// arranging is the tile editor.
type arranging struct {
	screen string
	order  []string // every choice: shown ones first, in order
	shown  map[string]bool
	cursor int
}

func (m Model) startArranging(screen string) Model {
	a := &arranging{screen: screen, shown: map[string]bool{}}
	for _, id := range m.shownTiles(screen) {
		a.order, a.shown[id] = append(a.order, id), true
	}
	for _, id := range tileChoices[screen] {
		if !a.shown[id] {
			a.order = append(a.order, id)
		}
	}
	m.arranging = a
	return m
}

// layoutKey handles o and z, and every key while the editor is open.
func (m Model) layoutKey(key string) (Model, tea.Cmd, bool) {
	if m.arranging == nil {
		switch {
		case key == "o" && m.tileScreen() != "":
			return m.startArranging(m.tileScreen()), nil, true
		case key == "z" && m.draft == nil && m.input == nil && m.onboarding == nil:
			m.mediumDigits = !m.mediumDigits
			m.layout.Digits = ""
			if m.mediumDigits {
				m.layout.Digits = "medium"
			}
			return m, m.saveLayout(), true
		}
		return m, nil, false
	}
	a := *m.arranging
	a.order = slices.Clone(a.order)
	a.shown = maps.Clone(a.shown)
	m.arranging = &a
	count := 0
	for _, v := range a.shown {
		if v {
			count++
		}
	}
	switch key {
	case "up", "k":
		a.cursor = max(0, a.cursor-1)
	case "down", "j":
		a.cursor = min(len(a.order)-1, a.cursor+1)
	case "K", "shift+up", "J", "shift+down":
		to := a.cursor - 1
		if key == "J" || key == "shift+down" {
			to = a.cursor + 1
		}
		if to >= 0 && to < len(a.order) {
			a.order[a.cursor], a.order[to] = a.order[to], a.order[a.cursor]
			a.cursor = to
		}
	case "space", " ", "x":
		id := a.order[a.cursor]
		switch {
		case a.shown[id] && count == 1:
			m.notice = "keep at least one tile"
		case !a.shown[id] && count >= maxTiles:
			m.notice = fmt.Sprintf("at most %d tiles", maxTiles)
		default:
			a.shown[id] = !a.shown[id]
		}
	case "r":
		def := m
		def.layout.Tiles = nil
		cursor := a.cursor
		m.arranging = def.startArranging(a.screen).arranging
		m.arranging.cursor = cursor
	case "enter":
		var ids []string
		for _, id := range a.order {
			if a.shown[id] {
				ids = append(ids, id)
			}
		}
		tiles := maps.Clone(m.layout.Tiles)
		if tiles == nil {
			tiles = map[string][]string{}
		}
		tiles[a.screen] = ids
		if slices.Equal(ids, tileDefaults[a.screen]) {
			delete(tiles, a.screen)
		}
		m.layout.Tiles = tiles
		m.arranging = nil
		return m, m.saveLayout(), true
	case "esc", "o":
		m.arranging = nil
	}
	return m, nil, true
}

func (m Model) arrangePanel(width, height int) string {
	a := m.arranging
	lines := []string{titleStyle.Render("TILES: " + screenNames[a.screen]), ""}
	n := 0
	for i, id := range a.order {
		box := "[ ]"
		style := dimStyle
		if a.shown[id] {
			n++
			box, style = fmt.Sprintf("[%d]", n), lipgloss.NewStyle()
		}
		cursor := "  "
		if i == a.cursor {
			cursor, style = "▸ ", style.Bold(true).Foreground(lipgloss.Color("220"))
		}
		lines = append(lines, style.Render(fmt.Sprintf("%s%s %s", cursor, box, tileLabels[id])))
	}
	size := "large"
	if m.mediumDigits {
		size = "medium"
	}
	lines = append(lines, "",
		dimStyle.Render("↑/↓ choose · hold shift and press ↑/↓ to move it · space show or hide · r defaults · enter save · esc cancel"),
		dimStyle.Render("z digit size: "+size+" (smaller digits fit more tiles)"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}
