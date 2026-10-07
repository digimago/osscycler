package replay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/record"
)

// testdata holds a ride recorded by the real core (-fake rider, Demo Hills
// from 1.48 km with a rolling start): the FIT activity and results.jsonl,
// written through the API, the recorder and the FIT encoder.
func demoHills(t *testing.T) *course.Course {
	t.Helper()
	f, err := os.Open("../demo/courses/demo-hills.gpx")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := course.ParseGPX("demo-hills", f)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReplayRecordedRide(t *testing.T) {
	rides, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(rides) != 1 || rides[0].Err != nil {
		t.Fatalf("rides %+v", rides)
	}
	r := rides[0]
	if r.CourseID != "demo-hills" || r.StartM != 1480 || len(r.Power) < 140 {
		t.Fatalf("ride %s from %.0f m, %d power samples", r.CourseID, r.StartM, len(r.Power))
	}

	got := r.Replay(demoHills(t), r.Params())
	if !got.Finished {
		t.Fatalf("replay didn't finish: %+v", got)
	}
	// The live ride ticked on wall-clock time with power at 4 Hz; the
	// replay has the 1 Hz records and starts at the lap's first one.
	recorded := time.Duration(r.ElapsedS * float64(time.Second))
	if d := (got.Elapsed - recorded).Abs(); d > 2*time.Second {
		t.Errorf("replayed %v, recorded %v (%v apart)", got.Elapsed, recorded, d)
	}
	if d := got.AvgPowerW - r.AvgPowerW; d > 5 || d < -5 {
		t.Errorf("average power %.0f W replayed, %.0f W recorded", got.AvgPowerW, r.AvgPowerW)
	}
	t.Logf("recorded %v, replayed %v", recorded.Round(100*time.Millisecond), got.Elapsed.Round(100*time.Millisecond))

	// What-if: a heavier rider on the same power is slower over the crest.
	p := r.Params()
	p.MassKg += 10
	if heavy := r.Replay(demoHills(t), p); heavy.Elapsed <= got.Elapsed {
		t.Errorf("10 kg more: %v, as recorded: %v", heavy.Elapsed, got.Elapsed)
	}
}

func TestLoadReportsBrokenRides(t *testing.T) {
	dir := t.TempDir()
	src, _ := os.ReadFile(filepath.Join("testdata", record.ResultsFile))
	lines := string(src) +
		`{"course_id":"gone","file":"missing.fit","lap":0}` + "\n" +
		`{"course_id":"old"}` + "\n"
	os.WriteFile(filepath.Join(dir, record.ResultsFile), []byte(lines), 0o600)
	fits, _ := filepath.Glob("testdata/*.fit")
	for _, f := range fits {
		b, _ := os.ReadFile(f)
		os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o600)
	}
	rides, err := Load(dir)
	if err != nil || len(rides) != 3 {
		t.Fatalf("%d rides, %v", len(rides), err)
	}
	if rides[0].Err != nil || rides[1].Err == nil || rides[2].Err == nil {
		t.Errorf("errors: %v / %v / %v", rides[0].Err, rides[1].Err, rides[2].Err)
	}
	if _, err := LapPower(filepath.Join(dir, rides[0].File), 9); err == nil || !strings.Contains(err.Error(), "no lap 9") {
		t.Errorf("missing lap: %v", err)
	}
	if p := rides[2].Params(); p.MassKg != 84 {
		t.Errorf("default params for an old result: %+v", p)
	}
}
