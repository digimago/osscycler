package api

import (
	"context"
	"errors"
	"testing"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/telemetry"
)

func TestPauseAndCarryOn(t *testing.T) {
	hub := telemetry.NewHub()
	rides := &stubRides{}
	c := startWith(t, hub, nil, rides, testToken)
	ctx := context.Background()

	resp, err := c.SetPaused(ctx, &pb.SetPausedRequest{Paused: true})
	if err != nil || !resp.GetPaused() {
		t.Fatalf("pause: %v, %v", resp, err)
	}
	if st, _ := hub.Latest(); !st.Paused || st.PausedSince.IsZero() {
		t.Errorf("state %v since %v", st.Paused, st.PausedSince)
	}
	if p := ToProto(func() telemetry.State { st, _ := hub.Latest(); return st }()); !p.GetPaused() || p.GetPausedSinceUnixMs() == 0 {
		t.Errorf("proto paused %v since %d", p.GetPaused(), p.GetPausedSinceUnixMs())
	}

	// A start that's refused leaves the pause; one that goes ahead ends it.
	rides.err = errors.New("no")
	c.StartRide(ctx, &pb.StartRideRequest{CourseId: "x"})
	if st, _ := hub.Latest(); !st.Paused {
		t.Error("a refused start ended the pause")
	}
	rides.err = nil
	if _, err := c.StartRide(ctx, &pb.StartRideRequest{CourseId: "x"}); err != nil {
		t.Fatal(err)
	}
	if st, _ := hub.Latest(); st.Paused {
		t.Error("starting a ride didn't carry on")
	}

	c.SetPaused(ctx, &pb.SetPausedRequest{Paused: true})
	if resp, _ := c.SetPaused(ctx, &pb.SetPausedRequest{Paused: false}); resp.GetPaused() {
		t.Error("resume didn't")
	}
}
