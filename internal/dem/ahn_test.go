package dem

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAHNFetchesOnceAndCaches(t *testing.T) {
	tile, err := os.ReadFile("testdata/ahn-diepesteeg-5m.tif")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.RawQuery
		if !strings.Contains(q, "CoverageId=dtm_05m") || !strings.Contains(q, "subset=x(201000,202000)") {
			http.Error(w, "unexpected "+q, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/tiff")
		w.Write(tile)
	}))
	defer srv.Close()

	cache := t.TempDir()
	a := &AHN{CacheDir: cache, Fetch: true, CellM: 5, Endpoint: srv.URL}
	// A point at the foot of the Diepesteeg, in tile 201,448.
	pt := []LatLon{{52.02186267, 6.06002376}}
	m, err := a.Load(context.Background(), pt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1", calls.Load())
	}
	// The fixture covers 201000-201200 × 448300-448500 only.
	if z, ok := m.Elevation(52.02186267, 6.06002376); !ok || z < 15 || z > 40 {
		t.Errorf("Elevation = %v, %v", z, ok)
	}

	// Again with fetching off: from the cache, no request.
	a.Fetch = false
	if _, err := a.Load(context.Background(), pt, 10); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("cached tile fetched again")
	}
}

func TestAHNOutside(t *testing.T) {
	a := &AHN{CacheDir: t.TempDir()}
	if _, err := a.Load(context.Background(), []LatLon{{-25.5, -20}}, 100); err != ErrOutside {
		t.Errorf("South Atlantic: err %v, want ErrOutside", err)
	}
}
