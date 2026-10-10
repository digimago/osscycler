package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubFiles map[string]string

func (s stubFiles) File(id string) (string, []byte, error) {
	b, ok := s[id]
	if !ok {
		return "", nil, fmt.Errorf("no %s: %w", id, os.ErrNotExist)
	}
	return id + ".gpx", []byte(b), nil
}

// A client builds a course's world where it draws it, from the GPX the
// core gives it.
func TestGetCourseFile(t *testing.T) {
	ctx := context.Background()
	srv, err := NewServer(telemetry.NewHub(), Services{CourseFiles: stubFiles{"alpe": "<gpx/>"}}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)
	r, err := cl.GetCourseFile(ctx, &pb.GetCourseFileRequest{CourseId: "alpe"})
	if err != nil || r.Name != "alpe.gpx" || string(r.Data) != "<gpx/>" {
		t.Errorf("alpe: %v, %v", r, err)
	}
	if _, err := cl.GetCourseFile(ctx, &pb.GetCourseFileRequest{CourseId: "posbank"}); status.Code(err) != codes.NotFound {
		t.Errorf("a course without a file: %v", err)
	}
	if _, err := start(t, telemetry.NewHub(), nil, testToken).GetCourseFile(ctx, &pb.GetCourseFileRequest{CourseId: "alpe"}); status.Code(err) != codes.NotFound {
		t.Errorf("no course files: %v", err)
	}
}
