package scenery

import (
	"fmt"
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
)

// at is a point in metres east/north of lat0, lon0.
func at(x, y float64) LatLon {
	kx := 111195 * math.Cos(lat0*math.Pi/180)
	return LatLon{Lat: lat0 + y/111195, Lon: lon0 + x/kx}
}

// way is a highway (or other) way through points in metres, with node IDs
// from ids.
func way(id int64, tags map[string]string, ids []int64, pts ...[2]float64) Element {
	el := Element{Type: "way", ID: id, Tags: tags, Nodes: ids}
	for _, p := range pts {
		el.Geometry = append(el.Geometry, at(p[0], p[1]))
	}
	return el
}

func node(id int64, tags map[string]string, x, y float64) Element {
	p := at(x, y)
	return Element{Type: "node", ID: id, Tags: tags, Lat: p.Lat, Lon: p.Lon}
}

// lCourse runs 1 km north, then 1 km east.
func lCourse(t *testing.T) *course.Course {
	t.Helper()
	var pts []course.Point
	for i := 0; i <= 100; i++ {
		p := at(0, float64(i)*10)
		pts = append(pts, course.Point{Lat: p.Lat, Lon: p.Lon, Ele: 5})
	}
	for i := 1; i <= 100; i++ {
		p := at(float64(i)*10, 1000)
		pts = append(pts, course.Point{Lat: p.Lat, Lon: p.Lon, Ele: 5})
	}
	c, err := course.New("l", "L", pts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHumanTouches(t *testing.T) {
	c := lCourse(t)
	hw := func(class, name string) map[string]string { return map[string]string{"highway": class, "name": name} }
	d := &Data{Elements: []Element{
		// The route: Noordweg north, which goes on past the corner; there
		// it turns onto Oostweg, which also runs west from the corner.
		way(1, hw("residential", "Noordweg"), []int64{10, 11, 12}, [2]float64{0, -50}, [2]float64{0, 1000}, [2]float64{0, 1300}),
		way(2, hw("residential", "Oostweg"), []int64{20, 11, 21, 22, 23}, [2]float64{-300, 1000}, [2]float64{0, 1000},
			[2]float64{500, 1000}, [2]float64{700, 1000}, [2]float64{1100, 1000}),
		// A side street at 500 m: not shown.
		way(3, hw("residential", "Zijstraat"), []int64{30, 31}, [2]float64{0, 500}, [2]float64{-200, 500}),
		// A primary road crossed at 1500 m along the course, on the level.
		way(4, hw("primary", "N1"), []int64{40, 21, 41}, [2]float64{500, 800}, [2]float64{500, 1000}, [2]float64{500, 1200}),
		// A motorway at 1700 m: never a junction.
		way(5, hw("motorway", "A1"), []int64{50, 22, 51}, [2]float64{700, 800}, [2]float64{700, 1000}, [2]float64{700, 1200}),
		// A public car park right of the road from 300 to 360 m, entered
		// by a service road from 310 m; a private one and a small one.
		way(6, map[string]string{"amenity": "parking", "parking": "surface", "name": "Bos"}, []int64{60, 61, 62, 63, 60},
			[2]float64{10, 300}, [2]float64{40, 300}, [2]float64{40, 360}, [2]float64{10, 360}, [2]float64{10, 300}),
		way(7, hw("service", ""), []int64{70, 71}, [2]float64{0, 310}, [2]float64{25, 330}),
		way(8, map[string]string{"amenity": "parking", "access": "private"}, []int64{80, 81, 82, 83, 80},
			[2]float64{-50, 700}, [2]float64{-10, 700}, [2]float64{-10, 760}, [2]float64{-50, 760}, [2]float64{-50, 700}),
		way(9, map[string]string{"amenity": "parking"}, []int64{90, 91, 92, 93, 90},
			[2]float64{10, 800}, [2]float64{20, 800}, [2]float64{20, 820}, [2]float64{10, 820}, [2]float64{10, 800}),
		// Into Dorp at 1200 m along (its centre lies ahead), out again at 1900.
		node(100, map[string]string{"traffic_sign": "city_limit", "name": "Dorp"}, 200, 1005),
		node(101, map[string]string{"traffic_sign": "city_limit", "name": "Dorp"}, 900, 1005),
		node(102, map[string]string{"place": "village", "name": "Dorp"}, 550, 1100),
	}}
	sc := Build(c, d)

	var turn, crossing, entrance *Junction
	for i, j := range sc.Junctions {
		switch j.Kind {
		case JunctionTurn:
			turn = &sc.Junctions[i]
		case JunctionCrossing:
			crossing = &sc.Junctions[i]
		case JunctionEntrance:
			entrance = &sc.Junctions[i]
		}
	}
	if len(sc.Junctions) != 3 || turn == nil || crossing == nil || entrance == nil {
		t.Fatalf("junctions %+v", sc.Junctions)
	}
	bearings := func(j *Junction) []float64 {
		var out []float64
		for _, b := range j.Branches {
			out = append(out, math.Round(b.BearingDeg))
		}
		return out
	}
	near := func(got, want, tol float64) bool { return math.Abs(got-want) <= tol }
	// At the corner Noordweg goes on north and Oostweg runs west.
	if b := bearings(turn); !near(turn.DistanceM, 1000, 10) || len(b) != 2 || !near(b[0], 0, 2) || !near(b[1], 270, 2) {
		t.Errorf("turn at %.0f m, bearings %v", turn.DistanceM, b)
	}
	if b := bearings(crossing); !near(crossing.DistanceM, 1500, 10) || len(b) != 2 || !near(b[0], 0, 2) || !near(b[1], 180, 2) {
		t.Errorf("crossing at %.0f m, bearings %v", crossing.DistanceM, b)
	}
	if b := bearings(entrance); !near(entrance.DistanceM, 310, 10) || len(b) != 1 || !near(b[0], 51, 3) {
		t.Errorf("entrance at %.0f m, bearings %v", entrance.DistanceM, b)
	}
	if len(sc.Parking) != 1 {
		t.Fatalf("car parks %+v", sc.Parking)
	}
	if p := sc.Parking[0]; p.Name != "Bos" || !near(p.DistanceM, 330, 2) || !near(p.OffsetM, 25, 2) || !near(p.LengthM, 60, 2) || !near(p.DepthM, 30, 2) {
		t.Errorf("car park %+v", p)
	}
	if len(sc.Signs) != 1 || sc.Signs[0].Name != "Dorp" || !near(sc.Signs[0].DistanceM, 1200, 10) {
		t.Errorf("signs %+v", sc.Signs)
	}

	// For the 3D world: the branches' lines and the car park's outline.
	for _, j := range sc.Junctions {
		for _, b := range j.Branches {
			l := b.Line
			if len(l) < 2 {
				t.Fatalf("branch at %.0f m without a line", j.DistanceM)
			}
			end := math.Hypot(l[len(l)-1][0]-l[0][0], l[len(l)-1][1]-l[0][1])
			if !near(end, b.LengthM, 1) || !near(bearing(l[0][0], l[0][1], l[len(l)-1][0], l[len(l)-1][1]), b.BearingDeg, 3) {
				t.Errorf("branch at %.0f m: line ends %.0f m out at %.0f°, want %.0f m at %.0f°", j.DistanceM, end,
					bearing(l[0][0], l[0][1], l[len(l)-1][0], l[len(l)-1][1]), b.LengthM, b.BearingDeg)
			}
		}
	}
	if o := sc.Parking[0].Outline; len(o) != 4 || !near(o[0][0], 10, 0.5) || !near(o[2][1], 360, 0.5) || sc.Parking[0].Surface != SurfaceAsphalt {
		t.Errorf("car park outline %v, surface %v", o, sc.Parking[0].Surface)
	}
}

func TestRouteRoads(t *testing.T) {
	c := lCourse(t)
	d := &Data{Elements: []Element{
		// North on klinkers, 6 m wide, a sidewalk on the right and a cycle
		// lane on the left, drawn the way the route rides.
		way(1, map[string]string{"highway": "residential", "name": "Noordweg", "surface": "paving_stones", "width": "6",
			"sidewalk": "right", "cycleway:left": "lane"}, []int64{10, 11}, [2]float64{0, -50}, [2]float64{0, 1000}),
		// Then east on a road drawn westwards: its "left" sidewalk is on
		// the route's right; its embankment carries over.
		way(2, map[string]string{"highway": "tertiary", "name": "Oostweg", "sidewalk": "left", "embankment": "yes"},
			[]int64{21, 11}, [2]float64{1100, 1000}, [2]float64{0, 1000}),
	}}
	rs := Build(c, d).Roads
	if len(rs) != 2 {
		t.Fatalf("%d stretches, want 2: %+v", len(rs), rs)
	}
	n, o := rs[0], rs[1]
	if n.Name != "Noordweg" || n.FromM != 0 || math.Abs(n.ToM-1000) > 10 || n.WidthM != 6 || n.Surface != SurfacePaving ||
		n.Sidewalk != [2]bool{false, true} || n.CycleLane != [2]bool{true, false} || n.Embankment {
		t.Errorf("Noordweg: %+v", n)
	}
	if o.Name != "Oostweg" || o.FromM != n.ToM || o.ToM != c.Distance || o.WidthM != 6.5 || o.Surface != SurfaceAsphalt ||
		o.Sidewalk != [2]bool{false, true} || !o.Embankment {
		t.Errorf("Oostweg: %+v", o)
	}
	// The same roads as the 3D world's ways, each by its own direction,
	// and the route on them.
	sc := Build(c, d)
	if len(sc.Ways) != 2 || sc.Ways[1].Sidewalk != [2]bool{true, false} || sc.Ways[0].CycleLane != [2]bool{true, false} || len(sc.Ways[1].Line) != 2 {
		t.Fatalf("ways %+v", sc.Ways)
	}
	on := func(d float64) int { return sc.RouteOn[int(math.Round(d/sc.RouteStepM))] }
	if on(500) != 0 || on(1500) != 1 || on(1000) < 0 {
		t.Errorf("the route on ways %d, %d, %d at 500, 1000, 1500 m; want 0, a road, 1", on(500), on(1000), on(1500))
	}
}

func TestSidewalksFromLandUse(t *testing.T) {
	// An untagged street, built up on the left from 300 to 600 m: a
	// sidewalk there on the left only. Beyond the map's roads: a path.
	c := straightCourse(t)
	d := &Data{Elements: []Element{
		way(1, map[string]string{"highway": "residential"}, []int64{1, 2}, [2]float64{0, -50}, [2]float64{0, 800}),
		{Type: "way", ID: 9, Tags: map[string]string{"landuse": "residential"}, Geometry: rect(-120, 300, -4, 600)},
	}}
	rs := Build(c, d).Roads
	var got []string
	for _, r := range rs {
		got = append(got, fmt.Sprintf("%.0f-%.0f %s %v", r.FromM, r.ToM, r.Class, r.Sidewalk))
	}
	want := "[0-300 residential [false false] 300-600 residential [true false] 600-800 residential [false false] 800-1000  [false false]]"
	if g := fmt.Sprint(got); g != want {
		// Boundaries may move a sample (5 m) with the smoothing.
		t.Logf("stretches: %s", g)
		if len(rs) != 4 || !rs[1].Sidewalk[0] || rs[1].Sidewalk[1] || rs[0].Sidewalk[0] || rs[2].Sidewalk[0] ||
			math.Abs(rs[1].FromM-300) > 20 || math.Abs(rs[1].ToM-600) > 20 || rs[3].Class != "" || rs[3].WidthM != pathWidthM {
			t.Errorf("want %s", want)
		}
	}
}

func TestTurnAtATJunction(t *testing.T) {
	// The route turns right off a road that goes on north under another
	// name, keeping its own road's name (as the Beekhuizenseweg at the
	// Posbank): a junction, the road going on drawn ahead.
	c := lCourse(t)
	d := &Data{Elements: []Element{
		way(1, map[string]string{"highway": "unclassified", "name": "Weg"}, []int64{10, 11, 12},
			[2]float64{0, -50}, [2]float64{0, 1000}, [2]float64{1100, 1000}),
		way(2, map[string]string{"highway": "unclassified", "name": "Doorweg"}, []int64{11, 20}, [2]float64{0, 1000}, [2]float64{0, 1300}),
		// A side road off the straight: still not shown.
		way(3, map[string]string{"highway": "residential", "name": "Zijweg"}, []int64{30, 31}, [2]float64{0, 400}, [2]float64{-200, 400}),
	}}
	js := Build(c, d).Junctions
	if len(js) != 1 || js[0].Kind != JunctionTurn || math.Abs(js[0].DistanceM-1000) > 10 {
		t.Fatalf("junctions %+v, want one turn at 1000 m", js)
	}
	if b := js[0].Branches; len(b) != 1 || angleDiff(b[0].BearingDeg, 0) > 3 {
		t.Errorf("branches %+v, want the road going on north", b)
	}
}

func TestRoadWidth(t *testing.T) {
	for _, tc := range []struct {
		class string
		tags  map[string]string
		want  float64
	}{
		{"secondary", nil, 7},
		{"secondary", map[string]string{"oneway": "yes"}, 3.5}, // a dual carriageway's half
		{"residential", map[string]string{"oneway": "-1"}, 3.5},
		{"primary", map[string]string{"oneway": "yes", "lanes": "2"}, 6.4},
		{"motorway", map[string]string{"oneway": "yes"}, 11},
		{"secondary", map[string]string{"oneway": "yes", "width": "5"}, 5},
	} {
		if w := roadWidth(tc.class, tc.tags); w != tc.want {
			t.Errorf("%s %v: %.1f m, want %.1f", tc.class, tc.tags, w, tc.want)
		}
	}
}

func TestNoSidewalkOnADivideOrWhereWalkersMayNot(t *testing.T) {
	// All built up. A main road north (foot=no); 9 m east of it a service
	// road, untagged; a lone street 100 m east.
	built := func(x, y float64) bool { return true }
	mk := func(x float64, class string, tags map[string]string) *road {
		return &road{x: []float64{x, x}, y: []float64{0, 200}, class: class, widthM: roadWidth(class, tags), tags: tags}
	}
	main := mk(0, "secondary", map[string]string{"foot": "no"})
	service := mk(9, "residential", map[string]string{})
	street := mk(100, "residential", map[string]string{})
	ws := ways([]*road{main, service, street}, built)
	// Going north, left is west.
	if ws[0].Sidewalk != [2]bool{false, false} {
		t.Errorf("main road (foot=no) sidewalks %v, want none", ws[0].Sidewalk)
	}
	if ws[1].Sidewalk != [2]bool{false, true} {
		t.Errorf("service road sidewalks %v, want only on the side away from the main road", ws[1].Sidewalk)
	}
	if ws[2].Sidewalk != [2]bool{true, true} {
		t.Errorf("lone street sidewalks %v, want both", ws[2].Sidewalk)
	}
}
