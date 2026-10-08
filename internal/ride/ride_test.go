package ride

import (
	"context"
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

type recordingTrainer struct {
	mu     sync.Mutex
	grades []float64
}

func (r *recordingTrainer) SetGrade(_ context.Context, g float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.grades = append(r.grades, g)
	return nil
}

func (r *recordingTrainer) all() []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]float64(nil), r.grades...)
}

// testCourse is 1 km flat, then 1 km at 8 %, then 1 km at -6 %.
func testCourse(t testing.TB) *course.Course {
	t.Helper()
	const mPerDeg = 6371000 * math.Pi / 180
	var pts []course.Point
	ele := 100.0
	for d := 0.0; d <= 3000; d += 5 {
		pts = append(pts, course.Point{Lat: d / mPerDeg, Ele: ele})
		switch {
		case d >= 2000:
			ele -= 5 * 0.06
		case d >= 1000:
			ele += 5 * 0.08
		}
	}
	c, err := course.New("test", "Test Course", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newSession(t testing.TB) (*Session, *telemetry.Hub, *recordingTrainer) {
	hub := telemetry.NewHub()
	tr := &recordingTrainer{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultConfig(sim.DefaultParams(87, 9))
	cfg.Difficulty = 1 // full grades keep the expectations simple
	s := NewSession(hub, tr, []*course.Course{testCourse(t)}, cfg, log)
	return s, hub, tr
}

func setPower(hub *telemetry.Hub, w uint16) {
	hub.Update(func(s *telemetry.State) bool {
		s.Trainer.PowerW = telemetry.Some(w)
		return true
	})
}

func ride(hub *telemetry.Hub) telemetry.Ride {
	st, _ := hub.Latest()
	return st.Ride
}

func TestStartAndArm(t *testing.T) {
	s, hub, _ := newSession(t)
	if err := s.Start("nope"); err != ErrUnknownCourse {
		t.Fatalf("unknown course: err = %v", err)
	}
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Start("test"); err != ErrRideActive {
		t.Fatalf("second start: err = %v, want ErrRideActive", err)
	}
	r := ride(hub)
	if r.Phase != telemetry.RideArmed || r.CourseName != "Test Course" || math.Abs(r.CourseDistanceM-3000) > 1 {
		t.Fatalf("ride after start: %+v", r)
	}

	// No power: the clock doesn't start.
	t0 := time.Now()
	for i := range 20 {
		s.tick(t0.Add(time.Duration(i) * tickInterval))
	}
	if r := ride(hub); r.Phase != telemetry.RideArmed || r.DistanceM != 0 {
		t.Fatalf("armed ride moved without power: %+v", r)
	}
}

func TestRideToFinish(t *testing.T) {
	s, hub, _ := newSession(t)
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	setPower(hub, 250)
	t0 := time.Now()
	now := t0
	var sawClimb, sawDescent bool
	for i := 0; i < 100000 && ride(hub).Phase != telemetry.RideFinished; i++ {
		now = now.Add(tickInterval)
		s.tick(now)
		r := ride(hub)
		switch {
		case r.DistanceM > 1100 && r.DistanceM < 1900:
			sawClimb = true
			if math.Abs(r.GradePct-8) > 0.5 || r.TrainerGradePct < 7 {
				t.Fatalf("on the climb: grade %.2f, trainer %.2f", r.GradePct, r.TrainerGradePct)
			}
		case r.DistanceM > 2100 && r.DistanceM < 2900:
			sawDescent = true
			if r.GradePct > -5 || math.Abs(r.TrainerGradePct-r.GradePct*sim.DescentFactor) > 0.5 {
				t.Fatalf("on the descent: grade %.2f, trainer %.2f (want half)", r.GradePct, r.TrainerGradePct)
			}
			if r.DistanceM > 2600 && r.SpeedMPS*3.6 < 45 {
				t.Fatalf("descent speed %.1f km/h: gravity should help", r.SpeedMPS*3.6)
			}
		}
	}
	r := ride(hub)
	if r.Phase != telemetry.RideFinished {
		t.Fatalf("never finished: %+v", r)
	}
	if !sawClimb || !sawDescent {
		t.Fatal("ride skipped the climb or the descent")
	}
	if math.Abs(r.DistanceM-r.CourseDistanceM) > 0.01 {
		t.Errorf("finished at %.1f m of %.1f", r.DistanceM, r.CourseDistanceM)
	}
	// The course runs due north from the equator: the finish is 3 km up.
	if north := r.Lat * 6371000 * math.Pi / 180; math.Abs(north-3000) > 1 || r.Lon != 0 {
		t.Errorf("finish position %.6f, %.6f is %.1f m north", r.Lat, r.Lon, north)
	}
	if math.Abs(r.ClimbedM-80) > 2 {
		t.Errorf("climbed %.1f m, want ~80", r.ClimbedM)
	}
	if math.Abs(r.AvgPowerW-250) > 1 {
		t.Errorf("average power %.1f, want 250", r.AvgPowerW)
	}
	// 3 km at 250 W with an 8 % km: a plausible few minutes, and the clock
	// stops at the line rather than at the next tick.
	if r.Elapsed < 4*time.Minute || r.Elapsed > 9*time.Minute {
		t.Errorf("elapsed %v", r.Elapsed)
	}
	if r.Elapsed > now.Sub(t0) {
		t.Errorf("elapsed %v exceeds ticks %v", r.Elapsed, now.Sub(t0))
	}

	// Ticks after the finish change nothing.
	before := ride(hub)
	s.tick(now.Add(time.Second))
	if after := ride(hub); after.Elapsed != before.Elapsed {
		t.Error("clock kept running after the finish")
	}

	// Dismissing the result clears the ride and allows a new one.
	s.Stop()
	if r := ride(hub); r.Phase != telemetry.RideNone {
		t.Fatalf("after dismiss: %+v", r)
	}
	if err := s.Start("test"); err != nil {
		t.Fatalf("new ride after finish: %v", err)
	}
}

func TestAbort(t *testing.T) {
	s, hub, _ := newSession(t)
	s.Start("test")
	setPower(hub, 200)
	now := time.Now()
	for range 40 {
		now = now.Add(tickInterval)
		s.tick(now)
	}
	s.Stop()
	if r := ride(hub); r.Phase != telemetry.RideAborted || r.DistanceM == 0 {
		t.Fatalf("after abort: %+v", r)
	}
	if err := s.Start("test"); err != nil {
		t.Fatalf("start after abort: %v", err)
	}
}

func TestSendsGradesThenHandsTrainerBack(t *testing.T) {
	s, hub, tr := newSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.sendGrades(ctx)

	s.Start("test")
	waitGrades(t, tr, func(g []float64) bool { return len(g) >= 1 })
	if g := tr.all(); g[0] != 0 {
		t.Fatalf("opening grade %v, want 0 (flat start)", g[0])
	}

	// Jump onto the climb.
	s.mu.Lock()
	s.run.rider.DistanceM = 1500
	s.mu.Unlock()
	setPower(hub, 300)
	s.tick(time.Now())
	s.tick(time.Now().Add(tickInterval))
	waitGrades(t, tr, func(g []float64) bool { return g[len(g)-1] > 7 })

	s.Stop()
	waitGrades(t, tr, func(g []float64) bool { return g[len(g)-1] == 0 })
	time.Sleep(3 * tickInterval)
	n := len(tr.all())
	time.Sleep(3 * tickInterval)
	if len(tr.all()) != n {
		t.Error("kept sending grades after the ride ended")
	}
}

func waitGrades(t *testing.T, tr *recordingTrainer, ok func([]float64) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if g := tr.all(); len(g) > 0 && ok(g) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grades sent: %v", tr.all())
}

func TestSetDifficulty(t *testing.T) {
	s, hub, _ := newSession(t)
	if d := ride(hub).DifficultyPct; d != 100 {
		t.Fatalf("initial difficulty %v, want 100 (published before any ride)", d)
	}
	if got := s.SetDifficulty(150); got != 100 {
		t.Errorf("SetDifficulty(150) = %v, want clamped to 100", got)
	}
	if got := s.SetDifficulty(-10); got != 0 {
		t.Errorf("SetDifficulty(-10) = %v, want 0", got)
	}

	// On the 8 % climb, halving the difficulty halves the trainer grade at
	// once, while the course grade (and so the riding time) is unchanged.
	s.SetDifficulty(100)
	s.Start("test")
	s.mu.Lock()
	s.run.rider.DistanceM = 1500
	s.mu.Unlock()
	setPower(hub, 300)
	now := time.Now()
	s.tick(now)
	s.tick(now.Add(tickInterval))
	full := ride(hub)
	s.SetDifficulty(50)
	half := ride(hub)
	if math.Abs(half.TrainerGradePct-full.TrainerGradePct/2) > 0.05 || half.DifficultyPct != 50 {
		t.Errorf("trainer grade %v at 50 %%, was %v at 100 %%", half.TrainerGradePct, full.TrainerGradePct)
	}
	if half.GradePct != full.GradePct {
		t.Error("course grade changed with difficulty")
	}
	// The setting survives the end of the ride.
	s.Stop()
	s.Stop()
	if d := ride(hub).DifficultyPct; d != 50 {
		t.Errorf("difficulty after the ride %v, want 50", d)
	}
}

func TestStartPartway(t *testing.T) {
	hub := telemetry.NewHub()
	cfg := DefaultConfig(sim.DefaultParams(87, 9))
	cfg.Difficulty = 1
	cfg.StartDistanceM = 1950 // 50 m below the crest of the 8 % climb
	s := NewSession(hub, &recordingTrainer{}, []*course.Course{testCourse(t)}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start("test"); err != nil {
		t.Fatal(err)
	}
	r := ride(hub)
	if r.DistanceM != 1950 || r.StartDistanceM != 1950 || r.SpeedMPS <= 0 {
		t.Fatalf("armed partway: %+v", r)
	}
	if r.TrainerGradePct < 7 {
		t.Errorf("opening trainer grade %.2f, want the climb's", r.TrainerGradePct)
	}

	setPower(hub, 200)
	now := time.Now()
	for range 120 { // 30 s
		now = now.Add(tickInterval)
		s.tick(now)
	}
	r = ride(hub)
	if r.DistanceM < 2000 || r.GradePct > -5 {
		t.Errorf("30 s from 50 m below the crest: at %.0f m on %.1f %%", r.DistanceM, r.GradePct)
	}
	if r.ClimbedM > 5 || r.Elapsed > 31*time.Second {
		t.Errorf("climbed %.1f m in %v: should count from the start point", r.ClimbedM, r.Elapsed)
	}

	// An offset beyond the course leaves the last stretch to ride.
	s.Stop()
	s.cfg.StartDistanceM = 1e6
	s.Start("test")
	if r := ride(hub); math.Abs(r.CourseDistanceM-r.DistanceM-minRideM) > 0.01 {
		t.Errorf("clamped start at %.1f of %.1f m", r.DistanceM, r.CourseDistanceM)
	}
}

func TestRefusedDuringManualControl(t *testing.T) {
	s, hub, _ := newSession(t)
	hub.Update(func(st *telemetry.State) bool { st.Control.Mode = telemetry.ControlGrade; return true })
	if err := s.Start("test"); err != ErrRideActive {
		t.Errorf("start during manual control: %v", err)
	}
}
