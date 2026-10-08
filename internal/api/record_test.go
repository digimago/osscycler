package api

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/history"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubRecorder struct {
	err      error
	discards []bool
}

func (s *stubRecorder) End(_ context.Context, discard bool) (string, error) {
	s.discards = append(s.discards, discard)
	if s.err != nil || discard {
		return "", s.err
	}
	return "ride.fit", nil
}

func TestEndActivity(t *testing.T) {
	ctx := context.Background()
	rec := &stubRecorder{}
	srv, err := NewServer(telemetry.NewHub(), Services{Recorder: rec}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)

	resp, err := cl.EndActivity(ctx, &pb.EndActivityRequest{})
	if err != nil || resp.GetFile() != "ride.fit" || rec.discards[0] {
		t.Errorf("default end: %v, %v, discards %v (saving is the default)", resp, err, rec.discards)
	}
	if _, err := cl.EndActivity(ctx, &pb.EndActivityRequest{Discard: true}); err != nil || !rec.discards[1] {
		t.Errorf("discard: %v, %v", err, rec.discards)
	}
	for want, e := range map[codes.Code]error{
		codes.FailedPrecondition: record.ErrBusy,
		codes.Unavailable:        record.ErrStopped,
	} {
		rec.err = e
		if _, err := cl.EndActivity(ctx, &pb.EndActivityRequest{}); status.Code(err) != want {
			t.Errorf("%v: code %v, want %v", e, status.Code(err), want)
		}
	}

	// A core that doesn't record says so.
	if _, err := start(t, telemetry.NewHub(), nil, testToken).EndActivity(ctx, &pb.EndActivityRequest{}); status.Code(err) != codes.Unimplemented {
		t.Errorf("no recorder: %v", err)
	}
}

type stubHistory []history.Entry

func (h stubHistory) Results() ([]history.Entry, error) { return h, nil }

func TestListResults(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 7, 18, 30, 0, 0, time.UTC)
	h := stubHistory{{Result: record.Result{Finished: at, CourseID: "mash", CourseName: "Mountain Mash", StartM: 1480, ElapsedS: 754}, PB: true}}
	srv, err := NewServer(telemetry.NewHub(), Services{History: h}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dialServer(t, srv, testToken).ListResults(ctx, &pb.ListResultsRequest{})
	if err != nil || len(resp.GetResults()) != 1 {
		t.Fatalf("%v, %v", resp, err)
	}
	r := resp.GetResults()[0]
	if r.GetFinishedUnixMs() != at.UnixMilli() || r.GetCourseId() != "mash" || r.GetStartM() != 1480 || !r.GetPersonalBest() {
		t.Errorf("result %v", r)
	}
	// Without history: an empty list, not an error.
	if resp, err := start(t, telemetry.NewHub(), nil, testToken).ListResults(ctx, &pb.ListResultsRequest{}); err != nil || len(resp.GetResults()) != 0 {
		t.Errorf("no history: %v, %v", resp, err)
	}
}
