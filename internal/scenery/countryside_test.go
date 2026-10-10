package scenery

import (
	"reflect"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func TestCountryside(t *testing.T) {
	c := course.Included()[len(course.Included())-1] // a real route, 32 km
	cs := Countryside(c)
	if again := Countryside(c); !reflect.DeepEqual(again.Data, cs.Data) {
		t.Error("not the same twice")
	}
	if len(Footprints(c, cs.Data)) == 0 {
		t.Error("no farmsteads")
	}
	lm := NewLandMap(c, cs.Data)
	count := map[Land]int{}
	n := 0
	for d := 0.0; d < c.Distance; d += 25 {
		lat, lon := c.Position(d)
		e, nn := c.Project(lat, lon)
		for _, off := range []float64{-40, 40, -250, 250} {
			l, _ := lm.Area(e+off, nn)
			count[l]++
			n++
		}
	}
	for _, l := range []Land{LandFarmland, LandMeadow, LandForest} {
		if count[l] < n/20 {
			t.Errorf("%v at %d of %d points beside the road, want more", l, count[l], n)
		}
	}
	if count[LandNone] > n/20 {
		t.Errorf("bare grass at %d of %d points: the corridor isn't covered", count[LandNone], n)
	}
}
