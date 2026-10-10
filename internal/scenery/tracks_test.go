package scenery

import (
	"reflect"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func TestTrackScenery(t *testing.T) {
	for _, c := range course.Tracks() {
		tr := TrackScenery(c)
		if tr == nil || len(tr.Data.Elements) == 0 {
			t.Fatalf("%s: no scenery", c.ID)
		}
		if again := TrackScenery(c); !reflect.DeepEqual(again.Data, tr.Data) {
			t.Errorf("%s: not the same twice", c.ID)
		}
		fps := Footprints(c, tr.Data)
		if len(fps) == 0 {
			t.Errorf("%s: no buildings", c.ID)
		}
		lm := NewLandMap(c, tr.Data)
		seen := map[Land]bool{}
		for d := 0.0; d < c.Distance; d += 25 {
			lat, lon := c.Position(d)
			e, n := c.Project(lat, lon)
			for _, off := range []float64{-40, 40} {
				l, _ := lm.Area(e+off, n)
				seen[l] = true
			}
		}
		switch c.ID {
		case "figure-8":
			for _, l := range []Land{LandFarmland, LandForest, LandHeath, LandMeadow} {
				if !seen[l] {
					t.Errorf("figure 8: no %v beside the road", l)
				}
			}
		case "oval-400":
			if len(tr.Lawns) == 0 || len(tr.Roads) != 1 || tr.Roads[0].Surface != SurfaceTrack {
				t.Errorf("oval: lawns %d, roads %+v", len(tr.Lawns), tr.Roads)
			}
		}
	}
	if TrackScenery(course.Included()[len(course.Included())-1]) != nil {
		t.Error("a real course got made-up scenery")
	}
}
