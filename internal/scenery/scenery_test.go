package scenery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/course"
)

const lat0, lon0 = 52.0, 5.0

// straightCourse runs 1 km due north from lat0, lon0.
func straightCourse(t *testing.T) *course.Course {
	t.Helper()
	var pts []course.Point
	for i := 0; i <= 100; i++ {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 5})
	}
	c, err := course.New("north", "North", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rect is a closed ring in metres east/north of the start.
func rect(x0, y0, x1, y1 float64) []LatLon {
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	p := func(x, y float64) LatLon { return LatLon{Lat: lat0 + y/111195, Lon: lon0 + x/kx} }
	return []LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
}

// fixture: forest on the left all along, farmland on the right with a
// small park in it from 400 to 600 m, a lake far right, and a house
// beside the road at 500 m.
func fixture() *Data {
	return &Data{Elements: []Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: rect(-200, -100, -5, 1100)},
		{Type: "way", ID: 2, Tags: map[string]string{"landuse": "farmland"}, Geometry: rect(5, -100, 40, 1100)},
		{Type: "way", ID: 3, Tags: map[string]string{"leisure": "park"}, Geometry: rect(5, 400, 30, 600)},
		{Type: "relation", ID: 4, Tags: map[string]string{"type": "multipolygon", "natural": "water"}, Members: []Member{
			{Type: "way", Role: "outer", Geometry: rect(45, -100, 300, 1100)},
		}},
		{Type: "way", ID: 5, Tags: map[string]string{"building": "house", "building:levels": "2"}, Geometry: rect(8, 490, 18, 506)},
		{Type: "way", ID: 6, Tags: map[string]string{"building": "barn"}, Geometry: rect(-2, 700, 6, 720)}, // on the road
	}}
}

func TestBuild(t *testing.T) {
	c := straightCourse(t)
	sc := Build(c, fixture())
	ele, _ := c.Profile()
	if len(sc.Land) != 4*len(ele) {
		t.Fatalf("%d land entries for %d samples", len(sc.Land), len(ele))
	}
	at := func(d float64) []Land {
		i := int(math.Round(d / c.Spacing))
		return sc.Land[4*i : 4*i+4]
	}
	want := []Land{LandForest, LandForest, LandFarmland, LandWater}
	if got := at(200); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("at 200 m: %v, want %v", got, want)
	}
	// The park lies inside the farmland: the smaller area wins.
	if got := at(500)[2]; got != LandMeadow {
		t.Errorf("right near at 500 m: %v, want the park (meadow)", got)
	}

	if len(sc.Buildings) != 2 {
		t.Fatalf("%d buildings, want 2: %+v", len(sc.Buildings), sc.Buildings)
	}
	h := sc.Buildings[0]
	if math.Abs(h.DistanceM-498) > 2 || math.Abs(h.OffsetM-13) > 1 || math.Abs(h.LengthM-16) > 1 || math.Abs(h.DepthM-10) > 1 {
		t.Errorf("house: %+v, want at 498 m, 13 m right, 16 long, 10 deep", h)
	}
	if h.Kind != KindHouse || h.HeightM != 9 {
		t.Errorf("house: kind %v height %.0f, want a house of 9 m (2 levels and a roof)", h.Kind, h.HeightM)
	}
	if b := sc.Buildings[1]; b.Kind != KindBarn || math.Abs(b.OffsetM)-b.DepthM/2 < buildingClearM-0.01 {
		t.Errorf("barn on the road: %+v, want it moved clear of the road", b)
	}
}

func TestQueryCoversRoute(t *testing.T) {
	q := Query(straightCourse(t))
	if !strings.Contains(q, `way["landuse"]`) || !strings.Contains(q, `way["building"]`) || !strings.HasSuffix(q, "out geom;\n") {
		t.Errorf("query lacks parts:\n%s", q)
	}
	// 1 km: one stretch.
	if n := strings.Count(q, `way["building"]`); n != 1 {
		t.Errorf("%d stretches for 1 km, want 1", n)
	}
}

func TestStoreFetchesOnceAndCaches(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "osscycler") {
			t.Errorf("User-Agent %q", ua)
		}
		if !strings.Contains(r.FormValue("data"), "out geom") {
			t.Errorf("no query in the request")
		}
		w.Write(raw)
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := straightCourse(t)
	cfg := Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL, UserAgent: "osscycler-test"}

	s := NewStore(cfg)
	if s.Get(c.ID) != nil {
		t.Fatal("scenery before running")
	}
	s.Run(context.Background(), []*course.Course{c})
	if s.Get(c.ID) == nil || calls.Load() != 1 {
		t.Fatalf("after a run: scenery %v, %d calls", s.Get(c.ID) != nil, calls.Load())
	}
	files, _ := filepath.Glob(filepath.Join(dir, "north-*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files: %v", files)
	}

	// A new store reads the cache without asking the server, even with
	// fetching off.
	cfg.Fetch = false
	s2 := NewStore(cfg)
	s2.Run(context.Background(), []*course.Course{c})
	if s2.Get(c.ID) == nil || calls.Load() != 1 {
		t.Errorf("from cache: scenery %v, %d calls", s2.Get(c.ID) != nil, calls.Load())
	}

	// A stale file for the course (an older GPX) is replaced on the next fetch.
	stale := filepath.Join(dir, "north-0123456789ab.json")
	os.WriteFile(stale, raw, 0o600)
	os.Remove(files[0])
	cfg.Fetch = true
	NewStore(cfg).Run(context.Background(), []*course.Course{c})
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale cache file kept")
	}
}

func TestStoreRetriesThenGivesUp(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Overpass reports overload as a remark in a 200 answer.
		w.Write([]byte(`{"elements":[],"remark":"runtime error: Query timed out"}`))
	}))
	defer srv.Close()
	saved := retryAfter
	retryAfter = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { retryAfter = saved }()

	dir := t.TempDir()
	s := NewStore(Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL})
	c := straightCourse(t)
	s.Run(context.Background(), []*course.Course{c})
	if s.Get(c.ID) != nil || calls.Load() != 3 {
		t.Errorf("scenery %v after %d calls, want none after 3", s.Get(c.ID) != nil, calls.Load())
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Errorf("a failed answer was cached: %v", files)
	}
}

func TestStorePendingWhileFetching(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write(raw)
	}))
	defer srv.Close()
	c := straightCourse(t)
	s := NewStore(Config{CacheDir: t.TempDir(), Fetch: true, Endpoint: srv.URL})
	if s.Pending(c.ID) {
		t.Error("pending before running")
	}
	done := make(chan struct{})
	go func() {
		s.Run(context.Background(), []*course.Course{c})
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !s.Pending(c.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.Pending(c.ID) || s.Get(c.ID) != nil {
		t.Errorf("while fetching: pending %v, scenery %v", s.Pending(c.ID), s.Get(c.ID) != nil)
	}
	close(release)
	<-done
	if s.Pending(c.ID) || s.Get(c.ID) == nil {
		t.Errorf("after fetching: pending %v, scenery %v", s.Pending(c.ID), s.Get(c.ID) != nil)
	}

	// Giving up ends it too.
	saved := retryAfter
	retryAfter = nil
	defer func() { retryAfter = saved }()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusGatewayTimeout)
	}))
	defer bad.Close()
	s2 := NewStore(Config{CacheDir: t.TempDir(), Fetch: true, Endpoint: bad.URL})
	s2.Run(context.Background(), []*course.Course{c})
	if s2.Pending(c.ID) {
		t.Error("still pending after giving up")
	}
}
