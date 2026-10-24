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
// it. This is the TUI's form for it: weight, height (optional: it sizes
// the rider's drag), FTP, then maximum heart rate and threshold heart
// rate (optional: the heart rate zones of ride summaries come from either),
// and with both, which the zones come from; each saved through SetProfile
// as it is entered. The menu's Profile opens it later to change any of
// them.

const (
	stepWeight = iota
	stepHeight
	stepFTP
	stepMaxHR
	stepThresholdHR
	stepHRZones // a choice, only with both heart rates set
	numSteps
)

// What heart rate zones come from (RiderProfile.hr_zones).
const (
	hrZonesMax       = "max"
	hrZonesThreshold = "threshold"
)

type onboarding struct {
	step  int
	value string // being typed; on stepHRZones the choice
	edit  bool   // opened with p, rather than because the core asked
	// saved is the profile the core returned for the last step saved:
	// newer than the state stream right after a save.
	saved *pb.RiderProfile
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

// latest is the profile as last known: the one just saved, else the
// state stream's.
func (m Model) latest() *pb.RiderProfile {
	if o := m.onboarding; o != nil && o.saved != nil {
		return o.saved
	}
	return m.profile()
}

// bothHRs reports whether the profile has a maximum and a threshold heart
// rate: then the rider chooses which the zones come from.
func bothHRs(p *pb.RiderProfile) bool { return p.GetMaxHrBpm() > 0 && p.GetThresholdHrBpm() > 0 }

func (m Model) onboardKey(key string) (Model, tea.Cmd, bool) {
	if m.onboarding == nil {
		return m, nil, false
	}
	o := *m.onboarding
	m.onboarding = &o
	if o.step == stepHRZones {
		return m.zonesKey(key)
	}
	switch {
	case key == "esc":
		if o.step > stepWeight && o.edit {
			o.step--
			o.value = m.stepValue(o.step)
			return m, nil, true
		}
		m.onboarding = nil
		if !o.edit {
			m.onboardLater = true
			m.notice = "rides and workouts wait for your profile: m, then p (Profile), when ready"
		}
	case key == "backspace":
		if len(o.value) > 0 {
			o.value = o.value[:len(o.value)-1]
		}
	case key == "enter":
		if o.value == "" { // the optional steps: skip it
			switch o.step {
			case stepHeight:
				o.step, o.value = stepFTP, m.stepValue(stepFTP)
				return m, nil, true
			case stepMaxHR:
				o.step, o.value = stepThresholdHR, m.stepValue(stepThresholdHR)
				return m, nil, true
			case stepThresholdHR:
				return m.afterThreshold(m.latest())
			}
		}
		v, err := strconv.ParseFloat(o.value, 64)
		lo, hi, what := 30.0, 200.0, "weight in kg"
		switch o.step {
		case stepHeight:
			lo, hi, what = 120, 220, "height in cm"
		case stepFTP:
			lo, hi, what = 50, 600, "FTP in watts"
		case stepMaxHR:
			lo, hi, what = 100, 230, "maximum heart rate in bpm"
		case stepThresholdHR:
			lo, hi, what = 80, 220, "threshold heart rate in bpm"
		}
		if err != nil || v < lo || v > hi {
			m.notice = fmt.Sprintf("%s: %.0f to %.0f", what, lo, hi)
			return m, nil, true
		}
		return m, m.saveProfile(o.step, v, ""), true
	case len(key) == 1 && (key[0] >= '0' && key[0] <= '9' || key == ".") && len(o.value) < 5:
		o.value += key
	}
	return m, nil, true
}

// zonesKey is the last step: which the heart rate zones come from, with
// both set. ← → (or m, t) choose, enter saves, esc goes back.
func (m Model) zonesKey(key string) (Model, tea.Cmd, bool) {
	o := m.onboarding
	switch key {
	case "left", "right", "space", " ", "tab":
		o.value = map[bool]string{true: hrZonesThreshold, false: hrZonesMax}[o.value != hrZonesThreshold]
	case "m":
		o.value = hrZonesMax
	case "t":
		o.value = hrZonesThreshold
	case "esc":
		o.step, o.value = stepThresholdHR, m.stepValue(stepThresholdHR)
	case "enter":
		return m, m.saveProfile(stepHRZones, 0, o.value), true
	}
	return m, nil, true
}

// afterThreshold goes on past the threshold step: to the choice of zones
// when both heart rates are set, else the form is done.
func (m Model) afterThreshold(p *pb.RiderProfile) (Model, tea.Cmd, bool) {
	if bothHRs(p) {
		next := *m.onboarding
		next.step, next.value = stepHRZones, stepValueFrom(p, stepHRZones)
		m.onboarding = &next
		return m, nil, true
	}
	m.onboarding, m.notice = nil, "profile saved"
	return m, nil, true
}

func (m Model) saveProfile(step int, v float64, choice string) tea.Cmd {
	cmds := m.cmds
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		var weight, ftp, height, maxHR, thresholdHR *float64
		var zones *string
		switch step {
		case stepWeight:
			weight = &v
		case stepHeight:
			height = &v
		case stepMaxHR:
			maxHR = &v
		case stepThresholdHR:
			thresholdHR = &v
		case stepHRZones:
			zones = &choice
		default:
			ftp = &v
		}
		p, err := cmds.SetProfile(ctx, weight, ftp, height, maxHR, thresholdHR, zones)
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
	if msg.step == stepThresholdHR {
		next := *o
		next.saved = msg.profile
		m.onboarding = &next
		m, _, _ = m.afterThreshold(msg.profile)
		return m, nil
	}
	if msg.step < stepThresholdHR {
		next := *o
		next.step, next.saved = msg.step+1, msg.profile
		next.value = stepValueFrom(msg.profile, next.step)
		m.onboarding = &next
		if next.step == stepMaxHR {
			return m, m.fetchActivities() // for the highest heart rates held in them
		}
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
	switch o.step {
	case stepWeight:
		prompt, unit, forced = "Your weight", "kg", p.GetWeightForced()
		why = "It sets how climbs feel on the trainer and your speed on a course."
	case stepHeight:
		prompt, unit, forced = "Your height", "cm", p.GetHeightForced()
		why = "Optional: with your weight it sizes how much air you push on a course.\nLeave it empty to skip; riders are then sized as 1.80 m, 75 kg."
		if cda := p.GetCda(); cda > 0 {
			why += fmt.Sprintf("\nDrag area now: %.3f m².", cda)
		}
		if p.GetCdaForced() {
			why += "\nThe core runs with -cda: a fixed drag area this run."
		}
	case stepMaxHR:
		prompt, unit = "Your maximum heart rate", "bpm"
		why = "Optional: the heart rate zones of your ride summaries can come from it.\nThe highest you have seen in an all-out effort; leave it empty to skip."
		if hr := heldMaxHR(m.activities); hr > 0 {
			why += fmt.Sprintf("\nThe highest you have held for 10 s in your rides: %d bpm, so your maximum is at least this.", hr)
		}
		why += "\nZones describe your rides; they are not medical advice."
	case stepThresholdHR:
		prompt, unit = "Your threshold heart rate", "bpm"
		why = "Optional: what you can hold for about an hour; the zones can come from it\ninstead (Friel's), for riders who don't know their maximum. Leave it empty to skip."
		if hr := heldThresholdHR(m.activities); hr > 0 {
			why += fmt.Sprintf("\nFrom your rides: 95 %% of your highest 20-minute average is %d bpm.\nAfter 20 minutes all-out that is your threshold; otherwise it is at least this.", hr)
		}
		why += "\nZones describe your rides; they are not medical advice."
	case stepHRZones:
		prompt = "Heart rate zones from"
		why = fmt.Sprintf("Your maximum (%.0f bpm) or your threshold (%.0f bpm): which the zones of\nyour ride summaries are taken from.", m.latest().GetMaxHrBpm(), m.latest().GetThresholdHrBpm())
	default:
		prompt, unit, forced = "Your FTP", "W", p.GetFtpForced()
		why = fmt.Sprintf("It sets the targets of ERG workouts. Not sure? %.0f W is 2.5 W/kg,\na fair start; change it any time (f, or p here).", p.GetSuggestedFtpW())
	}
	entry := typingStyle.Render(o.value+"▏") + " " + unit
	if o.step == stepHRZones {
		entry = choiceLine(o.value)
	}
	lines := []string{titleStyle.Render(title), "",
		fmt.Sprintf("%d/%d  %s: ", o.step+1, numSteps, prompt) + entry, "",
		dimStyle.Render(why)}
	if forced {
		lines = append(lines, "", warnStyle.Render("a flag on the core sets this for the current run: a change here lasts until the core restarts"))
	}
	hint := "enter next · esc later"
	switch {
	case o.step == stepHRZones:
		hint = "← → choose · enter save · esc back"
	case o.step >= stepMaxHR && o.edit:
		hint = "enter next (empty: skip) · esc back"
	case o.step >= stepMaxHR:
		hint = "enter next (empty: skip) · esc later"
	case o.edit:
		hint = "enter next · esc cancel"
	}
	if path := p.GetPath(); path != "" {
		lines = append(lines, "", dimStyle.Render("saved on the core in "+path))
	}
	lines = append(lines, "", dimStyle.Render(hint))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// choiceLine shows the zones' choice, the one chosen highlighted.
func choiceLine(v string) string {
	mx, th := dimStyle.Render("maximum"), dimStyle.Render("threshold")
	if v == hrZonesThreshold {
		th = typingStyle.Render("[threshold]")
	} else {
		mx = typingStyle.Render("[maximum]")
	}
	return mx + "  " + th
}

// stepValue is what a step starts with: the value in the profile (the
// one just saved, when there is one), or for the FTP the core's
// suggestion; empty for an unknown height or heart rate.
func (m Model) stepValue(step int) string { return stepValueFrom(m.latest(), step) }

func stepValueFrom(p *pb.RiderProfile, step int) string {
	var v float64
	switch step {
	case stepWeight:
		v = p.GetWeightKg()
	case stepHeight:
		v = p.GetHeightCm()
	case stepFTP:
		if v = p.GetFtpW(); v <= 0 {
			v = p.GetSuggestedFtpW()
		}
	case stepMaxHR:
		v = p.GetMaxHrBpm()
	case stepThresholdHR:
		v = p.GetThresholdHrBpm()
	case stepHRZones:
		if p.GetHrZones() == hrZonesThreshold {
			return hrZonesThreshold
		}
		return hrZonesMax
	}
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f", v)
}

// heldMaxHR is the highest heart rate held for 10 s over the recordings
// (0: none).
func heldMaxHR(as []*pb.Activity) uint32 {
	var best uint32
	for _, a := range as {
		best = max(best, a.GetHeldMaxHeartRateBpm())
	}
	return best
}

// heldThresholdHR is the highest threshold suggestion over the
// recordings: 95 % of a 20-minute average (0: none).
func heldThresholdHR(as []*pb.Activity) uint32 {
	var best uint32
	for _, a := range as {
		best = max(best, a.GetHeldThresholdHeartRateBpm())
	}
	return best
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
