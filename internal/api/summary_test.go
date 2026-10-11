package api

import (
	"context"
	"io"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/effort"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubActivities struct{ samples map[string][]effort.Sample }

func (s stubActivities) List() ([]record.Activity, error) { return nil, nil }
func (s stubActivities) Open(string) (io.ReadCloser, int64, error) {
	return nil, 0, record.ErrNoActivity
}
func (s stubActivities) Samples(name string) ([]effort.Sample, float64, error) {
	ss, ok := s.samples[name]
	if !ok {
		return nil, 0, record.ErrNoActivity
	}
	return ss, 200, nil
}

func steady(n, power, hr int) []effort.Sample {
	var ss []effort.Sample
	for i := range n {
		ss = append(ss, effort.Sample{T: float64(i), PowerW: power, HR: hr, Cadence: 90, SpeedMS: 8, DistM: 8 * float64(i+1)})
	}
	return ss
}

func TestActivityHistoryAndSummary(t *testing.T) {
	ctx := context.Background()
	hub := telemetry.NewHub()
	hub.Update(func(st *telemetry.State) bool {
		st.Profile.FTPW, st.Profile.MaxHRBpm = 250, 190
		return true
	})
	rec := &stubRecorder{}
	acts := stubActivities{samples: map[string][]effort.Sample{"2026-10-10-090000.fit": steady(600, 220, 150)}}
	srv, err := NewServer(hub, Services{Recorder: rec, Activities: acts}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)

	// Nothing under way.
	if _, err := cl.GetActivityHistory(ctx, &pb.GetActivityHistoryRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("history with nothing recorded: %v, want FAILED_PRECONDITION", err)
	}
	// Under way: 90 s, the graph asking for what it lacks.
	rec.file, rec.samples = "2026-10-11-100000.fit", steady(90, 180, 140)
	h, err := cl.GetActivityHistory(ctx, &pb.GetActivityHistoryRequest{SinceS: 60})
	if err != nil || h.GetName() != rec.file || h.GetFromS() != 60 || len(h.GetPowerW()) != 30 || h.GetHeartRateBpm()[0] != 140 {
		t.Errorf("history since 60 s: %v, %v", h, err)
	}
	s, err := cl.GetActivitySummary(ctx, &pb.GetActivitySummaryRequest{})
	if err != nil || s.GetSummary().GetTimerS() != 90 || s.GetSummary().GetAvgPowerW() != 180 {
		t.Errorf("summary under way: %v, %v", s, err)
	}
	// A finished one: zones from its own FTP (200: 220 W is Z5) and the
	// profile's max HR (190: 150 bpm is Z3).
	s, err = cl.GetActivitySummary(ctx, &pb.GetActivitySummaryRequest{Name: "2026-10-10-090000.fit"})
	sum := s.GetSummary()
	if err != nil || sum.GetFtpW() != 200 || sum.GetMaxHrBpm() != 190 || len(sum.GetPowerZoneS()) != 6 ||
		sum.GetPowerZoneS()[4] != 600 || len(sum.GetHeartRateZoneS()) != 5 || sum.GetHeartRateZoneS()[2] != 600 ||
		len(sum.GetPowerCurve()) != 7 || len(sum.GetPowerZoneUpper()) != 5 {
		t.Errorf("finished summary: %v, %v", sum, err)
	}
	if _, err := cl.GetActivitySummary(ctx, &pb.GetActivitySummaryRequest{Name: "nope.fit"}); status.Code(err) != codes.NotFound {
		t.Errorf("unknown activity: %v, want NOT_FOUND", err)
	}
}
