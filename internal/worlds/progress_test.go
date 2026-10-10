package worlds

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// A build says how far it is and roughly how long it has left: later
// phases from what is known ahead (stretches to fetch, km to build at this
// machine's learned rate), the build from its own rate once it has one.
func TestProgressEstimates(t *testing.T) {
	var got []Progress
	tr := &tracker{report: func(p Progress) { got = append(got, p) }, km: 40, buildSPerKm: 2, fetchS: 15, missing: 4}
	tr.start(PhaseMaps)
	if p := got[len(got)-1]; p.LeftS != 4*15+40*2+checkS {
		t.Errorf("map data, 4 stretches to fetch: %.0f s left, want %.0f", p.LeftS, 4*15+40*2+checkS)
	}
	tr.start(PhaseBuild)
	tr.step(0, 0, 0.1)
	if p := got[len(got)-1]; math.Abs(p.LeftS-(40*2*0.9+checkS)) > 1 {
		t.Errorf("building, 10%%: %.0f s left", p.LeftS)
	}
	if s := FormatProgress(Progress{Phase: PhaseMaps, Done: 3, Total: 8, Frac: 0.375, LeftS: 245}); !strings.HasPrefix(s, ProgressPrefix) ||
		!strings.Contains(s, `"done":3`) || !strings.Contains(s, `"left_s":245`) {
		t.Errorf("printed as %s", s)
	}
}

// The build rate is learned from builds and kept between them.
func TestTimingLearns(t *testing.T) {
	tm := &Timing{File: filepath.Join(t.TempDir(), ".timing.json")}
	if r := tm.perKm(); r != defaultBuildSKm {
		t.Errorf("before any build: %.2f s/km", r)
	}
	tm.learn(0.8)
	if r := tm.perKm(); r != 0.8 {
		t.Errorf("after one build of 0.8 s/km: %.2f", r)
	}
	tm.learn(1.2)
	if r := tm.perKm(); math.Abs(r-1.0) > 1e-9 {
		t.Errorf("after 0.8 and 1.2 s/km: %.2f", r)
	}
}
