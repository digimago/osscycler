package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// Commands are the requests the TUI can make of the core.
type Commands interface {
	StartCalibration(ctx context.Context) error
	CancelCalibration(ctx context.Context) error
	ListCourses(ctx context.Context) ([]*pb.Course, error)
	StartRide(ctx context.Context, courseID string) error
	// StartRideAgainst races an earlier ride, by when it finished.
	StartRideAgainst(ctx context.Context, courseID string, finishedUnixMs int64) error
	StopRide(ctx context.Context) error
	SetDifficulty(ctx context.Context, pct float64) (float64, error)
	ListWorkouts(ctx context.Context) ([]*pb.WorkoutDef, error)
	SaveWorkout(ctx context.Context, id string, w *pb.WorkoutDef) (string, error)
	StartWorkout(ctx context.Context, id string) error
	StopWorkout(ctx context.Context) error
	SkipSegment(ctx context.Context) error
	SetIntensity(ctx context.Context, pct float64) (float64, error)
	SetFTP(ctx context.Context, watts float64) (float64, error)
	EndActivity(ctx context.Context, discard bool) (file string, err error)
	ListResults(ctx context.Context) ([]*pb.RideResult, error)
	SetProfile(ctx context.Context, weightKg, ftpW, heightCm *float64) (*pb.RiderProfile, error)
	ListActivities(ctx context.Context) ([]*pb.Activity, error)
	ExportActivity(ctx context.Context, name string, w io.Writer) (int64, error)
	SetTrainerControl(ctx context.Context, mode pb.ControlMode, v float64) (float64, error)
	ReleaseTrainerControl(ctx context.Context) error
	// SetPaused parks the core or carries on; whether it is paused now.
	SetPaused(ctx context.Context, paused bool) (bool, error)
}

const (
	// CountdownSeconds gives the rider time to get ready after pressing c.
	CountdownSeconds = 10
	// resultShownFor keeps the calibration result on screen.
	resultShownFor = 10 * time.Second
	commandTimeout = 10 * time.Second
)

type countdownMsg struct{ id int }

// commandMsg reports the outcome of a request to the core.
type commandMsg struct {
	what string
	err  error
}

func countdownTick(id int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return countdownMsg{id: id} })
}

func (m Model) command(what string, f func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		return commandMsg{what: what, err: f(ctx)}
	}
}

func (m Model) calibration() *pb.Calibration { return m.st.GetTrainer().GetCalibration() }

func (m Model) calibrationActive() bool {
	switch m.calibration().GetPhase() {
	case pb.CalibrationPhase_CALIBRATION_PHASE_REQUESTED, pb.CalibrationPhase_CALIBRATION_PHASE_IN_PROGRESS:
		return true
	}
	return false
}

// mayCalibrate is when a spin-down can start at all: the trainer is
// connected and neither a calibration nor a ride is running.
func (m Model) mayCalibrate() bool {
	return m.cmds != nil && m.connected &&
		m.st.GetTrainer().GetSensor().GetStatus() == pb.SensorStatus_SENSOR_STATUS_CONNECTED &&
		!m.calibrationActive() && !m.rideActive() && !m.workoutActive() && m.countdown == 0
}

// canCalibrate is when c starts a countdown: the trainer also asks for a
// spin-down. C (shift) starts one regardless, to recalibrate after warming up.
func (m Model) canCalibrate() bool {
	return m.mayCalibrate() && m.st.GetTrainer().GetResistanceCalibrationRequired()
}

// showResult reports whether a recent calibration result should take over
// the screen.
func (m Model) showResult() bool {
	c := m.calibration()
	switch c.GetPhase() {
	case pb.CalibrationPhase_CALIBRATION_PHASE_SUCCEEDED,
		pb.CalibrationPhase_CALIBRATION_PHASE_FAILED,
		pb.CalibrationPhase_CALIBRATION_PHASE_CANCELLED:
		return time.Duration(m.st.GetCoreTimeNs()-c.GetPhaseChangedNs()) < resultShownFor
	}
	return false
}

// calibrationKey handles c and esc. It reports whether it used the key.
func (m Model) calibrationKey(key string) (Model, tea.Cmd, bool) {
	switch {
	case m.countdown > 0 && (key == "esc" || key == "c"):
		m.countdown = 0
		m.countdownID++
		m.notice = "calibration aborted"
		return m, nil, true
	case key == "esc" && m.calibrationActive():
		return m, m.command("cancel calibration", m.cmds.CancelCalibration), true
	case (key == "c" && m.canCalibrate()) || (key == "C" && m.mayCalibrate()):
		m.countdown = CountdownSeconds
		m.countdownID++
		m.notice = ""
		return m, countdownTick(m.countdownID), true
	}
	return m, nil, false
}

func (m Model) onCountdown(msg countdownMsg) (Model, tea.Cmd) {
	if msg.id != m.countdownID || m.countdown == 0 {
		return m, nil // aborted or superseded
	}
	m.countdown--
	if m.countdown > 0 {
		return m, countdownTick(m.countdownID)
	}
	return m, m.command("start calibration", m.cmds.StartCalibration)
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	bigWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	bigOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("114")).Bold(true)
	bigBad     = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

// calibrationPanel is shown instead of the metric tiles during the
// countdown, the calibration and briefly after it.
func (m Model) calibrationPanel(width, height int) string {
	big := height >= BigHeight+8 && width >= 40
	digits := func(s string, style lipgloss.Style) string {
		if big {
			return style.Render(Big(s))
		}
		return style.Render(s)
	}

	c := m.calibration()
	var lines []string
	add := func(s ...string) { lines = append(lines, s...) }
	add(titleStyle.Render("SPIN-DOWN CALIBRATION"), "")

	switch {
	case m.countdown > 0:
		add(digits(fmt.Sprint(m.countdown), bigWarn), "",
			"starting in "+fmt.Sprint(m.countdown)+" s",
			"",
			"When it starts: pedal up until the screen says STOP,",
			"then stop pedalling and let the trainer coast to a stop.",
			"",
			dimStyle.Render("esc to abort"))

	case c.GetPhase() == pb.CalibrationPhase_CALIBRATION_PHASE_REQUESTED:
		add("waiting for the trainer to start…", "", dimStyle.Render("esc to cancel"))

	case c.GetPhase() == pb.CalibrationPhase_CALIBRATION_PHASE_IN_PROGRESS:
		speed := "--"
		if tr := m.st.GetTrainer(); tr.SpeedMps != nil {
			speed = fmt.Sprintf("%.1f", tr.GetSpeedMps()*3.6)
		}
		target := ""
		if c.TargetSpeedMps != nil {
			target = fmt.Sprintf(" to %.0f km/h", c.GetTargetSpeedMps()*3.6)
		}
		switch c.GetSpeedCondition() {
		case pb.CalibrationCondition_CALIBRATION_CONDITION_OK:
			add(digits(speed, bigOK), unitStyle.Render("km/h"), "",
				bigOK.Render("STOP PEDALLING"),
				"let the trainer coast to a stop")
		default:
			add(digits(speed, bigWarn), unitStyle.Render("km/h"), "",
				bigWarn.Render("PEDAL UP"+target))
		}
		if c.GetTemperatureCondition() == pb.CalibrationCondition_CALIBRATION_CONDITION_TOO_LOW {
			add("", badStyle.Render("trainer too cold: ride ~10 minutes to warm up, then retry"))
		}
		if msg := c.GetMessage(); msg != "" {
			add("", warnStyle.Render(msg))
		} else {
			add("", dimStyle.Render("esc to cancel"))
		}

	case c.GetPhase() == pb.CalibrationPhase_CALIBRATION_PHASE_SUCCEEDED:
		result := "the trainer reports no spin-down time"
		if c.SpinDownMs != nil {
			result = fmt.Sprintf("spin-down %d ms", c.GetSpinDownMs())
			if c.TargetSpinDownMs != nil {
				result += fmt.Sprintf(" (target %d ms)", c.GetTargetSpinDownMs())
			}
		}
		add(bigOK.Render("✓ CALIBRATED"), "", result)

	case c.GetPhase() == pb.CalibrationPhase_CALIBRATION_PHASE_FAILED:
		add(bigBad.Render("✕ CALIBRATION FAILED"), "", c.GetMessage(), "", dimStyle.Render("press c to retry"))

	case c.GetPhase() == pb.CalibrationPhase_CALIBRATION_PHASE_CANCELLED:
		add(warnStyle.Render("calibration cancelled"))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center, lines...))
}
