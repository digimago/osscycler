package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The end-of-ride summary (owner's list, item 6): averages, the power
// curve and the time in each power and heart rate zone, from the core
// (GetActivitySummary). It opens after the rider ends a ride and saves it,
// on i in ACTIVITIES, and when a course ride finishes at its line (owner,
// 2026-10-13: the ride ends there with its summary: the course ride's own,
// its result on top); x, enter or esc close it (back to the list when it
// was opened there; after a course ride, back to free riding).

type summaryMsg struct {
	s       *pb.ActivitySummary
	fromTab bool
	ride    bool // the finished course ride's
	err     error
}

// fetchRideSummary asks for the finished course ride's summary.
func (m Model) fetchRideSummary() tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		s, err := cmds.RideSummary(ctx)
		return summaryMsg{s: s, ride: true, err: err}
	}
}

func (m Model) fetchSummary(name string, fromTab bool) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		s, err := cmds.ActivitySummary(ctx, name)
		return summaryMsg{s: s, fromTab: fromTab, err: err}
	}
}

func (m Model) onSummary(msg summaryMsg) Model {
	if msg.err != nil {
		if msg.fromTab { // after ending a ride, "ride saved" stays
			m.notice = "the ride's summary: " + friendlyErr(msg.err)
		}
		return m // after a course ride: its result screen as before
	}
	if msg.ride && m.ride().GetPhase() != pb.RidePhase_RIDE_PHASE_FINISHED {
		return m // closed meanwhile (here or on another screen)
	}
	m.summary, m.summaryFromTab, m.summaryRide = msg.s, msg.fromTab, msg.ride
	m.picking = false
	return m
}

// summaryKey closes the summary; while it is open nothing else takes keys.
func (m Model) summaryKey(key string) (Model, tea.Cmd, bool) {
	if m.summary == nil {
		return m, nil, false
	}
	switch key {
	case "x", "enter", "esc", "i", "q":
		m.summary = nil
		m.picking = m.summaryFromTab
		if m.summaryRide {
			// The course ride ends: back to free riding.
			m.summaryRide = false
			if m.ride().GetPhase() == pb.RidePhase_RIDE_PHASE_FINISHED {
				return m, m.command("close ride", m.cmds.StopRide), true
			}
		}
	}
	return m, nil, true
}

var zoneNames = []string{"recovery", "endurance", "tempo", "threshold", "VO2 max", "anaerobic"}

// summaryPanel draws the summary in width × height.
func (m Model) summaryPanel(width, height int) string {
	s := m.summary
	title := "RIDE SUMMARY"
	if t, err := time.ParseInLocation("2006-01-02-150405", strings.TrimSuffix(s.GetName(), ".fit"), time.Local); err == nil {
		title += "  " + dimStyle.Render(t.Format("Mon 2 Jan 15:04"))
	}
	lines := []string{titleStyle.Render(title),
		fmt.Sprintf("%s riding · %.2f km", clock(s.GetTimerS()), s.GetDistanceM()/1000), ""}
	if m.summaryRide {
		// The course ride's result on top: its time and how it went.
		r := m.ride()
		lines = []string{bigOK.Render("FINISHED  " + r.GetCourseName()), "",
			bigOK.Render(clock(r.GetElapsedS())) + "   " + rideVerdict(r), "",
			fmt.Sprintf("%.2f km · %.0f m climbed", s.GetDistanceM()/1000, r.GetClimbedM()), ""}
	}
	label := func(l string) string { return labelStyle.Render(fmt.Sprintf("%-11s", l)) }
	if s.GetAvgPowerW() > 0 {
		np := ""
		if s.GetNormalizedPowerW() > 0 {
			np = fmt.Sprintf("  NP %.0f W", s.GetNormalizedPowerW())
		}
		lines = append(lines, label("POWER")+powerStyle.Render(fmt.Sprintf("avg %.0f W%s  max %.0f W", s.GetAvgPowerW(), np, s.GetMaxPowerW()))+
			dimStyle.Render(fmt.Sprintf("  %.0f kJ", s.GetWorkKj())))
	}
	if s.GetAvgHeartRateBpm() > 0 {
		lines = append(lines, label("HEART RATE")+hrStyle.Render(fmt.Sprintf("avg %.0f  max %.0f bpm", s.GetAvgHeartRateBpm(), s.GetMaxHeartRateBpm())))
	}
	if s.GetAvgCadenceRpm() > 0 {
		lines = append(lines, label("CADENCE")+cadenceStyle.Render(fmt.Sprintf("avg %.0f rpm", s.GetAvgCadenceRpm())))
	}
	if c := s.GetPowerCurve(); len(c) > 0 {
		var head, vals []string
		for _, b := range c {
			d := fmt.Sprintf("%ds", b.GetDurationS())
			if b.GetDurationS() >= 60 {
				d = fmt.Sprintf("%dm", b.GetDurationS()/60)
			}
			head = append(head, fmt.Sprintf("%5s", d))
			v := fmt.Sprintf("%5.0f", b.GetPowerW())
			if ftp := s.GetFtpW(); ftp > 0 {
				v = lipgloss.NewStyle().Foreground(zoneColor(b.GetPowerW() / ftp)).Render(v)
			}
			vals = append(vals, v)
		}
		lines = append(lines, "", labelStyle.Render("BEST POWER"), dimStyle.Render(strings.Join(head, " ")), strings.Join(vals, " "))
	}
	bars := func(secs []float64, colour func(int) lipgloss.Style, name func(int) string) []string {
		total, most := 0.0, 0.0
		for _, v := range secs {
			total += v
			most = max(most, v)
		}
		var out []string
		for i, v := range secs {
			w := 0
			if most > 0 {
				w = int(v / most * 28)
			}
			pct := 0.0
			if total > 0 {
				pct = v / total * 100
			}
			out = append(out, fmt.Sprintf("Z%d %-10s ", i+1, name(i))+colour(i).Render(strings.Repeat("█", w))+
				strings.Repeat(" ", 28-w)+fmt.Sprintf(" %7s %3.0f%%", clock(v), pct))
		}
		return out
	}
	if z := s.GetPowerZoneS(); len(z) > 0 {
		lines = append(lines, "", labelStyle.Render(fmt.Sprintf("POWER ZONES  FTP %.0f W", s.GetFtpW())))
		lines = append(lines, bars(z, func(i int) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(zoneHex[min(i, len(zoneHex)-1)]))
		}, func(i int) string { return zoneNames[min(i, len(zoneNames)-1)] })...)
	}
	switch z := s.GetHeartRateZoneS(); {
	case len(z) > 0:
		lines = append(lines, "", labelStyle.Render(fmt.Sprintf("HEART RATE ZONES  max %.0f bpm", s.GetMaxHrBpm())))
		lines = append(lines, bars(z, func(int) lipgloss.Style { return hrStyle }, func(i int) string {
			lo, hi := 0, 0
			if up := s.GetHeartRateZoneUpper(); i < len(up) {
				hi = int(up[i] * 100)
			}
			if up := s.GetHeartRateZoneUpper(); i > 0 && i-1 < len(up) {
				lo = int(up[i-1] * 100)
			}
			switch {
			case i == 0:
				return fmt.Sprintf("<%d%%", hi)
			case hi == 0:
				return fmt.Sprintf(">%d%%", lo)
			}
			return fmt.Sprintf("%d-%d%%", lo, hi)
		})...)
	case s.GetAvgHeartRateBpm() > 0:
		lines = append(lines, "", dimStyle.Render("Heart rate zones: add your maximum heart rate to your profile (m, Profile)."))
	}
	lines = append(lines, "", dimStyle.Render("x close"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}
