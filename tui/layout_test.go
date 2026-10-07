package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
