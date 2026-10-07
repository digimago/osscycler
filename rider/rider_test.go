package rider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/erg"
	"github.com/digimago/osscycler/fec"
	"github.com/digimago/osscycler/profile"
	"github.com/digimago/osscycler/ride"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
	"github.com/digimago/osscycler/workout"
)

type trainer struct{ users []fec.UserConfig }

func (t *trainer) SetUser(_ context.Context, u fec.UserConfig) error {
	t.users = append(t.users, u)
	return nil
}
func (t *trainer) SetGrade(context.Context, float64) error       { return nil }
func (t *trainer) SetTargetPower(context.Context, float64) error { return nil }

type fixture struct {
	svc  *Service
	hub  *telemetry.Hub
	tr   *trainer
	path string
}

func setup(t *testing.T, force func(*profile.Manager)) fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.json")
	m, err := profile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if force != nil {
		force(m)
	}
	var pts []course.Point
	for d := 0.0; d <= 1000; d += 10 {
		pts = append(pts, course.Point{Lat: d / 111195, Ele: 10})
	}
	c, err := course.New("flat", "Flat", pts)
	if err != nil {
		t.Fatal(err)
	}
	hub, tr := telemetry.NewHub(), &trainer{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rides := ride.NewSession(hub, tr, []*course.Course{c}, ride.DefaultConfig(sim.DefaultParams(75, 9)), log)
	workouts := erg.NewSession(hub, tr, &workout.Library{Dir: t.TempDir()}, erg.DefaultConfig(200), log)
	svc := New(m, hub, tr, rides, workouts, fec.UserConfig{BikeWeightKg: 9, WheelDiameterM: 0.672}, log)
	return fixture{svc, hub, tr, path}
}

func (f fixture) state() telemetry.Profile {
	st, _ := f.hub.Latest()
	return st.Profile
}

func ptr(v float64) *float64 { return &v }

func TestOnboarding(t *testing.T) {
	f := setup(t, nil)
	p := f.state()
	if !p.Known || p.Complete || !p.NeedWeight || !p.NeedFTP || p.SuggestedFTPW != 200 || p.Path != f.path {
		t.Fatalf("new rider: %+v", p)
	}
	// The stack holds rides and workouts back, whatever the renderer.
	if err := f.svc.Rides().Start("flat"); !errors.Is(err, profile.ErrIncomplete) {
		t.Errorf("ride without a weight: %v", err)
	}
	if err := f.svc.Workouts().Start("any"); !errors.Is(err, profile.ErrIncomplete) {
		t.Errorf("workout without an FTP: %v", err)
	}

	// Weight first: rides may start; the trainer has the weight, and the
	// ride is simulated with rider plus bike.
	p, err := f.svc.SetProfile(context.Background(), ptr(87), nil)
	if err != nil || p.NeedWeight || !p.NeedFTP || p.SuggestedFTPW != 220 {
		t.Fatalf("after weight: %+v, %v", p, err)
	}
	if n := len(f.tr.users); n != 1 || f.tr.users[0].UserWeightKg != 87 || f.tr.users[0].BikeWeightKg != 9 {
		t.Errorf("trainer user configs %+v", f.tr.users)
	}
	if err := f.svc.Rides().Start("flat"); err != nil {
		t.Fatalf("ride with a weight: %v", err)
	}
	if st, _ := f.hub.Latest(); st.Ride.Sim.MassKg != 96 {
		t.Errorf("simulated mass %.0f kg, want 87 + 9", st.Ride.Sim.MassKg)
	}

	// Then FTP: complete, and workouts pass the gate (this one isn't in
	// the library).
	p, _ = f.svc.SetProfile(context.Background(), nil, ptr(265))
	if !p.Complete || f.state() != p {
		t.Errorf("after FTP: %+v (published %+v)", p, f.state())
	}
	if err := f.svc.Workouts().Start("any"); errors.Is(err, profile.ErrIncomplete) || err == nil {
		t.Errorf("workout with an FTP: %v", err)
	}
	if st, _ := f.hub.Latest(); st.Workout.FTPW != 265 {
		t.Errorf("ERG FTP %.0f", st.Workout.FTPW)
	}

	// All of it was saved.
	m, _ := profile.Open(f.path)
	if got := m.Get(); got.WeightKg != 87 || got.FTPW != 265 {
		t.Errorf("saved %+v", got)
	}
}

func TestSetProfileChecksFirst(t *testing.T) {
	f := setup(t, nil)
	if _, err := f.svc.SetProfile(context.Background(), ptr(80), ptr(5000)); !errors.Is(err, profile.ErrOutOfRange) {
		t.Fatalf("FTP 5000 W: %v", err)
	}
	if p := f.state(); p.WeightKg != 0 || len(f.tr.users) != 0 {
		t.Errorf("a valid weight was applied with an invalid FTP: %+v", p)
	}
}

func TestLiveChangesAreSavedUnlessForced(t *testing.T) {
	f := setup(t, func(m *profile.Manager) { m.Force(profile.FTP, 300) })
	if p := f.state(); !p.FTPForced || p.FTPW != 300 || !p.NeedWeight {
		t.Fatalf("forced FTP: %+v", p)
	}
	f.svc.Rides().SetDifficulty(70) // saved
	f.svc.Workouts().SetFTP(310)    // forced this run: not saved
	m, _ := profile.Open(f.path)
	if got := m.Get(); got.DifficultyPct != 70 || got.FTPW != 0 {
		t.Errorf("saved %+v: want difficulty 70 and no FTP", got)
	}
	if p := f.state(); p.FTPW != 310 || p.DifficultyPct != 70 {
		t.Errorf("in effect %+v", p)
	}
}

func TestSuggestedFTP(t *testing.T) {
	for kg, want := range map[float64]float64{0: 200, 87: 220, 60: 150, 10: 50} {
		if got := SuggestedFTP(kg); got != want {
			t.Errorf("SuggestedFTP(%v) = %v, want %v", kg, got, want)
		}
	}
}
