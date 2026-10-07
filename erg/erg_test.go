package erg

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digimago/osscycler/telemetry"
	"github.com/digimago/osscycler/workout"
)

type recorder struct {
	mu   sync.Mutex
	cmds []string
}

func (r *recorder) SetTargetPower(_ context.Context, w float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, "erg "+itoa(w))
	return nil
}

func (r *recorder) SetGrade(_ context.Context, g float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cmds = append(r.cmds, "grade "+itoa(g))
	return nil
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.cmds...)
}

func itoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

const text = `name Test ERG
steady 1m 50%
> 5s Hello
2x 30s 100% / 30s 50%
free 30s
`

func setup(t *testing.T) (*Session, *telemetry.Hub, *recorder) {
	t.Helper()
	lib := &workout.Library{Dir: t.TempDir()}
	w, err := workout.ParseText(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Save("test", w); err != nil {
		t.Fatal(err)
	}
	hub := telemetry.NewHub()
	rec := &recorder{}
	s := NewSession(hub, rec, lib, DefaultConfig(200), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return s, hub, rec
}

func power(hub *telemetry.Hub, w uint16) {
	hub.Update(func(s *telemetry.State) bool { s.Trainer.PowerW = telemetry.Some(w); return true })
}

func state(hub *telemetry.Hub) telemetry.Workout {
	st, _ := hub.Latest()
	return st.Workout
}

// ride ticks the session for d of synthetic time.
func ride(s *Session, now *time.Time, d time.Duration) {
	for end := now.Add(d); now.Before(end); {
		*now = now.Add(tickInterval)
		s.tick(*now)
	}
}

func TestWorkoutFlow(t *testing.T) {
	s, hub, _ := setup(t)
	if w := state(hub); w.FTPW != 200 || w.IntensityPct != 100 {
		t.Fatalf("settings not published before a workout: %+v", w)
	}
	if err := s.Start("nope"); err == nil {
		t.Fatal("started an unknown workout")
	}
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	if w := state(hub); w.Phase != telemetry.WorkoutArmed || w.TargetW != 100 || w.Duration != 210*time.Second {
		t.Fatalf("armed: %+v", w)
	}

	now := time.Now()
	ride(s, &now, 5*time.Second) // no power: still armed
	if w := state(hub); w.Phase != telemetry.WorkoutArmed || w.Elapsed != 0 {
		t.Fatalf("clock ran without pedalling: %+v", w)
	}

	power(hub, 100)
	ride(s, &now, 6*time.Second)
	w := state(hub)
	if w.Phase != telemetry.WorkoutRunning || w.Message != "Hello" || w.NextTargetW != 200 || w.NextLabel != "Interval 1/2 on" {
		t.Fatalf("running: %+v", w)
	}

	ride(s, &now, 60*time.Second)
	if w := state(hub); w.SegmentLabel != "Interval 1/2 on" || w.TargetW != 200 {
		t.Fatalf("into the intervals: %+v", w)
	}

	// Intensity scales targets at once; FTP too.
	s.SetIntensity(95)
	if w := state(hub); w.TargetW != 190 || w.IntensityPct != 95 {
		t.Errorf("at 95 %%: %+v", w)
	}
	if got := s.SetIntensity(400); got != MaxIntensity {
		t.Errorf("intensity clamp: %v", got)
	}
	s.SetIntensity(100)
	s.SetFTP(250)
	if w := state(hub); w.TargetW != 250 {
		t.Errorf("FTP 250: target %v", w.TargetW)
	}
	s.SetFTP(200)

	// Skip to the off part.
	s.Skip()
	if w := state(hub); w.SegmentLabel != "Interval 1/2 off" || w.TargetW != 100 {
		t.Errorf("after skip: %+v", w)
	}

	ride(s, &now, 200*time.Second)
	w = state(hub)
	if w.Phase != telemetry.WorkoutFinished || w.Elapsed != w.Duration {
		t.Fatalf("not finished: %+v", w)
	}
	if math.Abs(w.AvgPowerW-100) > 0.5 {
		t.Errorf("average power %v, want 100", w.AvgPowerW)
	}
	s.Stop()
	if w := state(hub); w.Phase != telemetry.WorkoutNone || w.FTPW != 200 {
		t.Errorf("after dismiss: %+v", w)
	}
}

func TestPauseAfterStopping(t *testing.T) {
	s, hub, _ := setup(t)
	s.Start("test")
	now := time.Now()
	power(hub, 150)
	ride(s, &now, 10*time.Second)
	power(hub, 0)
	ride(s, &now, 14*time.Second)
	if w := state(hub); w.Phase != telemetry.WorkoutRunning {
		t.Fatalf("paused before 15 s: %+v", w)
	}
	ride(s, &now, 2*time.Second)
	w := state(hub)
	if w.Phase != telemetry.WorkoutPaused {
		t.Fatalf("not paused after 16 s without power: %+v", w)
	}
	at := w.Elapsed
	ride(s, &now, time.Minute)
	if w := state(hub); w.Elapsed != at {
		t.Fatalf("clock moved while paused: %v → %v", at, w.Elapsed)
	}
	power(hub, 150)
	ride(s, &now, 5*time.Second)
	if w := state(hub); w.Phase != telemetry.WorkoutRunning || w.Elapsed <= at {
		t.Fatalf("did not resume: %+v", w)
	}
}

func TestExclusiveWithRides(t *testing.T) {
	s, hub, _ := setup(t)
	hub.Update(func(st *telemetry.State) bool { st.Ride.Phase = telemetry.RideRiding; return true })
	if err := s.Start("test"); err != ErrActive {
		t.Fatalf("started during a course ride: %v", err)
	}
}

func TestSendsCommands(t *testing.T) {
	s, hub, rec := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.send(ctx)
	s.Start("test")
	wait(t, rec, "erg 100")

	// Jump to the free ride: ERG off, flat grade.
	power(hub, 120)
	now := time.Now()
	s.tick(now)
	for range 5 {
		s.Skip()
	}
	if w := state(hub); !w.Free {
		t.Fatalf("not in the free segment: %+v", w)
	}
	wait(t, rec, "grade 0")
	s.Stop()
	time.Sleep(3 * tickInterval)
	n := len(rec.all())
	time.Sleep(3 * tickInterval)
	if len(rec.all()) != n {
		t.Errorf("kept commanding after the workout: %v", rec.all())
	}
}

func wait(t *testing.T, r *recorder, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c := r.all(); len(c) > 0 && c[len(c)-1] == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("commands %v, want last %q", r.all(), want)
}

func TestRefusedDuringManualControl(t *testing.T) {
	s, hub, _ := setup(t)
	hub.Update(func(st *telemetry.State) bool { st.Control.Mode = telemetry.ControlPower; return true })
	if err := s.Start("test"); err != ErrActive {
		t.Errorf("start during manual control: %v", err)
	}
}
