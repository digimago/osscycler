package dem

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// rgeAltiServer serves fixture as every tile, and fails requests that
// aren't for the tile whose BBOX starts with corner, at 5 m.
func rgeAltiServer(t *testing.T, fixture, corner string, calls *atomic.Int32) *httptest.Server {
	tile, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("LAYERS") != rgeAltiLayer || q.Get("CRS") != "EPSG:2154" || q.Get("WIDTH") != "200" ||
			!strings.HasPrefix(q.Get("BBOX"), corner+",") {
			http.Error(w, "unexpected "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/geotiff")
		w.Write(tile)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRGEAltiFetchesOnceAndCaches(t *testing.T) {
	// A real RGE ALTI tile from the Géoplateforme's WMS, 5 m cells
	// (uncompressed float32): the start of the Route des Américains course
	// in La Bresse, 988800-989000 × 6774200-6774400.
	var calls atomic.Int32
	srv := rgeAltiServer(t, "testdata/rgealti-labresse-5m.tif", "988000,6774000", &calls)
	r := &RGEAlti{CacheDir: t.TempDir(), Fetch: true, CellM: 5, Endpoint: srv.URL}
	pt := []LatLon{{48.00510, 6.87651}}
	m, err := r.Load(context.Background(), pt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1", calls.Load())
	}
	// IGN's altimetry service gives 636.1 m there.
	if z, ok := m.Elevation(48.00510, 6.87651); !ok || z < 635.5 || z > 636.7 {
		t.Errorf("Elevation = %v, %v, want 636.1", z, ok)
	}

	r.Fetch = false
	if _, err := r.Load(context.Background(), pt, 10); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("cached tile fetched again")
	}
}

func TestRGEAltiOutside(t *testing.T) {
	r := &RGEAlti{CacheDir: t.TempDir()}
	if _, err := r.Load(context.Background(), []LatLon{{-25.5, -20}}, 100); err != ErrOutside {
		t.Errorf("South Atlantic: err %v, want ErrOutside", err)
	}
	// Inside Lambert-93's box but past France's border (Frankfurt): the
	// service answers with nodata.
	var calls atomic.Int32
	srv := rgeAltiServer(t, "testdata/rgealti-nodata-5m.tif", fmtTile(50.11, 8.68), &calls)
	r = &RGEAlti{CacheDir: t.TempDir(), Fetch: true, CellM: 5, Endpoint: srv.URL}
	if _, err := r.Load(context.Background(), []LatLon{{50.11, 8.68}}, 10); !errors.Is(err, ErrOutside) {
		t.Errorf("Frankfurt: err %v, want ErrOutside", err)
	}
	if calls.Load() == 0 {
		t.Errorf("no tile asked for")
	}
}

// fmtTile is the corner of the Lambert-93 tile lat, lon is in, as BBOX
// starts.
func fmtTile(lat, lon float64) string {
	x, y := ToLambert93(lat, lon)
	return fmt.Sprintf("%d,%d", int(math.Floor(x/tileM))*tileM, int(math.Floor(y/tileM))*tileM)
}
