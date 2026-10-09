package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/telemetry"
)

func TestExportActivity(t *testing.T) {
	dir := t.TempDir()
	// A real recording (the replay fixture), and a large one to need
	// several chunks; its content doesn't matter for the transfer.
	fitFile, err := os.ReadFile("../replay/testdata/2026-10-07-082817.fit")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "2026-10-07-082817.fit"), fitFile, 0o600)
	big := make([]byte, 3*exportChunk+123)
	rand.Read(big)
	os.WriteFile(filepath.Join(dir, "2026-10-08-100000.fit"), big, 0o600)

	srv, err := NewServer(telemetry.NewHub(), Services{Activities: &record.Catalog{Dir: dir}}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	cl := dialServer(t, srv, testToken)
	ctx := context.Background()

	list, err := cl.ListActivities(ctx, &pb.ListActivitiesRequest{})
	if err != nil || len(list.GetActivities()) != 2 {
		t.Fatalf("%v, %v", list, err)
	}
	ride := list.GetActivities()[1]
	if ride.GetName() != "2026-10-07-082817.fit" || ride.GetError() != "" || !ride.GetVirtual() || ride.GetLaps() != 3 || ride.GetDistanceM() < 1500 {
		t.Errorf("listed %v", ride)
	}
	if list.GetActivities()[0].GetError() == "" {
		t.Error("the random file should list with an error")
	}

	for name, want := range map[string][]byte{"2026-10-07-082817.fit": fitFile, "2026-10-08-100000.fit": big} {
		var buf bytes.Buffer
		n, err := DownloadActivity(ctx, cl, name, &buf)
		if err != nil || n != int64(len(want)) || !bytes.Equal(buf.Bytes(), want) {
			t.Errorf("%s: %d bytes, %v", name, n, err)
		}
	}
	for _, bad := range []string{"../profile.json", "nope.fit"} {
		if _, err := DownloadActivity(ctx, cl, bad, &bytes.Buffer{}); status.Code(err) != codes.NotFound {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
