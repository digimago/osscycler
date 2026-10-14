package erg

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/telemetry"
	"github.com/digimago/osscycler/internal/workout"
)

func cadence(hub *telemetry.Hub, rpm uint8, ok bool) {
	hub.Update(func(s *telemetry.State) bool {
		s.Trainer.CadenceRPM = telemetry.Opt[uint8]{V: rpm, OK: ok}
		return true
	})
}

// The "on" part of an interval with a cadence is checked against it,
// within 5 % either way, over the 5 s the screens' tile shows; the "off"
// part has no target and isn't checked.
func TestCadenceCheckedAgainstTheTarget(t *testing.T) {
	lib := &workout.Library{Dir: t.TempDir()}
	w, err := workout.ParseText(strings.NewReader("name Spin-ups\n2x 60s 100% / 30s 50% @90rpm\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Save("spin", w); err != nil {
		t.Fatal(err)
	}
	hub := telemetry.NewHub()
	s := NewSession(hub, &recorder{}, lib, DefaultConfig(200), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start("spin"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cadence(hub, 80, true)
	ride(s, &now, time.Second)
	if got := state(hub).CadenceCheck; got != telemetry.CadenceNone {
		t.Errorf("armed, not pedalling: %v, want none", got)
	}

	power(hub, 200)
	for _, tc := range []struct {
		rpm  uint8
		want telemetry.CadenceCheck
	}{
		{80, telemetry.CadenceLow},
		{85, telemetry.CadenceLow}, // 85.5 is the edge
		{86, telemetry.CadenceOn},
		{90, telemetry.CadenceOn},
		{94, telemetry.CadenceOn}, // 94.5 is the edge
		{95, telemetry.CadenceHigh},
	} {
		cadence(hub, tc.rpm, true)
		ride(s, &now, cadenceWindow+tickInterval)
		w := state(hub)
		if w.TargetCadence != 90 || w.CadenceCheck != tc.want || w.CadenceAvg != float64(tc.rpm) {
			t.Errorf("at %d rpm: target %d, check %v, avg %v; want 90, %v, %d", tc.rpm, w.TargetCadence, w.CadenceCheck, w.CadenceAvg, tc.want, tc.rpm)
		}
	}

	// Halfway into the window the average is between the two cadences.
	cadence(hub, 100, true)
	ride(s, &now, cadenceWindow/2)
	if w := state(hub); w.CadenceAvg < 96 || w.CadenceAvg > 99 {
		t.Errorf("2.5 s after 95 → 100 rpm the average is %v", w.CadenceAvg)
	}

	cadence(hub, 0, false)
	ride(s, &now, cadenceWindow+tickInterval)
	if w := state(hub); w.CadenceCheck != telemetry.CadenceNone || w.CadenceAvg != 0 {
		t.Errorf("no cadence reading: check %v, avg %v; want none, 0", w.CadenceCheck, w.CadenceAvg)
	}

	// Into the "off" part: no target, nothing checked.
	cadence(hub, 70, true)
	for state(hub).SegmentLabel != "Interval 1/2 off" {
		ride(s, &now, tickInterval)
	}
	ride(s, &now, cadenceWindow+tickInterval)
	if w := state(hub); w.TargetCadence != 0 || w.CadenceCheck != telemetry.CadenceNone || w.CadenceAvg != 70 {
		t.Errorf("off part: target %d, check %v, avg %v; want 0, none, 70", w.TargetCadence, w.CadenceCheck, w.CadenceAvg)
	}
}

func TestCheckCadence(t *testing.T) {
	for _, tc := range []struct {
		rpm    float64
		ok     bool
		target int
		want   telemetry.CadenceCheck
	}{
		{100, true, 100, telemetry.CadenceOn},
		{95, true, 100, telemetry.CadenceOn},
		{94.9, true, 100, telemetry.CadenceLow},
		{105, true, 100, telemetry.CadenceOn},
		{105.1, true, 100, telemetry.CadenceHigh},
		{100, false, 100, telemetry.CadenceNone},
		{100, true, 0, telemetry.CadenceNone},
	} {
		if got := checkCadence(tc.rpm, tc.ok, tc.target); got != tc.want {
			t.Errorf("checkCadence(%v, %v, %d) = %v, want %v", tc.rpm, tc.ok, tc.target, got, tc.want)
		}
	}
}
