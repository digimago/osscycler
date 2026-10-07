// Command osscycler-tui shows the core's live telemetry in a terminal, sized
// to be read from the bike.
//
// The API token comes from OSSCYCLER_API_TOKEN or -token-file.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/digimago/osscycler/api"
	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/home"
	"github.com/digimago/osscycler/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "osscycler-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", api.DefaultAddr, "core API address")
	tokenFile := flag.String("token-file", "", "file holding the API token (default: $"+api.TokenEnv+", else the core's "+home.Token()+")")
	caFile := flag.String("tls-ca", "", "CA certificate to verify the core; enables TLS")
	rate := flag.Uint("rate", 10, "maximum updates per second")
	exportDir := flag.String("export-dir", defaultExportDir(), "where s in the ACTIVITIES tab saves a ride's FIT file")
	tour := flag.Bool("tour", false, "demo: walk through the features automatically (any key takes over); made for a core started with -fake")
	flag.Parse()

	// An explicit -token-file, else $OSSCYCLER_API_TOKEN, else the token the
	// core made in osscycler's folder on this machine.
	var token string
	var err error
	switch {
	case *tokenFile != "" || os.Getenv(api.TokenEnv) != "":
		token, err = api.LoadToken(*tokenFile)
	default:
		if token, err = home.ReadToken(); err != nil {
			return fmt.Errorf("no API token: start osscycler-core on this machine once, or copy %s from the core's machine and pass -token-file (%w)", home.Token(), err)
		}
	}
	if err != nil {
		return err
	}
	var tlsCfg *tls.Config
	if *caFile != "" {
		pem, err := os.ReadFile(*caFile)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return errors.New("no certificates found in " + *caFile)
		}
		tlsCfg = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}
	}
	conn, err := api.Dial(*addr, token, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := pb.NewTelemetryServiceClient(conn)
	model := tui.New(*addr, commands{client}).WithExportDir(*exportDir)
	var t *tui.Tour
	if *tour {
		t = tui.NewTour()
		model = model.WithTour(t)
	}
	p := tea.NewProgram(model)
	go stream(ctx, client, uint32(*rate), p.Send)
	if t != nil {
		go t.Run(ctx, p.Send)
	}
	_, err = p.Run()
	return err
}

// stream follows the core's state, reconnecting with backoff, and forwards
// everything to the UI.
func stream(ctx context.Context, c pb.TelemetryServiceClient, rate uint32, send func(tea.Msg)) {
	const maxBackoff = 5 * time.Second
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		s, err := c.StreamState(ctx, &pb.StreamStateRequest{MaxRateHz: rate})
		for err == nil {
			var msg *pb.StreamStateResponse
			if msg, err = s.Recv(); err == nil {
				send(tui.StateMsg{State: msg.GetState()})
				backoff = 500 * time.Millisecond
			}
		}
		if ctx.Err() != nil {
			return
		}
		send(tui.ConnMsg{Err: friendly(err)})
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func friendly(err error) error {
	switch status.Code(err) {
	case codes.Unauthenticated:
		return errors.New("token rejected")
	case codes.Unavailable:
		return errors.New("core not reachable, retrying")
	}
	return err
}

// commands adapts the gRPC client to the TUI's Commands.
type commands struct{ c pb.TelemetryServiceClient }

func (c commands) StartCalibration(ctx context.Context) error {
	_, err := c.c.StartCalibration(ctx, &pb.StartCalibrationRequest{Type: pb.CalibrationType_CALIBRATION_TYPE_SPIN_DOWN})
	return err
}

func (c commands) CancelCalibration(ctx context.Context) error {
	_, err := c.c.CancelCalibration(ctx, &pb.CancelCalibrationRequest{})
	return err
}

func (c commands) ListCourses(ctx context.Context) ([]*pb.Course, error) {
	resp, err := c.c.ListCourses(ctx, &pb.ListCoursesRequest{})
	return resp.GetCourses(), err
}

func (c commands) StartRide(ctx context.Context, id string) error {
	_, err := c.c.StartRide(ctx, &pb.StartRideRequest{CourseId: id})
	return err
}

func (c commands) StopRide(ctx context.Context) error {
	_, err := c.c.StopRide(ctx, &pb.StopRideRequest{})
	return err
}

func (c commands) SetDifficulty(ctx context.Context, pct float64) (float64, error) {
	resp, err := c.c.SetDifficulty(ctx, &pb.SetDifficultyRequest{DifficultyPct: pct})
	return resp.GetDifficultyPct(), err
}

func (c commands) ListWorkouts(ctx context.Context) ([]*pb.WorkoutDef, error) {
	resp, err := c.c.ListWorkouts(ctx, &pb.ListWorkoutsRequest{})
	return resp.GetWorkouts(), err
}

func (c commands) SaveWorkout(ctx context.Context, id string, w *pb.WorkoutDef) (string, error) {
	resp, err := c.c.SaveWorkout(ctx, &pb.SaveWorkoutRequest{Id: id, Workout: w})
	return resp.GetId(), err
}

func (c commands) StartWorkout(ctx context.Context, id string) error {
	_, err := c.c.StartWorkout(ctx, &pb.StartWorkoutRequest{Id: id})
	return err
}

func (c commands) StopWorkout(ctx context.Context) error {
	_, err := c.c.StopWorkout(ctx, &pb.StopWorkoutRequest{})
	return err
}

func (c commands) SkipSegment(ctx context.Context) error {
	_, err := c.c.SkipSegment(ctx, &pb.SkipSegmentRequest{})
	return err
}

func (c commands) SetIntensity(ctx context.Context, pct float64) (float64, error) {
	resp, err := c.c.SetIntensity(ctx, &pb.SetIntensityRequest{IntensityPct: pct})
	return resp.GetIntensityPct(), err
}

func (c commands) SetTrainerControl(ctx context.Context, mode pb.ControlMode, v float64) (float64, error) {
	req := &pb.SetTrainerControlRequest{}
	switch mode {
	case pb.ControlMode_CONTROL_MODE_POWER:
		req.Target = &pb.SetTrainerControlRequest_PowerW{PowerW: v}
	case pb.ControlMode_CONTROL_MODE_GRADE:
		req.Target = &pb.SetTrainerControlRequest_GradePct{GradePct: v}
	case pb.ControlMode_CONTROL_MODE_LEVEL:
		req.Target = &pb.SetTrainerControlRequest_LevelPct{LevelPct: v}
	}
	resp, err := c.c.SetTrainerControl(ctx, req)
	return resp.GetTarget(), err
}

func (c commands) ReleaseTrainerControl(ctx context.Context) error {
	_, err := c.c.ReleaseTrainerControl(ctx, &pb.ReleaseTrainerControlRequest{})
	return err
}

func (c commands) ListActivities(ctx context.Context) ([]*pb.Activity, error) {
	resp, err := c.c.ListActivities(ctx, &pb.ListActivitiesRequest{})
	return resp.GetActivities(), err
}

func (c commands) ExportActivity(ctx context.Context, name string, w io.Writer) (int64, error) {
	return api.DownloadActivity(ctx, c.c, name, w)
}

// defaultExportDir is ~/Downloads if there is one, else the home directory.
func defaultExportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if info, err := os.Stat(filepath.Join(home, "Downloads")); err == nil && info.IsDir() {
		return filepath.Join(home, "Downloads")
	}
	return home
}

func (c commands) SetProfile(ctx context.Context, weightKg, ftpW *float64) (*pb.RiderProfile, error) {
	resp, err := c.c.SetProfile(ctx, &pb.SetProfileRequest{WeightKg: weightKg, FtpW: ftpW})
	return resp.GetProfile(), err
}

func (c commands) ListResults(ctx context.Context) ([]*pb.RideResult, error) {
	resp, err := c.c.ListResults(ctx, &pb.ListResultsRequest{})
	return resp.GetResults(), err
}

func (c commands) EndActivity(ctx context.Context, discard bool) (string, error) {
	resp, err := c.c.EndActivity(ctx, &pb.EndActivityRequest{Discard: discard})
	return resp.GetFile(), err
}

func (c commands) SetFTP(ctx context.Context, watts float64) (float64, error) {
	resp, err := c.c.SetFtp(ctx, &pb.SetFtpRequest{FtpW: watts})
	return resp.GetFtpW(), err
}
