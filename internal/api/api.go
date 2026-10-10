// Package api serves the core's state to renderers over gRPC, with a
// bearer token required on every call.
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/control"
	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/history"
	"github.com/digimago/osscycler/internal/profile"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/ride"
	"github.com/digimago/osscycler/internal/scenery"
	"github.com/digimago/osscycler/internal/telemetry"
)

const (
	// TokenEnv is where the core and clients look for the API token when no
	// token file is given.
	TokenEnv = "OSSCYCLER_API_TOKEN"
	// DefaultAddr listens on loopback only.
	DefaultAddr   = "127.0.0.1:7420"
	DefaultRateHz = 60
	minTokenLen   = 16
)

// Rides is the course-ride control the API exposes.
type Rides interface {
	Courses() []*course.Course
	Start(courseID string) error
	// StartAgainst races the earlier ride that finished at finished.
	StartAgainst(courseID string, finished time.Time) error
	Stop() error
	SetDifficulty(pct float64) float64
}

// History lists finished course rides with personal bests marked.
type History interface {
	Results() ([]history.Entry, error)
}

// Activities lists and opens the finished recordings.
type Activities interface {
	List() ([]record.Activity, error)
	Open(name string) (io.ReadCloser, int64, error)
}

// DownloadActivity fetches a recording from the core into w and returns
// its size. A stream that ends short of the announced size is an error.
func DownloadActivity(ctx context.Context, cl pb.TelemetryServiceClient, name string, w io.Writer) (int64, error) {
	st, err := cl.ExportActivity(ctx, &pb.ExportActivityRequest{Name: name})
	if err != nil {
		return 0, err
	}
	size, got := int64(-1), int64(0)
	for {
		m, err := st.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return got, err
		}
		if size < 0 {
			size = m.GetSizeBytes()
		}
		n, err := w.Write(m.GetChunk())
		got += int64(n)
		if err != nil {
			return got, err
		}
	}
	if got != size {
		return got, fmt.Errorf("export of %s ended at %d of %d bytes", name, got, size)
	}
	return got, nil
}

// exportChunk is the size of ExportActivity's messages.
const exportChunk = 64 << 10

// Control is manual trainer control.
type Control interface {
	Set(m telemetry.ControlMode, v float64) (float64, error)
	Release()
}

// Profile changes the rider's settings.
type Profile interface {
	SetProfile(ctx context.Context, weightKg, ftpW, heightCm *float64, view *string) (telemetry.Profile, error)
}

// Recorder ends the recorded activity on the rider's request.
type Recorder interface {
	End(ctx context.Context, discard bool) (file string, err error)
}

// Services are the optional commands behind the API; nil ones answer
// UNIMPLEMENTED.
type Services struct {
	Calibrator telemetry.Calibrator
	Rides      Rides
	Workouts   Workouts
	Recorder   Recorder
	History    History
	Profile    Profile
	Activities Activities
	Control    Control
	Scenery    Scenery
}

// Scenery is what lies along each course, from map data; nil while
// unknown. Pending tells whether more of it is being fetched; Want asks
// for a course's map data when a ride on it starts, from where it starts.
type Scenery interface {
	Get(courseID string) *scenery.Scenery
	Pending(courseID string) bool
	Want(courseID string, fromM float64)
}

// NewServer returns a gRPC server exposing hub and svc. Calls without the
// token are rejected. Pass grpc.Creds to serve TLS.
func NewServer(hub *telemetry.Hub, svc Services, token string, opts ...grpc.ServerOption) (*grpc.Server, error) {
	if len(token) < minTokenLen {
		return nil, fmt.Errorf("api: token must be at least %d characters", minTokenLen)
	}
	auth := func(ctx context.Context) error { return authorize(ctx, token) }
	opts = append(opts,
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if err := auth(ctx); err != nil {
				return nil, err
			}
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			if err := auth(ss.Context()); err != nil {
				return err
			}
			return h(srv, ss)
		}),
	)
	s := grpc.NewServer(opts...)
	pb.RegisterTelemetryServiceServer(s, &telemetryServer{hub: hub, cal: svc.Calibrator, rides: svc.Rides, workouts: svc.Workouts, recorder: svc.Recorder, history: svc.History, profile: svc.Profile, activities: svc.Activities, control: svc.Control, scenery: svc.Scenery, maxHz: DefaultRateHz})
	return s, nil
}

func authorize(ctx context.Context, token string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get("authorization") {
		if t, ok := strings.CutPrefix(v, "Bearer "); ok && subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "missing or invalid bearer token")
}

type telemetryServer struct {
	pb.UnimplementedTelemetryServiceServer
	hub        *telemetry.Hub
	cal        telemetry.Calibrator // nil: calibration unavailable
	rides      Rides                // nil: no course rides
	workouts   Workouts             // nil: no workout library
	recorder   Recorder             // nil: rides aren't recorded
	history    History              // nil: no history
	profile    Profile              // nil: no rider profile
	activities Activities           // nil: nothing recorded
	control    Control              // nil: no manual control
	scenery    Scenery              // nil: no map data
	maxHz      uint32
}

func (s *telemetryServer) SetTrainerControl(_ context.Context, req *pb.SetTrainerControlRequest) (*pb.SetTrainerControlResponse, error) {
	if s.control == nil {
		return nil, status.Error(codes.Unimplemented, "this core has no manual control")
	}
	var m telemetry.ControlMode
	var v float64
	switch t := req.GetTarget().(type) {
	case *pb.SetTrainerControlRequest_PowerW:
		m, v = telemetry.ControlPower, t.PowerW
	case *pb.SetTrainerControlRequest_GradePct:
		m, v = telemetry.ControlGrade, t.GradePct
	case *pb.SetTrainerControlRequest_LevelPct:
		m, v = telemetry.ControlLevel, t.LevelPct
	default:
		return nil, status.Error(codes.InvalidArgument, "set one of power_w, grade_pct or level_pct")
	}
	got, err := s.control.Set(m, v)
	switch {
	case errors.Is(err, control.ErrBusy):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	s.carryOn()
	return &pb.SetTrainerControlResponse{Mode: pb.ControlMode(m) + 1, Target: got}, nil
}

// SetPaused parks the core or carries on (telemetry.Hub.SetPaused); the
// rides, workouts, manual control and recorder each hold while paused.
func (s *telemetryServer) SetPaused(_ context.Context, req *pb.SetPausedRequest) (*pb.SetPausedResponse, error) {
	s.hub.SetPaused(req.GetPaused())
	st, _ := s.hub.Latest()
	return &pb.SetPausedResponse{Paused: st.Paused}, nil
}

// carryOn ends a pause before something new starts: starting it means
// riding again.
func (s *telemetryServer) carryOn() { s.hub.SetPaused(false) }

func (s *telemetryServer) ReleaseTrainerControl(context.Context, *pb.ReleaseTrainerControlRequest) (*pb.ReleaseTrainerControlResponse, error) {
	if s.control != nil {
		s.control.Release()
	}
	return &pb.ReleaseTrainerControlResponse{}, nil
}

func (s *telemetryServer) ListActivities(context.Context, *pb.ListActivitiesRequest) (*pb.ListActivitiesResponse, error) {
	resp := &pb.ListActivitiesResponse{}
	if s.activities == nil {
		return resp, nil
	}
	as, err := s.activities.List()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	for _, a := range as {
		out := &pb.Activity{Name: a.Name, SizeBytes: a.Size}
		if a.Err != nil {
			out.Error = a.Err.Error()
		} else {
			out.StartUnixMs, out.ElapsedS, out.TimerS = a.Start.UnixMilli(), a.Elapsed.Seconds(), a.Timer.Seconds()
			out.DistanceM, out.AvgPowerW, out.Virtual, out.Laps = a.DistanceM, a.AvgPowerW, a.Virtual, uint32(a.Laps)
		}
		resp.Activities = append(resp.Activities, out)
	}
	return resp, nil
}

func (s *telemetryServer) ExportActivity(req *pb.ExportActivityRequest, stream pb.TelemetryService_ExportActivityServer) error {
	if s.activities == nil {
		return status.Error(codes.NotFound, "this core has no recordings")
	}
	f, size, err := s.activities.Open(req.GetName())
	switch {
	case errors.Is(err, record.ErrNoActivity):
		return status.Error(codes.NotFound, err.Error())
	case err != nil:
		return status.Error(codes.Internal, err.Error())
	}
	defer f.Close()
	if err := stream.Send(&pb.ExportActivityResponse{SizeBytes: size}); err != nil {
		return err
	}
	buf := make([]byte, exportChunk)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if err := stream.Send(&pb.ExportActivityResponse{Chunk: buf[:n]}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
	}
}

func (s *telemetryServer) SetProfile(ctx context.Context, req *pb.SetProfileRequest) (*pb.SetProfileResponse, error) {
	if s.profile == nil {
		return nil, status.Error(codes.Unimplemented, "this core has no rider profile")
	}
	p, err := s.profile.SetProfile(ctx, req.WeightKg, req.FtpW, req.HeightCm, req.View)
	switch {
	case errors.Is(err, profile.ErrOutOfRange):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.SetProfileResponse{Profile: profileToProto(p)}, nil
}

func (s *telemetryServer) ListResults(context.Context, *pb.ListResultsRequest) (*pb.ListResultsResponse, error) {
	resp := &pb.ListResultsResponse{}
	if s.history == nil {
		return resp, nil
	}
	es, err := s.history.Results()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	for _, e := range es {
		resp.Results = append(resp.Results, &pb.RideResult{
			FinishedUnixMs: e.Finished.UnixMilli(), CourseId: e.CourseID, CourseName: e.CourseName,
			StartM: e.StartM, DistanceM: e.DistanceM, ElapsedS: e.ElapsedS, AvgPowerW: e.AvgPowerW,
			ClimbedM: e.ClimbedM, DifficultyPct: e.DifficultyPct, PersonalBest: e.PB, File: e.File, Paused: e.Paused,
		})
	}
	return resp, nil
}

func (s *telemetryServer) EndActivity(ctx context.Context, req *pb.EndActivityRequest) (*pb.EndActivityResponse, error) {
	if s.recorder == nil {
		return nil, status.Error(codes.Unimplemented, "this core doesn't record rides")
	}
	file, err := s.recorder.End(ctx, req.GetDiscard())
	switch {
	case errors.Is(err, record.ErrNotRecording), errors.Is(err, record.ErrBusy):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, record.ErrStopped):
		return nil, status.Error(codes.Unavailable, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.EndActivityResponse{File: file}, nil
}

func (s *telemetryServer) ListCourses(context.Context, *pb.ListCoursesRequest) (*pb.ListCoursesResponse, error) {
	if s.rides == nil {
		return &pb.ListCoursesResponse{}, nil
	}
	resp := &pb.ListCoursesResponse{}
	for _, c := range s.rides.Courses() {
		var sc *scenery.Scenery
		pending := false
		if s.scenery != nil {
			sc = s.scenery.Get(c.ID)
			pending = s.scenery.Pending(c.ID)
		}
		pc := CourseToProto(c, sc)
		pc.MapDataPending = pending
		resp.Courses = append(resp.Courses, pc)
	}
	return resp, nil
}

func (s *telemetryServer) StartRide(_ context.Context, req *pb.StartRideRequest) (*pb.StartRideResponse, error) {
	if s.rides == nil {
		return nil, status.Error(codes.Unimplemented, "this core has no courses")
	}
	var err error
	if req.AgainstFinishedUnixMs != nil {
		err = s.rides.StartAgainst(req.GetCourseId(), time.UnixMilli(req.GetAgainstFinishedUnixMs()))
	} else {
		err = s.rides.Start(req.GetCourseId())
	}
	switch {
	case errors.Is(err, ride.ErrUnknownCourse), errors.Is(err, ride.ErrUnknownRide):
		return nil, status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ride.ErrRideActive), errors.Is(err, profile.ErrIncomplete), errors.Is(err, ride.ErrCourseChanged), errors.Is(err, ride.ErrPausedRide):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case err != nil:
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.carryOn()
	if s.scenery != nil {
		from := 0.0
		if st, _ := s.hub.Latest(); st.Ride.CourseID == req.GetCourseId() {
			from = st.Ride.DistanceM
		}
		s.scenery.Want(req.GetCourseId(), from)
	}
	return &pb.StartRideResponse{}, nil
}

func (s *telemetryServer) SetDifficulty(_ context.Context, req *pb.SetDifficultyRequest) (*pb.SetDifficultyResponse, error) {
	if s.rides == nil {
		return nil, status.Error(codes.Unimplemented, "this core has no courses")
	}
	return &pb.SetDifficultyResponse{DifficultyPct: s.rides.SetDifficulty(req.GetDifficultyPct())}, nil
}

func (s *telemetryServer) StopRide(context.Context, *pb.StopRideRequest) (*pb.StopRideResponse, error) {
	if s.rides == nil {
		return nil, status.Error(codes.Unimplemented, "this core has no courses")
	}
	if err := s.rides.Stop(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &pb.StopRideResponse{}, nil
}

func (s *telemetryServer) StartCalibration(ctx context.Context, req *pb.StartCalibrationRequest) (*pb.StartCalibrationResponse, error) {
	if s.cal == nil {
		return nil, status.Error(codes.Unimplemented, "this core cannot calibrate")
	}
	if req.GetType() != pb.CalibrationType_CALIBRATION_TYPE_SPIN_DOWN {
		return nil, status.Error(codes.InvalidArgument, "only spin-down calibration is supported")
	}
	if err := s.cal.StartSpinDown(ctx); err != nil {
		return nil, commandError(err)
	}
	return &pb.StartCalibrationResponse{}, nil
}

func (s *telemetryServer) CancelCalibration(ctx context.Context, _ *pb.CancelCalibrationRequest) (*pb.CancelCalibrationResponse, error) {
	if s.cal == nil {
		return nil, status.Error(codes.Unimplemented, "this core cannot calibrate")
	}
	if err := s.cal.CancelCalibration(ctx); err != nil {
		return nil, commandError(err)
	}
	return &pb.CancelCalibrationResponse{}, nil
}

// commandError maps telemetry and radio errors to gRPC status codes.
func commandError(err error) error {
	switch {
	case errors.Is(err, telemetry.ErrTrainerUnavailable), errors.Is(err, telemetry.ErrCalibrationActive):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ant.ErrTxFailed), errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.Unavailable, "trainer did not acknowledge: "+err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

func (s *telemetryServer) StreamState(req *pb.StreamStateRequest, stream grpc.ServerStreamingServer[pb.StreamStateResponse]) error {
	hz := req.GetMaxRateHz()
	if hz == 0 || hz > s.maxHz {
		hz = s.maxHz
	}
	minGap := time.Second / time.Duration(hz)
	ctx := stream.Context()

	var (
		sentSeq  uint64
		sentAny  bool
		lastSent time.Time
	)
	for {
		st, changed := s.hub.Latest()
		if !sentAny || st.Seq != sentSeq {
			if err := stream.Send(&pb.StreamStateResponse{State: ToProto(st)}); err != nil {
				return err
			}
			sentAny, sentSeq, lastSent = true, st.Seq, time.Now()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
		}
		// Rate limit: changes that arrive meanwhile are merged into the
		// next send by reading Latest again.
		if wait := minGap - time.Since(lastSent); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil
			case <-t.C:
			}
		}
	}
}

// NewToken returns a random 32-byte token, hex encoded.
func NewToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LoadToken reads the token from file if given, otherwise from TokenEnv.
func LoadToken(file string) (string, error) {
	var tok string
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		tok = strings.TrimSpace(string(b))
	} else {
		tok = os.Getenv(TokenEnv)
	}
	if tok == "" {
		return "", fmt.Errorf("no API token: set %s or pass a token file (generate one with -new-token)", TokenEnv)
	}
	return tok, nil
}

// IsLoopback reports whether a host:port address only reaches this machine.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// bearer attaches the token to every call.
type bearer struct {
	token  string
	secure bool
}

func (b bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearer) RequireTransportSecurity() bool { return b.secure }

// Dial connects to a core. Without TLS only loopback addresses are allowed,
// so the token never crosses a network in clear text.
func Dial(addr, token string, tlsCfg *tls.Config) (*grpc.ClientConn, error) {
	var tc credentials.TransportCredentials
	switch {
	case tlsCfg != nil:
		tc = credentials.NewTLS(tlsCfg)
	case IsLoopback(addr):
		tc = insecure.NewCredentials()
	default:
		return nil, errors.New("api: refusing to send the token to a non-loopback address without TLS")
	}
	return grpc.NewClient(addr,
		grpc.WithTransportCredentials(tc),
		grpc.WithPerRPCCredentials(bearer{token: token, secure: tlsCfg != nil}))
}
