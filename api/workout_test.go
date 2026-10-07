package api

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/digimago/osscycler/erg"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/telemetry"
	"github.com/digimago/osscycler/workout"
)

type nopTrainer struct{}

func (nopTrainer) SetTargetPower(context.Context, float64) error { return nil }
func (nopTrainer) SetGrade(context.Context, float64) error       { return nil }

const ergText = `name Two by Five
warmup 5m 40-70%
2x 5m 95% / 2m 50% @90rpm
> 30s Stay seated
cooldown 3m 60-30%
`

func TestWorkoutProtoRoundTrip(t *testing.T) {
	w, err := workout.ParseText(strings.NewReader(ergText))
	if err != nil {
		t.Fatal(err)
	}
	d := WorkoutToProto("x", w)
	if len(d.GetTimeline()) != 1+4+1 || d.GetDurationS() != (5+14+3)*60 {
		t.Errorf("timeline %d segments, %v s", len(d.GetTimeline()), d.GetDurationS())
	}
	if back := WorkoutFromProto(d); !reflect.DeepEqual(back, w) {
		t.Errorf("proto round trip changed the workout:\n%+v\n%+v", back, w)
	}
}

func TestWorkoutRPCs(t *testing.T) {
	ctx := context.Background()
	hub := telemetry.NewHub()
	lib := &workout.Library{Dir: t.TempDir()}
	sess := erg.NewSession(hub, nopTrainer{}, lib, erg.DefaultConfig(250), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv, err := NewServer(hub, Services{Workouts: sess}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)

	w, _ := workout.ParseText(strings.NewReader(ergText))
	saved, err := cl.SaveWorkout(ctx, &pb.SaveWorkoutRequest{Workout: WorkoutToProto("", w)})
	if err != nil || saved.GetId() != "two-by-five" {
		t.Fatalf("SaveWorkout: %v, %v", saved, err)
	}
	list, err := cl.ListWorkouts(ctx, &pb.ListWorkoutsRequest{})
	if err != nil || len(list.GetWorkouts()) != 1 || list.GetWorkouts()[0].GetName() != "Two by Five" {
		t.Fatalf("ListWorkouts: %v, %v", list, err)
	}

	bad := WorkoutToProto("", w)
	bad.Blocks[0].DurationS = 0
	if _, err := cl.SaveWorkout(ctx, &pb.SaveWorkoutRequest{Workout: bad}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("invalid workout: %v", err)
	}
	if _, err := cl.SaveWorkout(ctx, &pb.SaveWorkoutRequest{Id: "../x", Workout: WorkoutToProto("", w)}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("escaping id: %v", err)
	}

	if _, err := cl.StartWorkout(ctx, &pb.StartWorkoutRequest{Id: "nope"}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown workout: %v", err)
	}
	if _, err := cl.StartWorkout(ctx, &pb.StartWorkoutRequest{Id: "two-by-five"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.StartWorkout(ctx, &pb.StartWorkoutRequest{Id: "two-by-five"}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("second start: %v", err)
	}
	if r, _ := cl.SetIntensity(ctx, &pb.SetIntensityRequest{IntensityPct: 103}); r.GetIntensityPct() != 103 {
		t.Errorf("SetIntensity: %v", r)
	}
	if r, _ := cl.SetFtp(ctx, &pb.SetFtpRequest{FtpW: 9999}); r.GetFtpW() != erg.MaxFTP {
		t.Errorf("SetFtp clamp: %v", r)
	}
	if _, err := cl.SkipSegment(ctx, &pb.SkipSegmentRequest{}); err != nil {
		t.Errorf("SkipSegment: %v", err)
	}

	st := ToProto(func() telemetry.State { s, _ := hub.Latest(); return s }()).GetWorkout()
	if st.GetPhase() != pb.WorkoutPhase_WORKOUT_PHASE_ARMED || st.GetSegmentLabel() != "Interval 1/2 on" ||
		st.GetTargetW() != 587 || st.GetIntensityPct() != 103 { // 600 W FTP x 95 % x 103 %, rounded
		t.Errorf("progress: %v", st)
	}

	if _, err := cl.StopWorkout(ctx, &pb.StopWorkoutRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.SkipSegment(ctx, &pb.SkipSegmentRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("skip with nothing running: %v", err)
	}

	// Without a library the list is empty and the rest is unimplemented.
	bare := start(t, telemetry.NewHub(), nil, testToken)
	if r, err := bare.ListWorkouts(ctx, &pb.ListWorkoutsRequest{}); err != nil || len(r.GetWorkouts()) != 0 {
		t.Errorf("bare list: %v, %v", r, err)
	}
	if _, err := bare.StartWorkout(ctx, &pb.StartWorkoutRequest{Id: "x"}); status.Code(err) != codes.Unimplemented {
		t.Errorf("bare start: %v", err)
	}
}
