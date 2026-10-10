package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/protobuf/proto"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

func withHeads(m Model, n uint32) Model {
	st := proto.Clone(m.st).(*pb.State)
	st.Heads = n
	if st.Recording == nil {
		st.Recording = &pb.Recording{}
	}
	st.Recording.Active = true
	next, _ := m.Update(StateMsg{State: st})
	return next.(Model)
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQOnARideOpensTheMenuPaused(t *testing.T) {
	m, cmds := pausing(t)
	m, cmd := press(m, "q")
	if m.menu == nil {
		t.Fatal("q on a ride didn't open the menu")
	}
	run(m, cmd)
	if !slices.Equal(cmds.pauses, []bool{true}) {
		t.Errorf("q on a ride: asked %v, want a pause", cmds.pauses)
	}
}

func TestQuitAsksWhenAloneAndEndsTheRide(t *testing.T) {
	m, cmds := pausing(t)
	m = withHeads(m, 1)
	m = pressAll(m, "q")
	m, cmd := press(m, "q") // the menu's Quit
	if !m.quitting || quits(cmd) {
		t.Fatalf("Quit with a ride under way and no other screen: quitting %v, quit at once %v", m.quitting, quits(cmd))
	}
	if out := plain(m.render()); !containsAll(out, "QUIT?", "enter end it", "l  quit and leave it paused") {
		t.Errorf("the question:\n%s", out)
	}
	m, cmd = press(m, "enter")
	if !quits(cmd) {
		t.Fatal("enter didn't quit")
	}
	if cmds.stops != 1 || !slices.Equal(cmds.ends, []bool{false}) {
		t.Errorf("ended: stops %d, activity ends %v (want the ride stopped and saved)", cmds.stops, cmds.ends)
	}
}

func TestQuitLeavingItPaused(t *testing.T) {
	m, cmds := pausing(t)
	m = withHeads(m, 1)
	m = pressAll(m, "q", "q")
	_, cmd := press(m, "l")
	if !quits(cmd) || cmds.stops != 0 || cmds.pauses[len(cmds.pauses)-1] != true {
		t.Errorf("l: quit %v, stops %d, pauses %v", quits(cmd), cmds.stops, cmds.pauses)
	}
}

func TestQuitBackToTheMenu(t *testing.T) {
	m, _ := pausing(t)
	m = withHeads(m, 1)
	m = pressAll(m, "q", "q", "esc")
	if m.quitting || m.menu == nil || m.menu.sel != len(menuItems)-1 {
		t.Errorf("esc: quitting %v, menu %v", m.quitting, m.menu)
	}
}

func TestQuitAtOnceWithAnotherScreen(t *testing.T) {
	m, cmds := pausing(t)
	m = withHeads(m, 2) // the 3D view watches the ride too
	m = pressAll(m, "q")
	_, cmd := press(m, "q")
	if !quits(cmd) || cmds.stops != 0 {
		t.Errorf("another screen connected: quit at once %v, stops %d", quits(cmd), cmds.stops)
	}
}

func TestQOnTheDashboardOpensTheMenu(t *testing.T) {
	cmds := &stubCommands{}
	m := calModel(cmds)
	m, cmd := press(m, "q")
	if m.menu == nil || cmd != nil {
		t.Fatalf("q on the dashboard: menu %v, cmd %v", m.menu != nil, cmd != nil)
	}
	_, cmd = press(m, "q")
	if !quits(cmd) {
		t.Error("q in the menu with nothing under way didn't quit")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, x := range subs {
		if !strings.Contains(s, x) {
			return false
		}
	}
	return true
}

func TestQuitQuestionGoesWhenAnotherScreenEndsTheRide(t *testing.T) {
	m, _ := pausing(t)
	m = withHeads(m, 1)
	m = pressAll(m, "q", "q")
	st := proto.Clone(m.st).(*pb.State)
	st.Ride.Phase = pb.RidePhase_RIDE_PHASE_ABORTED // ended on the 3D view
	_, cmd := m.Update(StateMsg{State: st})
	if !quits(cmd) {
		t.Error("the ride ended elsewhere while asking: the screen should close as asked")
	}
}
