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

// The polder's gates stand at field entrances, set back from the road with
// a strip of bare earth between, not across a ditch's end by the road
// (owner, 2026-10-12). Each strip begins at the road's edge, keeps clear
// of the ditches, has its gate across its far end, and nothing grows on it.
func TestFieldEntrances(t *testing.T) {
	var c *course.Course
	for _, x := range course.Tracks() {
		if x.ID == "figure-8" {
			c = x
		}
	}
	tr := TrackScenery(c)
	g := newTrackGen(c)
	var ditches [][][2]float64
	for _, el := range tr.Data.Elements {
		if el.Tags["water"] == "ditch" {
			var ring [][2]float64
			for _, p := range el.Geometry {
				e, n := c.Project(p.Lat, p.Lon)
				ring = append(ring, [2]float64{e, n})
			}
			ditches = append(ditches, ring)
		}
	}
	if len(tr.Fences) < 10 || len(ditches) == 0 {
		t.Fatalf("%d entrances, %d ditches", len(tr.Fences), len(ditches))
	}
	lawns := map[[2]float64]bool{}
	for _, l := range tr.Lawns {
		lawns[l[0]] = true
	}
	for i, f := range tr.Fences {
		if len(f.Entry) != 4 {
			t.Fatalf("fence %d: no entrance strip", i)
		}
		near := [2]float64{(f.Entry[0][0] + f.Entry[1][0]) / 2, (f.Entry[0][1] + f.Entry[1][1]) / 2}
		far := [2]float64{(f.Entry[2][0] + f.Entry[3][0]) / 2, (f.Entry[2][1] + f.Entry[3][1]) / 2}
		if d := g.dist(near); d > entryEdgeM+0.5 {
			t.Errorf("fence %d: the strip begins %.1f m off the road's line", i, d)
		}
		// The gate across the strip, entryGateM off the road's edge.
		mid := [2]float64{(f.Line[0][0] + f.Line[1][0]) / 2, (f.Line[0][1] + f.Line[1][1]) / 2}
		if d := g.dist(mid); d < entryEdgeM+entryGateM-0.5 || d > entryEdgeM+entryGateM+0.5 {
			t.Errorf("fence %d: the gate %.1f m off the road's line", i, d)
		}
		for _, q := range append([][2]float64{near, far}, f.Entry...) {
			for _, ring := range ditches {
				if inRing(q, ring) {
					t.Errorf("fence %d: its strip runs into a ditch at %.0f, %.0f", i, q[0], q[1])
				}
			}
		}
		if !lawns[f.Entry[0]] {
			t.Errorf("fence %d: plants may grow on its strip", i)
		}
	}
}

func inRing(p [2]float64, ring [][2]float64) bool {
	in := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		a, b := ring[i], ring[j]
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}
