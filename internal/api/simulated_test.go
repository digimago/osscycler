package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubSimulator struct{ calls []bool }

func (s *stubSimulator) SetSimulated(on bool) { s.calls = append(s.calls, on) }

func TestSetSimulated(t *testing.T) {
	ctx := context.Background()
	sim := &stubSimulator{}
	srv, err := NewServer(telemetry.NewHub(), Services{Simulator: sim}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)
	if r, err := cl.SetSimulated(ctx, &pb.SetSimulatedRequest{Simulated: true}); err != nil || !r.GetSimulated() || len(sim.calls) != 1 || !sim.calls[0] {
		t.Errorf("on: %v, %v, calls %v", r, err, sim.calls)
	}
	// A -fake core has no stick to stand in for.
	if _, err := start(t, telemetry.NewHub(), nil, testToken).SetSimulated(ctx, &pb.SetSimulatedRequest{Simulated: true}); status.Code(err) != codes.Unimplemented {
		t.Errorf("without a simulator: %v, want UNIMPLEMENTED", err)
	}
}
