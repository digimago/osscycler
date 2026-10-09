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

// Loops: osscycler's test tracks go round until the rider stops, each lap
// timed and raced against the best lap. A workout or manual control may
// run on one; the loop is then the road to look at.

// lapsShownFor keeps the lap times on screen after a lap.
const lapsShownFor = 15 * time.Second

// onLoop reports whether a loop ride is under way.
func (m Model) onLoop() bool { return m.rideActive() && m.ride().GetLoop() }

// courseRide reports whether a course ride (not a loop) is under way: the
// trainer is then the ride's alone.
func (m Model) courseRide() bool { return m.rideActive() && !m.ride().GetLoop() }

// loopGhost is the ghost's distance for drawing near pos: on a loop the
// ghost may be a lap on (just over the line ahead) or back.
func (m Model) loopGhost(pos float64) float64 { return m.nearPos(m.ghostAt(), pos) }

// nearPos puts distance g on the lap nearest pos, on a loop.
func (m Model) nearPos(g, pos float64) float64 {
	r := m.ride()
	if g < 0 || !r.GetLoop() || r.GetCourseDistanceM() <= 0 {
		return g
	}
	l := r.GetCourseDistanceM()
	return g + math.Round((pos-g)/l)*l
}

// trackLaps follows the laps of a loop ride from the state stream: it
// keeps this ride's completed laps and shows them for a while after each.
func (m Model) trackLaps(old *pb.Ride) (Model, tea.Cmd) {
	r := m.ride()
	if !r.GetLoop() {
		m.laps = nil
		return m, nil
	}
	if r.GetPhase() == pb.RidePhase_RIDE_PHASE_ARMED || r.GetCourseId() != old.GetCourseId() {
		m.laps = nil
	}
	l := r.GetLastLap()
	if l == nil || (len(m.laps) > 0 && m.laps[len(m.laps)-1].GetNumber() == l.GetNumber()) {
		return m, nil
	}
	if len(m.laps) > 0 && l.GetNumber() < m.laps[len(m.laps)-1].GetNumber() {
		m.laps = nil // a new ride
	}
	m.laps = append(m.laps, l)
	if old.GetCourseId() != r.GetCourseId() || old.GetLastLap().GetNumber() == l.GetNumber() {
		return m, nil // joined mid-ride: no lap just done
	}
	m.lapsUntil = m.now().Add(lapsShownFor)
	if m.cmds == nil {
		return m, nil
	}
	return m, m.fetchResults() // the history's best on this loop, with this lap
}

var (
	lapsBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	lapFastest  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	lapRunning  = lipgloss.NewStyle().Italic(true)
	lapOldPB    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcc3f"))
	lapsBoxCols = 30 // inside the border and padding
)

// lapTime formats a lap time with tenths: 0:39.4.
func lapTime(s float64) string {
	tenths := int(math.Round(s * 10))
	return fmt.Sprintf("%d:%02d.%d", tenths/600, tenths/10%60, tenths%10)
}

// lapsPanel is the pop-in with the lap times: the best lap ever on this
// loop when it is from an earlier ride, this ride's laps with when each
// was ridden, and the lap under way. The fastest is bold, the running
// lap italic. Empty when it isn't shown.
func (m Model) lapsPanel(maxRows int) string {
	r := m.ride()
	if !m.onLoop() || !m.now().Before(m.lapsUntil) || maxRows < 6 || m.help || m.arranging != nil ||
		m.draft != nil || m.picking || m.input != nil || m.ending != nil || m.onboarding != nil || m.menu != nil {
		return ""
	}
	type row struct {
		name, when string
		s          float64
		pb         bool
	}
	var rows []row
	fastest := math.Inf(1)
	if best, ok := m.loopPB(); ok {
		rows = append(rows, row{"★ PB", time.UnixMilli(best.GetFinishedUnixMs()).Format("2 Jan 15:04"), best.GetElapsedS(), true})
		fastest = best.GetElapsedS()
	}
	laps := m.laps
	if keep := maxRows - 4 - len(rows); len(laps) > keep { // border, title, running lap
		laps = laps[len(laps)-max(keep, 1):]
	}
	for _, l := range laps {
		rows = append(rows, row{fmt.Sprintf("lap %d", l.GetNumber()), time.UnixMilli(l.GetFinishedUnixMs()).Format("2 Jan 15:04"), l.GetTimeS(), false})
		fastest = math.Min(fastest, l.GetTimeS())
	}
	for _, l := range m.laps { // the fastest of the ride may have scrolled off
		fastest = math.Min(fastest, l.GetTimeS())
	}
	lines := []string{titleStyle.Render(truncate("LAPS · "+r.GetCourseName(), lapsBoxCols))}
	for _, x := range rows {
		text := fmt.Sprintf("%-7s %7s  %s", x.name, lapTime(x.s), x.when)
		if x.s == fastest {
			text = lapFastest.Render(text)
		} else if x.pb {
			text = lapOldPB.Render(text)
		}
		lines = append(lines, text)
	}
	lines = append(lines, lapRunning.Render(fmt.Sprintf("%-7s %7s  now", fmt.Sprintf("lap %d", r.GetLap()), lapTime(r.GetLapElapsedS()))))
	return lapsBox.Width(lapsBoxCols + 4).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// loopPB is the best lap ever on the loop being ridden, from the history,
// when it is from an earlier ride: a lap of this ride is in its own rows.
func (m Model) loopPB() (*pb.RideResult, bool) {
	best, ok := m.coursePB(m.ride().GetCourseId())
	if !ok {
		return nil, false
	}
	for _, l := range m.laps {
		if math.Abs(float64(l.GetFinishedUnixMs()-best.GetFinishedUnixMs())) < 1500 {
			return nil, false
		}
	}
	return best, true
}

// withLaps lays the lap times over the left of body.
func (m Model) withLaps(body string) string {
	h := lipgloss.Height(body)
	panel := m.lapsPanel(h - 2)
	if panel == "" {
		return body
	}
	y := min(max(0, h/4), max(0, h-lipgloss.Height(panel)))
	return lipgloss.NewCompositor(lipgloss.NewLayer(body), lipgloss.NewLayer(panel).X(1).Y(y).Z(1)).Render()
}

// loopTracks are the loops on offer, for free riding.
func (m Model) loopTracks() []*pb.Course {
	var out []*pb.Course
	for _, c := range m.courseList {
		if c.GetLoop() {
			out = append(out, c)
		}
	}
	return out
}

// rideLoop starts a loop ride on the track with that ID.
func (m Model) rideLoop(id string) tea.Cmd {
	cmds := m.cmds
	return m.command("start ride", func(ctx context.Context) error { return cmds.StartRide(ctx, id) })
}
