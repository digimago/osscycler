package scenery

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

// Farmland is sown as fields (owner, 2026-10-12): each area one of
// ploughed, maize, wheat or potatoes, by its map ID (the same every
// build), all four in a polder; its rows along its longest side; other
// land as it was; arable land still arable.
func TestFieldsAreSown(t *testing.T) {
	var c *course.Course
	for _, x := range course.Tracks() {
		if x.ID == "figure-8" {
			c = x
		}
	}
	tr := TrackScenery(c)
	a, b := NewLandMap(c, tr.Data), NewLandMap(c, tr.Data)
	a.Fields()
	b.Fields()
	seen := map[Land]int{}
	for i, p := range a.polys {
		if p.land != b.polys[i].land || a.RowDir(i) != b.RowDir(i) {
			t.Fatalf("area %d sown differently in two builds", i)
		}
		if p.land == LandFarmland {
			t.Errorf("area %d left as farmland", i)
		}
		seen[p.land]++
		if p.land >= LandPloughed && p.land <= LandPotatoes {
			// The polder's parcels run north and south: rows at 90°.
			if d := a.RowDir(i); math.Abs(d-math.Pi/2) > 0.05 {
				t.Errorf("area %d's rows at %.0f°, want 90 (along the parcel)", i, d*180/math.Pi)
			}
			if !Arable(p.land) {
				t.Errorf("%v isn't arable", p.land)
			}
		}
	}
	for _, l := range []Land{LandPloughed, LandMaize, LandWheat, LandPotatoes, LandMeadow, LandForest} {
		if seen[l] == 0 {
			t.Errorf("no %v in the figure 8's surroundings: %v", l, seen)
		}
	}
	if Arable(LandMeadow) || !Arable(LandFarmland) {
		t.Error("Arable: meadow is grass, farmland arable")
	}
	var none *LandMap
	none.Fields() // no map data: nothing to sow
}
