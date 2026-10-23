package ride

import (
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

// bothWays is c as loaded made ready to ride both ways, as course.LoadDir
// does: lapped, and its reverse lapped.
func bothWays(t *testing.T, raw *course.Course) (fwd, rev *course.Course) {
	t.Helper()
	fwd, rev = raw.Lapped(), raw.Reversed().Lapped()
	rev.Forward = fwd
	return fwd, rev
}

// square is TestRunInThenLaps's course: a 500 m run-in east, a 2 km
// square lap north of it, back west.
func square(t *testing.T) *course.Course {
	t.Helper()
	m := 1 / 111195.0
	k := m / math.Cos(52*math.Pi/180)
	var pts []course.Point
	add := func(e, n float64) { pts = append(pts, course.Point{Lat: 52 + n*m, Lon: 5 + e*k, Ele: 10}) }
	for _, seg := range [][4]float64{{0, 0, 500, 0}, {500, 0, 500, 500}, {500, 500, 1000, 500}, {1000, 500, 1000, 0}, {1000, 0, 500, 0}, {500, 0, 0, 0}} {
		for i := range 50 {
			f := float64(i) / 50
			add(seg[0]+f*(seg[2]-seg[0]), seg[1]+f*(seg[3]-seg[1]))
		}
	}
	add(0, 0)
	c, err := course.New("square", "Square", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sessionOf(cs ...*course.Course) (*Session, *telemetry.Hub) {
	hub := telemetry.NewHub()
	return NewSession(hub, &recordingTrainer{}, cs, DefaultConfig(sim.DefaultParams(87, 9)), slog.New(slog.NewTextHandler(io.Discard, nil))), hub
}

// metresApart is how far apart two points are.
func metresApart(lat1, lon1, lat2, lon2 float64) float64 {
	return math.Hypot((lat1-lat2)*111195, (lon1-lon2)*111195*math.Cos(lat1*math.Pi/180))
}

// Turning round partway round a lap (owner, 2026-10-20): the ride so far
// counts for nothing, the rider rides on from the same spot the other way,
// untimed until the other way's lap line, which starts a timed lap.
func TestTurnAroundOnTheLap(t *testing.T) {
	fwd, rev := bothWays(t, square(t))
	s, hub := sessionOf(fwd, rev)
	if err := s.Start(fwd.ID); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	at := t0
	for r := ride(hub); r.Lap < 1 || r.LapDistanceM < 700; r = ride(hub) {
		at = pedal(s, hub, at, tickInterval, 300, nil)
	}
	before := ride(hub)
	id, err := s.TurnAround()
	if err != nil || id != rev.ID {
		t.Fatalf("turned round onto %q, %v", id, err)
	}
	r := ride(hub)
	if r.CourseID != rev.ID || !r.Untimed || r.Phase != telemetry.RideRiding || r.Lap != 0 || r.LapDistanceM >= 0 || r.Elapsed != 0 {
		t.Fatalf("after turning round: %+v", r)
	}
	if g := metresApart(before.Lat, before.Lon, r.Lat, r.Lon); g > 1 || r.SpeedMPS != before.SpeedMPS {
		t.Errorf("turned round %.1f m away, speed %.1f m/s from %.1f", g, r.SpeedMPS, before.SpeedMPS)
	}
	// The rest of the lap joined is untimed: no lap ends before the line,
	// and the first lap after it is a whole one.
	var first telemetry.Lap
	toLine := -r.LapDistanceM
	pedal(s, hub, at, 10*time.Minute, 300, func(r telemetry.Ride) {
		if r.Lap == 0 && r.LastLap.N > 0 {
			t.Fatalf("a lap ended before the line: %+v", r.LastLap)
		}
		if first.N == 0 && r.LastLap.N == 1 {
			first = r.LastLap
		}
	})
	if first.N != 1 {
		t.Fatalf("no timed lap in 10 minutes after the line (%.0f m away)", toLine)
	}
	// A whole lap of 2 km at about 40 km/h, flying: about 3 minutes.
	if first.Elapsed < 2*time.Minute || first.Elapsed > 5*time.Minute || first.StartSpeedMPS < 5 {
		t.Errorf("the first timed lap took %v, entered at %.1f m/s", first.Elapsed, first.StartSpeedMPS)
	}
}

// A ride practising a section of a lap (-ride-start-m past the lap's
// line) starts there, untimed until the line, as turning round does; it
// was put back to the lap's start.
func TestStartPartwayRoundALap(t *testing.T) {
	c := square(t).Lapped()
	cfg := DefaultConfig(sim.DefaultParams(87, 9))
	cfg.StartDistanceM = c.Lap.StartM + 1000
	hub := telemetry.NewHub()
	s := NewSession(hub, &recordingTrainer{}, []*course.Course{c}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(c.ID); err != nil {
		t.Fatal(err)
	}
	r := ride(hub)
	if math.Abs(r.DistanceM-cfg.StartDistanceM) > 1 || !r.Untimed || r.Lap != 0 || r.LapDistanceM >= 0 {
		t.Fatalf("starting %.0f m into a lapped course: %+v", cfg.StartDistanceM, r)
	}
	var first telemetry.Lap
	pedal(s, hub, time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), 10*time.Minute, 300, func(r telemetry.Ride) {
		if r.Lap == 0 && r.LastLap.N > 0 {
			t.Fatalf("a lap ended before the line: %+v", r.LastLap)
		}
		if first.N == 0 && r.LastLap.N == 1 {
			first = r.LastLap
		}
	})
	if first.N != 1 || first.Elapsed < 2*time.Minute || first.Elapsed > 5*time.Minute {
		t.Errorf("the first timed lap after the line: %+v", first)
	}
}

// On the run-in out the other way has no way back to the start: turning
// round is refused there (owner, 2026-10-20: ending the ride was a
// surprise), and the ride goes on.
func TestTurnAroundOnTheRunInIsRefused(t *testing.T) {
	fwd, rev := bothWays(t, square(t))
	s, hub := sessionOf(fwd, rev)
	if err := s.Start(fwd.ID); err != nil {
		t.Fatal(err)
	}
	pedal(s, hub, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), 20*time.Second, 300, nil)
	if r := ride(hub); r.Lap != 0 || r.DistanceM < 50 {
		t.Fatalf("not on the run-in: %+v", r)
	}
	if id, err := s.TurnAround(); !errors.Is(err, ErrOnRunIn) || id != "" {
		t.Fatalf("turned round onto %q, %v", id, err)
	}
	if r := ride(hub); r.Phase != telemetry.RideRiding || r.CourseID != fwd.ID || r.Untimed {
		t.Errorf("after turning round on the run-in: phase %v on %s, untimed %v", r.Phase, r.CourseID, r.Untimed)
	}
}

// A test track turns round onto its reverse, the other way round from the
// same spot, untimed until the line.
func TestTurnAroundOnATrack(t *testing.T) {
	oval := course.Tracks()[0]
	rev := oval.Reversed()
	rev.Forward = oval
	s, hub := sessionOf(oval, rev)
	if err := s.Start(oval.ID); err != nil {
		t.Fatal(err)
	}
	at := pedal(s, hub, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), 20*time.Second, 300, nil)
	before := ride(hub)
	if id, err := s.TurnAround(); err != nil || id != rev.ID {
		t.Fatalf("turned round onto %q, %v", id, err)
	}
	r := ride(hub)
	if g := metresApart(before.Lat, before.Lon, r.Lat, r.Lon); g > 1 || !r.Untimed || r.Lap != 0 || r.LapDistanceM >= 0 {
		t.Fatalf("on the reversed oval %.1f m away: %+v", g, r)
	}
	var first telemetry.Lap
	pedal(s, hub, at, 3*time.Minute, 300, func(r telemetry.Ride) {
		if first.N == 0 && r.LastLap.N == 1 {
			first = r.LastLap
		}
	})
	// A whole flying lap of 400 m: about 40 s.
	if first.N != 1 || first.Elapsed < 30*time.Second || first.Elapsed > 60*time.Second {
		t.Errorf("the first timed lap the other way round: %+v", first)
	}
}

// A one-way course turned round finishes at the other way's line, not
// timed; turning round again goes back the first way.
func TestTurnAroundOneWay(t *testing.T) {
	fwd, rev := bothWays(t, testCourse(t))
	s, hub := sessionOf(fwd, rev)
	if err := s.Start(fwd.ID); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for ride(hub).DistanceM < 800 {
		at = pedal(s, hub, at, tickInterval, 300, nil)
	}
	d := ride(hub).DistanceM
	if id, err := s.TurnAround(); err != nil || id != rev.ID {
		t.Fatalf("turned round onto %q, %v", id, err)
	}
	if r := ride(hub); math.Abs(r.DistanceM-(fwd.Distance-d)) > 1e-6 || !r.Untimed || r.StartDistanceM != r.DistanceM {
		t.Fatalf("turned round at %.0f m: %+v", d, r)
	}
	// Twice: back the first way, still untimed.
	if id, err := s.TurnAround(); err != nil || id != fwd.ID {
		t.Fatalf("turned round again onto %q, %v", id, err)
	}
	if r := ride(hub); math.Abs(r.DistanceM-d) > 1e-6 || !r.Untimed {
		t.Fatalf("back the first way: %+v", r)
	}
	if _, err := s.TurnAround(); err != nil {
		t.Fatal(err)
	}
	pedal(s, hub, at, 10*time.Minute, 300, nil)
	if r := ride(hub); r.Phase != telemetry.RideFinished || !r.Untimed || r.CourseID != rev.ID {
		t.Errorf("at the other way's finish: phase %v untimed %v on %s", r.Phase, r.Untimed, r.CourseID)
	}
}

func TestTurnAroundRefused(t *testing.T) {
	oval := course.Tracks()[0]
	fwd, rev := bothWays(t, testCourse(t))
	s, hub := sessionOf(oval, fwd, rev)
	if _, err := s.TurnAround(); !errors.Is(err, ErrNoRide) {
		t.Errorf("no ride: %v", err)
	}
	if err := s.Start(oval.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TurnAround(); !errors.Is(err, ErrNoOtherWay) {
		t.Errorf("on a test track: %v", err)
	}
	if r := ride(hub); r.CourseID != oval.ID || !r.Phase.Active() {
		t.Errorf("the track's ride after a refused turn: %+v", r)
	}
}
