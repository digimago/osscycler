package history

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/ride"
)

var t0 = time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)

func write(t *testing.T, rs ...record.Result) string {
	dir := t.TempDir()
	var b []byte
	for _, r := range rs {
		line, _ := json.Marshal(r)
		b = append(append(b, line...), '\n')
	}
	b = append(b, `{"course_id": "half-writ`...) // being appended right now
	os.WriteFile(filepath.Join(dir, record.ResultsFile), b, 0o600)
	return dir
}

func res(day int, course string, startM, distM, secs float64) record.Result {
	return record.Result{Finished: t0.AddDate(0, 0, day), CourseID: course, StartM: startM, DistanceM: distM, ElapsedS: secs}
}

func TestResultsAndBests(t *testing.T) {
	s := &Store{Dir: write(t,
		res(0, "mash", 0, 10000, 1500),
		res(1, "mash", 0, 10000, 1450),   // PB on the full course
		res(2, "mash", 1480, 8520, 1200), // another stretch: its own PB
		res(3, "mash", 0, 10000, 1460),   // slower
		res(4, "mash", 0, 12000, 1300),   // course file changed: not comparable
		res(5, "herpel", 0, 30000, 3600), // another course
		res(6, "mash", 0, 10000, 1450.0), // ties the PB, later: not a PB
	)}
	es, err := s.Results()
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 7 {
		t.Fatalf("%d results (the half-written line must be skipped)", len(es))
	}
	if !es[0].Finished.Equal(t0.AddDate(0, 0, 6)) {
		t.Error("not newest first")
	}
	var pbs []int
	for i := len(es) - 1; i >= 0; i-- { // back to oldest first, by day
		if es[i].PB {
			pbs = append(pbs, int(es[i].Finished.Sub(t0).Hours()/24))
		}
	}
	if want := []int{1, 2, 4, 5}; len(pbs) != len(want) || pbs[0] != 1 || pbs[1] != 2 || pbs[2] != 4 || pbs[3] != 5 {
		t.Errorf("PBs on days %v, want %v", pbs, want)
	}

	if b, ok := s.Best("mash", 0, 10000); !ok || b.ElapsedS != 1450 || !b.Finished.Equal(t0.AddDate(0, 0, 1)) {
		t.Errorf("best on mash: %+v, %v", b, ok)
	}
	if b, ok := s.Best("mash", 1480.4, 8520); !ok || b.ElapsedS != 1200 {
		t.Errorf("best from 1.48 km: %+v, %v", b, ok)
	}
	if _, ok := s.Best("mash", 500, 9500); ok {
		t.Error("a PB for a stretch never ridden")
	}
}

func TestNoHistory(t *testing.T) {
	es, err := (&Store{Dir: t.TempDir()}).Results()
	if err != nil || len(es) != 0 {
		t.Errorf("empty dir: %v, %v", es, err)
	}
}

func TestGhostFromRecording(t *testing.T) {
	// The ride recorded by the real core in replay/testdata: Demo Hills
	// from 1.48 km.
	dir := t.TempDir()
	for _, f := range []string{record.ResultsFile, "2026-10-07-082817.fit"} {
		b, err := os.ReadFile(filepath.Join("../replay/testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, f), b, 0o600)
	}
	gpx, err := os.Open("../../demo/courses/demo-hills.gpx")
	if err != nil {
		t.Fatal(err)
	}
	defer gpx.Close()
	c, err := course.ParseGPX("demo-hills", gpx)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{Dir: dir}

	g, err := s.Ghost(c, 1480)
	if err != nil || g == nil {
		t.Fatalf("ghost: %v, %v", g, err)
	}
	if g.Elapsed != time.Duration(153.622768181*float64(time.Second)) {
		t.Errorf("ghost time %v, want the recorded 2:33.6", g.Elapsed)
	}
	last := g.Trace[len(g.Trace)-1]
	if last.At != g.Elapsed || math.Abs(last.DistanceM-c.Distance) > 0.01 || g.Trace[0].DistanceM != 1480 {
		t.Errorf("trace from %+v to %+v", g.Trace[0], last)
	}
	if !strings.HasPrefix(g.Label, "PB ") {
		t.Errorf("label %q", g.Label)
	}

	// No PB from the start of the course: no ghost, no error.
	if g, err := s.Ghost(c, 0); g != nil || err != nil {
		t.Errorf("full course: %v, %v", g, err)
	}

	// The same ride picked by when it finished: from where it started.
	es, _ := s.Results()
	race, startM, err := s.Race(c, time.UnixMilli(es[0].Finished.UnixMilli()))
	if err != nil || startM != 1480 || race.Elapsed != g.Elapsed {
		t.Errorf("race: start %.0f, %v, %v; want from 1480 m in %v", startM, race, err, g.Elapsed)
	}
	if _, _, err := s.Race(c, es[0].Finished.Add(time.Hour)); err != ride.ErrUnknownRide {
		t.Errorf("unknown ride: %v", err)
	}
	// A course edited since (here: a different ID's results don't apply,
	// and a shorter course doesn't match the stretch).
	short, _ := course.New("demo-hills", "Shorter", shorten(t, c, 2500))
	if _, _, err := s.Race(short, es[0].Finished); err != ride.ErrCourseChanged {
		t.Errorf("changed course: %v", err)
	}
}

// shorten returns c's track up to distance d as points.
func shorten(t *testing.T, c *course.Course, d float64) []course.Point {
	t.Helper()
	var pts []course.Point
	for x := 0.0; x <= d; x += 10 {
		lat, lon := c.Position(x)
		ele, _ := c.At(x)
		pts = append(pts, course.Point{Lat: lat, Lon: lon, Ele: ele})
	}
	return pts
}
