package tui

import (
	"strings"
	"testing"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// r during a course ride asks, r again turns round (owner, 2026-10-20:
// r r, as x x: the ride so far no longer counts); outside a ride r still
// opens the rides.
func TestTurnRoundKeys(t *testing.T) {
	rev := hillCourse()
	rev.Id, rev.Name = "hill-reverse", "Hill Test (reverse)"
	cmds := &stubCommands{courses: []*pb.Course{hillCourse(), rev}}
	m := run(calModel(cmds), nil)
	next, cmd := m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1200)})
	next, _ = next.Update(cmd())
	m = next.(Model)

	m, cmd = press(m, "r")
	if out := plain(m.render()); cmd != nil || !strings.Contains(out, "press r again to turn round") || m.picking {
		t.Fatalf("first r: picking %v, asked:\n%s", m.picking, out)
	}
	m, cmd = press(m, "r")
	run(m, cmd)
	if cmds.turns != 1 {
		t.Fatalf("TurnAround calls %d after r r", cmds.turns)
	}

	// Untimed after the turn: the clock says so, and the finish too.
	st := rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1800)
	st.Ride.CourseId, st.Ride.Untimed = "hill-reverse", true
	next, _ = m.Update(StateMsg{State: st})
	m = next.(Model)
	if out := plain(m.render()); !strings.Contains(out, "NOT TIMED") {
		t.Errorf("an untimed ride's clock:\n%s", out)
	}
	if v := plain(rideVerdict(st.Ride)); !strings.Contains(v, "not timed") {
		t.Errorf("an untimed ride's verdict %q", v)
	}
}

// On the run-in out r r ends the ride; on a course with no other way r
// says so and asks nothing.
func TestTurnRoundElsewhere(t *testing.T) {
	rev := hillCourse()
	rev.Id = "hill-reverse"
	cmds := &stubCommands{courses: []*pb.Course{hillCourse(), rev}}
	st := rideState(pb.RidePhase_RIDE_PHASE_RIDING, 200)
	st.Ride.Loop, st.Ride.LapStartM, st.Ride.LapLengthM, st.Ride.LapDistanceM = true, 500, 2000, -300
	m := run(calModel(cmds), nil)
	next, cmd := m.Update(StateMsg{State: st})
	next, _ = next.Update(cmd())
	m, _ = press(next.(Model), "r")
	if out := plain(m.render()); !strings.Contains(out, "press r again to end the ride") {
		t.Errorf("r on the run-in:\n%s", out)
	}

	cmds = &stubCommands{courses: []*pb.Course{hillCourse()}}
	m = run(calModel(cmds), nil)
	next, cmd = m.Update(StateMsg{State: rideState(pb.RidePhase_RIDE_PHASE_RIDING, 1200)})
	next, _ = next.Update(cmd())
	m, cmd = press(next.(Model), "r")
	if m.asking != "" || !strings.Contains(m.notice, "can't be ridden the other way") || cmd != nil {
		t.Errorf("r with no other way: asking %q, notice %q", m.asking, m.notice)
	}
	m, _ = press(m, "r")
	if cmds.turns != 0 || m.picking {
		t.Errorf("r r with no other way: %d turns, picking %v", cmds.turns, m.picking)
	}
}
