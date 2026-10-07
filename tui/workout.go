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

	"github.com/digimago/osscycler/api"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

const (
	intensityStep = 1.0 // percent per + / - press during a workout
	// workoutShownFor keeps an ended workout's summary on screen.
	workoutShownFor = 10 * time.Second
)

// Picker tabs: r cycles through the ride tabs, w opens the workouts.
const (
	tabCourses = iota
	tabHistory
	tabActivities
	tabWorkouts
	numTabs
	rideTabs = tabWorkouts
)

type workoutsMsg struct {
	workouts []*pb.WorkoutDef
	err      error
	open     bool
}

// input is a one-line numeric prompt, used for FTP.
type input struct {
	prompt string
	value  string
	// control is the manual control mode the number sets; unspecified
	// means FTP.
	control pb.ControlMode
}

func (m Model) fetchWorkouts(open bool) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		ws, err := cmds.ListWorkouts(ctx)
		return workoutsMsg{workouts: ws, err: err, open: open}
	}
}

func (m Model) wk() *pb.WorkoutProgress { return m.st.GetWorkout() }

func (m Model) workoutActive() bool {
	switch m.wk().GetPhase() {
	case pb.WorkoutPhase_WORKOUT_PHASE_ARMED, pb.WorkoutPhase_WORKOUT_PHASE_RUNNING, pb.WorkoutPhase_WORKOUT_PHASE_PAUSED:
		return true
	}
	return false
}

// showWorkout reports whether the workout screen replaces the dashboard.
func (m Model) showWorkout() bool {
	switch m.wk().GetPhase() {
	case pb.WorkoutPhase_WORKOUT_PHASE_ARMED, pb.WorkoutPhase_WORKOUT_PHASE_RUNNING,
		pb.WorkoutPhase_WORKOUT_PHASE_PAUSED, pb.WorkoutPhase_WORKOUT_PHASE_FINISHED:
		return true
	case pb.WorkoutPhase_WORKOUT_PHASE_ABORTED:
		return time.Duration(m.st.GetCoreTimeNs()-m.wk().GetPhaseChangedNs()) < workoutShownFor
	}
	return false
}

func (m Model) onWorkouts(msg workoutsMsg) (Model, tea.Cmd) {
	m.fetchingWorkouts = false
	if msg.err != nil {
		m.notice = "loading workouts failed: " + friendlyErr(msg.err)
		return m, nil
	}
	m.workoutList = msg.workouts
	m.workoutDefs = make(map[string]*pb.WorkoutDef, len(msg.workouts))
	for _, w := range msg.workouts {
		m.workoutDefs[w.GetId()] = w
	}
	m.pickIdx[tabWorkouts] = min(m.pickIdx[tabWorkouts], len(msg.workouts)) // row 0 is fixed power
	return m, nil
}

// needWorkout fetches the library if the workout under way isn't known yet.
func (m Model) needWorkout() (Model, tea.Cmd) {
	id := m.wk().GetId()
	if id == "" || m.cmds == nil || m.fetchingWorkouts {
		return m, nil
	}
	if _, ok := m.workoutDefs[id]; ok {
		return m, nil
	}
	m.fetchingWorkouts = true
	return m, m.fetchWorkouts(false)
}

// workoutKey handles keys during a workout and in the workout tab. It
// reports whether it used the key.
func (m Model) workoutKey(key string) (Model, tea.Cmd, bool) {
	if m.input != nil {
		return m.inputKey(key)
	}
	if m.draft != nil {
		return m.draftKey(key)
	}
	if m.picking && m.tab == tabWorkouts {
		switch key {
		case "n":
			m.draft = newDraft("", newWorkout())
			return m, nil, true
		case "e", "E":
			w := m.selectedWorkout()
			if w == nil || w.GetError() != "" {
				return m, nil, true
			}
			m.draft = newDraft(w.GetId(), api.WorkoutFromProto(w))
			if key == "E" {
				return m.openEditor(m.draft)
			}
			return m, nil, true
		case "enter":
			if m.pickIdx[tabWorkouts] == 0 {
				m.picking, m.input = false, m.ergPrompt()
				return m, nil, true
			}
			w := m.selectedWorkout()
			if w == nil {
				return m, nil, true
			}
			if w.GetError() != "" {
				m.notice = w.GetId() + ": " + w.GetError()
				return m, nil, true
			}
			id := w.GetId()
			m.picking = false
			return m, m.command("start workout", func(ctx context.Context) error { return m.cmds.StartWorkout(ctx, id) }), true
		}
	}
	switch {
	case key == "f" && !m.workoutActive() && !m.courseRide():
		m.input = &input{prompt: "FTP in watts: ", value: fmt.Sprintf("%.0f", m.wk().GetFtpW())}
		return m, nil, true
	case !m.workoutActive():
		if (key == "x" || key == "enter") && m.showWorkout() && !m.picking {
			return m, m.command("close workout", m.cmds.StopWorkout), true
		}
		return m, nil, false
	case key == "+" || key == "=" || key == "-" || key == "_":
		step := intensityStep
		if key == "-" || key == "_" {
			step = -step
		}
		want := math.Round(m.wk().GetIntensityPct() + step)
		cmds := m.cmds
		return m, m.command("set intensity", func(ctx context.Context) error {
			_, err := cmds.SetIntensity(ctx, want)
			return err
		}), true
	case key == "n":
		return m, m.command("skip segment", m.cmds.SkipSegment), true
	case key == "x":
		if m.now().Before(m.abortUntil) {
			m.abortUntil, m.asking = time.Time{}, ""
			return m, m.command("abort workout", m.cmds.StopWorkout), true
		}
		m.abortUntil = m.now().Add(abortConfirm)
		m = m.ask("press x again to abort the workout", m.abortUntil)
		return m, nil, true
	}
	return m, nil, false
}

// selectedWorkout is the workout under the cursor; nil on the fixed power
// row above them.
func (m Model) selectedWorkout() *pb.WorkoutDef {
	if i := m.pickIdx[tabWorkouts] - 1; i >= 0 && i < len(m.workoutList) {
		return m.workoutList[i]
	}
	return nil
}

func (m Model) inputKey(key string) (Model, tea.Cmd, bool) {
	in := *m.input
	switch {
	case key == "esc":
		m.input = nil
	case key == "enter":
		m.input = nil
		var v float64
		if in.control != pb.ControlMode_CONTROL_MODE_UNSPECIFIED {
			if _, err := fmt.Sscan(in.value, &v); err != nil {
				m.notice = "not a number: " + in.value
				return m, nil, true
			}
			return m, m.setControl(in.control, v), true
		}
		if _, err := fmt.Sscan(in.value, &v); err != nil || v <= 0 {
			m.notice = "FTP must be a number of watts"
			return m, nil, true
		}
		cmds := m.cmds
		return m, m.command("set FTP", func(ctx context.Context) error {
			_, err := cmds.SetFTP(ctx, v)
			return err
		}), true
	case key == "backspace":
		if len(in.value) > 0 {
			in.value = in.value[:len(in.value)-1]
		}
		m.input = &in
	case len(key) == 1 && key[0] >= '0' && key[0] <= '9' && len(in.value) < 5,
		key == "." && in.control != pb.ControlMode_CONTROL_MODE_UNSPECIFIED,
		key == "-" && in.control == pb.ControlMode_CONTROL_MODE_GRADE && in.value == "":
		in.value += key
		m.input = &in
	}
	return m, nil, true
}

// avgTarget is the time-weighted mean target (fraction of FTP), ERG parts only.
func avgTarget(w *pb.WorkoutDef) float64 {
	var sum, t float64
	for _, s := range w.GetTimeline() {
		if s.GetFree() {
			continue
		}
		sum += (s.GetFromFtp() + s.GetToFtp()) / 2 * s.GetDurationS()
		t += s.GetDurationS()
	}
	if t == 0 {
		return 0
	}
	return sum / t
}

func (m Model) workoutRows(width int) []string {
	fixed := "Fixed power (ERG)"
	if c := m.control(); c.GetMode() == pb.ControlMode_CONTROL_MODE_POWER {
		fixed += fmt.Sprintf(": now %.0f W", c.GetTarget())
	}
	cursor, style := "  ", lipgloss.NewStyle()
	if m.pickIdx[tabWorkouts] == 0 {
		cursor, style = "▸ ", style.Bold(true).Foreground(lipgloss.Color("220"))
	}
	rows := []string{style.Render(fmt.Sprintf("%s%-34s %s", cursor, fixed, dimStyle.Render("hold one power, no workout")))}
	for i, w := range m.workoutList {
		cursor, style := "  ", lipgloss.NewStyle()
		if i+1 == m.pickIdx[tabWorkouts] {
			cursor, style = "▸ ", style.Bold(true).Foreground(lipgloss.Color("220"))
		}
		if w.GetError() != "" {
			rows = append(rows, style.Render(cursor)+badStyle.Render(fmt.Sprintf("%-34s ✕ %s", truncate(w.GetId(), 34), truncate(w.GetError(), 40))))
			continue
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%-34s %8s  ~%3.0f%% FTP",
			cursor, truncate(w.GetName(), 34), clock(w.GetDurationS()), avgTarget(w)*100)))
	}
	if len(m.workoutList) == 0 {
		rows = append(rows, dimStyle.Render("  no workouts yet: press n to write one, or drop .zwo files in the library folder"))
	}
	return rows
}

// workoutBody is the workout screen above the footer.
func (m Model) workoutBody(width, height int) string {
	p := m.wk()
	switch p.GetPhase() {
	case pb.WorkoutPhase_WORKOUT_PHASE_FINISHED, pb.WorkoutPhase_WORKOUT_PHASE_ABORTED:
		return m.workoutResult(width, height)
	}
	info := m.workoutInfo(width)
	tilesH := height - lipgloss.Height(info)

	tiles := m.screenTiles(screenWorkout)
	// On a loop the workout rides along the road.
	if sc := m.roadScene(); sc != nil && m.onLoop() && !m.tiles {
		if body := m.roadBody(sc, tiles, width, tilesH, info, ""); body != "" {
			return body
		}
	}
	body := m.grid(tiles, width, tilesH)
	if body == "" {
		body = tileList(tiles, 18, width, max(tilesH, 4))
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, info)
}

func (m Model) workoutInfo(width int) string {
	p := m.wk()
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	var lines []string
	switch p.GetPhase() {
	case pb.WorkoutPhase_WORKOUT_PHASE_ARMED:
		lines = append(lines, bigWarn.Render("start pedalling to start the workout"))
	case pb.WorkoutPhase_WORKOUT_PHASE_PAUSED:
		lines = append(lines, bigWarn.Render("paused: pedal to resume"))
	}
	if msg := p.GetMessage(); msg != "" {
		lines = append(lines, bigOK.Render("» "+msg))
	}
	next := "last segment"
	if p.GetNextLabel() != "" {
		next = "next: " + p.GetNextLabel()
		if p.GetNextFree() {
			next += " (free)"
		} else {
			next += fmt.Sprintf(" @ %.0f W", p.GetNextTargetW())
		}
	}
	cad := m.metrics()[2].value + " rpm"
	if c := p.GetTargetCadence(); c > 0 {
		cad += fmt.Sprintf(" (target %d)", c)
	}
	lines = append(lines, fmt.Sprintf("%s · %s · %s / %s · %s",
		next, cad, clock(p.GetElapsedS()), clock(p.GetDurationS()), dimStyle.Render(truncate(p.GetName(), 40))))
	for i := range lines {
		lines[i] = center.Render(lines[i])
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) workoutResult(width, height int) string {
	p := m.wk()
	var lines []string
	if p.GetPhase() == pb.WorkoutPhase_WORKOUT_PHASE_FINISHED {
		lines = append(lines, bigOK.Render("WORKOUT DONE  "+p.GetName()), "",
			fmt.Sprintf("%s · %.0f W average", clock(p.GetElapsedS()), p.GetAvgPowerW()),
			"", dimStyle.Render("x or enter to close"))
	} else {
		lines = append(lines, warnStyle.Render("workout aborted"),
			fmt.Sprintf("%s of %s", clock(p.GetElapsedS()), clock(p.GetDurationS())))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}

// zoneStops are Zwift's power zone colours by fraction of FTP.
var zoneStops = []struct {
	upTo float64
	c    color.Color
}{
	{0.60, lipgloss.Color("#7f7f7f")}, // Z1 recovery
	{0.76, lipgloss.Color("#338cff")}, // Z2 endurance
	{0.90, lipgloss.Color("#59bf59")}, // Z3 tempo
	{1.05, lipgloss.Color("#ffcc3f")}, // Z4 threshold
	{1.19, lipgloss.Color("#ff6639")}, // Z5 VO2 max
}

var zone6 = lipgloss.Color("#ff330c") // anaerobic and up

func zoneColor(frac float64) color.Color {
	for _, z := range zoneStops {
		if frac < z.upTo {
			return z.c
		}
	}
	return zone6
}

// workoutProfile draws the whole workout as zone-coloured bars over time,
// Zwift style, with the done part dimmed and a marker at elapsed seconds
// (negative: no marker).
func workoutProfile(w *pb.WorkoutDef, elapsed float64, width, height int) string {
	total := w.GetDurationS()
	segs := w.GetTimeline()
	if total <= 0 || len(segs) == 0 || width < 10 || height < 1 {
		return ""
	}
	top := 1.2
	for _, s := range segs {
		top = math.Max(top, math.Max(s.GetFromFtp(), s.GetToFtp()))
	}
	here := -1
	if elapsed >= 0 {
		here = int(elapsed / total * float64(width-1))
	}
	levels := []rune(" ▁▂▃▄▅▆▇█")
	rows := make([]strings.Builder, height)
	si := 0
	for x := range width {
		t := (float64(x) + 0.5) / float64(width) * total
		for si < len(segs)-1 && t >= segs[si].GetStartS()+segs[si].GetDurationS() {
			si++
		}
		s := segs[si]
		frac := 0.35 // free segments show as a low grey bar
		c := lipgloss.Color("#505050")
		if !s.GetFree() {
			f := (t - s.GetStartS()) / s.GetDurationS()
			frac = s.GetFromFtp() + (s.GetToFtp()-s.GetFromFtp())*f
			c = zoneColor(frac)
		}
		style := lipgloss.NewStyle().Foreground(c)
		switch {
		case x == here:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff"))
		case x < here:
			style = dimStyle
		}
		h := math.Max(1, frac/top*float64(height*8))
		for row := range height {
			fill := h - float64((height-1-row)*8)
			r := ' '
			switch {
			case fill >= 8:
				r = '█'
			case fill > 0:
				r = levels[int(fill)]
			case x == here:
				r = '│'
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
