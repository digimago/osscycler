package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The ride graph fetches only what it lacks, as the recording runs on,
// draws the whole activity, and t hides it.
func TestRideGraph(t *testing.T) {
	var power, hr []int32
	for i := range 300 {
		p := int32(150)
		if i >= 200 {
			p = 320
		}
		power, hr = append(power, p), append(hr, int32(110+i/5))
	}
	cmds := &stubCommands{history: &pb.GetActivityHistoryResponse{Name: "2026-10-11-100000.fit", PowerW: power, HeartRateBpm: hr}}
	m0, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	st := sample()
	st.Profile = &pb.RiderProfile{Complete: true, WeightKg: 80, FtpW: 250}
	st.Recording = &pb.Recording{Active: true, File: "2026-10-11-100000.fit", TimerS: 300}
	next, _ := m0.Update(StateMsg{State: st})
	m := next.(Model)
	fetch := func(m Model) Model {
		m.fetchingHistory = false
		m, cmd := m.needHistory()
		if cmd == nil {
			return m
		}
		return m.onHistory(cmd().(historyMsg))
	}
	m = fetch(m)
	if len(m.history.power) != 300 || len(cmds.historyCalls) == 0 || cmds.historyCalls[len(cmds.historyCalls)-1] != 0 {
		t.Fatalf("history %d samples, calls %v", len(m.history.power), cmds.historyCalls)
	}
	out := m.render()
	if !strings.Contains(plain(out), "350 W") || !strings.Contains(plain(out), "5:00") || !strings.Contains(out, "▀") {
		t.Errorf("graph lacks its scale (350 W, 5:00) or its bars:\n%s", plain(out))
	}
	// A second on: not worth a fetch; two on: from 300.
	calls := len(cmds.historyCalls)
	st.Recording.TimerS = 301
	m.st = st
	if m = fetch(m); len(cmds.historyCalls) != calls {
		t.Errorf("fetched after 1 s: %v", cmds.historyCalls)
	}
	st.Recording.TimerS = 302
	if m = fetch(m); cmds.historyCalls[len(cmds.historyCalls)-1] != 300 {
		t.Errorf("fetch after 2 s from %v, want 300", cmds.historyCalls)
	}
	// A new activity starts the graph afresh.
	cmds.history.Name = "2026-10-11-120000.fit"
	st.Recording.File = "2026-10-11-120000.fit"
	if m = fetch(m); m.history.name != "2026-10-11-120000.fit" || cmds.historyCalls[len(cmds.historyCalls)-1] != 0 {
		t.Errorf("new activity: %q, calls %v", m.history.name, cmds.historyCalls)
	}
	// t hides it, and it stays hidden in the layout.
	m = pressAll(m, "t")
	if m.layout.Graph != "off" || strings.Contains(plain(m.render()), "350 W") {
		t.Errorf("t: layout %+v\n%s", m.layout, plain(m.render()))
	}
}

// The summary opens from ACTIVITIES (i) and after a ride is saved; it
// tells the rider where heart rate zones come from when the profile has
// no maximum heart rate.
func TestSummary(t *testing.T) {
	sum := &pb.ActivitySummary{Name: "2026-10-10-090000.fit", TimerS: 3600, DistanceM: 30000, AvgPowerW: 200,
		NormalizedPowerW: 215, MaxPowerW: 600, WorkKj: 720, AvgHeartRateBpm: 140, MaxHeartRateBpm: 172, AvgCadenceRpm: 88,
		PowerCurve: []*pb.PowerBest{{DurationS: 5, PowerW: 600}, {DurationS: 60, PowerW: 380}, {DurationS: 1200, PowerW: 240}},
		PowerZoneS: []float64{600, 1800, 600, 300, 200, 100}, PowerZoneUpper: []float64{0.6, 0.76, 0.9, 1.05, 1.19}, FtpW: 250}
	cmds := &stubCommands{summary: sum, activities: []*pb.Activity{{Name: "2026-10-10-090000.fit"}}}
	m0, _ := New("x:1", cmds).Update(tea.WindowSizeMsg{Width: 110, Height: 45})
	next, _ := m0.Update(StateMsg{State: sample()})
	m := next.(Model)
	m.picking, m.tab, m.activities = true, tabActivities, cmds.activities
	m, cmd := press(m, "i")
	m = run(m, cmd)
	out := plain(m.render())
	for _, want := range []string{"RIDE SUMMARY", "1:00:00 riding · 30.00 km", "avg 200 W  NP 215 W  max 600 W", "BEST POWER", "20m",
		"POWER ZONES  FTP 250 W", "Z2 endurance", "50%", "add your maximum or threshold heart rate"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if m = pressAll(m, "x"); m.summary != nil || !m.picking {
		t.Error("x should close the summary, back to the list")
	}
	// With a maximum heart rate: its zones.
	sum.HeartRateZoneS, sum.HeartRateZoneUpper, sum.MaxHrBpm = []float64{0, 600, 1800, 1200, 0}, []float64{0.6, 0.7, 0.8, 0.9}, 190
	m, cmd = press(m, "i")
	m = run(m, cmd)
	if out := plain(m.render()); !strings.Contains(out, "HEART RATE ZONES  max 190 bpm") || !strings.Contains(out, "70-80%") {
		t.Errorf("heart rate zones:\n%s", out)
	}
	// From a threshold heart rate (owner, 2026-10-21): Friel's bounds.
	m = pressAll(m, "x")
	sum.HeartRateZoneUpper, sum.MaxHrBpm, sum.ThresholdHrBpm = []float64{0.81, 0.90, 0.94, 1.00}, 0, 165
	m, cmd = press(m, "i")
	m = run(m, cmd)
	if out := plain(m.render()); !strings.Contains(out, "HEART RATE ZONES  threshold 165 bpm") || !strings.Contains(out, "81-90%") || !strings.Contains(out, ">100%") {
		t.Errorf("threshold heart rate zones:\n%s", out)
	}
	// After a ride is saved.
	m = pressAll(m, "x")
	m.picking = false
	m, _ = m.onEnded(endedMsg{file: "2026-10-10-090000.fit"})
	if m.notice != "ride saved: 2026-10-10-090000.fit" {
		t.Errorf("notice %q", m.notice)
	}
	m = m.onSummary(m.fetchSummary("2026-10-10-090000.fit", false)().(summaryMsg))
	if m.summary == nil || m.picking {
		t.Error("the summary after saving should show, without the list")
	}
}
