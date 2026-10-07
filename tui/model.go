// Package tui is a text renderer for the core's telemetry, meant for a
// screen you can read from the bike.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// StateMsg carries a new state from the core.
type StateMsg struct{ State *pb.State }

// ConnMsg reports the connection to the core. Err is nil once connected.
type ConnMsg struct{ Err error }

type Model struct {
	addr          string
	cmds          Commands // nil: read-only
	width, height int
	connected     bool
	connErr       error
	st            *pb.State

	countdown   int // seconds until a calibration starts; 0 = none
	countdownID int // invalidates ticks of an aborted countdown
	notice      string

	courses    map[string]*pb.Course // by ID, with profiles
	courseList []*pb.Course
	fetching   bool
	picking    bool
	tab        int          // tabCourses, tabWorkouts or tabHistory
	pickIdx    [numTabs]int // selection per tab
	results    []*pb.RideResult
	activities []*pb.Activity
	exportDir  string

	workoutList      []*pb.WorkoutDef
	workoutDefs      map[string]*pb.WorkoutDef
	fetchingWorkouts bool
	draft            *draft  // workout being edited
	ending           *ending // end-ride question
	onboarding       *onboarding
	onboardLater     bool      // the rider put onboarding off this session
	input            *input    // numeric prompt
	abortUntil       time.Time // a second x before this aborts the ride
	now              func() time.Time

	tour        *Tour // demo autopilot; nil without -tour
	tourCaption string

	scenes    map[string]*roadScene // road views by course ID
	tiles     bool                  // v: big tiles instead of the road view
	stateAt   time.Time             // when the last state arrived
	animating bool                  // a frame tick is pending
}

// frameMsg asks for the next frame of the road view.
type frameMsg struct{}

func frameTick() tea.Cmd {
	return tea.Tick(roadFrame, func(time.Time) tea.Msg { return frameMsg{} })
}

// animate starts the road view's frames when they should run and aren't.
func (m Model) animate() (Model, tea.Cmd) {
	if m.animating || !m.roadMoving() {
		return m, nil
	}
	m.animating = true
	return m, frameTick()
}

// New returns a model for the core at addr. cmds may be nil for a
// read-only display.
func New(addr string, cmds Commands) Model { return Model{addr: addr, cmds: cmds, now: time.Now} }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// A real key press ends a running tour; the key still counts.
		if m.tour != nil && m.tour.Stop() {
			m.tourCaption = "Tour stopped: the keys are yours"
		}
		return m.handleKey(msg.String())
	case tourKeyMsg:
		return m.handleKey(string(msg))
	case tourCaptionMsg:
		m.tourCaption = string(msg)
	case coursesMsg:
		return m.onCourses(msg)
	case workoutsMsg:
		return m.onWorkouts(msg)
	case resultsMsg:
		return m.onResults(msg)
	case activitiesMsg:
		return m.onActivities(msg)
	case exportedMsg:
		return m.onExported(msg)
	case editedMsg:
		return m.onEdited(msg)
	case savedMsg:
		return m.onSaved(msg)
	case endedMsg:
		return m.onEnded(msg)
	case profileSavedMsg:
		return m.onProfileSaved(msg)
	case countdownMsg:
		return m.onCountdown(msg)
	case commandMsg:
		if msg.err != nil {
			m.notice = msg.what + " failed: " + friendlyErr(msg.err)
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case frameMsg:
		if m.roadMoving() {
			return m, frameTick()
		}
		m.animating = false
	case StateMsg:
		m.st, m.connected, m.connErr = msg.State, true, nil
		m.stateAt = m.now()
		if m.needsOnboarding() {
			m = m.startOnboarding(false)
		}
		m, c1 := m.needCourse()
		m, c2 := m.needWorkout()
		m, c3 := m.animate()
		return m, tea.Batch(c1, c2, c3)
	case ConnMsg:
		m.connected, m.connErr = msg.Err == nil, msg.Err
		if msg.Err != nil {
			m.st = nil // never show numbers we can no longer vouch for
		}
	}
	return m, nil
}

var (
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	unitStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	powerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	hrStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cadenceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	speedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	okStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	badStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

type metric struct {
	label, value, unit string
	style              lipgloss.Style
	unitStyle          *lipgloss.Style // nil: the usual grey
}

func (m Model) metrics() []metric {
	tr := m.st.GetTrainer()
	if tr == nil {
		tr = &pb.Trainer{} // no state yet: every field reads as unset
	}
	power, cadence, speed, hr := "--", "--", "--", "--"
	if tr.PowerW != nil {
		power = fmt.Sprint(tr.GetPowerW())
	}
	if tr.CadenceRpm != nil {
		cadence = fmt.Sprint(tr.GetCadenceRpm())
	}
	if tr.SpeedMps != nil {
		speed = fmt.Sprintf("%.1f", tr.GetSpeedMps()*3.6)
	}
	// Prefer the strap; fall back to heart rate the trainer forwards.
	if h := m.st.GetHeartRate(); h != nil && h.Bpm != nil {
		hr = fmt.Sprint(h.GetBpm())
	} else if tr.HeartRateBpm != nil {
		hr = fmt.Sprint(tr.GetHeartRateBpm())
	}
	return []metric{
		{"POWER", power, "W", powerStyle, nil},
		{"HEART RATE", hr, "bpm", hrStyle, nil},
		{"CADENCE", cadence, "rpm", cadenceStyle, nil},
		{"SPEED", speed, "km/h", speedStyle, nil},
	}
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "osscycler"
	return v
}

// handleKey routes a key press, real or from the tour.
func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	// In the workout editor q is just a letter; ctrl+c still quits.
	if key == "ctrl+c" || (key == "q" && m.draft == nil) {
		return m, tea.Quit
	}
	if m.cmds != nil {
		if next, cmd, ok := m.onboardKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.endKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.workoutKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.controlKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.rideKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.calibrationKey(key); ok {
			return next, cmd
		}
	}
	return m, nil
}

// render draws the screen, with the tour banner on top when there is one.
func (m Model) render() string {
	if m.tourCaption == "" || m.width == 0 {
		return m.renderScreen()
	}
	banner := tourStyle.Width(m.width).Render(" " + truncate(m.tourCaption, m.width-2))
	m.height--
	return lipgloss.JoinVertical(lipgloss.Left, banner, m.renderScreen())
}

func (m Model) renderScreen() string {
	if m.width == 0 {
		return ""
	}
	footer := m.footer()
	ms := m.metrics()

	// The grade strip (rides) or workout profile (workouts) sits at the
	// very bottom.
	var strip string
	switch {
	case m.draft != nil || m.picking || m.input != nil || m.ending != nil || m.onboarding != nil:
	case m.showWorkout() && m.workoutActive():
		if w := m.workoutDefs[m.wk().GetId()]; w != nil && m.height >= 20 {
			strip = workoutProfile(w, m.wk().GetElapsedS(), m.width, 4)
		}
	case m.showRide() && m.rideActive():
		strip = gradeStrip(m.courses[m.ride().GetCourseId()], m.ride().GetDistanceM(), m.ghostAt(), m.width)
	}

	// Big tiles need two rows of label + digits + unit, plus the footer.
	bodyH := m.height - lipgloss.Height(footer)
	if strip != "" {
		bodyH -= lipgloss.Height(strip)
	}
	tileH := bodyH / 2
	tileW := m.width / 2
	var body string
	switch {
	case m.onboarding != nil:
		body = m.onboardPanel(m.width, bodyH)
	case m.ending != nil:
		body = m.endPanel(m.width, bodyH)
	case m.input != nil:
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center,
			titleStyle.Render(m.input.prompt+m.input.value+"▏")+"\n\n"+dimStyle.Render("enter set · esc cancel"))
	case m.draft != nil:
		body = m.formPanel(m.width, bodyH)
	case m.picking:
		body = m.pickerPanel(m.width, bodyH)
	case m.showWorkout():
		body = m.workoutBody(m.width, bodyH)
	case m.showRide():
		body = m.rideBody(m.width, bodyH)
	case m.countdown > 0 || m.calibrationActive() || m.showResult():
		body = m.calibrationPanel(m.width, bodyH)
	case tileH >= BigHeight+3 && tileW >= 30:
		var rows []string
		for i := 0; i < len(ms); i += 2 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
				bigTile(ms[i], tileW, tileH), bigTile(ms[i+1], m.width-tileW, tileH)))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	default:
		body = m.compact(ms)
	}
	if strip != "" {
		return lipgloss.JoinVertical(lipgloss.Left, body, footer, strip)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func bigTile(mt metric, w, h int) string {
	us := unitStyle
	if mt.unitStyle != nil {
		us = *mt.unitStyle
	}
	content := lipgloss.JoinVertical(lipgloss.Center,
		labelStyle.Render(mt.label),
		mt.style.Render(Big(mt.value)),
		us.Render(mt.unit))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) compact(ms []metric) string {
	var b strings.Builder
	for _, mt := range ms {
		fmt.Fprintf(&b, "%s %s %s\n", labelStyle.Render(fmt.Sprintf("%-10s", mt.label)),
			mt.style.Bold(true).Render(fmt.Sprintf("%6s", mt.value)), unitStyle.Render(mt.unit))
	}
	return lipgloss.Place(m.width, max(m.height-3, len(ms)), lipgloss.Center, lipgloss.Center, b.String())
}

func (m Model) footer() string {
	tr := m.st.GetTrainer()

	ride := dimStyle.Render("--:--:--   --.-- km")
	if p := m.wk(); m.workoutActive() {
		ride = fmt.Sprintf("FTP %.0f W · intensity %.0f%% · average %.0f W", p.GetFtpW(), p.GetIntensityPct(), p.GetAvgPowerW())
	} else if r := m.ride(); m.rideActive() {
		// On a course the ride clock is in the tiles; show position and
		// what the trainer is asked to do.
		ride = fmt.Sprintf("%.2f / %.2f km   %.0f m   trainer %.1f%% at %.0f%% difficulty",
			r.GetDistanceM()/1000, r.GetCourseDistanceM()/1000, r.GetElevationM(), r.GetTrainerGradePct(), m.difficulty())
	} else if m.controlActive() {
		ride = warnStyle.Render(controlText(m.control())) + fmt.Sprintf("   %.2f km   %s", tr.GetDistanceM()/1000, trainerState(tr.GetState()))
	} else if m.st != nil {
		el := time.Duration(tr.GetElapsedS() * float64(time.Second))
		ride = fmt.Sprintf("%d:%02d:%02d   %.2f km   %s",
			int(el.Hours()), int(el.Minutes())%60, int(el.Seconds())%60,
			tr.GetDistanceM()/1000, trainerState(tr.GetState()))
	}

	core := okStyle.Render("● core " + m.addr)
	switch {
	case m.connErr != nil:
		core = badStyle.Render("✕ core " + m.addr + ": " + m.connErr.Error())
	case !m.connected:
		core = warnStyle.Render("◌ connecting to " + m.addr)
	}
	sensors := fmt.Sprintf("%s   %s   %s", core,
		sensorLine("trainer", tr.GetSensor()), sensorLine("hrm", m.st.GetHeartRate().GetSensor()))
	if r := m.st.GetRadio(); r != nil && !r.GetPresent() {
		sensors = core + "   " + badStyle.Render("✕ ANT+ stick not found: plug it in (the core keeps looking)")
	}
	if rec := recordingLine(m.st.GetRecording()); rec != "" {
		sensors += "   " + rec
	}

	var warnings []string
	if tr.GetResistanceCalibrationRequired() && !m.calibrationActive() && m.countdown == 0 && !m.rideActive() && !m.workoutActive() {
		hint := "spin-down calibration recommended"
		if m.canCalibrate() {
			hint += ": press c"
		}
		warnings = append(warnings, hint)
	}
	if tr.GetUserConfigRequired() {
		warnings = append(warnings, "trainer wants your weight: press p")
	}
	switch tr.GetTargetPowerLimit() {
	case pb.TargetPowerLimit_TARGET_POWER_LIMIT_SPEED_TOO_LOW:
		warnings = append(warnings, "ERG: speed too low for target, shift up")
	case pb.TargetPowerLimit_TARGET_POWER_LIMIT_SPEED_TOO_HIGH:
		warnings = append(warnings, "ERG: speed too high for target, shift down")
	}
	if e := m.st.GetRecording().GetError(); e != "" {
		warnings = append(warnings, "recording failed: "+e)
	}
	if m.notice != "" {
		warnings = append(warnings, m.notice)
	}
	warn := m.keyHints()
	if len(warnings) > 0 {
		warn = warnStyle.Render("⚠ " + strings.Join(warnings, " · "))
	}
	center := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center)
	return lipgloss.JoinVertical(lipgloss.Left, center.Render(ride), center.Render(sensors), center.Render(warn))
}

func sensorLine(name string, s *pb.Sensor) string {
	id := ""
	if s.GetDeviceNumber() != 0 {
		id = fmt.Sprintf(" #%d", s.GetDeviceNumber())
	}
	switch s.GetStatus() {
	case pb.SensorStatus_SENSOR_STATUS_CONNECTED:
		return okStyle.Render("● " + name + id)
	case pb.SensorStatus_SENSOR_STATUS_SEARCHING:
		return warnStyle.Render("◌ " + name + " searching")
	case pb.SensorStatus_SENSOR_STATUS_LOST:
		return badStyle.Render("✕ " + name + id + " lost")
	case pb.SensorStatus_SENSOR_STATUS_DISABLED:
		return dimStyle.Render("– " + name + " off")
	}
	return dimStyle.Render("? " + name)
}

// recordingLine shows the core's FIT recorder: recording, auto-paused,
// or the last file saved. Empty when the core doesn't record.
func recordingLine(r *pb.Recording) string {
	switch {
	case r.GetActive() && r.GetPaused():
		return warnStyle.Render("⏸ rec " + clock(r.GetTimerS()))
	case r.GetActive():
		return badStyle.Render("● rec " + clock(r.GetTimerS()))
	case r.GetLastSaved() != "":
		return dimStyle.Render("✓ saved " + r.GetLastSaved())
	}
	return ""
}

func trainerState(s pb.TrainerState) string {
	switch s {
	case pb.TrainerState_TRAINER_STATE_ASLEEP:
		return "asleep"
	case pb.TrainerState_TRAINER_STATE_READY:
		return "ready"
	case pb.TrainerState_TRAINER_STATE_IN_USE:
		return "riding"
	case pb.TrainerState_TRAINER_STATE_FINISHED:
		return "paused"
	}
	return "-"
}

// friendlyErr strips gRPC framing from errors shown to the rider.
func friendlyErr(err error) string {
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

// keyHints lists the keys that do something right now.
func (m Model) keyHints() string {
	if m.cmds == nil {
		return dimStyle.Render("q quit")
	}
	var h []string
	switch {
	case m.draft != nil:
		return dimStyle.Render("ctrl+c quit")
	case m.workoutActive():
		return dimStyle.Render(fmt.Sprintf("+/- intensity %.0f%% · n skip · x x abort · q quit", m.wk().GetIntensityPct()))
	case m.controlActive():
		step := controlStep[m.control().GetMode()]
		return dimStyle.Render(fmt.Sprintf("+/- %s · w watts · g grade · l level · x free ride · q quit", num(step)))
	case m.showWorkout():
		h = append(h, "x close")
	case m.rideActive():
		if m.roadScene() != nil {
			h = append(h, map[bool]string{false: "v tiles", true: "v road"}[m.tiles])
		}
		h = append(h, "x x abort ride")
	case m.showRide():
		h = append(h, "x close")
	default:
		h = append(h, "r ride", "w/g/l trainer")
		if need := missingText(m.profile()); need != "" {
			h = append(h, "p add "+need)
		} else if m.profile() != nil {
			h = append(h, "p profile")
		}
		if m.canEnd() {
			h = append(h, "e end ride")
		}
	}
	switch {
	case m.canCalibrate():
		h = append(h, "c calibrate")
	case m.mayCalibrate():
		h = append(h, "C recalibrate")
	}
	h = append(h, fmt.Sprintf("+/- difficulty %.0f%%", m.difficulty()))
	return dimStyle.Render(strings.Join(append(h, "q quit"), " · "))
}
