package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

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

// A ride already paused (p, another screen, nobody watching) still opens
// the menu on esc (owner, 2026-10-20: now and then esc did nothing during
// a ride); leaving it leaves the pause as it was.
func TestEscOnAPausedRideOpensTheMenu(t *testing.T) {
	m, cmds := pausing(t)
	m = paused(m, true)
	m, cmd := press(m, "esc")
	m = run(m, cmd)
	if m.menu == nil || len(cmds.pauses) != 0 {
		t.Fatalf("esc on a paused ride: menu %v, asked %v", m.menu != nil, cmds.pauses)
	}
	m, cmd = press(m, "esc")
	run(m, cmd)
	if m.menu != nil || len(cmds.pauses) != 0 {
		t.Errorf("esc out of it: menu %v, asked %v (want it left paused)", m.menu != nil, cmds.pauses)
	}
}

// The menu opened over a paused ride stays open as the core's states come
// in, and takes keys (owner, 2026-10-22: esc, q or q q on a paused ride
// opened it for a blink, the next state closing it again).
func TestMenuOverAPausedRideStays(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		m, cmds := pausing(t)
		m = paused(m, true)
		m, cmd := press(m, key)
		m = run(m, cmd)
		m = paused(m, true) // the core's next state, still paused
		m = paused(m, true)
		if m.menu == nil {
			t.Fatalf("%s on a paused ride: the next state closed the menu", key)
		}
		m, cmd = press(m, "down")
		run(m, cmd)
		if m.menu == nil || m.menu.sel != 1 {
			t.Errorf("%s: the menu didn't take down", key)
		}
		if len(cmds.pauses) != 0 {
			t.Errorf("%s: asked %v, want the pause left as it was", key, cmds.pauses)
		}
	}
}

// A menu open on the dashboard closes when another screen starts a ride,
// to show it.
func TestRideStartedElsewhereClosesTheMenu(t *testing.T) {
	m, _ := pausing(t)
	idle := proto.Clone(m.st).(*pb.State)
	idle.Ride = nil
	next, _ := m.Update(StateMsg{State: idle})
	m = next.(Model)
	m, cmd := press(m, "m")
	m = run(m, cmd)
	if m.menu == nil {
		t.Fatal("m on the dashboard: no menu")
	}
	next, _ = m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 10)})
	if next.(Model).menu != nil {
		t.Error("a ride started on another screen: the menu stayed over it")
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

// An announcement shows on top until it expires (Announce).
func TestAnnouncement(t *testing.T) {
	m, _ := pausing(t)
	st := proto.Clone(m.st).(*pb.State)
	st.Announcement = &pb.Announcement{Text: "GRADE -5 %: lighter than flat?", UntilUnixMs: time.Now().Add(time.Minute).UnixMilli()}
	next, _ := m.Update(StateMsg{State: st})
	m = next.(Model)
	if out := plain(m.render()); !strings.Contains(out, "GRADE -5 %: lighter than flat?") {
		t.Errorf("no announcement:\n%s", out)
	}
	st = proto.Clone(st).(*pb.State)
	st.Announcement.UntilUnixMs = time.Now().Add(-time.Second).UnixMilli()
	next, _ = m.Update(StateMsg{State: st})
	if out := plain(next.(Model).render()); strings.Contains(out, "lighter than flat") {
		t.Errorf("an expired announcement still shows:\n%s", out)
	}
}
