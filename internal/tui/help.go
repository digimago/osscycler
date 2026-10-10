package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// Help: ?, h or F1 shows every key for the screen it is pressed on; any
// key closes it. While a text field takes letters (the workout form),
// only F1 opens it.

type helpEntry struct{ keys, does string }

// helpKey reports whether key opens the help here.
func (m Model) helpKey(key string) bool {
	switch key {
	case "f1":
		return true
	case "?", "h":
		return m.draft == nil || m.draft.buf == nil
	}
	return false
}

// helpContext names the screen and lists its keys.
func (m Model) helpContext() (string, []helpEntry) {
	always := []helpEntry{{"? h F1", "this help (any key closes it)"}, {"ctrl+c", "quit"}}
	layout := []helpEntry{{"o", "arrange the tiles on this screen"}, {"z", "digit size: large or medium (medium fits more tiles)"}}
	switch {
	case m.arranging != nil:
		return "TILES", append([]helpEntry{
			{"↑ ↓", "choose a tile"},
			{"shift + ↑ ↓", "move it up or down: the order on screen"},
			{"1 - 8", "put it in that place; the ones from there move down one"},
			{"space  x", "show or hide it"},
			{"r", "back to the defaults"},
			{"z", "digit size: large or medium"},
			{"enter", "save"},
			{"esc  o", "cancel"},
		}, always...)
	case m.menu != nil:
		return "MENU", append(menuHelp(), always...)
	case m.onboarding != nil:
		return "YOUR PROFILE", append([]helpEntry{
			{"0-9 .", "type the value"}, {"backspace", "delete a digit"},
			{"enter", "save it and go on"}, {"esc", "back, or later (rides and workouts wait for it)"},
		}, always...)
	case m.quitting:
		return "QUIT", append([]helpEntry{
			{"enter  q", "end what is under way, save the ride and quit"},
			{"l", "quit and leave it paused (the core ends it after 30 minutes without a screen)"},
			{"esc", "back to the menu"},
		}, always...)
	case m.ending != nil:
		return "END RIDE", append([]helpEntry{
			{"enter", "save the ride"}, {"d d", "discard it: the file is deleted"}, {"esc", "keep riding"},
		}, always...)
	case m.input != nil:
		return "SET A VALUE", append([]helpEntry{
			{"0-9 . -", "type the value (minus for a descent)"}, {"backspace", "delete"},
			{"enter", "set it"}, {"esc", "cancel"},
		}, always...)
	case m.draft != nil && m.draft.buf != nil:
		return "WORKOUT EDITOR: TYPING", append([]helpEntry{
			{"keys", "type the value"}, {"backspace", "delete"}, {"enter", "keep it"}, {"esc", "cancel"},
		}, always...)
	case m.draft != nil:
		return "WORKOUT EDITOR", append([]helpEntry{
			{"↑ ↓ ← → tab", "move between rows and values"},
			{"+ / -", "step the value under the cursor"},
			{"enter", "type a new value"},
			{"t / T", "change the block type, keeping its values"},
			{"a / c", "add a block / copy this one"},
			{"d", "delete this block"},
			{"J / K", "move this block down / up"},
			{"E", "edit the whole workout as text in $EDITOR"},
			{"s", "save to the library"},
			{"esc esc", "close without saving"},
		}, always...)
	case m.picking && m.tab == tabWorkouts:
		return "WORKOUTS", append([]helpEntry{
			{"↑ ↓  k j", "choose"},
			{"enter", "start the workout; on Fixed power: hold one power (ERG)"},
			{"n", "write a new workout"},
			{"e / E", "edit the workout in the form / as text in $EDITOR"},
			{"f", fmt.Sprintf("set your FTP (now %.0f W): targets are relative to it", m.wk().GetFtpW())},
			{"esc  w", "back"},
		}, always...)
	case m.picking:
		var tab []helpEntry
		switch m.tab {
		case tabCourses:
			tab = []helpEntry{{"enter", "ride the course, racing your best time on it"}}
		case tabHistory:
			tab = []helpEntry{{"enter", "ride that course again, racing that ride (★ marks your best)"}}
		case tabActivities:
			tab = []helpEntry{
				{"enter", "a course ride: race it again; anything else: save the FIT file"},
				{"s", "save the FIT file to " + m.exportDir},
			}
		}
		return "RIDES", append(append([]helpEntry{
			{"tab ← →", "COURSES, HISTORY, ACTIVITIES"},
			{"↑ ↓  k j", "choose"},
		}, tab...), append([]helpEntry{{"esc  r", "back"}}, always...)...)
	case m.countdown > 0 || m.calibrationActive():
		return "CALIBRATION", append([]helpEntry{
			{"esc", "abort the countdown, or cancel the calibration"},
		}, always...)
	case m.showWorkout() && m.workoutActive():
		return "WORKOUT", append([]helpEntry{
			{"+ / -", fmt.Sprintf("intensity in 1 %% steps (now %.0f %%)", m.wk().GetIntensityPct())},
			{"n", "skip to the next part"},
			{"x x", "abort the workout"},
			{"p  space", "pause: the trainer goes flat and the clocks stand (a paused ride never counts as a PB)"},
			{"esc", "pause and open the menu (esc again carries on)"},
			layout[0], layout[1],
			{"q", "the menu (pauses the workout); quit from there"},
		}, always...)
	case m.onLoop():
		return "TRACK", append([]helpEntry{
			{"+ / -", fmt.Sprintf("trainer difficulty in 10 %% steps (now %.0f %%)", m.difficulty())},
			{"v", "road view or big numbers"},
			{"w", "a workout or a fixed power on this track (the track goes on)"},
			{"g / l", "a fixed grade / brake level instead of the track's"},
			layout[0], layout[1],
			{"x x", "end the ride (each lap is kept; your best lap is the ghost)"},
			{"p  space", "pause: the trainer goes flat and the clocks stand (a paused ride never counts as a PB)"},
			{"esc", "pause and open the menu (esc again carries on)"},
			{"q", "the menu (pauses the ride); quit from there"},
		}, always...)
	case m.showRide() && m.rideActive():
		return "COURSE RIDE", append([]helpEntry{
			{"+ / -", fmt.Sprintf("trainer difficulty in 10 %% steps (now %.0f %%)", m.difficulty())},
			{"v", "road view or big numbers"},
			layout[0], layout[1],
			{"x x", "abort the ride (still recorded; only a finished ride counts as a ghost)"},
			{"p  space", "pause: the trainer goes flat and the clocks stand (a paused ride never counts as a PB)"},
			{"esc", "pause and open the menu (esc again carries on)"},
			{"q", "the menu (pauses the ride); quit from there"},
		}, always...)
	case m.showRide() || m.showWorkout():
		return "FINISHED", append([]helpEntry{{"x  enter", "close"}}, always...)
	case m.controlActive():
		return "FIXED POWER, GRADE OR LEVEL", append([]helpEntry{
			{"+ / -", "step the target"},
			{"w", "a new power, or a workout"},
			{"g / l", "a fixed grade / brake level instead"},
			{"x  0", "back to free riding (the trainer is set flat)"},
			{"p  space", "pause: the trainer goes flat until you carry on"},
			{"esc", "pause and open the menu (esc again carries on)"},
			{"q", "the menu (pauses it); quit from there"},
		}, always...)
	}
	return "DASHBOARD", append([]helpEntry{
		{"m", "the menu: everything you can do from here"},
		{"p  space", "pause the recording (and anything you start carries on)"},
		{"r", "rides: courses, your history, recordings"},
		{"w", "workouts, or a fixed power (ERG)"},
		{"g", "ride at a fixed grade"},
		{"l", "ride at a fixed brake level"},
		{"+ / -", "trainer difficulty on courses, in 10 % steps"},
		{"c / C", "spin-down calibration: when the trainer asks / any time"},
		{"e", "end the ride: save or discard the recording"},
		layout[0], layout[1],
		{"q", "the menu; quit from there (the core keeps running)"},
	}, always...)
}

func (m Model) helpPanel(width, height int) string {
	title, entries := m.helpContext()
	keyW := 0
	for _, e := range entries {
		keyW = max(keyW, lipgloss.Width(e.keys))
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220")).Width(keyW + 3)
	lines := []string{titleStyle.Render("KEYS: " + title), ""}
	for _, e := range entries {
		lines = append(lines, keyStyle.Render(e.keys)+truncate(e.does, max(10, width-keyW-8)))
	}
	lines = append(lines, "", dimStyle.Render("any key closes this"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// helpHint is the footer's pointer to the help.
const helpHint = "? help"
