package tui

import (
	"testing"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

func TestOtherHeadGivesTheProfile(t *testing.T) {
	m := calModel(&stubCommands{})
	st := sample()
	st.Profile = &pb.RiderProfile{Missing: []string{"weight_kg", "ftp_w"}, SuggestedFtpW: 200}
	m = update(m, StateMsg{State: st})
	if m.onboarding == nil {
		t.Fatal("no onboarding while the profile is missing")
	}
	st = sample()
	st.Profile = &pb.RiderProfile{WeightKg: 75, FtpW: 200}
	if m = update(m, StateMsg{State: st}); m.onboarding != nil || m.notice != "profile set" {
		t.Errorf("onboarding still open (notice %q) after the profile came from elsewhere", m.notice)
	}
}

func TestOtherHeadEndsTheRide(t *testing.T) {
	m := calModel(&stubCommands{})
	st := sample()
	st.Recording = &pb.Recording{Active: true}
	m, _ = press(update(m, StateMsg{State: st}), "e")
	if m.ending == nil {
		t.Fatal("e asked nothing")
	}
	st = sample()
	st.Recording = &pb.Recording{}
	if m = update(m, StateMsg{State: st}); m.ending != nil {
		t.Error("the end-ride question stayed after the recording ended elsewhere")
	}
}

func TestOtherHeadStartsARideDuringTheCountdown(t *testing.T) {
	cmds := &stubCommands{}
	m, _ := press(calModel(cmds), "c")
	id := m.countdownID
	st := sample()
	st.Ride = &pb.Ride{Phase: pb.RidePhase_RIDE_PHASE_RIDING, CourseId: "oval-400", CourseDistanceM: 400}
	m = update(m, StateMsg{State: st})
	if m.countdown != 0 || m.notice != "calibration aborted: the trainer is in use" {
		t.Fatalf("countdown %d, notice %q after a ride started elsewhere", m.countdown, m.notice)
	}
	if next, cmd := m.Update(countdownMsg{id: id}); next.(Model).countdown != 0 || cmd != nil {
		t.Error("a tick of the aborted countdown went on")
	}
	if cmds.started != 0 {
		t.Error("calibration started")
	}
}
