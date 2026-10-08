package api

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/fec"
	"github.com/digimago/osscycler/internal/ride"
	"github.com/digimago/osscycler/internal/telemetry"
)

const testToken = "0123456789abcdef0123456789abcdef"

// start serves hub over an in-memory listener and returns a client whose
// calls carry the given token ("" = none).
func start(t *testing.T, hub *telemetry.Hub, cal telemetry.Calibrator, token string) pb.TelemetryServiceClient {
	return startWith(t, hub, cal, nil, token)
}

func startWith(t *testing.T, hub *telemetry.Hub, cal telemetry.Calibrator, rides Rides, token string) pb.TelemetryServiceClient {
	t.Helper()
	srv, err := NewServer(hub, Services{Calibrator: cal, Rides: rides}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return dialServer(t, srv, token)
}

// dialServer serves srv over an in-memory listener and returns a client
// whose calls carry token ("" = none).
func dialServer(t *testing.T, srv *grpc.Server, token string) pb.TelemetryServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	opts := []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	if token != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearer{token: token}))
	}
	conn, err := grpc.NewClient("passthrough:///bufnet", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewTelemetryServiceClient(conn)
}

func TestRejectsMissingOrWrongToken(t *testing.T) {
	hub := telemetry.NewHub()
	for name, tok := range map[string]string{"missing": "", "wrong": "not-the-token-0000000000"} {
		c := start(t, hub, nil, tok)
		stream, err := c.StreamState(context.Background(), &pb.StreamStateRequest{})
		if err == nil {
			_, err = stream.Recv()
		}
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("%s token: err = %v, want Unauthenticated", name, err)
		}
	}
}

func TestShortTokenRefused(t *testing.T) {
	if _, err := NewServer(telemetry.NewHub(), Services{}, "short"); err == nil {
		t.Fatal("NewServer accepted a short token")
	}
}

func TestStreamState(t *testing.T) {
	hub := telemetry.NewHub()
	c := start(t, hub, nil, testToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.StreamState(ctx, &pb.StreamStateRequest{MaxRateHz: 1000})
	if err != nil {
		t.Fatal(err)
	}

	// The current state arrives immediately, even before any update.
	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if first.GetState().GetSequence() != 0 {
		t.Fatalf("first sequence = %d", first.GetState().GetSequence())
	}

	hub.Update(func(s *telemetry.State) bool {
		s.Trainer.Sensor = telemetry.Sensor{Status: telemetry.StatusConnected, DeviceNumber: 47508}
		s.Trainer.State = fec.StateInUse
		s.Trainer.PowerW = telemetry.Some[uint16](250)
		s.Trainer.Flags = fec.ResistanceCalibrationRequired
		s.HeartRate.Sensor.Status = telemetry.StatusSearching
		return true
	})
	got, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	tr := got.GetState().GetTrainer()
	if tr.GetSensor().GetStatus() != pb.SensorStatus_SENSOR_STATUS_CONNECTED || tr.GetSensor().GetDeviceNumber() != 47508 {
		t.Errorf("trainer sensor = %v", tr.GetSensor())
	}
	if tr.PowerW == nil || *tr.PowerW != 250 || tr.CadenceRpm != nil {
		t.Errorf("power %v cadence %v, want 250 and unset", tr.PowerW, tr.CadenceRpm)
	}
	if tr.GetState() != pb.TrainerState_TRAINER_STATE_IN_USE || !tr.GetResistanceCalibrationRequired() {
		t.Errorf("state %v, resistance cal %v", tr.GetState(), tr.GetResistanceCalibrationRequired())
	}
	if hr := got.GetState().GetHeartRate(); hr.GetSensor().GetStatus() != pb.SensorStatus_SENSOR_STATUS_SEARCHING || hr.Bpm != nil {
		t.Errorf("heart rate = %v", hr)
	}
}

func TestStreamConflatesForSlowClients(t *testing.T) {
	hub := telemetry.NewHub()
	c := start(t, hub, nil, testToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.StreamState(ctx, &pb.StreamStateRequest{MaxRateHz: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	for i := range 100 {
		hub.Update(func(s *telemetry.State) bool { s.Trainer.PowerW = telemetry.Some(uint16(i)); return true })
	}
	// At 5 Hz the client sees far fewer than 100 messages, and the last one
	// it sees is the newest state.
	var n int
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		n++
		if msg.GetState().GetSequence() == 100 {
			break
		}
	}
	if n > 5 {
		t.Errorf("client received %d messages for 100 updates at 5 Hz", n)
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:7420": true, "localhost:7420": true, "[::1]:7420": true,
		"0.0.0.0:7420": false, "192.168.1.10:7420": false, ":7420": false, "nonsense": false,
	} {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
	if _, err := Dial("192.168.1.10:7420", testToken, nil); err == nil {
		t.Error("Dial sent a token to a LAN address without TLS")
	}
}

// stubCalibrator records calls and returns a fixed error.
type stubCalibrator struct {
	started, cancelled int
	err                error
}

func (s *stubCalibrator) StartSpinDown(context.Context) error     { s.started++; return s.err }
func (s *stubCalibrator) CancelCalibration(context.Context) error { s.cancelled++; return s.err }

func TestCalibrationRPCs(t *testing.T) {
	ctx := context.Background()
	spinDown := &pb.StartCalibrationRequest{Type: pb.CalibrationType_CALIBRATION_TYPE_SPIN_DOWN}

	if _, err := start(t, telemetry.NewHub(), nil, testToken).StartCalibration(ctx, spinDown); status.Code(err) != codes.Unimplemented {
		t.Errorf("without calibrator: err = %v, want Unimplemented", err)
	}

	cal := &stubCalibrator{}
	c := start(t, telemetry.NewHub(), cal, testToken)
	if _, err := c.StartCalibration(ctx, &pb.StartCalibrationRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("unspecified type: err = %v, want InvalidArgument", err)
	}
	if _, err := c.StartCalibration(ctx, spinDown); err != nil || cal.started != 1 {
		t.Errorf("start: err %v, calls %d", err, cal.started)
	}
	if _, err := c.CancelCalibration(ctx, &pb.CancelCalibrationRequest{}); err != nil || cal.cancelled != 1 {
		t.Errorf("cancel: err %v, calls %d", err, cal.cancelled)
	}
	cal.err = telemetry.ErrTrainerUnavailable
	if _, err := c.StartCalibration(ctx, spinDown); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("trainer unavailable: err = %v, want FailedPrecondition", err)
	}

	// Commands need the token like everything else.
	if _, err := start(t, telemetry.NewHub(), cal, "").StartCalibration(ctx, spinDown); status.Code(err) != codes.Unauthenticated {
		t.Errorf("no token: err = %v, want Unauthenticated", err)
	}
}

func TestCalibrationStateConversion(t *testing.T) {
	var st telemetry.State
	st.Trainer.Calibration = telemetry.Calibration{
		Phase:          telemetry.CalInProgress,
		SpeedCondition: fec.ConditionTooLow,
		TargetSpeedMPS: telemetry.Some(9.7),
	}
	c := ToProto(st).GetTrainer().GetCalibration()
	if c.GetPhase() != pb.CalibrationPhase_CALIBRATION_PHASE_IN_PROGRESS ||
		c.GetSpeedCondition() != pb.CalibrationCondition_CALIBRATION_CONDITION_TOO_LOW ||
		c.GetTargetSpeedMps() != 9.7 || c.SpinDownMs != nil {
		t.Errorf("calibration = %v", c)
	}
	// Every telemetry phase maps to a named proto phase.
	for p := telemetry.CalIdle; p <= telemetry.CalCancelled; p++ {
		st.Trainer.Calibration.Phase = p
		if got := ToProto(st).GetTrainer().GetCalibration().GetPhase(); got.String() == "" || got == pb.CalibrationPhase_CALIBRATION_PHASE_UNSPECIFIED {
			t.Errorf("phase %v maps to %v", p, got)
		}
	}
}

type stubRides struct {
	courses    []*course.Course
	started    string
	stopped    int
	difficulty float64
	err        error
	against    time.Time
}

func (s *stubRides) Courses() []*course.Course { return s.courses }
func (s *stubRides) Start(id string) error     { s.started = id; return s.err }
func (s *stubRides) StartAgainst(id string, finished time.Time) error {
	s.started, s.against = id, finished
	return s.err
}
func (s *stubRides) Stop() error { s.stopped++; return nil }
func (s *stubRides) SetDifficulty(p float64) float64 {
	s.difficulty = max(0, min(p, 100))
	return s.difficulty
}

func TestRideRPCs(t *testing.T) {
	ctx := context.Background()
	const mPerDeg = 6371000 * 3.141592653589793 / 180
	var pts []course.Point
	for d := 0.0; d <= 500; d += 5 {
		pts = append(pts, course.Point{Lat: d / mPerDeg, Ele: 10 + d*0.04})
	}
	c, err := course.New("hill", "Hill", pts)
	if err != nil {
		t.Fatal(err)
	}
	rides := &stubRides{courses: []*course.Course{c}}
	cl := startWith(t, telemetry.NewHub(), nil, rides, testToken)

	resp, err := cl.ListCourses(ctx, &pb.ListCoursesRequest{})
	if err != nil || len(resp.GetCourses()) != 1 {
		t.Fatalf("ListCourses: %v, %v", resp, err)
	}
	pc := resp.GetCourses()[0]
	if pc.GetId() != "hill" || pc.GetProfileStepM() <= 0 || pc.GetProfileStepM() > course.Step+1e-9 ||
		len(pc.GetProfileGradePct()) != len(pc.GetProfileElevationM()) || len(pc.GetProfileGradePct()) < 50 {
		t.Errorf("course: id %q step %v, %d grades, %d elevations", pc.GetId(), pc.GetProfileStepM(),
			len(pc.GetProfileGradePct()), len(pc.GetProfileElevationM()))
	}
	if g := pc.GetProfileGradePct()[25]; g < 3.9 || g > 4.1 {
		t.Errorf("profile grade %v, want 4", g)
	}

	if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "hill"}); err != nil || rides.started != "hill" {
		t.Errorf("StartRide: %v, started %q", err, rides.started)
	}
	rides.err = ride.ErrUnknownCourse
	if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "x"}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown course: %v", err)
	}
	rides.err = ride.ErrRideActive
	if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "hill"}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("ride active: %v", err)
	}
	// Racing an earlier ride.
	rides.err = nil
	when := time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)
	ms := when.UnixMilli()
	if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "hill", AgainstFinishedUnixMs: &ms}); err != nil || !rides.against.Equal(when) {
		t.Errorf("race: %v, against %v", err, rides.against)
	}
	for e, code := range map[error]codes.Code{ride.ErrUnknownRide: codes.NotFound, ride.ErrCourseChanged: codes.FailedPrecondition} {
		rides.err = e
		if _, err := cl.StartRide(ctx, &pb.StartRideRequest{CourseId: "hill", AgainstFinishedUnixMs: &ms}); status.Code(err) != code {
			t.Errorf("race, %v: %v, want %v", e, err, code)
		}
	}
	rides.err = nil
	if _, err := cl.StopRide(ctx, &pb.StopRideRequest{}); err != nil || rides.stopped != 1 {
		t.Errorf("StopRide: %v, stops %d", err, rides.stopped)
	}
	if resp, err := cl.SetDifficulty(ctx, &pb.SetDifficultyRequest{DifficultyPct: 140}); err != nil || resp.GetDifficultyPct() != 100 {
		t.Errorf("SetDifficulty(140): %v, %v; want clamped 100", resp, err)
	}

	// Without a ride service the list is empty and starting is unimplemented.
	bare := start(t, telemetry.NewHub(), nil, testToken)
	if resp, err := bare.ListCourses(ctx, &pb.ListCoursesRequest{}); err != nil || len(resp.GetCourses()) != 0 {
		t.Errorf("bare ListCourses: %v, %v", resp, err)
	}
	if _, err := bare.StartRide(ctx, &pb.StartRideRequest{CourseId: "hill"}); status.Code(err) != codes.Unimplemented {
		t.Errorf("bare StartRide: %v", err)
	}
}

func TestRidePhaseConversion(t *testing.T) {
	var st telemetry.State
	for p := telemetry.RideNone; p <= telemetry.RideAborted; p++ {
		st.Ride.Phase = p
		if got := ToProto(st).GetRide().GetPhase(); got == pb.RidePhase_RIDE_PHASE_UNSPECIFIED || got.String() == "" {
			t.Errorf("phase %v maps to %v", p, got)
		}
	}
	st.Ride = telemetry.Ride{Phase: telemetry.RideRiding, DistanceM: 1234, Elapsed: 90 * time.Second}
	r := ToProto(st).GetRide()
	if r.GetPhase() != pb.RidePhase_RIDE_PHASE_RIDING || r.GetDistanceM() != 1234 || r.GetElapsedS() != 90 {
		t.Errorf("ride = %v", r)
	}
}
