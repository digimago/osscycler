package tui

import (
	"context"
	"fmt"
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

// timeTile is the ride clock, with the gap to the ghost under it: green
// ahead, red behind.
func (m Model) timeTile() metric {
	r := m.ride()
	t := metric{label: "TIME", value: clock(r.GetElapsedS()), style: lipgloss.NewStyle()}
	g := r.GetGhost()
	if g == nil {
		return t
	}
	t.label = "TIME vs " + g.GetLabel()
	gap := g.GetGapS()
	switch {
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
