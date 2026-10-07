package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// Onboarding is the stack's: the core says what the rider profile lacks
// (RiderProfile.missing) and holds rides and workouts back until it has
// it. This is the TUI's form for it: weight, then FTP, each saved through
// SetProfile as it is entered. p opens it later to change either.

const (
	stepWeight = iota
	stepFTP
)

type onboarding struct {
	step  int
	value string // being typed
	edit  bool   // opened with p, rather than because the core asked
}

type profileSavedMsg struct {
	profile *pb.RiderProfile
	step    int // the step that was saved
	err     error
}

func (m Model) profile() *pb.RiderProfile { return m.st.GetProfile() }

// needsOnboarding reports whether to ask now: the core lacks something,
// the rider hasn't put it off this session, and nothing else is on.
func (m Model) needsOnboarding() bool {
	p := m.profile()
	return m.cmds != nil && p != nil && len(p.GetMissing()) > 0 && !m.onboardLater && m.onboarding == nil &&
		!m.rideActive() && !m.workoutActive() && !m.picking && m.draft == nil && m.input == nil && m.ending == nil
}

func (m Model) startOnboarding(edit bool) Model {
	o := &onboarding{edit: edit}
	p := m.profile()
	if w := p.GetWeightKg(); w > 0 {
		o.value = fmt.Sprintf("%.0f", w)
		if !edit { // only the FTP is missing
			o.step, o.value = stepFTP, fmt.Sprintf("%.0f", p.GetSuggestedFtpW())
		}
	}
	m.onboarding, m.notice = o, ""
	return m
}

func (m Model) onboardKey(key string) (Model, tea.Cmd, bool) {
	if m.onboarding == nil {
		if key == "p" && m.profile() != nil && !m.rideActive() && !m.workoutActive() && !m.picking && m.draft == nil && m.input == nil {
			return m.startOnboarding(true), nil, true
		}
		return m, nil, false
	}
	o := *m.onboarding
	m.onboarding = &o
	switch {
	case key == "esc":
		if o.step == stepFTP && o.edit {
			o.step, o.value = stepWeight, fmt.Sprintf("%.0f", m.profile().GetWeightKg())
			return m, nil, true
		}
		m.onboarding = nil
		if !o.edit {
			m.onboardLater = true
			m.notice = "rides and workouts wait for your profile: press p when ready"
		}
	case key == "backspace":
		if len(o.value) > 0 {
			o.value = o.value[:len(o.value)-1]
		}
	case key == "enter":
		v, err := strconv.ParseFloat(o.value, 64)
		lo, hi, what := 30.0, 200.0, "weight in kg"
		if o.step == stepFTP {
			lo, hi, what = 50, 600, "FTP in watts"
		}
		if err != nil || v < lo || v > hi {
			m.notice = fmt.Sprintf("%s: %.0f to %.0f", what, lo, hi)
			return m, nil, true
		}
		return m, m.saveProfile(o.step, v), true
	case len(key) == 1 && (key[0] >= '0' && key[0] <= '9' || key == ".") && len(o.value) < 5:
		o.value += key
	}
	return m, nil, true
}

func (m Model) saveProfile(step int, v float64) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		var weight, ftp *float64
		if step == stepWeight {
			weight = &v
		} else {
			ftp = &v
		}
		p, err := cmds.SetProfile(ctx, weight, ftp)
		return profileSavedMsg{profile: p, step: step, err: err}
	}
}

func (m Model) onProfileSaved(msg profileSavedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "saving the profile failed: " + friendlyErr(msg.err)
		return m, nil
	}
	o := m.onboarding
	if o == nil {
		return m, nil
	}
	if msg.step == stepWeight {
		// On to FTP: the one already set, or the core's suggestion.
		next := *o
		next.step = stepFTP
		ftp := msg.profile.GetFtpW()
		if ftp <= 0 {
			ftp = msg.profile.GetSuggestedFtpW()
		}
		next.value = fmt.Sprintf("%.0f", ftp)
		m.onboarding = &next
		return m, nil
	}
	m.onboarding = nil
	m.notice = "profile saved"
	if msg.profile.GetWeightForced() || msg.profile.GetFtpForced() {
		m.notice = "profile set; what flags on the core force applies to this run only"
	}
	return m, nil
}

func (m Model) onboardPanel(width, height int) string {
	o := m.onboarding
	p := m.profile()
	title := "WELCOME TO OSSCYCLER"
	if o.edit {
		title = "RIDER PROFILE"
	}
	var prompt, why, unit string
	forced := false
	if o.step == stepWeight {
		prompt, unit, forced = "Your weight", "kg", p.GetWeightForced()
		why = "It sets how climbs feel on the trainer and your speed on a course."
	} else {
		prompt, unit, forced = "Your FTP", "W", p.GetFtpForced()
		why = fmt.Sprintf("It sets the targets of ERG workouts. Not sure? %.0f W is 2.5 W/kg,\na fair start; change it any time (f, or p here).", p.GetSuggestedFtpW())
	}
	lines := []string{titleStyle.Render(title), "",
		fmt.Sprintf("%d/2  %s: ", o.step+1, prompt) + typingStyle.Render(o.value+"▏") + " " + unit, "",
		dimStyle.Render(why)}
	if forced {
		lines = append(lines, "", warnStyle.Render("a flag on the core sets this for the current run: a change here lasts until the core restarts"))
	}
	hint := "enter next · esc later"
	switch {
	case o.step == stepFTP && o.edit:
		hint = "enter save · esc back"
	case o.step == stepFTP:
		hint = "enter save · esc later"
	case o.edit:
		hint = "enter next · esc cancel"
	}
	if path := p.GetPath(); path != "" {
		lines = append(lines, "", dimStyle.Render("saved on the core in "+path))
	}
	lines = append(lines, "", dimStyle.Render(hint))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// missingText names what the core still needs, for hints.
func missingText(p *pb.RiderProfile) string {
	switch ms := p.GetMissing(); {
	case slices.Contains(ms, "weight_kg"):
		return "your weight"
	case slices.Contains(ms, "ftp_w"):
		return "your FTP"
	}
	return ""
}
