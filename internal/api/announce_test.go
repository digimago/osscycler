package api

import (
	"context"
	"strings"
	"testing"
	"time"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Announce puts a message in the state for every screen, for a while;
// empty text takes it off; too long or too lasting is refused.
func TestAnnounce(t *testing.T) {
	hub := telemetry.NewHub()
	c := startWith(t, hub, nil, &stubRides{}, testToken)
	ctx := context.Background()
	latest := func() *pb.State { st, _ := hub.Latest(); return ToProto(st) }

	if _, err := c.Announce(ctx, &pb.AnnounceRequest{Text: "  GRADE -5 %  ", Seconds: 45}); err != nil {
		t.Fatal(err)
	}
	a := latest().GetAnnouncement()
	if a.GetText() != "GRADE -5 %" {
		t.Errorf("text %q", a.GetText())
	}
	if left := time.Until(time.UnixMilli(a.GetUntilUnixMs())); left < 40*time.Second || left > 46*time.Second {
		t.Errorf("shown for %v more, want about 45 s", left)
	}
	if _, err := c.Announce(ctx, &pb.AnnounceRequest{}); err != nil {
		t.Fatal(err)
	}
	if a := latest().GetAnnouncement(); a != nil {
		t.Errorf("cleared, still %v", a)
	}
	for _, req := range []*pb.AnnounceRequest{{Text: strings.Repeat("x", 201)}, {Text: "x", Seconds: 601}, {Text: "x", Seconds: -1}} {
		if _, err := c.Announce(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%v: %v, want InvalidArgument", req, err)
		}
	}
}
