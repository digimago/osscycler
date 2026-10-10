package scenery

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

// The coastline is a basemap: each whole-degree tile is asked for once,
// kept simplified, and shared by every course; the sea's distance comes
// from it.
func TestCoastBasemap(t *testing.T) {
	var asked atomic.Int32
	// A straight coast along lon 4.5, from lat 52.0 to 52.6, in many points.
	var pts []string
	for i := 0; i <= 600; i++ {
		pts = append(pts, fmt.Sprintf(`{"lat":%.4f,"lon":4.5}`, 52+float64(i)*0.001))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		q := r.FormValue("data")
		if strings.Contains(q, "(52,4,53,5)") {
			fmt.Fprintf(w, `{"elements":[{"type":"way","id":1,"geometry":[%s]}]}`, strings.Join(pts, ","))
			return
		}
		fmt.Fprint(w, `{"elements":[]}`)
	}))
	defer srv.Close()
	dir := t.TempDir()
	s := NewStore(Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL, UserAgent: "test"}, nil)

	// A course 3 km east of the coast, inland.
	kx := 111195 * math.Cos(52.3*math.Pi/180)
	var cp []course.Point
	for i := 0; i <= 50; i++ {
		cp = append(cp, course.Point{Lat: 52.3 + float64(i)*0.0005, Lon: 4.5 + 3000/kx, Ele: 5})
	}
	c, err := course.New("dunes", "Dunes", cp)
	if err != nil {
		t.Fatal(err)
	}
	co, err := s.Coast(context.Background(), c, true)
	if err != nil {
		t.Fatal(err)
	}
	first := asked.Load()
	if first == 0 {
		t.Fatal("nothing asked")
	}
	if d := co.Distance(52.31, 4.5+3000/kx); math.Abs(d-3000) > 40 {
		t.Errorf("3 km inland: %.0f m from the sea", d)
	}
	if d := co.Distance(52.31, 4.5+12000/kx); !math.IsInf(d, 1) {
		t.Errorf("12 km inland: %.0f m, want beyond reach", d)
	}
	// Another course in the same tiles: from the cache, nothing asked.
	if _, err := s.Coast(context.Background(), c, true); err != nil {
		t.Fatal(err)
	}
	if n := asked.Load(); n != first {
		t.Errorf("the tiles asked for again: %d queries, then %d", first, n)
	}
	// Kept simplified: a straight coast is two points.
	raw, _ := s.coastTile(context.Background(), 52, 4, false)
	if raw == nil || len(raw.Lines) != 1 || len(raw.Lines[0]) != 2 {
		t.Errorf("the cached coast: %+v", raw)
	}
	// With fetching off and nothing cached, the tile is missing and said so.
	s2 := NewStore(Config{CacheDir: t.TempDir(), Endpoint: srv.URL}, nil)
	if co, err := s2.Coast(context.Background(), c, true); err == nil || !math.IsInf(co.Distance(52.31, 4.5), 1) {
		t.Errorf("uncached, fetching off: err %v", err)
	}
}
