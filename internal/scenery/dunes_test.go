package scenery

import (
	"math"
	"testing"
)

// By the sea, sand, scrub, heath and grassland next to them are dunes;
// inland they stay heath and grassland (owner, 2026-10-10: the Amsterdam
// Water Supply Dunes had grown purple heather and juniper).
func TestDunesOnlyByTheSea(t *testing.T) {
	c := straightCourse(t)
	// The sea lies west: x metres east of the start are 1 km + x from it.
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	dist := func(lat, lon float64) float64 { return 1000 + (lon-lon0)*kx }
	far := DuneM // inland areas start this far east of the coastal ones
	d := &Data{Elements: []Element{
		{Type: "way", ID: 1, Tags: map[string]string{"natural": "sand"}, Geometry: rect(-300, 0, -10, 200)},
		{Type: "way", ID: 2, Tags: map[string]string{"natural": "scrub"}, Geometry: rect(10, 0, 300, 200)},
		{Type: "way", ID: 3, Tags: map[string]string{"natural": "heath"}, Geometry: rect(10, 300, 300, 500)},
		{Type: "way", ID: 4, Tags: map[string]string{"natural": "grassland"}, Geometry: rect(-300, 300, -10, 500)},  // 100 m from the sand
		{Type: "way", ID: 5, Tags: map[string]string{"natural": "grassland"}, Geometry: rect(-300, 800, -10, 1000)}, // 600 m from any dune
		{Type: "way", ID: 6, Tags: map[string]string{"natural": "beach"}, Geometry: rect(-1000, 0, -600, 1000)},
		{Type: "way", ID: 7, Tags: map[string]string{"natural": "sand"}, Geometry: rect(far+10, 0, far+300, 200)},
		{Type: "way", ID: 8, Tags: map[string]string{"natural": "scrub"}, Geometry: rect(far+10, 300, far+300, 500)},
		{Type: "way", ID: 9, Tags: map[string]string{"landuse": "forest"}, Geometry: rect(10, 600, 300, 700)},
	}}
	m := NewLandMap(c, d)
	m.ByTheSea(c, dist)
	for _, tc := range []struct {
		what string
		x, y float64
		want Land
	}{
		{"sand by the sea", -100, 100, LandDuneSand},
		{"scrub by the sea", 100, 100, LandDuneScrub},
		{"heath by the sea", 100, 400, LandDuneGrass},
		{"grassland beside the dunes", -100, 400, LandDuneGrass},
		{"grassland away from them", -100, 900, LandMeadow},
		{"the beach", -800, 500, LandBeach},
		{"drift sand inland", far + 100, 100, LandHeath},
		{"scrub inland", far + 100, 400, LandHeath},
		{"a wood by the sea", 100, 650, LandForest},
	} {
		if got, _ := m.Area(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.what, got, tc.want)
		}
	}

	// Without a coast nothing changes.
	m = NewLandMap(c, d)
	m.ByTheSea(c, nil)
	if got, _ := m.Area(-100, 100); got != LandHeath {
		t.Errorf("no coast known: sand is %d, want heath", got)
	}
}
