// Package tui is a text renderer for the core's telemetry, meant for a
// screen you can read from the bike.
package tui

import (
	"fmt"
	"math"
	"slices"
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
	// menuPaused: esc opened the menu during a ride, workout or manual
	// control and paused the core; leaving the menu with esc carries on.
	menuPaused bool
	// quitting: Quit was chosen with something under way and no other
	// screen connected; the question is on screen.
	quitting      bool
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
	fetchedAt  time.Time // when the course list last arrived
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
	onboardLater     bool          // the rider put onboarding off this session
	menu             *menuState    // the start menu (m)
	laps             []*pb.RideLap // this loop ride's completed laps
	lapsUntil        time.Time     // the lap times show until then
	input            *input        // numeric prompt
	abortUntil       time.Time     // a second x before this aborts the ride
	now              func() time.Time

	tour        *Tour // demo autopilot; nil without -tour
	tourCaption string

	scenes map[string]*roadScene // road views by course ID
	tiles  bool                  // v: big tiles instead of the road view
	posAt  time.Time             // when the ride distance last changed
	posErr float64               // shown minus reported then, blended out
	// The ghost, carried forward the same way: where it was reported, when
	// that changed, and its speed from the reports before.
	ghostD, ghostV float64
	ghostSeen      time.Time
	animating      bool            // a frame tick is pending
	help           bool            // the key help is open
	cadence        []cadenceSample // recent readings, for the averaged tile
	// z: medium digits even where large ones would fit.
	mediumDigits bool
	layout       Layout     // the rider's tile arrangement
	layoutPath   string     // where it is saved; "" for this session only
	arranging    *arranging // the tile editor (o)
	// asking is a second key press being asked for, shown over the
	// screen until askUntil.
	asking   string
	askUntil time.Time
}

// ask shows a request for a confirming key press over the screen.
func (m Model) ask(text string, until time.Time) Model {
	m.asking, m.askUntil = text, until
	return m
}

var askStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#b3261e")).Bold(true).Padding(1, 4)

// withAsk lays the pending question over the middle of body.
func (m Model) withAsk(body string) string {
	if m.asking == "" || !m.now().Before(m.askUntil) {
		return body
	}
	bar := askStyle.Render(m.asking)
	x := max(0, (lipgloss.Width(body)-lipgloss.Width(bar))/2)
	y := max(0, (lipgloss.Height(body)-lipgloss.Height(bar))/2)
	return lipgloss.NewCompositor(lipgloss.NewLayer(body), lipgloss.NewLayer(bar).X(x).Y(y).Z(1)).Render()
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

// Init fetches the courses for the start menu's tracks.
func (m Model) Init() tea.Cmd {
	if m.menu != nil && m.cmds != nil {
		return m.fetchCourses(false)
	}
	return nil
}

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
	case layoutSavedMsg:
		if msg.err != nil {
			m.notice = "saving the tile layout failed: " + msg.err.Error()
		}
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
		shown, old, wasPaused := m.ridePos(), m.ride(), m.paused()
		m.st, m.connected, m.connErr = msg.State, true, nil
		m = m.withCadence(m.now())
		if r := m.ride(); r.GetDistanceM() != old.GetDistanceM() || r.GetPhase() != old.GetPhase() {
			// Carry the position forward from here; blend out the difference
			// from what was shown rather than jumping.
			m.posAt, m.posErr = m.now(), 0
			if r.GetCourseId() == old.GetCourseId() && r.GetPhase() == pb.RidePhase_RIDE_PHASE_RIDING {
				if e := shown - r.GetDistanceM(); math.Abs(e) < 20 {
					m.posErr = e
				}
			}
		}
		m = m.followGhost(old)
		m = m.others()
		if m.needsOnboarding() {
			m = m.startOnboarding(false)
		}
		if m.menuPaused && wasPaused && !m.paused() {
			m.menu, m.menuPaused = nil, false // another screen carried on: back to the ride
		}
		if m.menu != nil && m.busy() && !m.menuPaused {
			m.menu = nil // joined something already running: show it
		}
		if m.quitting && (!m.underWay() || m.st.GetHeads() > 1) {
			// Ended, or watched, on another screen meanwhile: nothing to ask.
			return m, tea.Quit
		}
		m, c1 := m.needCourse()
		m, c2 := m.needWorkout()
		m, c3 := m.animate()
		m, c4 := m.trackLaps(old)
		return m, tea.Batch(c1, c2, c3, c4)
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
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	// Any key closes the help; it does nothing else.
	if m.help {
		m.help = false
		return m, nil
	}
	if m.helpKey(key) {
		m.help = true
		return m, nil
	}
	if next, cmd, ok := m.layoutKey(key); ok {
		return next, cmd
	}
	if key == "q" && m.draft == nil && m.cmds == nil {
		return m, tea.Quit // a read-only screen has no menu to go back to
	}
	if m.cmds != nil {
		if next, cmd, ok := m.quitKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.pauseKey(key); ok {
			return next, cmd
		}
		if next, cmd, ok := m.menuKey(key); ok {
			return next, cmd
		}
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
	var banners []string
	if m.paused() && m.width > 0 {
		banners = append(banners, m.pauseBanner())
	}
	if m.tourCaption != "" && m.width > 0 {
		banners = append(banners, tourStyle.Width(m.width).Render(" "+truncate(m.tourCaption, m.width-2)))
	}
	if len(banners) == 0 {
		return m.renderScreen()
	}
	m.height -= len(banners)
	return lipgloss.JoinVertical(lipgloss.Left, append(banners, m.renderScreen())...)
}

func (m Model) renderScreen() string {
	if m.width == 0 {
		return ""
	}
	footer := m.footer()
	ms := m.screenTiles(screenDashboard)

	// The grade strip (rides) or workout profile (workouts) sits at the
	// very bottom.
	var strip string
	switch {
	case m.help || m.arranging != nil || m.draft != nil || m.picking || m.input != nil || m.ending != nil || m.onboarding != nil || m.menu != nil || m.quitting:
	case m.showWorkout() && m.workoutActive():
		if w := m.workoutDefs[m.wk().GetId()]; w != nil && m.height >= 20 {
			strip = workoutProfile(w, m.wk().GetElapsedS(), m.width, 4)
		}
	case m.showRide() && m.rideActive():
		pos := m.ride().GetDistanceM()
		strip = gradeStrip(m.courses[m.ride().GetCourseId()], pos, m.loopGhost(pos), m.width)
	}

	// Big tiles need two rows of label + digits + unit, plus the footer.
	bodyH := m.height - lipgloss.Height(footer)
	if strip != "" {
		bodyH -= lipgloss.Height(strip)
	}
	var body string
	switch {
	case m.help:
		body = m.helpPanel(m.width, bodyH)
	case m.arranging != nil:
		body = m.arrangePanel(m.width, bodyH)
	case m.onboarding != nil:
		body = m.onboardPanel(m.width, bodyH)
	case m.quitting:
		body = m.quitPanel(m.width, bodyH)
	case m.menu != nil:
		body = m.menuPanel(m.width, bodyH)
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
	case m.grid(ms, m.width, bodyH) != "":
		body = m.grid(ms, m.width, bodyH)
	default:
		body = m.compact(ms)
	}
	body = m.withAsk(m.withLaps(body))
	if strip != "" {
		return lipgloss.JoinVertical(lipgloss.Left, body, footer, strip)
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

// sizes are the digit sizes tiles may use, largest first: z keeps them
// medium.
func (m Model) sizes() []digitSize {
	if m.mediumDigits {
		return []digitSize{sizeMedium}
	}
	return []digitSize{sizeLarge, sizeMedium}
}

// grid lays metrics out as tiles with the largest digits that fit; empty
// when none do.
func (m Model) grid(ms []metric, width, height int) string {
	for _, size := range m.sizes() {
		if g := tileGrid(ms, width, height, size); g != "" {
			return g
		}
	}
	return ""
}

func bigTile(mt metric, w, h int, size digitSize) string {
	us := unitStyle
	if mt.unitStyle != nil {
		us = *mt.unitStyle
	}
	content := lipgloss.JoinVertical(lipgloss.Center,
		labelStyle.Render(mt.label),
		mt.style.Render(size.render(mt.value)),
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
	} else if m.controlActive() {
		ride = warnStyle.Render(controlText(m.control())) + fmt.Sprintf("   %.2f km   %s", tr.GetDistanceM()/1000, trainerState(tr.GetState()))
		if r := m.ride(); m.onLoop() {
			ride = warnStyle.Render(controlText(m.control())) + fmt.Sprintf("   lap %d   %.2f / %.2f km", r.GetLap(), r.GetDistanceM()/1000, r.GetCourseDistanceM()/1000)
		}
	} else if r := m.ride(); m.rideActive() {
		// On a course the ride clock is in the tiles; show position and
		// what the trainer is asked to do.
		ride = fmt.Sprintf("%.2f / %.2f km   %.0f m   trainer %.1f%% at %.0f%% difficulty",
			r.GetDistanceM()/1000, r.GetCourseDistanceM()/1000, r.GetElevationM(), r.GetTrainerGradePct(), m.difficulty())
		if r.GetLoop() {
			ride = fmt.Sprintf("lap %d   ", r.GetLap()) + ride
		}
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

	// The trainer's wish for a spin-down is a key hint, not a warning: the
	// Flux asks after every power-up, and a warning hid the other hints.
	var warnings []string
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
		if m.st.GetTrainer().GetResistanceCalibrationRequired() && !m.calibrationActive() {
			return dimStyle.Render("trainer asks for a spin-down calibration · q quit")
		}
		return dimStyle.Render("q quit")
	}
	var h []string
	switch {
	case m.menu != nil && m.menu.tracks:
		return dimStyle.Render("↑↓ choose · enter ride · esc back · " + helpHint + " · q quit")
	case m.menu != nil:
		return dimStyle.Render("↑↓ choose · enter do it · esc free ride · " + helpHint + " · q quit")
	case m.picking:
		// The picker lists its own keys; the dashboard's don't apply here.
		return dimStyle.Render("esc back · " + helpHint + " · q quit")
	case m.draft != nil:
		return dimStyle.Render("F1 help · ctrl+c quit")
	case m.workoutActive():
		return dimStyle.Render(fmt.Sprintf("+/- intensity %.0f%% · n skip · x x abort · %s · q quit", m.wk().GetIntensityPct(), helpHint))
	case m.controlActive():
		step := controlStep[m.control().GetMode()]
		return dimStyle.Render(fmt.Sprintf("+/- %s · w ERG or workout · g grade · l level · x free ride · %s · q quit", num(step), helpHint))
	case m.showWorkout():
		h = append(h, "x close")
	case m.rideActive():
		if m.roadScene() != nil {
			h = append(h, map[bool]string{false: "v tiles", true: "v road"}[m.tiles])
		}
		if m.onLoop() {
			h = append(h, "w workout", "g/l trainer", "x x end ride")
		} else {
			h = append(h, "x x abort ride")
		}
	case m.showRide():
		h = append(h, "x close")
	default:
		h = append(h, "m menu", "r ride", "w workout", "g/l trainer")
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
	h = append(h, helpHint, "q quit")
	// On a narrow screen leave out the hints least needed (the help lists
	// every key), rather than wrapping onto a second line.
	for _, drop := range []string{"+/- difficulty", "g/l trainer", "w workout", "r ride"} {
		if m.width == 0 || lipgloss.Width(strings.Join(h, " · ")) <= m.width {
			break
		}
		h = slices.DeleteFunc(h, func(s string) bool { return strings.HasPrefix(s, drop) })
	}
	return dimStyle.Render(strings.Join(h, " · "))
}
