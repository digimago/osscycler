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
	"sync"
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
	c := straightCourse(t)
	q := queries(c, []float64{0, c.Distance})[0]
	for _, part := range []string{`way["landuse"]`, `way["building"]`, `way["highway"~`, `node["place"`, "[timeout:90]", "out geom;\n"} {
		if !strings.Contains(q, part) {
			t.Errorf("query lacks %s:\n%s", part, q)
		}
	}
	// A straight road north: one column of tiles per layer, one box each.
	if n := strings.Count(q, `way["building"]`); n != 1 {
		t.Errorf("%d building boxes for 1 km due north, want 1:\n%s", n, q)
	}
}

// outAndBack runs 2 km north and back on the same road.
func outAndBack(t *testing.T) *course.Course {
	t.Helper()
	var pts []course.Point
	for i := 0; i <= 200; i++ {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 5})
	}
	for i := 199; i >= 0; i-- {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0 + 1e-6, Ele: 5})
	}
	c, err := course.New("back", "Back", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQueriesAskForATileOnce(t *testing.T) {
	c := outAndBack(t)
	qs := queries(c, []float64{0, c.Distance / 2, c.Distance})
	if !strings.Contains(qs[0], `way["building"]`) {
		t.Fatalf("the way out asks for no buildings:\n%s", qs[0])
	}
	// The way back passes the same tiles: only its places remain.
	for _, part := range []string{`["landuse"]`, `["building"]`, `["highway"`, `["amenity"`} {
		if strings.Contains(qs[1], part) {
			t.Errorf("the way back asks again for %s:\n%s", part, qs[1])
		}
	}
	if !strings.Contains(qs[1], `node["place"`) {
		t.Error("the way back lost its places")
	}
}

func TestTilesNearReachEveryPoint(t *testing.T) {
	const margin = 300.0
	mLon := 111195 * math.Cos(lat0*math.Pi/180)
	ts := tilesNear(lat0+0.0012, lon0+0.0031, margin, tileLat, tileLon)
	in := map[tile]bool{}
	for _, tl := range ts {
		in[tl] = true
	}
	for a := 0.0; a < 2*math.Pi; a += 0.05 {
		for r := 0.0; r <= margin; r += 25 {
			lat := lat0 + 0.0012 + r*math.Sin(a)/111195
			lon := lon0 + 0.0031 + r*math.Cos(a)/mLon
			if tl := (tile{int(math.Floor(lon / tileLon)), int(math.Floor(lat / tileLat))}); !in[tl] {
				t.Fatalf("point %.0f m away at %.2f rad lies in tile %v, not listed", r, a, tl)
			}
		}
	}
	if len(ts) > 16 {
		t.Errorf("%d tiles for a 600 m circle in 278 m tiles, want at most 16", len(ts))
	}
}

func TestBlocksCoverTheirTilesExactly(t *testing.T) {
	// An L: a column of 3 and a row of 4 sharing a corner, and a loose one.
	ts := []tile{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {2, 0}, {3, 0}, {7, 7}}
	bs := blocks(ts)
	covered := map[tile]int{}
	for _, b := range bs {
		for i := b.i0; i <= b.i1; i++ {
			for j := b.j0; j <= b.j1; j++ {
				covered[tile{i, j}]++
			}
		}
	}
	for _, tl := range ts {
		if covered[tl] != 1 {
			t.Errorf("tile %v covered %d times", tl, covered[tl])
		}
	}
	if len(covered) != len(ts) || len(bs) > 3 {
		t.Errorf("%d blocks covering %d tiles, want at most 3 covering %d", len(bs), len(covered), len(ts))
	}
}

// longCourse runs 12 km due north: two stretches, 0-5 km and 5-12 km.
func longCourse(t *testing.T) *course.Course {
	t.Helper()
	var pts []course.Point
	for i := 0; i <= 1200; i++ {
		pts = append(pts, course.Point{Lat: lat0 + float64(i)*10/111195, Lon: lon0, Ele: 5})
	}
	c, err := course.New("long", "Long", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// overpass is a test server answering with the fixture, and the queries
// it was asked.
type overpass struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
}

func newOverpass(t *testing.T, answer func() []byte) *overpass {
	o := &overpass{}
	o.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "osscycler") {
			t.Errorf("User-Agent %q", ua)
		}
		o.mu.Lock()
		o.queries = append(o.queries, r.FormValue("data"))
		o.mu.Unlock()
		w.Write(answer())
	}))
	t.Cleanup(o.Close)
	return o
}

func (o *overpass) asked() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.queries...)
}

func fixtureJSON() []byte {
	raw, _ := json.Marshal(fixture())
	return raw
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// runStore runs s until the test ends.
func runStore(t *testing.T, s *Store) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestStoreFetchesOnlyWhatIsRiddenFromTheRider(t *testing.T) {
	srv := newOverpass(t, fixtureJSON)
	dir := t.TempDir()
	c := longCourse(t)
	cfg := Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL, UserAgent: "osscycler-test"}

	s := NewStore(cfg, []*course.Course{c})
	runStore(t, s)
	time.Sleep(20 * time.Millisecond)
	if n := len(srv.asked()); n != 0 || s.Get(c.ID) != nil || s.Pending(c.ID) {
		t.Fatalf("before a ride: %d fetches, scenery %v, pending %v", n, s.Get(c.ID) != nil, s.Pending(c.ID))
	}
	// A ride from 6 km: the second stretch first, then the first.
	s.Want(c.ID, 6000)
	waitFor(t, "both stretches", func() bool { return !s.Pending(c.ID) })
	q := srv.asked()
	if e := s.courses[c.ID]; len(q) != 2 || q[0] != e.parts[1].query || q[1] != e.parts[0].query {
		t.Fatalf("%d queries, want the stretch from 5 km, then the one from 0", len(q))
	}
	if s.Get(c.ID) == nil {
		t.Fatal("no scenery after fetching")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "long-*.json"))
	if len(files) != 2 {
		t.Fatalf("cache files: %v", files)
	}
	// Wanting a complete course fetches nothing.
	s.Want(c.ID, 0)
	time.Sleep(20 * time.Millisecond)
	if n := len(srv.asked()); n != 2 || s.Pending(c.ID) {
		t.Errorf("complete course wanted again: %d fetches, pending %v", n, s.Pending(c.ID))
	}

	// A new store reads the cache without asking the server, even with
	// fetching off.
	cfg.Fetch = false
	s2 := NewStore(cfg, []*course.Course{c})
	s2.loadCache(context.Background())
	if s2.Get(c.ID) == nil || len(srv.asked()) != 2 {
		t.Errorf("from cache: scenery %v, %d fetches", s2.Get(c.ID) != nil, len(srv.asked()))
	}
}

func TestStoreReplacesStaleCacheFiles(t *testing.T) {
	srv := newOverpass(t, fixtureJSON)
	dir := t.TempDir()
	c := longCourse(t)
	stale := []string{
		"long-0123456789ab.json",   // the whole course, from before stretches
		"long-0-0123456789ab.json", // stretch 0 of an older GPX
		"long-7-0123456789ab.json", // a stretch the course no longer has
	}
	other := "longer-0-0123456789ab.json" // another course's
	for _, name := range append(stale, other) {
		os.WriteFile(filepath.Join(dir, name), fixtureJSON(), 0o600)
	}
	s := NewStore(Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL, UserAgent: "osscycler-test"}, nil)
	if _, err := s.Load(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for _, name := range stale {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s kept", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, other)); err != nil {
		t.Errorf("another course's file: %v", err)
	}
}

func TestStoreRetriesThenGivesUp(t *testing.T) {
	// Overpass reports overload as a remark in a 200 answer.
	srv := newOverpass(t, func() []byte { return []byte(`{"elements":[],"remark":"runtime error: Query timed out"}`) })
	saved := retryAfter
	retryAfter = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryAfter = saved }) // after the store stops

	dir := t.TempDir()
	c := straightCourse(t)
	s := NewStore(Config{CacheDir: dir, Fetch: true, Endpoint: srv.URL, UserAgent: "osscycler-test"}, []*course.Course{c})
	runStore(t, s)
	s.Want(c.ID, 0)
	waitFor(t, "giving up", func() bool { return !s.Pending(c.ID) })
	if s.Get(c.ID) != nil || len(srv.asked()) != 3 {
		t.Errorf("scenery %v after %d calls, want none after 3", s.Get(c.ID) != nil, len(srv.asked()))
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Errorf("a failed answer was cached: %v", files)
	}
	// Riding it again tries again.
	s.Want(c.ID, 0)
	waitFor(t, "another attempt", func() bool { return len(srv.asked()) > 3 })
}

func TestStorePendingWhileFetching(t *testing.T) {
	release := make(chan struct{})
	srv := newOverpass(t, func() []byte {
		<-release
		return fixtureJSON()
	})
	c := straightCourse(t)
	track := course.Tracks()[0]
	s := NewStore(Config{CacheDir: t.TempDir(), Fetch: true, Endpoint: srv.URL, UserAgent: "osscycler-test"}, []*course.Course{c, track})
	runStore(t, s)
	s.Want(c.ID, 0)
	s.Want(track.ID, 0)
	if !s.Pending(c.ID) || s.Get(c.ID) != nil {
		t.Errorf("while fetching: pending %v, scenery %v", s.Pending(c.ID), s.Get(c.ID) != nil)
	}
	if s.Pending(track.ID) {
		t.Error("a test track, which has no map, is pending")
	}
	close(release)
	waitFor(t, "the map data", func() bool { return !s.Pending(c.ID) })
	if s.Get(c.ID) == nil {
		t.Error("no scenery after fetching")
	}
}

func TestLoadForTheWorldBuilder(t *testing.T) {
	srv := newOverpass(t, fixtureJSON)
	dir := t.TempDir()
	c := longCourse(t)
	cfg := Config{CacheDir: dir, Endpoint: srv.URL, UserAgent: "osscycler-test"}
	if d, err := NewStore(cfg, nil).Load(context.Background(), c); err == nil || len(d.Elements) != 0 {
		t.Errorf("nothing cached, fetching off: %d elements, err %v", len(d.Elements), err)
	}
	cfg.Fetch = true
	d, err := NewStore(cfg, nil).Load(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	// Both stretches answer with the same elements: kept once.
	if len(d.Elements) != len(fixture().Elements) || len(srv.asked()) != 2 {
		t.Errorf("%d elements from %d fetches, want %d from 2", len(d.Elements), len(srv.asked()), len(fixture().Elements))
	}
	// The core's cache serves it next time.
	cfg.Fetch = false
	if d, err := NewStore(cfg, nil).Load(context.Background(), c); err != nil || len(d.Elements) != len(fixture().Elements) {
		t.Errorf("from the cache: %d elements, err %v", len(d.Elements), err)
	}
}

func TestParseSlotWait(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   time.Duration
		ok     bool
	}{
		{"Rate limit: 2\n2 slots available now.\nCurrently running queries", 0, true},
		{"Rate limit: 2\n1 slots available now.\nSlot available after: 2026-10-08T13:57:02Z, in 19 seconds.\n", 0, true},
		{"Rate limit: 2\nSlot available after: 2026-10-08T13:56:51Z, in 8 seconds.\nSlot available after: 2026-10-08T13:57:02Z, in 19 seconds.\n", 9 * time.Second, true},
		{"Rate limit: 2\nSlot available after: 2026-10-08T13:56:51Z, in -1 seconds.\n", time.Second, true},
		{"<html>not a status page</html>", 0, false},
	} {
		got, ok := parseSlotWait(tc.status)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: %v %v, want %v %v", tc.status, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFetchWaitsForASlot(t *testing.T) {
	var calls, statusCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/interpreter", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "<?xml version=\"1.0\"?>\nrate limited", http.StatusTooManyRequests)
			return
		}
		w.Write(fixtureJSON())
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		statusCalls.Add(1)
		fmt.Fprint(w, "Rate limit: 2\nSlot available after: 2026-10-08T13:56:51Z, in 0 seconds.\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := NewStore(Config{CacheDir: t.TempDir(), Fetch: true, Endpoint: srv.URL + "/api/interpreter", UserAgent: "osscycler-test"}, nil)
	start := time.Now()
	if _, err := s.Load(context.Background(), straightCourse(t)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || statusCalls.Load() != 1 {
		t.Errorf("%d queries, %d status checks; want 2 and 1", calls.Load(), statusCalls.Load())
	}
	if d := time.Since(start); d < time.Second || d > 5*time.Second {
		t.Errorf("waited %v for a slot free in 0 s, want about 1 s", d)
	}
}

func TestBandedContainsMatchesEveryEdge(t *testing.T) {
	// A ring with a wavy edge of many vertices and a hole: the banded test
	// must agree with counting every edge.
	var ring []LatLon
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	for k := 0; k <= 720; k++ {
		a := float64(k) / 720 * 2 * math.Pi
		r := 300 + 40*math.Sin(9*a)
		ring = append(ring, LatLon{Lat: lat0 + r*math.Sin(a)/111195, Lon: lon0 + r*math.Cos(a)/kx})
	}
	ring[len(ring)-1] = ring[0]
	d := &Data{Elements: []Element{{Type: "relation", ID: 1, Tags: map[string]string{"type": "multipolygon", "natural": "wood"},
		Members: []Member{{Type: "way", Role: "outer", Geometry: ring}, {Type: "way", Role: "inner", Geometry: rect(-50, -50, 50, 50)}}}}}
	p := polygons(straightCourse(t), d)[0]
	for x := -400.0; x <= 400; x += 7 {
		for y := -400.0; y <= 400; y += 7 {
			want := false
			for _, e := range p.edges {
				if (e[1] > y) != (e[3] > y) && x < e[0]+(y-e[1])*(e[2]-e[0])/(e[3]-e[1]) {
					want = !want
				}
			}
			if got := p.contains(x, y); got != want {
				t.Fatalf("at %v, %v: banded %v, every edge %v", x, y, got, want)
			}
		}
	}
}
