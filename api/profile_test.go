package api

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/profile"
	"github.com/digimago/osscycler/telemetry"
)

type stubProfile struct{ weights, ftps []*float64 }

func (s *stubProfile) SetProfile(_ context.Context, w, f *float64) (telemetry.Profile, error) {
	s.weights, s.ftps = append(s.weights, w), append(s.ftps, f)
	if f != nil && *f > profile.MaxFTPW {
		return telemetry.Profile{}, profile.Check(profile.FTP, *f)
	}
	return telemetry.Profile{Known: true, NeedFTP: f == nil, WeightKg: 87, SuggestedFTPW: 220}, nil
}

type gatedRides struct{ Rides }

func (gatedRides) Start(string) error {
	return fmt.Errorf("%w: a course ride needs the rider's weight", profile.ErrIncomplete)
}

func TestSetProfile(t *testing.T) {
	ctx := context.Background()
	sp := &stubProfile{}
	srv, err := NewServer(telemetry.NewHub(), Services{Profile: sp, Rides: gatedRides{}}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)

	w := 87.0
	resp, err := cl.SetProfile(ctx, &pb.SetProfileRequest{WeightKg: &w})
	if err != nil {
		t.Fatal(err)
	}
	if sp.weights[0] == nil || *sp.weights[0] != 87 || sp.ftps[0] != nil {
		t.Errorf("only the weight should pass through: %v %v", sp.weights, sp.ftps)
	}
	if p := resp.GetProfile(); fmt.Sprint(p.GetMissing()) != "[ftp_w]" || p.GetSuggestedFtpW() != 220 {
		t.Errorf("profile %v", p)
	}
	f := 5000.0
	if _, err := cl.SetProfile(ctx, &pb.SetProfileRequest{FtpW: &f}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("FTP 5000: %v", err)
	}
	// A refused start says why, as a precondition any renderer can show.
	if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "x"}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("ride without a weight: %v", err)
	}
	if _, err := start(t, telemetry.NewHub(), nil, testToken).SetProfile(ctx, &pb.SetProfileRequest{}); status.Code(err) != codes.Unimplemented {
		t.Errorf("no profile: %v", err)
	}
}

func TestProfileInState(t *testing.T) {
	if ToProto(telemetry.State{}).GetProfile() != nil {
		t.Error("a core without profiles sends one")
	}
	st := telemetry.State{Profile: telemetry.Profile{Known: true, NeedWeight: true, NeedFTP: true, SuggestedFTPW: 200, Path: "/p.json"}}
	p := ToProto(st).GetProfile()
	if p.GetComplete() || fmt.Sprint(p.GetMissing()) != "[weight_kg ftp_w]" || p.GetPath() != "/p.json" {
		t.Errorf("profile %v", p)
	}
}
