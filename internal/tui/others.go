package tui

// Two heads on one core (owner, 2026-10-09: the TUI and the renderer can
// both be connected to the core and share its state): rides, workouts,
// the profile and trainer control live in the core, so either head starts
// or ends what the other shows. What a head keeps for itself while the
// rider is half way through it can go stale; others closes it.

// others closes what another head made stale: the profile asked for and
// then given there, the end-ride question once the recording ended or a
// ride began, the countdown to a spin-down once something else drives the
// trainer. (The menu already gives way to a ride or workout under way.)
func (m Model) others() Model {
	if o := m.onboarding; o != nil && !o.edit && m.profile() != nil && len(m.profile().GetMissing()) == 0 {
		m.onboarding, m.notice = nil, "profile set"
	}
	if m.ending != nil && (!m.st.GetRecording().GetActive() || m.rideActive() || m.workoutActive()) {
		m.ending, m.notice = nil, "the ride was ended, or one started, on another screen"
	}
	if m.countdown > 0 && (m.rideActive() || m.workoutActive() || m.controlActive() || m.calibrationActive()) {
		m.countdown = 0
		m.countdownID++ // its ticks find it superseded
		m.notice = "calibration aborted: the trainer is in use"
	}
	return m
}
