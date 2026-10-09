package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestMediumDigits(t *testing.T) {
	got := Medium("10")
	want := "▀█  █▀█\n █  █ █\n▀▀▀ ▀▀▀"
	if got != want {
		t.Errorf("Medium(10) =\n%s\nwant\n%s", got, want)
	}
	if h := lipgloss.Height(Medium("1:23")); h != MediumHeight {
		t.Errorf("height %d", h)
	}
	if lw, mw := lipgloss.Width(Big("12:34")), lipgloss.Width(Medium("12:34")); mw*2 > lw+2 {
		t.Errorf("medium %d wide against large %d: not about half", mw, lw)
	}
}

func TestGridFallsBackToMedium(t *testing.T) {
	ms := []metric{{"POWER", "1234", "W", powerStyle, nil}, {"TIME", "1:23:45", "", powerStyle, nil}, {"TO GO", "12.34", "km", powerStyle, nil}}
	if g := tileGrid(ms, 90, 20, sizeLarge); g != "" {
		t.Error("large digits claimed to fit three tiles across 90 columns")
	}
	m := Model{}
	if g := m.grid(ms, 90, 20); g == "" || !strings.Contains(g, "▀") {
		t.Errorf("no medium fallback:\n%s", g)
	}
	m.mediumDigits = true
	if g := m.grid(ms, 200, 30); strings.Contains(g, "██  ██") {
		t.Errorf("large digits after z:\n%s", g)
	}
}

func TestArrangeTiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.json")
	m := calModel(&stubCommands{}).WithLayout(Layout{}, path)
	m.st.Trainer.ResistanceCalibrationRequired = false // no calibration prompt
	m = pressAll(m, "o")
	if m.arranging == nil || m.arranging.screen != screenDashboard {
		t.Fatalf("o on the dashboard: %+v", m.arranging)
	}
	if out := plain(m.render()); !strings.Contains(out, "TILES: DASHBOARD") || !strings.Contains(out, "[1] Power") || !strings.Contains(out, "[ ] Distance") {
		t.Errorf("editor:\n%s", out)
	}
	// Move cadence to the top, hide speed, show distance; save.
	m = pressAll(m, "down", "down", "K", "K")        // cadence first
	m = pressAll(m, "down", "down", "down", "space") // speed hidden
	m = pressAll(m, "down", "down", "space")         // distance shown
	m, cmd := press(m, "enter")
	if m.arranging != nil {
		t.Fatal("editor still open")
	}
	want := []string{"cadence", "power", "heart_rate", "distance"}
	if got := m.shownTiles(screenDashboard); !slices.Equal(got, want) {
		t.Errorf("tiles %v, want %v", got, want)
	}
	if msg := cmd(); msg.(layoutSavedMsg).err != nil {
		t.Fatal(msg)
	}
	l, err := LoadLayout(path)
	if err != nil || !slices.Equal(l.Tiles[screenDashboard], want) {
		t.Errorf("saved %v, %v", l.Tiles, err)
	}
	if out := plain(m.render()); strings.Index(out, "CADENCE") > strings.Index(out, "POWER") || !strings.Contains(out, "DISTANCE") {
		t.Errorf("dashboard doesn't follow the layout:\n%s", out)
	}

	// At least one tile stays; r restores the defaults; esc cancels.
	m2 := pressAll(m, "o")
	for range 4 {
		m2 = pressAll(m2, "space", "down")
	}
	if n := len(slices.DeleteFunc(slices.Clone(m2.arranging.order), func(id string) bool { return !m2.arranging.shown[id] })); n < 1 {
		t.Error("all tiles hidden")
	}
	m2 = pressAll(m2, "r", "enter")
	if got := m2.shownTiles(screenDashboard); !slices.Equal(got, tileDefaults[screenDashboard]) {
		t.Errorf("after r: %v", got)
	}
	if m3 := pressAll(m, "o", "space", "esc"); !slices.Equal(m3.shownTiles(screenDashboard), want) {
		t.Errorf("esc changed the layout: %v", m3.shownTiles(screenDashboard))
	}

	// z keeps digits medium, and is saved.
	m, cmd = press(m, "z")
	cmd()
	if l, _ := LoadLayout(path); !m.mediumDigits || l.Digits != "medium" {
		t.Errorf("z: medium %v, saved %q", m.mediumDigits, l.Digits)
	}
}

func TestLoadLayoutDropsUnknownTiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tui.json")
	os.WriteFile(path, []byte(`{"tiles":{"ride":["power","warp_speed","power","grade"]},"digits":"medium"}`), 0o600)
	l, err := LoadLayout(path)
	if err != nil || !slices.Equal(l.Tiles[screenRide], []string{"power", "grade"}) || l.Digits != "medium" {
		t.Errorf("%+v, %v", l, err)
	}
	if l, err := LoadLayout(filepath.Join(t.TempDir(), "none.json")); err != nil || l.Tiles != nil {
		t.Errorf("missing file: %+v, %v", l, err)
	}
	os.WriteFile(path, []byte(`{"tiles":`), 0o600)
	if _, err := LoadLayout(path); err == nil {
		t.Error("a broken file loaded")
	}
}

// The help and the editor tell the rider to move tiles with shift and
// the arrows, and that works.
func TestArrangeWithShiftArrows(t *testing.T) {
	m := calModel(&stubCommands{})
	m.st.Trainer.ResistanceCalibrationRequired = false
	m = pressAll(m, "o", "down")
	if out := plain(m.render()); !strings.Contains(out, "hold shift and press ↑/↓ to move it") {
		t.Errorf("editor hint:\n%s", out)
	}
	if out := plain(pressAll(m, "?").render()); !strings.Contains(out, "shift + ↑ ↓") {
		t.Errorf("help:\n%s", out)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	if got := next.(Model).arranging.order[0]; got != "heart_rate" {
		t.Errorf("shift+↑ moved %q to the top, want heart_rate", got)
	}
}

func TestArrangeByNumber(t *testing.T) {
	m := calModel(&stubCommands{})
	m.st.Trainer.ResistanceCalibrationRequired = false
	m = pressAll(m, "o") // power, heart_rate, cadence, speed shown; cadence_5s, distance hidden
	shown := func(m Model) []string {
		var ids []string
		for _, id := range m.arranging.order {
			if m.arranging.shown[id] {
				ids = append(ids, id)
			}
		}
		return ids
	}
	// Speed (4th) to place 1: the others shift down.
	m2 := pressAll(m, "down", "down", "down", "1")
	if got := shown(m2); !slices.Equal(got, []string{"speed", "power", "heart_rate", "cadence"}) {
		t.Errorf("4th to 1: %v", got)
	}
	if m2.arranging.order[m2.arranging.cursor] != "speed" {
		t.Error("the cursor didn't follow the tile")
	}
	// A hidden tile (distance) to place 2 is shown there.
	m2 = pressAll(m, "down", "down", "down", "down", "down", "2")
	if got := shown(m2); !slices.Equal(got, []string{"power", "distance", "heart_rate", "cadence", "speed"}) {
		t.Errorf("hidden to 2: %v", got)
	}
	// Past the end: last.
	m2 = pressAll(m, "8")
	if got := shown(m2); !slices.Equal(got, []string{"heart_rate", "cadence", "speed", "power"}) {
		t.Errorf("1st to 8: %v", got)
	}
	if out := plain(m.render()); !strings.Contains(out, "press its place: 1-8") {
		t.Errorf("hint:\n%s", out)
	}
}
