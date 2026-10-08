package api

import (
	"context"
	"testing"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/scenery"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubScenery struct {
	have    map[string]*scenery.Scenery
	pending map[string]bool
}

func (s stubScenery) Get(id string) *scenery.Scenery { return s.have[id] }
func (s stubScenery) Pending(id string) bool         { return s.pending[id] }

func TestListCoursesSaysMapDataIsComing(t *testing.T) {
	var cs []*course.Course
	for _, id := range []string{"fetching", "fetched", "none"} {
		var pts []course.Point
		for d := 0.0; d <= 500; d += 5 {
			pts = append(pts, course.Point{Lat: d / 111195, Ele: 10})
		}
		c, err := course.New(id, id, pts)
		if err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	sc := stubScenery{
		have: map[string]*scenery.Scenery{"fetched": {Land: make([]scenery.Land, 4*51),
			Junctions: []scenery.Junction{{DistanceM: 100, Kind: scenery.JunctionCrossing, Branches: []scenery.Branch{{BearingDeg: 90, WidthM: 7, LengthM: 80}}}},
			Parking:   []scenery.Parking{{DistanceM: 200, OffsetM: 20, LengthM: 40, DepthM: 30, Name: "Bos"}},
			Signs:     []scenery.PlaceSign{{DistanceM: 300, Name: "Dorp"}}}},
		pending: map[string]bool{"fetching": true},
	}
	srv, err := NewServer(telemetry.NewHub(), Services{Rides: &stubRides{courses: cs}, Scenery: sc}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dialServer(t, srv, testToken).ListCourses(context.Background(), &pb.ListCoursesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range resp.GetCourses() {
		want := c.GetId() == "fetching"
		if c.GetMapDataPending() != want {
			t.Errorf("%s: map_data_pending %v, want %v", c.GetId(), c.GetMapDataPending(), want)
		}
		if (c.GetId() == "fetched") != (c.GetAttribution() != "") {
			t.Errorf("%s: attribution %q", c.GetId(), c.GetAttribution())
		}
		if c.GetId() == "fetched" {
			j, p, sg := c.GetJunctions(), c.GetParking(), c.GetPlaceSigns()
			if len(j) != 1 || j[0].GetKind() != pb.JunctionKind_JUNCTION_KIND_CROSSING || j[0].GetBranches()[0].GetBearingDeg() != 90 ||
				len(p) != 1 || p[0].GetName() != "Bos" || len(sg) != 1 || sg[0].GetName() != "Dorp" {
				t.Errorf("junctions %v, parking %v, signs %v", j, p, sg)
			}
		}
	}
}
