package api

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestHeadsAreCounted(t *testing.T) {
	hub := telemetry.NewHub()
	c := start(t, hub, nil, testToken)
	heads := func() int { st, _ := hub.Latest(); return st.Heads }
	ctx, cancel := context.WithCancel(context.Background())
	head, err := c.StreamState(ctx, &pb.StreamStateRequest{Head: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := head.Recv(); err != nil {
		t.Fatal(err)
	}
	watcher, _ := c.StreamState(context.Background(), &pb.StreamStateRequest{}) // a script: not a head
	watcher.Recv()
	if heads() != 1 {
		t.Errorf("%d heads, want 1", heads())
	}
	cancel() // the screen quits
	for i := 0; i < 100 && heads() != 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if heads() != 0 {
		t.Errorf("%d heads after the screen went", heads())
	}
}
