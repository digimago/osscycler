package tui

import (
	"context"
	"fmt"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The HISTORY tab lists finished course rides, newest first, with each
// stretch's personal best starred; the COURSES tab shows each course's
// full-course PB.

type resultsMsg struct {
	results []*pb.RideResult
	err     error
}

func (m Model) fetchResults() tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		rs, err := cmds.ListResults(ctx)
		return resultsMsg{results: rs, err: err}
	}
}

func (m Model) onResults(msg resultsMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "loading history failed: " + friendlyErr(msg.err)
		return m, nil
	}
	m.results = msg.results
	m.pickIdx[tabHistory] = min(m.pickIdx[tabHistory], max(0, len(m.results)-1))
	return m, nil
}

// coursePB is the personal best over the whole of a course, if any.
func (m Model) coursePB(id string) (*pb.RideResult, bool) {
	for _, r := range m.results {
		if r.GetCourseId() == id && r.GetStartM() < 1 && r.GetPersonalBest() {
			return r, true
		}
	}
	return nil, false
}

var pbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcc3f")).Bold(true)

// historyRows lists results, at most visible rows around the selection.
func (m Model) historyRows(visible int) []string {
	if len(m.results) == 0 {
		return []string{dimStyle.Render("  no finished course rides yet: press tab to pick a course")}
	}
	sel := m.pickIdx[tabHistory]
	first := max(0, min(len(m.results)-visible, sel-visible/2))
	var rows []string
	for i := first; i < min(len(m.results), first+max(visible, 1)); i++ {
		r := m.results[i]
		cursor, style := "  ", lipgloss.NewStyle()
		if i == sel {
			cursor, style = "▸ ", style.Bold(true).Foreground(lipgloss.Color("220"))
		}
		name := r.GetCourseName()
		if s := r.GetStartM(); s >= 1 {
			name += fmt.Sprintf(" from %.2f km", s/1000)
		}
		mark := ""
		if r.GetPersonalBest() {
			mark = pbStyle.Render(" ★ PB")
		}
		when := time.UnixMilli(r.GetFinishedUnixMs()).Local().Format("2006-01-02 15:04")
		rows = append(rows, style.Render(fmt.Sprintf("%s%s  %-36s %8s  %4.0f W  %5.1f km",
			cursor, when, truncate(name, 36), clock(r.GetElapsedS()), r.GetAvgPowerW(), r.GetDistanceM()/1000))+mark)
	}
	return rows
}

var (
	ghostStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d070ff"))
	aheadStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#59bf59")).Bold(true)
	behindStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6639")).Bold(true)
)

// ghostAt is where the ghost is on the course, or -1 without one.
func (m Model) ghostAt() float64 {
	if g := m.ride().GetGhost(); g != nil {
		return g.GetDistanceM()
	}
	return -1
}

// followGhost notes where the ghost is reported and how fast it goes, so
// the road view can carry it forward between the core's updates (4 a
// second) as it does the rider; drawn where reported, it moved in steps
// past the smoothly moving road.
func (m Model) followGhost(old *pb.Ride) Model {
	g, r := m.ride().GetGhost(), m.ride()
	if g == nil || r.GetPhase() != pb.RidePhase_RIDE_PHASE_RIDING {
		m.ghostV, m.ghostSeen = 0, time.Time{}
		return m
	}
	d, now := g.GetDistanceM(), m.now()
	if r.GetCourseId() != old.GetCourseId() || m.ghostSeen.IsZero() {
		m.ghostV, m.ghostD, m.ghostSeen = 0, d, now // a new ride: from here
		return m
	}
	if d == m.ghostD {
		return m
	}
	moved := d - m.ghostD
	if l := r.GetCourseDistanceM(); r.GetLoop() && moved < -l/2 {
		moved += l // over the line
	}
	// Faster than any rider is a jump (a new ghost, a new lap), not speed.
	if dt := now.Sub(m.ghostSeen).Seconds(); dt > 0.05 && dt < 2 && moved >= 0 && moved/dt < 30 {
		m.ghostV = 0.5*m.ghostV + 0.5*moved/dt // the reports arrive a little unevenly
	}
	m.ghostD, m.ghostSeen = d, now
	return m
}

// ghostMoving is the ghost's distance for drawing: carried forward from
// its last report at its speed, as ridePos does for the rider; it stops at
// a finish line it has reached.
func (m Model) ghostMoving() float64 {
	g := m.ghostAt()
	r := m.ride()
	if g < 0 || m.ghostSeen.IsZero() || r.GetPhase() != pb.RidePhase_RIDE_PHASE_RIDING || g >= r.GetCourseDistanceM()-0.01 {
		return g // waiting at the line, done
	}
	g += m.ghostV * math.Min(math.Max(0, m.now().Sub(m.ghostSeen).Seconds()), 0.5)
	if !r.GetLoop() {
		g = math.Min(g, r.GetCourseDistanceM())
	}
	return g
}

// timeTile is the ride clock, with the gap to the ghost under it: green
// ahead, red behind.
func (m Model) timeTile() metric {
	r := m.ride()
	t := metric{label: "TIME", value: clock(r.GetElapsedS()), style: lipgloss.NewStyle()}
	if r.GetLoop() { // a loop is timed lap by lap
		t.label, t.value = fmt.Sprintf("LAP %d", r.GetLap()), clock(r.GetLapElapsedS())
	}
	g := r.GetGhost()
	if g == nil {
		return t
	}
	if !r.GetLoop() { // on a loop the ghost is always the best lap
		t.label += " vs " + g.GetLabel()
	}
	gap := g.GetGapS()
	switch {
	case r.GetPhase() == pb.RidePhase_RIDE_PHASE_ARMED && r.GetLoop():
		t.unit = "lap PB " + lapTime(g.GetTimeS())
	case r.GetPhase() == pb.RidePhase_RIDE_PHASE_ARMED:
		t.unit = "PB " + clock(g.GetTimeS())
	case gap < 0:
		t.unit, t.unitStyle = "-"+secs(-gap)+" ahead", &aheadStyle
	default:
		t.unit, t.unitStyle = "+"+secs(gap)+" behind", &behindStyle
	}
	return t
}

// secs formats a positive gap: 4.2 s, or 1:05 from a minute.
func secs(s float64) string {
	if s >= 60 {
		return clock(s)
	}
	return fmt.Sprintf("%.1f s", s)
}

// race starts the course of an earlier ride, with that ride as the ghost.
func (m Model) race(r *pb.RideResult) (Model, tea.Cmd, bool) {
	id, at := r.GetCourseId(), r.GetFinishedUnixMs()
	if _, ok := m.courses[id]; !ok {
		m.notice = r.GetCourseName() + " is no longer in the courses folder"
		return m, nil, true
	}
	m.picking = false
	cmds := m.cmds
	return m, m.command("start ride", func(ctx context.Context) error { return cmds.StartRideAgainst(ctx, id, at) }), true
}

// activityResult is the finished course ride in the selected recording,
// if there is one: the first, for a recording with several.
func (m Model) activityResult() *pb.RideResult {
	i := m.pickIdx[tabActivities]
	if i >= len(m.activities) {
		return nil
	}
	name := m.activities[i].GetName()
	for j := len(m.results) - 1; j >= 0; j-- { // results are newest first
		if r := m.results[j]; r.GetFile() == name {
			return r
		}
	}
	return nil
}
