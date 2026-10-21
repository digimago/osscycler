package tui

import (
	"context"
	"strings"
	"time"
)

// reverseSuffix ends a reversed course's ID (course.ReverseSuffix).
const reverseSuffix = "-reverse"

// turnConfirm is how long a first r waits for the second: longer than x
// x's, as its question takes a while to read (owner, 2026-10-20: a second
// r after reading it only asked again).
const turnConfirm = 6 * time.Second

// runInHint is what r says on a run-in out, where it does nothing.
const runInHint = "no way back along the course from the run-in: turn round once you're on the lap"

// otherWay is the course that rides the one being ridden the other way:
// its reverse, or the forward course of a reverse; "" when it has none.
func (m Model) otherWay() string {
	id := m.ride().GetCourseId()
	if base, ok := strings.CutSuffix(id, reverseSuffix); ok && m.courses[base] != nil {
		return base
	}
	if m.courses[id+reverseSuffix] != nil {
		return id + reverseSuffix
	}
	return ""
}

// onRunIn reports whether the rider is on a lapped course's run-in out,
// which the other way has no way back along: no turning round there.
func (m Model) onRunIn() bool {
	r := m.ride()
	return r.GetLoop() && r.GetLapLengthM() > 0 && r.GetDistanceM() < r.GetLapStartM()
}

// turnKey is r during a course ride: r again within a few seconds turns
// round onto the course's other way (owner, 2026-10-20: r r, as x x, since
// the ride so far no longer counts), untimed until a timing line.
func (m Model) turnKey() (Model, bool, func(context.Context) error) {
	switch {
	case m.otherWay() == "":
		m.notice = "this course can't be ridden the other way"
		return m, false, nil
	case m.onRunIn():
		m.notice = runInHint
		return m, false, nil
	}
	if m.now().Before(m.turnUntil) {
		m.turnUntil, m.asking = time.Time{}, ""
		cmds := m.cmds
		return m, true, func(ctx context.Context) error {
			_, err := cmds.TurnAround(ctx)
			return err
		}
	}
	m.turnUntil = m.now().Add(turnConfirm)
	m = m.ask("press r again to turn round (this ride won't count; timed again from the next line)", m.turnUntil)
	return m, false, nil
}
