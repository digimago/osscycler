package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/protobuf/proto"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// pausing is a TUI on a course ride under way, with the stub commands.
func pausing(t *testing.T) (Model, *stubCommands) {
	t.Helper()
	cmds := &stubCommands{}
	st := rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1200)
	st.Profile = &pb.RiderProfile{WeightKg: 80, FtpW: 200}
	m, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(StateMsg{State: st})
	return m.(Model), cmds
}

// paused sends the TUI the state with the core paused or not: a copy, as
// the core sends a new state each time.
func paused(m Model, on bool) Model {
	st := proto.Clone(m.st).(*pb.State)
	st.Paused = on
	next, _ := m.Update(StateMsg{State: st})
	return next.(Model)
}

func TestPauseKeys(t *testing.T) {
	for _, key := range []string{"P", "space", "p"} {
		m, cmds := pausing(t)
		m, cmd := press(m, key)
		m = run(m, cmd)
		if !slices.Equal(cmds.pauses, []bool{true}) {
			t.Fatalf("%s: asked %v, want a pause", key, cmds.pauses)
		}
		m = paused(m, true)
		if out := plain(m.render()); !strings.Contains(out, "PAUSED") {
			t.Errorf("%s: no PAUSED banner:\n%s", key, out)
		}
		m, cmd = press(m, key)
		run(m, cmd)
		if !slices.Equal(cmds.pauses, []bool{true, false}) {
			t.Errorf("%s again: asked %v, want to carry on", key, cmds.pauses)
		}
	}
}

func TestEscPausesIntoTheMenu(t *testing.T) {
	m, cmds := pausing(t)
	m, cmd := press(m, "esc")
	m = run(m, cmd)
	if m.menu == nil || !slices.Equal(cmds.pauses, []bool{true}) {
		t.Fatalf("esc on a ride: menu %v, asked %v", m.menu != nil, cmds.pauses)
	}
	m = paused(m, true)
	m, cmd = press(m, "esc") // leave the menu: carry on
	run(m, cmd)
	if m.menu != nil || !slices.Equal(cmds.pauses, []bool{true, false}) {
		t.Errorf("esc in the menu: menu %v, asked %v", m.menu != nil, cmds.pauses)
	}
}

func TestPauseKeysWaitForOpenThings(t *testing.T) {
	m, cmds := pausing(t)
	m.input = &input{} // typing a value: space and P are its own
	m, cmd := press(m, "P")
	run(m, cmd)
	if len(cmds.pauses) != 0 {
		t.Errorf("paused while typing: %v", cmds.pauses)
	}
}

func TestAnotherScreenCarryingOnClosesTheMenu(t *testing.T) {
	m, _ := pausing(t)
	m, cmd := press(m, "esc")
	m = run(m, cmd)
	m = paused(m, false) // the core hasn't said yet: the menu stays
	if m.menu == nil {
		t.Fatal("the menu closed before the pause was in")
	}
	m = paused(m, true)
	m = paused(m, false) // the renderer carried on
	if m.menu != nil || m.menuPaused {
		t.Errorf("carried on elsewhere: menu %v, menuPaused %v", m.menu != nil, m.menuPaused)
	}
}
