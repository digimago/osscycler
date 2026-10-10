package api

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/erg"
	"github.com/digimago/osscycler/internal/profile"
	"github.com/digimago/osscycler/internal/telemetry"
	"github.com/digimago/osscycler/internal/workout"
)

// Workouts is the ERG workout control the API exposes.
type Workouts interface {
	Library() *workout.Library
	Start(id string) error
	Stop() error
	Skip() error
	SetIntensity(pct float64) float64
	SetFTP(watts float64) float64
}

func (s *telemetryServer) needWorkouts() error {
	if s.workouts == nil {
		return status.Error(codes.Unimplemented, "this core has no workout library")
	}
	return nil
}

func (s *telemetryServer) ListWorkouts(context.Context, *pb.ListWorkoutsRequest) (*pb.ListWorkoutsResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return &pb.ListWorkoutsResponse{}, nil
	}
	entries, err := s.workouts.Library().List()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &pb.ListWorkoutsResponse{}
	for _, e := range entries {
		if e.Err != nil {
			resp.Workouts = append(resp.Workouts, &pb.WorkoutDef{Id: e.ID, Name: e.ID, Error: e.Err.Error()})
			continue
		}
		resp.Workouts = append(resp.Workouts, WorkoutToProto(e.ID, e.Workout))
	}
	return resp, nil
}

func (s *telemetryServer) SaveWorkout(_ context.Context, req *pb.SaveWorkoutRequest) (*pb.SaveWorkoutResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	w := WorkoutFromProto(req.GetWorkout())
	if err := w.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	id, err := s.workouts.Library().Save(req.GetId(), w)
	switch {
	case errors.Is(err, workout.ErrBadID):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SaveWorkoutResponse{Id: id}, nil
}

func (s *telemetryServer) StartWorkout(_ context.Context, req *pb.StartWorkoutRequest) (*pb.StartWorkoutResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	if err := s.workouts.Start(req.GetId()); err != nil {
		return nil, workoutError(err)
	}
	s.carryOn()
	return &pb.StartWorkoutResponse{}, nil
}

func (s *telemetryServer) StopWorkout(context.Context, *pb.StopWorkoutRequest) (*pb.StopWorkoutResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	if err := s.workouts.Stop(); err != nil {
		return nil, workoutError(err)
	}
	return &pb.StopWorkoutResponse{}, nil
}

func (s *telemetryServer) SkipSegment(context.Context, *pb.SkipSegmentRequest) (*pb.SkipSegmentResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	if err := s.workouts.Skip(); err != nil {
		return nil, workoutError(err)
	}
	return &pb.SkipSegmentResponse{}, nil
}

func (s *telemetryServer) SetIntensity(_ context.Context, req *pb.SetIntensityRequest) (*pb.SetIntensityResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	return &pb.SetIntensityResponse{IntensityPct: s.workouts.SetIntensity(req.GetIntensityPct())}, nil
}

func (s *telemetryServer) SetFtp(_ context.Context, req *pb.SetFtpRequest) (*pb.SetFtpResponse, error) {
	if err := s.needWorkouts(); err != nil {
		return nil, err
	}
	return &pb.SetFtpResponse{FtpW: s.workouts.SetFTP(req.GetFtpW())}, nil
}

func workoutError(err error) error {
	switch {
	case errors.Is(err, workout.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, erg.ErrActive), errors.Is(err, erg.ErrIdle), errors.Is(err, profile.ErrIncomplete):
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func secs(d time.Duration) float64 { return d.Seconds() }

func dur(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// WorkoutToProto converts a workout, with its derived timeline.
func WorkoutToProto(id string, w *workout.Workout) *pb.WorkoutDef {
	d := &pb.WorkoutDef{Id: id, Name: w.Name, Author: w.Author, Description: w.Description, DurationS: secs(w.Duration())}
	for _, b := range w.Blocks {
		pbb := &pb.WorkoutBlock{
			Type: string(b.Type), DurationS: secs(b.Duration),
			Power: b.Power, PowerLow: b.PowerLow, PowerHigh: b.PowerHigh,
			Repeat: uint32(b.Repeat), OnDurationS: secs(b.OnDuration), OffDurationS: secs(b.OffDuration),
			OnPower: b.OnPower, OffPower: b.OffPower, Cadence: uint32(b.Cadence),
		}
		for _, t := range b.Texts {
			pbb.Texts = append(pbb.Texts, &pb.WorkoutText{OffsetS: secs(t.Offset), Message: t.Message})
		}
		d.Blocks = append(d.Blocks, pbb)
	}
	for _, s := range w.Timeline() {
		d.Timeline = append(d.Timeline, &pb.WorkoutSegment{
			Free: s.Kind == workout.KindFree, StartS: secs(s.Start), DurationS: secs(s.Duration),
			FromFtp: s.From, ToFtp: s.To, Label: s.Label, Cadence: uint32(s.Cadence),
		})
	}
	return d
}

// WorkoutFromProto converts the authored part of a workout; derived fields
// are ignored. The result is not validated.
func WorkoutFromProto(d *pb.WorkoutDef) *workout.Workout {
	w := &workout.Workout{Name: d.GetName(), Author: d.GetAuthor(), Description: d.GetDescription()}
	for _, b := range d.GetBlocks() {
		wb := workout.Block{
			Type: workout.BlockType(b.GetType()), Duration: dur(b.GetDurationS()),
			Power: b.GetPower(), PowerLow: b.GetPowerLow(), PowerHigh: b.GetPowerHigh(),
			Repeat: int(b.GetRepeat()), OnDuration: dur(b.GetOnDurationS()), OffDuration: dur(b.GetOffDurationS()),
			OnPower: b.GetOnPower(), OffPower: b.GetOffPower(), Cadence: int(b.GetCadence()),
		}
		for _, t := range b.GetTexts() {
			wb.Texts = append(wb.Texts, workout.Text{Offset: dur(t.GetOffsetS()), Message: t.GetMessage()})
		}
		w.Blocks = append(w.Blocks, wb)
	}
	return w
}

func workoutProgressToProto(w telemetry.Workout) *pb.WorkoutProgress {
	return &pb.WorkoutProgress{
		Phase:             pb.WorkoutPhase(w.Phase) + 1, // proto reserves 0 for unspecified
		Id:                w.ID,
		Name:              w.Name,
		DurationS:         secs(w.Duration),
		ElapsedS:          secs(w.Elapsed),
		Segment:           uint32(w.Segment),
		SegmentLabel:      w.SegmentLabel,
		SegmentRemainingS: secs(w.SegmentRemaining),
		Free:              w.Free,
		TargetW:           w.TargetW,
		TargetCadence:     uint32(w.TargetCadence),
		NextLabel:         w.NextLabel,
		NextTargetW:       w.NextTargetW,
		NextFree:          w.NextFree,
		Message:           w.Message,
		FtpW:              w.FTPW,
		IntensityPct:      w.IntensityPct,
		AvgPowerW:         w.AvgPowerW,
		PhaseChangedNs:    int64(w.Changed),
	}
}
