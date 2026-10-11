package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Faults marked on a new course (owner, 2026-10-11), each kept fixed.

// flat is a square of material m at height y, x and z from a to b.
func flat(m int, a, b float64, y float32) gltf.Primitive {
	return gltf.Primitive{Material: m,
		Positions: []float32{float32(a), y, float32(a), float32(b), y, float32(a), float32(b), y, float32(b), float32(a), y, float32(b)},
		Normals:   []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0},
		Indices:   []uint32{0, 2, 1, 0, 3, 2}}
}

// Plants stand on the land as drawn: near a road that is the verge, not
// the terrain mesh cut away under it (bushes floated and sank by up to
// 1.4 m).
func TestPlantsStandOnTheLandAsDrawn(t *testing.T) {
	const land = 1
	s := &surface{drawn: newLandSurface([]gltf.Primitive{flat(land, -4, 4, 3)}, func(m int) bool { return m == land })}
	if h := s.stand(2, 2); h != 3 {
		t.Errorf("a plant on drawn land at 3 m stands at %.2f m", h)
	}
}

// A path across a junction's corner is on the junction, not on open
// ground: no road of the course's own is laid over the corner (it lay
// under the junction's sidewalk, onto the grass, and the rider rode the
// sidewalk).
func TestNoOwnRoadAcrossAJunctionCorner(t *testing.T) {
	line := func(from, to [2]float64, node int64, at float64) *roadLine {
		l := &roadLine{nodes: map[int64][]float64{node: {at}}}
		length := math.Hypot(to[0]-from[0], to[1]-from[1])
		for k := 0; ; k++ {
			d := math.Min(float64(k)*fineStep, length)
			f := d / length
			l.samples = append(l.samples, sample{d: d, e: from[0] + f*(to[0]-from[0]), n: from[1] + f*(to[1]-from[1]), hw: 3})
			if d == length {
				break
			}
		}
		return l
	}
	// East along n = 0 to the node at the origin, then north: two roads
	// 6 m wide meeting there.
	n := &network{lines: []*roadLine{
		line([2]float64{-100, 0}, [2]float64{0, 0}, 7, 100),
		line([2]float64{0, 0}, [2]float64{0, 100}, 7, 0),
	}}
	// The path cuts the corner: 6 m inside it at the bend, off both roads.
	var d, x, y []float64
	add := func(e, no float64) {
		if len(x) > 0 {
			d = append(d, d[len(d)-1]+math.Hypot(e-x[len(x)-1], no-y[len(y)-1]))
		} else {
			d = append(d, 0)
		}
		x, y = append(x, e), append(y, no)
	}
	for e := -60.0; e <= -12; e += 5 {
		add(e, 0)
	}
	add(-7, 2)
	add(-4.2, 4.2) // 6 m from the node, inside the corner
	add(-2, 7)
	for no := 12.0; no <= 60; no += 5 {
		add(0, no)
	}
	if _, any := n.openRuns(d, x, y, nil); any {
		t.Error("a road of the course's own laid across the junction's corner")
	}
}

// Settling looks both ways across the path for the road: at a junction's
// inside corner the nearest carriageway lies ahead, and a move towards it
// came short onto the sidewalk.
func TestSettleLooksBothWaysAcross(t *testing.T) {
	const road = 1
	// The path heads east at the origin; a road 2 m ahead (east), and a
	// road 4 m to the north, alongside.
	ahead := gltf.Primitive{Material: road,
		Positions: []float32{2, 0, -1, 10, 0, -1, 10, 0, 1, 2, 0, 1}, // x east, z south
		Indices:   []uint32{0, 2, 1, 0, 3, 2}}
	north := gltf.Primitive{Material: road,
		Positions: []float32{-20, 0, -4, 20, 0, -4, 20, 0, -12, -20, 0, -12},
		Indices:   []uint32{0, 1, 2, 0, 2, 3}}
	ix := newRoadIndex([]gltf.Primitive{ahead, north}, func(m int) bool { return m == road })
	s, ok := ix.sideways(0, 0, 0, -1) // right of east is south
	if !ok || s > -4 || s < -5 {
		t.Errorf("the move across: %.2f m (found %v), want about 4.4 m to the north (negative: left)", s, ok)
	}
}

// A small piece of land shut in by roads is paved; a larger one stays.
func TestSmallEnclosedLandIsPaved(t *testing.T) {
	const ground, asphalt = 0, 1
	// A road square 0..40 m with a 2 × 2 m hole in it at 10..12, and a
	// 10 × 10 m one at 25..35: rings of road round each.
	ring := func(x0, z0, x1, z1, w float64) []gltf.Primitive {
		r := func(a, b, c, d float64) gltf.Primitive {
			return gltf.Primitive{Material: asphalt,
				Positions: []float32{float32(a), 0, float32(b), float32(c), 0, float32(b), float32(c), 0, float32(d), float32(a), 0, float32(d)},
				Indices:   []uint32{0, 2, 1, 0, 3, 2}}
		}
		return []gltf.Primitive{r(x0-w, z0-w, x1+w, z0), r(x0-w, z1, x1+w, z1+w), r(x0-w, z0, x0, z1), r(x1, z0, x1+w, z1)}
	}
	// Chunk (0, -1): x 0..250 east, z -250..0 south (n 0..250).
	roads := map[[2]int][]gltf.Primitive{{0, 0}: append(ring(10, -12, 12, -10, 3), ring(25, -35, 35, -25, 3)...)}
	paved := pavedIslands(roads, ground, asphalt, 250)
	p, ok := paved[[2]int{0, 0}]
	if !ok {
		t.Fatal("nothing paved")
	}
	in := func(x, z float64) bool {
		for k := 0; k+2 < len(p.Indices); k += 3 {
			var tri [3][3]float64
			for c := range 3 {
				v := p.Indices[k+c]
				tri[c] = [3]float64{float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])}
			}
			if inTri2([6]float64{tri[0][0], tri[0][2], tri[1][0], tri[1][2], tri[2][0], tri[2][2]}, x, z) {
				return true
			}
		}
		return false
	}
	if !in(11, -11) {
		t.Error("the 2 × 2 m piece of land is not paved")
	}
	if in(30, -30) {
		t.Error("the 10 × 10 m piece of land is paved")
	}
}

// Roads joined side by side have no skirt along the edge they share: its
// face showed as a dark line along the seam.
func TestJoinedRoadsHaveNoSkirtBetween(t *testing.T) {
	strip := func(e float64) *roadLine {
		l := &roadLine{}
		for k := 0; k <= 50; k++ {
			l.samples = append(l.samples, sample{d: float64(k) * 2, e: e, n: float64(k) * 2, hw: 2})
		}
		return l
	}
	a, b := strip(0), strip(4.5) // edges 0.5 m apart
	joinAlongside([]*roadLine{a, b}, 5)
	mid := 25
	if !a.samples[mid].butt[1] || !b.samples[mid].butt[0] {
		t.Errorf("the facing edges: butt %v and %v, want no skirt on either", a.samples[mid].butt, b.samples[mid].butt)
	}
	if a.samples[mid].butt[0] || b.samples[mid].butt[1] {
		t.Error("an outer edge lost its skirt")
	}
}

// A sidewalk never runs into a building: houses at the street had it
// drawn through their fronts. It narrows to the wall where there is room
// (a house right at a narrow sidewalk keeps it), else that side has none.
func TestSidewalkNeverRunsIntoABuilding(t *testing.T) {
	line := func() *roadLine {
		l := &roadLine{}
		for k := 0; k <= 50; k++ {
			s := sample{d: float64(k) * 2, e: 0, n: float64(k) * 2, hw: 3}
			s.walk = [2]bool{true, true}
			l.samples = append(l.samples, s)
		}
		return l
	}
	// Heading north, right is east: the sidewalk runs 3 to 4.8 m east of
	// the centre. A house from 4 m east between n 40 and 50 leaves 1 m.
	l := line()
	house := scenery.Footprint{Outline: [][2]float64{{4, 40}, {14, 40}, {14, 50}, {4, 50}}}
	clearWalksOfBuildings([]*roadLine{l}, []scenery.Footprint{house})
	s := l.samples[22] // n 44
	if !s.walk[1] || s.hw+s.walkWidth(1) > 4 || s.walkWidth(1) < walkMinM {
		t.Errorf("at the house: sidewalk %v, %.2f m wide; want one up to the wall at 4 m", s.walk[1], s.walkWidth(1))
	}
	if !l.samples[22].walk[0] || l.samples[5].walkWidth(1) != walkM {
		t.Error("sidewalks away from the house were changed")
	}
	// A house from 3.3 m east leaves no room: no sidewalk there.
	l = line()
	house.Outline = [][2]float64{{3.3, 40}, {14, 40}, {14, 50}, {3.3, 50}}
	clearWalksOfBuildings([]*roadLine{l}, []scenery.Footprint{house})
	if l.samples[22].walk[1] {
		t.Error("a sidewalk where a house leaves 0.3 m")
	}
}

// Water is drawn from its own outline, the land cut away under it: no
// ground stands up into it (green triangles stood in the ditches along
// the roads).
func TestWaterFromItsOutline(t *testing.T) {
	var pts []course.Point
	for i := range 51 {
		pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: 5})
	}
	c, err := course.New("ditch", "Ditch", pts)
	if err != nil {
		t.Fatal(err)
	}
	kx := 111195 * math.Cos(52*math.Pi/180)
	ll := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	// A ditch 4 m wide, 6 to 10 m east of the road, from n 100 to 400, at
	// a slant across the terrain's cells.
	ditch := []scenery.LatLon{ll(6, 100), ll(10, 100), ll(13, 400), ll(9, 400), ll(6, 100)}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"natural": "water", "water": "ditch"}, Geometry: ditch},
	}})
	ground := func(lat, lon float64) (float64, bool) { return 5, true }
	w, err := Build(c, Options{Generator: "test", Elevation: ground, Land: lm})
	if err != nil {
		t.Fatal(err)
	}
	var water []gltf.Primitive
	var land []gltf.Primitive
	for _, p := range w.prims {
		switch w.doc.MaterialName(p.Material) {
		case "water":
			water = append(water, p)
		case "terrain":
			land = append(land, p)
		}
	}
	if len(water) == 0 {
		t.Fatal("no water drawn")
	}
	// top is the highest of ps over e, n (false: none).
	top := func(ps []gltf.Primitive, e, n float64) (float64, bool) {
		x, z := e, -n
		best, ok := math.Inf(-1), false
		for _, p := range ps {
			for k := 0; k+2 < len(p.Indices); k += 3 {
				var px, py, pz [3]float64
				for c := range 3 {
					v := p.Indices[k+c]
					px[c], py[c], pz[c] = float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])
				}
				den := (pz[1]-pz[2])*(px[0]-px[2]) + (px[2]-px[1])*(pz[0]-pz[2])
				if math.Abs(den) < 1e-12 {
					continue
				}
				a := ((pz[1]-pz[2])*(x-px[2]) + (px[2]-px[1])*(z-pz[2])) / den
				b := ((pz[2]-pz[0])*(x-px[2]) + (px[0]-px[2])*(z-pz[2])) / den
				if a < -1e-9 || b < -1e-9 || 1-a-b < -1e-9 {
					continue
				}
				if h := a*py[0] + b*py[1] + (1-a-b)*py[2]; h > best {
					best, ok = h, true
				}
			}
		}
		return best, ok
	}
	// The land under the water: below it everywhere inside the outline
	// (the bank's face reaches 2 cm over the land's edge, so the edge is
	// left out).
	high := 0
	for n := 105.0; n <= 395; n += 0.5 {
		f := (n - 100) / 300
		for e := 6.4 + 3*f; e <= 9.6+3*f; e += 0.25 {
			lv, ok := top(water, e, n)
			if !ok {
				t.Fatalf("no water surface at %.2f, %.1f inside the ditch", e, n)
			}
			if h, ok := top(land, e, n); ok && h > lv+0.01 {
				high++
			}
		}
	}
	if high > 0 {
		t.Errorf("land stands above the water at %d points inside the ditch", high)
	}
	// Beside it the land never lies under the water's surface: lowered
	// ground inside sloped the land across the shore under it, and from
	// the road the eye went under the water's edge.
	low := 0
	for n := 105.0; n <= 395; n += 1 {
		f := (n - 100) / 300
		for _, e := range []float64{6 + 3*f - 0.4, 10 + 3*f + 0.4} {
			lv, ok := top(water, e+map[bool]float64{true: 0.8, false: -0.8}[e < 8+3*f], n)
			h, okl := top(land, e, n)
			if ok && okl && h < lv-0.01 {
				low++
			}
		}
	}
	if low > 0 {
		t.Errorf("the land beside the ditch lies under its water at %d points", low)
	}
}

// A sidewalk or cycle lane that stops between two cross-sections ends in
// a taper to the road's edge: ended square, the wedge between it and the
// road widening over the step was a hole (or land showing in the road).
func TestSidewalksAndLanesEndInATaper(t *testing.T) {
	c := northCourse(t)
	for _, which := range []string{"sidewalk", "cycle lane"} {
		tr := testTerrain(c, Options{})
		for i := range tr.fine[:100] { // to 198 m, then none
			if which == "sidewalk" {
				tr.fine[i].walk[1] = true
			} else {
				tr.fine[i].lane[1] = true
			}
		}
		m := addRoadMaterials(gltf.New("test"))
		ps := roadMesh(tr.fine[:200], tr.fine, 0, courseLine(c, nil), false, m, nil)
		covered := func(e, n float64) bool {
			for _, p := range ps {
				for k := 0; k+2 < len(p.Indices); k += 3 {
					var tri [6]float64
					for j := range 3 {
						v := p.Indices[k+j]
						tri[2*j], tri[2*j+1] = float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])
					}
					if math.Abs((tri[2]-tri[0])*(tri[5]-tri[1])-(tri[4]-tri[0])*(tri[3]-tri[1])) > 1e-9 && inTri2(tri, e, n) {
						return true
					}
				}
			}
			return false
		}
		// Heading north, right is east: just inside the road's edge (the
		// lane) or just outside it (the sidewalk), a quarter into the step.
		hw := tr.fine[99].hw
		e := hw - 0.3
		if which == "sidewalk" {
			e = hw + 0.3
		}
		if !covered(e, 198.5) {
			t.Errorf("the %s's end leaves a hole at %.2f m east, 198.5 m north", which, e)
		}
	}
}

// The rider's path never steps back against its way: a point past its
// next neighbour is put between them.
func TestPathHasNoFolds(t *testing.T) {
	x := []float64{0, 5, 10, 15, 14.5, 20, 25}
	y := make([]float64, len(x))
	unfold(x, y, false)
	for i := 1; i < len(x); i++ {
		if x[i] <= x[i-1] {
			t.Errorf("point %d at %.2f m, behind point %d at %.2f m", i, x[i], i-1, x[i-1])
		}
	}
}

// A car park meets the road beside it at the road's height, under it
// where they overlap, and lies on the ground further in (it lay on the 5 m
// terrain, up to 0.6 m off the road, and the step showed as slits).
func TestCarParkMeetsTheRoad(t *testing.T) {
	const road = 1
	// A road 10 m wide at 10 m, x 0..10 east, the whole length north.
	rs := newSurfaceIndex([]gltf.Primitive{{Material: road,
		Positions: []float32{0, 10, 0, 10, 10, 0, 10, 10, -100, 0, 10, -100},
		Indices:   []uint32{0, 1, 2, 0, 2, 3}}}, func(m int) bool { return m == road }, 200)
	const ground = 11.0
	if h := parkingHeight(ground, 5, 50, rs); h > 10 || h < 9.9 {
		t.Errorf("under the road: %.3f m, want just under its 10 m", h)
	}
	if h := parkingHeight(ground, 10.25, 50, rs); h > 10.05 {
		t.Errorf("at the road's edge: %.3f m, want the road's height", h)
	}
	if h := parkingHeight(ground, 10+parkBlendM+1, 50, rs); math.Abs(h-(ground+parkingLiftM)) > 1e-9 {
		t.Errorf("away from the road: %.3f m, want the ground's %.2f", h, ground+parkingLiftM)
	}
}
