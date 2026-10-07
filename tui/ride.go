package tui

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

const (
	// lookaheadM and lookaheadCellM shape the grade strip at the bottom.
	lookaheadM     = 250.0
	lookaheadCellM = 10.0
	labelEveryM    = 50.0
	// difficultyStep is how much + and - change the difficulty.
	difficultyStep = 10.0
	// abortConfirm is how long a first x waits for the second.
	abortConfirm = 3 * time.Second
	// abortedShownFor keeps an aborted ride's summary on screen.
	abortedShownFor = 10 * time.Second
)

type coursesMsg struct {
	courses []*pb.Course
	err     error
	open    bool // open the picker when they arrive
}

func (m Model) fetchCourses(open bool) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		cs, err := cmds.ListCourses(ctx)
		return coursesMsg{courses: cs, err: err, open: open}
	}
}

func (m Model) ride() *pb.Ride { return m.st.GetRide() }

// difficulty is the core's current setting; a core without courses reports
// none, which reads as 100 %.
func (m Model) difficulty() float64 {
	if m.st.GetRide() == nil {
		return 100
	}
	return m.ride().GetDifficultyPct()
}

func (m Model) ridePhase() pb.RidePhase { return m.ride().GetPhase() }

func (m Model) rideActive() bool {
	p := m.ridePhase()
	return p == pb.RidePhase_RIDE_PHASE_ARMED || p == pb.RidePhase_RIDE_PHASE_RIDING
}

// showRide reports whether the ride screen replaces the dashboard.
func (m Model) showRide() bool {
	switch m.ridePhase() {
	case pb.RidePhase_RIDE_PHASE_ARMED, pb.RidePhase_RIDE_PHASE_RIDING, pb.RidePhase_RIDE_PHASE_FINISHED:
		return true
	case pb.RidePhase_RIDE_PHASE_ABORTED:
		return time.Duration(m.st.GetCoreTimeNs()-m.ride().GetPhaseChangedNs()) < abortedShownFor
	}
	return false
}

// rideKey handles the course picker and ride keys. It reports whether it
// used the key.
func (m Model) rideKey(key string) (Model, tea.Cmd, bool) {
	if m.picking {
		n := [numTabs]int{len(m.courseList), len(m.workoutList), len(m.results), len(m.activities)}[m.tab]
		switch key {
		case "tab", "right", "l":
			m.tab = (m.tab + 1) % numTabs
		case "shift+tab", "left", "h":
			m.tab = (m.tab + numTabs - 1) % numTabs
		case "up", "k":
			m.pickIdx[m.tab] = max(0, m.pickIdx[m.tab]-1)
		case "down", "j":
			m.pickIdx[m.tab] = max(0, min(n-1, m.pickIdx[m.tab]+1))
		case "s":
			if m.tab == tabActivities {
				return m, m.exportSelected(), true
			}
		case "enter":
			if m.tab == tabActivities {
				return m, m.exportSelected(), true
			}
			if m.tab == tabCourses && n > 0 {
				id := m.courseList[m.pickIdx[tabCourses]].GetId()
				m.picking = false
				return m, m.command("start ride", func(ctx context.Context) error { return m.cmds.StartRide(ctx, id) }), true
			}
		case "esc", "r":
			m.picking = false
		}
		return m, nil, true
	}
	switch {
	case key == "+" || key == "=" || key == "-" || key == "_":
		// Move to the next multiple of the step in the key's direction, so
		// presses land on 10, 20, ... even from -difficulty 55 (55 → 60 or 50).
		d := m.difficulty() / difficultyStep
		want := (math.Floor(d) + 1) * difficultyStep
		if key == "-" || key == "_" {
			want = (math.Ceil(d) - 1) * difficultyStep
		}
		want = math.Max(0, math.Min(want, 100))
		cmds := m.cmds
		return m, m.command("set difficulty", func(ctx context.Context) error {
			_, err := cmds.SetDifficulty(ctx, want)
			return err
		}), true
	case key == "r" && !m.rideActive() && !m.workoutActive() && !m.calibrationActive() && m.countdown == 0:
		m.notice = ""
		m.picking = true
		return m, tea.Batch(m.fetchCourses(false), m.fetchWorkouts(false), m.fetchResults(), m.fetchActivities()), true
	case key == "x" && m.rideActive():
		if m.now().Before(m.abortUntil) {
			m.abortUntil = time.Time{}
			m.notice = ""
			return m, m.command("abort ride", m.cmds.StopRide), true
		}
		m.abortUntil = m.now().Add(abortConfirm)
		m.notice = "press x again to abort the ride"
		return m, nil, true
	case (key == "x" || key == "enter") && m.showRide() && !m.rideActive():
		return m, m.command("close ride", m.cmds.StopRide), true
	}
	return m, nil, false
}

func (m Model) onCourses(msg coursesMsg) (Model, tea.Cmd) {
	m.fetching = false
	if msg.err != nil {
		m.notice = "loading courses failed: " + friendlyErr(msg.err)
		return m, nil
	}
	m.courseList = msg.courses
	m.courses = make(map[string]*pb.Course, len(msg.courses))
	for _, c := range msg.courses {
		m.courses[c.GetId()] = c
	}
	m.pickIdx[tabCourses] = min(m.pickIdx[tabCourses], max(0, len(msg.courses)-1))
	return m, nil
}

// needCourse fetches the course list if a ride references a course we
// don't have yet, e.g. one started by another client.
func (m Model) needCourse() (Model, tea.Cmd) {
	id := m.ride().GetCourseId()
	if id == "" || m.cmds == nil || m.fetching {
		return m, nil
	}
	if _, ok := m.courses[id]; ok {
		return m, nil
	}
	m.fetching = true
	return m, m.fetchCourses(false)
}

func (m Model) pickerPanel(width, height int) string {
	tabName := func(t int, name string) string {
		if t == m.tab {
			return titleStyle.Underline(true).Render(name)
		}
		return dimStyle.Render(name)
	}
	lines := []string{tabName(tabCourses, "COURSES") + "    " + tabName(tabWorkouts, "WORKOUTS") + "    " + tabName(tabHistory, "HISTORY") + "    " + tabName(tabActivities, "ACTIVITIES"), ""}
	var hint string
	switch m.tab {
	case tabHistory:
		lines = append(lines, m.historyRows(height-6)...)
		hint = "↑/↓ scroll · ★ fastest on that course from that start · tab activities · esc back"
	case tabActivities:
		lines = append(lines, m.activityRows(height-6)...)
		hint = "↑/↓ choose · s save the FIT file to " + m.exportDir + " · tab courses · esc back"
	case tabCourses:
		for i, c := range m.courseList {
			cursor := "  "
			style := lipgloss.NewStyle()
			if i == m.pickIdx[tabCourses] {
				cursor = "▸ "
				style = style.Bold(true).Foreground(lipgloss.Color("220"))
			}
			pbText := ""
			if r, ok := m.coursePB(c.GetId()); ok {
				pbText = pbStyle.Render("  PB " + clock(r.GetElapsedS()))
			}
			lines = append(lines, style.Render(fmt.Sprintf("%s%-34s %6.2f km  +%4.0f m  max %4.1f%%",
				cursor, truncate(c.GetName(), 34), c.GetDistanceM()/1000, c.GetGainM(), c.GetMaxGradePct()))+pbText)
		}
		if len(m.courseList) == 0 {
			lines = append(lines, dimStyle.Render("  no courses: start the core with -courses DIR"))
		}
		hint = "↑/↓ choose · enter ride · tab workouts · esc back"
	default:
		lines = append(lines, m.workoutRows(width)...)
		if w := m.selectedWorkout(); w != nil && w.GetError() == "" {
			lines = append(lines, "", workoutProfile(w, -1, min(width-4, 80), 4))
		}
		hint = fmt.Sprintf("↑/↓ choose · enter ride · n new · e edit · E edit as text · f FTP %.0f W · tab history · esc back", m.wk().GetFtpW())
	}
	lines = append(lines, "", dimStyle.Render(hint))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// rideBody is the ride screen above the footer.
func (m Model) rideBody(width, height int) string {
	r := m.ride()
	c := m.courses[r.GetCourseId()]
	switch r.GetPhase() {
	case pb.RidePhase_RIDE_PHASE_FINISHED:
		return m.rideResult(width, height, true)
	case pb.RidePhase_RIDE_PHASE_ABORTED:
		return m.rideResult(width, height, false)
	}

	info := m.rideInfo(width)
	var profile string
	if c != nil && height >= 16 {
		profile = elevationProfile(c, r.GetDistanceM(), m.ghostAt(), width, 3)
	}
	tilesH := height - lipgloss.Height(info) - lipgloss.Height(profile)
	if profile == "" {
		tilesH++ // JoinVertical of an empty string still takes a line
	}

	grade := r.GetGradePct()
	ms := []metric{
		m.metrics()[0], // power
		{"GRADE", fmt.Sprintf("%.1f", grade), "%", lipgloss.NewStyle().Foreground(gradeColor(grade)), nil},
		m.timeTile(),
		{"TO GO", fmt.Sprintf("%.2f", math.Max(0, r.GetCourseDistanceM()-r.GetDistanceM())/1000), "km", speedStyle, nil},
	}
	var tiles string
	tileH, tileW := tilesH/2, width/2
	if tileH >= BigHeight+3 && tileW >= 30 {
		tiles = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Top, bigTile(ms[0], tileW, tileH), bigTile(ms[1], width-tileW, tileH)),
			lipgloss.JoinHorizontal(lipgloss.Top, bigTile(ms[2], tileW, tilesH-tileH), bigTile(ms[3], width-tileW, tilesH-tileH)))
	} else {
		var b strings.Builder
		for _, mt := range ms {
			fmt.Fprintf(&b, "%s %s %s\n", labelStyle.Render(fmt.Sprintf("%-6s", mt.label)),
				mt.style.Bold(true).Render(fmt.Sprintf("%8s", mt.value)), unitStyle.Render(mt.unit))
		}
		tiles = lipgloss.Place(width, max(tilesH, 4), lipgloss.Center, lipgloss.Center, b.String())
	}
	return lipgloss.JoinVertical(lipgloss.Left, tiles, info, profile)
}

// rideInfo is one line of secondary numbers, or the start instruction.
func (m Model) rideInfo(width int) string {
	r := m.ride()
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	if r.GetPhase() == pb.RidePhase_RIDE_PHASE_ARMED {
		race := ""
		if g := r.GetGhost(); g != nil {
			race = "  ·  racing your " + g.GetLabel() + " (" + clock(g.GetTimeS()) + ")"
		}
		return center.Render(bigWarn.Render("start pedalling to start the clock") + dimStyle.Render("  ·  "+r.GetCourseName()+race))
	}
	ms := m.metrics()
	return center.Render(fmt.Sprintf("%s bpm · %s rpm · %.1f km/h · %.0f/%.0f m climbed · %s",
		ms[1].value, ms[2].value, r.GetSpeedMps()*3.6, r.GetClimbedM(), r.GetCourseGainM(),
		dimStyle.Render(truncate(r.GetCourseName(), 40))))
}

func (m Model) rideResult(width, height int, finished bool) string {
	r := m.ride()
	var lines []string
	if finished {
		avgSpeed := 0.0
		if r.GetElapsedS() > 0 {
			avgSpeed = (r.GetCourseDistanceM() - r.GetStartDistanceM()) / r.GetElapsedS() * 3.6
		}
		big := height >= BigHeight+8 && width >= 50
		t := clock(r.GetElapsedS())
		if big {
			t = Big(t)
		}
		from := ""
		if s := r.GetStartDistanceM(); s > 0 {
			from = fmt.Sprintf(" (from %.2f km)", s/1000)
		}
		verdict := dimStyle.Render("first ride on this stretch: the PB to beat next time")
		if g := r.GetGhost(); g != nil {
			if gap := g.GetGapS(); gap < 0 {
				verdict = pbStyle.Render(fmt.Sprintf("★ NEW PB  %s faster than %s", secs(-gap), clock(g.GetTimeS())))
			} else {
				verdict = warnStyle.Render(fmt.Sprintf("%s off your %s (%s)", secs(gap), g.GetLabel(), clock(g.GetTimeS())))
			}
		}
		lines = append(lines,
			bigOK.Render("FINISHED  "+r.GetCourseName()+from), "",
			bigOK.Render(t), "", verdict, "",
			fmt.Sprintf("%.0f W average · %.1f km/h · %.0f m climbed", r.GetAvgPowerW(), avgSpeed, r.GetClimbedM()),
			"", dimStyle.Render("x or enter to close"))
	} else {
		lines = append(lines, warnStyle.Render("ride aborted"),
			fmt.Sprintf("%.2f of %.2f km in %s", r.GetDistanceM()/1000, r.GetCourseDistanceM()/1000, clock(r.GetElapsedS())))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}

// gradeStrip shows the next lookaheadM metres as heat-coloured cells, like
// Zwift's climb bar: blue is easy, deep red is 16 % and up, slate is
// downhill, chequered is past the finish. Three lines: grade labels on the
// colours, a plain colour band, and a distance ruler.
func gradeStrip(c *pb.Course, pos, ghost float64, width int) string {
	cells := int(lookaheadM / lookaheadCellM)
	if c == nil || width < cells {
		return ""
	}
	finish := c.GetDistanceM()
	bg := make([]color.Color, width) // background per column
	label := []rune(strings.Repeat(" ", width))
	ruler := []rune(strings.Repeat(" ", width))
	put := func(row []rune, col int, text string) {
		for i, r := range []rune(text) {
			if col+i < width {
				row[col+i] = r
			}
		}
	}
	col, finishLabelled := 0, false
	for i := range cells {
		// Spread the width over the cells, giving the remainder to the first ones.
		w := width / cells
		if i < width%cells {
			w++
		}
		mid := pos + (float64(i)+0.5)*lookaheadCellM
		onLabel := math.Mod(float64(i)*lookaheadCellM, labelEveryM) == 0
		var cellBG color.Color
		switch {
		case mid > finish:
			cellBG = finishColor(i)
			if !finishLabelled {
				put(label, col, "FINISH")
				finishLabelled = true
			}
		default:
			g := gradeAt(c, mid)
			cellBG = gradeColor(g)
			if onLabel {
				put(label, col, fmt.Sprintf("%.0f%%", g))
			}
		}
		for x := col; x < col+w; x++ {
			bg[x] = cellBG
		}
		if onLabel {
			tick := fmt.Sprintf("%.0f", float64(i)*lookaheadCellM)
			if i == 0 {
				tick = "now"
			}
			put(ruler, col, tick)
		}
		col += w
	}
	// The ghost, when it is in the window ahead, gets a mark on the ruler.
	if off := ghost - pos; ghost >= 0 && off >= 0 && off < lookaheadM {
		put(ruler, int(off/lookaheadM*float64(width)), "◆PB")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		paint(label, bg), paint([]rune(strings.Repeat(" ", width)), bg), dimStyle.Render(string(ruler)))
}

// paint renders text over per-column backgrounds, one style per run of
// equal colours.
func paint(text []rune, bg []color.Color) string {
	var b strings.Builder
	for start := 0; start < len(text); {
		end := start + 1
		for end < len(text) && bg[end] == bg[start] {
			end++
		}
		style := lipgloss.NewStyle().Background(bg[start]).Foreground(lipgloss.Color("#000000")).Bold(true)
		b.WriteString(style.Render(string(text[start:end])))
		start = end
	}
	return b.String()
}

// gradeAt reads the course profile: the grade from the sample at or before d.
func gradeAt(c *pb.Course, d float64) float64 {
	g := c.GetProfileGradePct()
	step := c.GetProfileStepM()
	if len(g) == 0 || step <= 0 {
		return 0
	}
	i := int(d / step)
	return float64(g[max(0, min(i, len(g)-1))])
}

// heatStops map grade to colour, from easy blue to deep red at 16 %.
var heatStops = []struct {
	grade   float64
	r, g, b float64
}{
	{0, 0x2c, 0x7b, 0xb6},
	{3, 0x00, 0xa6, 0xca},
	{5, 0x00, 0xcc, 0xbc},
	{7, 0x90, 0xeb, 0x9d},
	{9, 0xff, 0xff, 0x8c},
	{11, 0xf9, 0xd0, 0x57},
	{13, 0xf2, 0x9e, 0x2e},
	{14.5, 0xd7, 0x19, 0x1c},
	{16, 0x7f, 0x00, 0x00},
}

// descentColor is used for any downhill; descents vary little in feel
// (the trainer gets at most half their grade).
var descentColor = lipgloss.Color("#6c7a89")

func gradeColor(g float64) color.Color {
	if g < -0.5 {
		return descentColor
	}
	g = math.Max(0, math.Min(g, heatStops[len(heatStops)-1].grade))
	for i := 1; i < len(heatStops); i++ {
		a, b := heatStops[i-1], heatStops[i]
		if g <= b.grade {
			t := (g - a.grade) / (b.grade - a.grade)
			mix := func(x, y float64) uint8 { return uint8(math.Round(x + (y-x)*t)) }
			return color.RGBA{R: mix(a.r, b.r), G: mix(a.g, b.g), B: mix(a.b, b.b), A: 0xff}
		}
	}
	return lipgloss.Color("#7f0000")
}

// finishColor checkers the cells past the finish line.
func finishColor(i int) color.Color {
	if i%2 == 0 {
		return lipgloss.Color("#e0e0e0")
	}
	return lipgloss.Color("#404040")
}

// elevationProfile draws the whole course as a heat-coloured area chart
// with the rider's position marked and the ridden part dimmed.
func elevationProfile(c *pb.Course, pos, ghost float64, width, height int) string {
	ele := c.GetProfileElevationM()
	if len(ele) < 2 || width < 10 || height < 1 {
		return ""
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, e := range ele {
		lo, hi = math.Min(lo, float64(e)), math.Max(hi, float64(e))
	}
	span := math.Max(hi-lo, 1)
	dist := c.GetDistanceM()
	here := int(pos / dist * float64(width-1))
	ghostX := -1
	if ghost >= 0 {
		ghostX = int(ghost / dist * float64(width-1))
	}
	levels := []rune(" ▁▂▃▄▅▆▇█")

	rows := make([]strings.Builder, height)
	for x := range width {
		d := float64(x) / float64(width-1) * dist
		e := profileElevation(c, d)
		// At least one eighth so the lowest point still shows.
		h := math.Max(1, (e-lo)/span*float64(height*8))
		style := lipgloss.NewStyle().Foreground(gradeColor(gradeAt(c, d)))
		switch {
		case x == here:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff"))
		case x == ghostX:
			style = ghostStyle
		case x < here:
			style = dimStyle
		}
		for row := range height {
			fill := h - float64((height-1-row)*8)
			var r rune
			switch {
			case fill >= 8:
				r = '█'
			case fill <= 0:
				r = ' '
				if x == here || x == ghostX {
					r = '│'
				}
			default:
				r = levels[int(fill)]
			}
			rows[row].WriteString(style.Render(string(r)))
		}
	}
	lines := make([]string, height)
	for i := range rows {
		lines[i] = rows[i].String()
	}
	return strings.Join(lines, "\n")
}

func profileElevation(c *pb.Course, d float64) float64 {
	ele := c.GetProfileElevationM()
	step := c.GetProfileStepM()
	x := d / step
	i := int(x)
	if i >= len(ele)-1 {
		return float64(ele[len(ele)-1])
	}
	f := x - float64(i)
	return float64(ele[i])*(1-f) + float64(ele[i+1])*f
}

// clock formats seconds as m:ss, or h:mm:ss from an hour.
func clock(s float64) string {
	d := time.Duration(s * float64(time.Second))
	if d >= time.Hour {
		return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
