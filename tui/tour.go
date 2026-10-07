package tui

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Tour walks the TUI through its features for a demo, pressing keys on a
// schedule and captioning each step. Any real key press stops it. The
// timings suit the core's -fake rider.
type Tour struct {
	stopped atomic.Bool
	steps   []tourStep
}

type tourStep struct {
	caption string
	keys    []string
	wait    time.Duration // after the keys
}

// tourCaptionMsg sets the banner; tourKeyMsg is a key pressed by the tour.
type (
	tourCaptionMsg string
	tourKeyMsg     string
)

const tourKeyGap = 700 * time.Millisecond

func NewTour() *Tour {
	return &Tour{steps: []tourStep{
		{"Live dashboard: power, heart rate, cadence and speed from the trainer and strap", nil, 7 * time.Second},
		{"The trainer asks for a spin-down: c starts a 10 s countdown, then guides you through it", []string{"c"}, 25 * time.Second},
		{"+ and - set trainer difficulty in 10 % steps, like Zwift's slider", []string{"-", "-", "+"}, 3 * time.Second},
		{"r opens the picker: GPX courses and ERG workouts", []string{"r"}, 3 * time.Second},
		{"Ride a course (the demo joins 1.48 km in, just below a 9 % crest)", []string{"enter"}, 6 * time.Second},
		{"Bottom strip: the next 250 m of grade, blue for easy to deep red for steep", nil, 14 * time.Second},
		{"x twice aborts a ride", []string{"x", "x"}, 5 * time.Second},
		{"The workouts tab: targets relative to FTP, profile in Zwift's zone colours", []string{"r", "tab"}, 4 * time.Second},
		{"Start a workout: the trainer holds each target in ERG", []string{"enter"}, 12 * time.Second},
		{"+ and - adjust workout intensity in 1 % steps", []string{"+", "+", "+"}, 4 * time.Second},
		{"n skips to the next segment", []string{"n"}, 8 * time.Second},
		{"x twice aborts; in the picker, n builds a new workout in a form, or as text in $EDITOR", []string{"x", "x"}, 5 * time.Second},
	}}
}

// Stop ends the tour and reports whether it was still running.
func (t *Tour) Stop() bool { return !t.stopped.Swap(true) }

// Run plays the tour until it ends, is stopped, or ctx is done.
func (t *Tour) Run(ctx context.Context, send func(tea.Msg)) {
	wait := func(d time.Duration) bool {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			if t.stopped.Load() {
				return false
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(100 * time.Millisecond):
			}
		}
		return !t.stopped.Load()
	}
	if !wait(2 * time.Second) { // let the first state arrive
		return
	}
	for i, s := range t.steps {
		send(tourCaptionMsg(fmt.Sprintf("DEMO %d/%d  %s", i+1, len(t.steps), s.caption)))
		for _, k := range s.keys {
			if !wait(tourKeyGap) {
				return
			}
			send(tourKeyMsg(k))
		}
		if !wait(s.wait) {
			return
		}
	}
	if t.Stop() {
		send(tourCaptionMsg("Tour done: the keys are yours (r ride · c calibrate · q quit)"))
	}
}

var tourStyle = lipgloss.NewStyle().Background(lipgloss.Color("#ffcc3f")).Foreground(lipgloss.Color("#000000")).Bold(true)

// WithTour attaches a tour, so a real key press can stop it.
func (m Model) WithTour(t *Tour) Model {
	m.tour = t
	return m
}
