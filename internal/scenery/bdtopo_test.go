package scenery

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

func bdCourse(t *testing.T) *course.Course {
	var pts []course.Point
	for i := range 101 {
		pts = append(pts, course.Point{Lat: 48 + float64(i)*10/111195, Lon: 6.9, Ele: 700})
	}
	c, err := course.New("bd", "BD", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// bdBuilding is a feature for a rectangle w × l metres whose south-west
// corner lies x east, y north of the course's start.
func bdBuilding(id string, x, y, w, l float64, props map[string]any) map[string]any {
	kx := 111320 * math.Cos(48*math.Pi/180)
	ll := func(dx, dy float64) []float64 { return []float64{6.9 + (x+dx)/kx, 48 + (y+dy)/111195, 700} }
	props["cleabs"] = id
	return map[string]any{
		"type": "Feature", "id": id,
		"geometry":   map[string]any{"type": "MultiPolygon", "coordinates": [][][][]float64{{{ll(0, 0), ll(w, 0), ll(w, l), ll(0, l), ll(0, 0)}}}},
		"properties": props,
	}
}

// BD TOPO's buildings come with their measured heights, capped where the
// lidar saw a tree, and their materials' colours; tiles are fetched once.
func TestBDTopoFootprints(t *testing.T) {
	feats := []any{
		// A two-storey house, measured: eave 6.1, ridge 9 m.
		bdBuilding("house", 10, 300, 10, 9, map[string]any{"hauteur": 6.1, "altitude_minimale_sol": 700.0, "altitude_maximale_toit": 709.0,
			"nombre_d_etages": 2, "usage_1": "Résidentiel", "materiaux_des_murs": "10", "materiaux_de_la_toiture": "20", "etat_de_l_objet": "En service"}),
		// One storey under a tree: eave 14.9 measured.
		bdBuilding("tree", 10, 400, 10, 8, map[string]any{"hauteur": 14.9, "altitude_minimale_sol": 700.0, "altitude_maximale_toit": 718.0,
			"nombre_d_etages": 1, "usage_1": "Résidentiel"}),
		// A shed with a tree over it.
		bdBuilding("shed", 10, 500, 4, 7, map[string]any{"hauteur": 3.0, "altitude_minimale_sol": 700.0, "altitude_maximale_toit": 726.0,
			"nombre_d_etages": 1, "usage_1": "Annexe"}),
		// Far from the route.
		bdBuilding("far", 500, 500, 10, 10, map[string]any{"hauteur": 6.0, "usage_1": "Résidentiel"}),
		// Planned, not built.
		bdBuilding("planned", 10, 600, 10, 10, map[string]any{"hauteur": 6.0, "usage_1": "Résidentiel", "etat_de_l_objet": "En projet"}),
	}
	body, _ := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("TYPENAMES") != bdTopoLayer {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()
	c := bdCourse(t)
	b := &BDTopo{CacheDir: t.TempDir(), Fetch: true, Endpoint: srv.URL}
	fps, err := b.Footprints(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	tiles := calls.Load()
	if len(fps) != 3 {
		t.Fatalf("%d footprints, want the house, the one under a tree and the shed", len(fps))
	}
	var house, tree, shed Footprint
	for _, fp := range fps {
		switch {
		case fp.Outline[0][1] < 350:
			house = fp
		case fp.Outline[0][1] < 450:
			tree = fp
		default:
			shed = fp
		}
	}
	if !house.FromLowest || house.WallM > 6.1 || house.WallM < 4.5 || math.Abs(house.WallM+house.RoofM-9) > 0.01 {
		t.Errorf("the house: walls %.1f, roof %.1f; want its 9 m ridge, the eave at most the measured 6.1", house.WallM, house.RoofM)
	}
	if house.WallColour == "" || house.RoofColour == "" {
		t.Errorf("the stone house under slate: colours %q, %q", house.WallColour, house.RoofColour)
	}
	if tree.WallM > 4.5 {
		t.Errorf("a one-storey house's walls %.1f m (the lidar saw a tree)", tree.WallM)
	}
	if shed.Kind != KindBarn || shed.WallM+shed.RoofM > 9 {
		t.Errorf("the shed: kind %v, %.1f m high", shed.Kind, shed.WallM+shed.RoofM)
	}

	// Again from the cache only.
	b.Fetch = false
	again, err := b.Footprints(context.Background(), c)
	if err != nil || len(again) != 3 || calls.Load() != tiles {
		t.Errorf("from the cache: %d footprints, %v, %d more requests", len(again), err, calls.Load()-tiles)
	}
	// Without the cache and fetching off: says so.
	b2 := &BDTopo{CacheDir: t.TempDir()}
	if _, err := b2.Footprints(context.Background(), c); err == nil {
		t.Error("no cache, no fetching: no error")
	}
}
