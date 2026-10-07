package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/digimago/osscycler/control"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/telemetry"
)

type stubControl struct {
	set      []telemetry.ControlMode
	busy     bool
	released int
}

func (s *stubControl) Set(m telemetry.ControlMode, v float64) (float64, error) {
	if s.busy {
		return 0, control.ErrBusy
	}
	s.set = append(s.set, m)
	return v + 0.5, nil // as if clamped
}
func (s *stubControl) Release() { s.released++ }

func TestTrainerControl(t *testing.T) {
	ctx := context.Background()
	sc := &stubControl{}
	srv, err := NewServer(telemetry.NewHub(), Services{Control: sc}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)
	for _, req := range []*pb.SetTrainerControlRequest{
		{Target: &pb.SetTrainerControlRequest_PowerW{PowerW: 200}},
		{Target: &pb.SetTrainerControlRequest_GradePct{GradePct: 6}},
		{Target: &pb.SetTrainerControlRequest_LevelPct{LevelPct: 40}},
	} {
		if _, err := cl.SetTrainerControl(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if len(sc.set) != 3 || sc.set[0] != telemetry.ControlPower || sc.set[1] != telemetry.ControlGrade || sc.set[2] != telemetry.ControlLevel {
		t.Errorf("modes %v", sc.set)
	}
	resp, _ := cl.SetTrainerControl(ctx, &pb.SetTrainerControlRequest{Target: &pb.SetTrainerControlRequest_PowerW{PowerW: 150}})
	if resp.GetMode() != pb.ControlMode_CONTROL_MODE_POWER || resp.GetTarget() != 150.5 {
		t.Errorf("response %v", resp)
	}
	if _, err := cl.SetTrainerControl(ctx, &pb.SetTrainerControlRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("no target: %v", err)
	}
	sc.busy = true
	if _, err := cl.SetTrainerControl(ctx, &pb.SetTrainerControlRequest{Target: &pb.SetTrainerControlRequest_GradePct{GradePct: 3}}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("busy: %v", err)
	}
	if _, err := cl.ReleaseTrainerControl(ctx, &pb.ReleaseTrainerControlRequest{}); err != nil || sc.released != 1 {
		t.Errorf("release: %v, %d", err, sc.released)
	}
}

func TestControlInState(t *testing.T) {
	st := telemetry.State{Control: telemetry.Control{Mode: telemetry.ControlGrade, Target: 6}}
	c := ToProto(st).GetControl()
	if c.GetMode() != pb.ControlMode_CONTROL_MODE_GRADE || c.GetTarget() != 6 {
		t.Errorf("control %v", c)
	}
	if ToProto(telemetry.State{}).GetControl().GetMode() != pb.ControlMode_CONTROL_MODE_OFF {
		t.Error("idle should be OFF, not unspecified")
	}
}

func TestRadioInState(t *testing.T) {
	if ToProto(telemetry.State{}).GetRadio() != nil {
		t.Error("a core without a stick to look for sends a radio")
	}
	r := ToProto(telemetry.State{Radio: telemetry.Radio{Known: true, Error: "no stick"}}).GetRadio()
	if r == nil || r.GetPresent() || r.GetError() != "no stick" {
		t.Errorf("radio %v", r)
	}
}
