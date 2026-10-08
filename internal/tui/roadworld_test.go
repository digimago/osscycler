package tui

import (
	"testing"

	"github.com/digimago/osscycler/internal/api"
	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
)

// straightScene is 2 km due north, with the given scenery.
func straightScene(t *testing.T, sc *scenery.Scenery) *roadScene {
	t.Helper()
	var pts []course.Point
	for i := 0; i <= 200; i++ {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 10})
	}
	c, err := course.New("n", "N", pts)
	if err != nil {
		t.Fatal(err)
	}
	if sc == nil {
		sc = &scenery.Scenery{Land: make([]scenery.Land, 4*201)}
	}
	return newRoadScene(api.CourseToProto(c, sc))
}

func TestPlaceNameAfterItsSign(t *testing.T) {
	sc := straightScene(t, &scenery.Scenery{Land: make([]scenery.Land, 4*201),
		Signs: []scenery.PlaceSign{{DistanceM: 800, Name: "Dorp"}}})
	for pos, want := range map[float64]string{790: "", 800: "Dorp", 1200: "Dorp", 1400: ""} {
		if got := sc.placeAt(pos); got != want {
			t.Errorf("at %v m: %q, want %q", pos, got, want)
		}
	}
}

func TestHumanTouchesStayOffTheRoad(t *testing.T) {
	plain := straightScene(t, nil)
	busy := straightScene(t, &scenery.Scenery{Land: make([]scenery.Land, 4*201),
		Junctions: []scenery.Junction{{DistanceM: 540, Kind: scenery.JunctionCrossing,
			Branches: []scenery.Branch{{BearingDeg: 90, WidthM: 7, LengthM: 80}, {BearingDeg: 270, WidthM: 7, LengthM: 80}}}},
		Parking: []scenery.Parking{{DistanceM: 560, OffsetM: 25, LengthM: 60, DepthM: 40}},
		Signs:   []scenery.PlaceSign{{DistanceM: 530, Name: "Dorp"}}})
	const w, h = 120, 60
	a, b := plain.pixels(500, -1, w, h), busy.pixels(500, -1, w, h)
	changed := 0
	for i := range a {
		if a[i] != b[i] {
			changed++
		}
	}
	if changed < 200 {
		t.Errorf("the crossing, car park and sign changed %d pixels", changed)
	}
	// The road itself stays as it was: rows at the bottom of the view are
	// all road in the middle.
	for y := h - 6; y < h; y++ {
		for x := w/2 - 3; x <= w/2+3; x++ {
			if a[y*w+x] != b[y*w+x] {
				t.Fatalf("road pixel %d,%d changed", x, y)
			}
		}
	}
}
