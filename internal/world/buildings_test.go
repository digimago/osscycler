package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

func TestBuildings(t *testing.T) {
	// Flat ground at 10 m, sloping 10 % east beyond 20 m.
	c := northCourse(t)
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 10 + 0.1*math.Max(0, e-20), true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	ys := func(p *gltf.Primitive) (lo, hi float64) {
		lo, hi = math.Inf(1), math.Inf(-1)
		for k := 1; k < len(p.Positions); k += 3 {
			lo, hi = math.Min(lo, float64(p.Positions[k])), math.Max(hi, float64(p.Positions[k]))
		}
		return lo, hi
	}

	// A 12 by 8 m house, 5 m walls and a 4 m roof: a gable, its ridge 9 m
	// over the highest ground under it.
	house := scenery.Footprint{ID: 1, Kind: scenery.KindHouse, WallM: 5, RoofM: 4,
		Outline: [][2]float64{{-30, 400}, {-18, 400}, {-18, 408}, {-30, 408}}}
	var p gltf.Primitive
	buildingMesh(&p, s, &styleNL, house, nil, nil)
	top := math.Inf(-1)
	for _, q := range house.Outline {
		top = math.Max(top, s.at(q[0], q[1]))
	}
	ridge := top + house.WallM + house.RoofM
	if lo, hi := ys(&p); math.Abs(hi-ridge) > 0.01 || lo > top-plinthM+0.01 {
		t.Errorf("house from %.2f to %.2f m, want from below the ground to the ridge at %.2f", lo, hi, ridge)
	}
	// The ridge runs along the long side (east-west): its ends lie at the
	// short walls' middle, north 404.
	for k := 0; k < len(p.Positions); k += 3 {
		if y := float64(p.Positions[k+1]); math.Abs(y-ridge) < 0.01 {
			if n := -float64(p.Positions[k+2]); math.Abs(n-404) > 0.01 {
				t.Errorf("a ridge point at north %.2f, want 404 (along the long side)", n)
			}
		}
	}

	// A flat-roofed block on the slope: the roof level, walls from below
	// the lowest ground.
	block := scenery.Footprint{ID: 2, Kind: scenery.KindFlat, WallM: 12,
		Outline: [][2]float64{{30, 500}, {50, 500}, {50, 520}, {30, 520}}}
	p = gltf.Primitive{}
	buildingMesh(&p, s, &styleNL, block, nil, nil)
	lo, hi := ys(&p)
	glo, ghi := math.Inf(1), math.Inf(-1)
	for _, q := range block.Outline {
		g := s.at(q[0], q[1])
		glo, ghi = math.Min(glo, g), math.Max(ghi, g)
	}
	if ghi-glo < 0.5 {
		t.Fatalf("the fixture's slope is gone: ground %.2f to %.2f", glo, ghi)
	}
	if math.Abs(lo-(glo-plinthM)) > 0.01 || math.Abs(hi-(ghi+12)) > 0.01 {
		t.Errorf("block from %.2f to %.2f m, want %.2f (below its lowest ground) to %.2f (12 m over its highest)", lo, hi, glo-plinthM, ghi+12)
	}
	if len(p.Colors) != len(p.Positions) {
		t.Errorf("%d colours for %d positions", len(p.Colors)/3, len(p.Positions)/3)
	}

	// Colours: the map's tags win; otherwise the same building, the same
	// colours, every build.
	tagged := house
	tagged.WallColour, tagged.RoofColour = "#ffffff", "red"
	wall, roof := colours(&styleNL, tagged, 7)
	if wall != srgbLinear(0xffffff) || roof != srgbLinear(namedColor["red"]) {
		t.Errorf("tagged colours %v %v", wall, roof)
	}
	a1, b1 := colours(&styleNL, house, splitmix(1))
	a2, b2 := colours(&styleNL, house, splitmix(1))
	if a1 != a2 || b1 != b2 {
		t.Error("colours differ between builds")
	}
}

// A gabled house whose outline isn't quite its rectangle (a notch, a
// corner cut) stands on the rectangle: every gable's base corner is a
// wall's top corner, so they meet in one plane (owner, 2026-10-16: a seam
// between the floors and the gable).
func TestGablesMeetTheirWalls(t *testing.T) {
	c := northCourse(t)
	tr := testTerrain(c, Options{})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	house := scenery.Footprint{ID: 2, Kind: scenery.KindHouse, WallM: 5, RoofM: 4,
		Outline: [][2]float64{{-30, 400}, {-18, 400}, {-18, 407.3}, {-18.6, 408}, {-30, 408}}}
	var p gltf.Primitive
	buildingMesh(&p, s, &styleNL, house, nil, nil)
	// Wall top corners: vertices of upright faces at their highest.
	type v2 [2]float32
	walls := map[v2]bool{}
	for t := 0; t+2 < len(p.Indices); t += 3 {
		var q [3][3]float32
		for k := range 3 {
			i := p.Indices[t+k]
			q[k] = [3]float32{p.Positions[3*i], p.Positions[3*i+1], p.Positions[3*i+2]}
		}
		ny := (q[1][2]-q[0][2])*(q[2][0]-q[0][0]) - (q[1][0]-q[0][0])*(q[2][2]-q[0][2])
		if math.Abs(float64(ny)) < 1e-4 {
			for _, v := range q {
				walls[v2{v[0], v[2]}] = true
			}
		}
	}
	// The rectangle's 4 corners and each gable's apex.
	if len(walls) != 6 {
		t.Errorf("the upright faces have %d corners, want the rectangle's 4 and 2 gable apexes", len(walls))
	}
}

// Windows are laid out per face (owner, 2026-10-16: windows cut at the
// corners, through the roof line): every wall's bays lie whole within it,
// clear of its ends; a gable's row only where it fits under the slope.
func TestWindowsFitTheirFaces(t *testing.T) {
	c := northCourse(t)
	tr := testTerrain(c, Options{})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	house := scenery.Footprint{ID: 3, Kind: scenery.KindHouse, WallM: 5.6, RoofM: 4,
		Outline: [][2]float64{{-30, 400}, {-18.5, 400}, {-18.5, 408}, {-30, 408}}}
	var p gltf.Primitive
	buildingMesh(&p, s, &styleNL, house, nil, nil)
	if len(p.UV2s) != len(p.UVs) {
		t.Fatalf("%d UV2 values for %d UV", len(p.UV2s), len(p.UVs))
	}
	walls, gables := 0, 0
	for i := 0; i+2 < len(p.Indices); i += 3 {
		ids := p.Indices[i : i+3]
		cnt := math.Mod(math.Floor(float64(p.UV2s[2*ids[0]])), 100)
		if cnt == 0 {
			continue
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, v := range ids {
			lo, hi = math.Min(lo, float64(p.UVs[2*v])), math.Max(hi, float64(p.UVs[2*v]))
		}
		maxTop := math.Mod(float64(p.UV2s[2*ids[0]]), 1) * 100
		// One ground for the whole face: rows whole, never cut on the slant.
		if g := p.UV2s[2*ids[0]+1]; p.UV2s[2*ids[1]+1] != g || p.UV2s[2*ids[2]+1] != g {
			t.Errorf("a face's ground varies along it: rows would be cut where the slope crosses them")
		}
		if lo > 0.1 || hi < cnt-0.1 {
			t.Errorf("a face's %v bays lie past it (u %.2f..%.2f)", cnt, lo, hi)
		}
		if maxTop < winFloorM+winSillM+winHeightM {
			t.Errorf("a face with %v windows has room only to %.2f m", cnt, maxTop)
		}
		if hi-lo > cnt+0.01 && lo < -0.01 {
			walls++
		} else {
			gables++
		}
	}
	if walls == 0 {
		t.Error("no wall has windows")
	}
}

// A free-standing house's walls keep their windows (a wall's scan for
// adjoining buildings once lost its last stretch, and every window, when
// its length wasn't a multiple of the step); a wall an annex covers half
// of has its bays on the other half only.
func TestWindowsAvoidAdjoiningBuildings(t *testing.T) {
	c := northCourse(t)
	tr := testTerrain(c, Options{})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	house := scenery.Footprint{ID: 4, Kind: scenery.KindFlat, WallM: 6,
		Outline: [][2]float64{{-30, 400}, {-18.3, 400}, {-18.3, 410}, {-30, 410}}}
	count := func(covered func(e, n float64) bool) (faces int, minE, maxE float64) {
		var p gltf.Primitive
		buildingMesh(&p, s, &styleNL, house, nil, covered)
		minE, maxE = math.Inf(1), math.Inf(-1)
		for i := 0; i+2 < len(p.Indices); i += 3 {
			ids := p.Indices[i : i+3]
			if math.Mod(math.Floor(float64(p.UV2s[2*ids[0]])), 100) == 0 {
				continue
			}
			faces++
			// The south wall (n 400): its window bays' extent in e.
			if p.Positions[3*ids[0]+2] == -400 && p.Positions[3*ids[1]+2] == -400 && p.Positions[3*ids[2]+2] == -400 {
				for _, v := range ids {
					u := float64(p.UVs[2*v])
					if u >= 0 && u <= 1 {
						minE = math.Min(minE, float64(p.Positions[3*v]))
					}
					maxE = math.Max(maxE, float64(p.Positions[3*v]))
				}
			}
		}
		return
	}
	free, _, _ := count(func(e, n float64) bool { return false })
	if free < 8 {
		t.Errorf("a free-standing house has windows on %d faces, want all four walls'", free)
	}
	// An annex against the south wall's west half (e -30 .. -24).
	annexed, _, _ := count(func(e, n float64) bool { return n < 400 && e < -24 })
	if annexed < free-2 || annexed > free {
		t.Errorf("with an annex: windows on %d faces (free: %d)", annexed, free)
	}
}

// On a hillside a French house turns a gable to the valley, a chalet
// longer along the slope's contour too (owner, 2026-10-16: the Rue du
// Hohneck's chalets): its ridge runs down the fall line; a Dutch one
// keeps it along its length.
func TestGablesTurnToTheView(t *testing.T) {
	c := northCourse(t)
	// The ground falls 15 % to the east.
	ground := func(lat, lon float64) (float64, bool) {
		e, _ := c.Project(lat, lon)
		return 30 - 0.15*e, true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	// 9 m east-west, 18 m north-south: long along the contour.
	chalet := scenery.Footprint{ID: 5, Kind: scenery.KindHouse, WallM: 6, RoofM: 4,
		Outline: [][2]float64{{-60, 400}, {-51, 400}, {-51, 418}, {-60, 418}}}
	ridgeAlongEast := func(st *style) bool {
		var p gltf.Primitive
		buildingMesh(&p, s, st, chalet, nil, nil)
		top := float32(-1e9)
		for k := 1; k < len(p.Positions); k += 3 {
			top = max(top, p.Positions[k])
		}
		minX, maxX, minZ, maxZ := float32(1e9), float32(-1e9), float32(1e9), float32(-1e9)
		for k := 0; k+2 < len(p.Positions); k += 3 {
			if p.Positions[k+1] > top-0.01 {
				minX, maxX = min(minX, p.Positions[k]), max(maxX, p.Positions[k])
				minZ, maxZ = min(minZ, p.Positions[k+2]), max(maxZ, p.Positions[k+2])
			}
		}
		return maxX-minX > maxZ-minZ
	}
	if !ridgeAlongEast(&styleFR) {
		t.Error("a French chalet on the hillside keeps its ridge along the contour")
	}
	if ridgeAlongEast(&styleNL) {
		t.Error("a Dutch house turned its ridge")
	}
}

// A mountain farm of attached sections (an L) is split into rectangles,
// each with its own low gable roof whose ridge runs down its middle, not
// one roof over the whole (owner, 2026-10-18: the Ferme Auberge
// Breitzhousen, several farm buildings joined).
func TestFarmIsSplitIntoGabledSections(t *testing.T) {
	// An L: 40 m long, 12 m deep, a 12 by 20 m wing at its west end.
	pts := [][2]float64{{-60, 400}, {-20, 400}, {-20, 412}, {-48, 412}, {-48, 432}, {-60, 432}}
	secs := farmSections(pts)
	area := 0.0
	for _, r := range secs {
		area += math.Abs(polyArea(r))
	}
	if len(secs) < 2 || math.Abs(area-math.Abs(polyArea(pts))) > 5 {
		t.Fatalf("%d sections covering %.0f m², want at least two covering the outline's %.0f", len(secs), area, math.Abs(polyArea(pts)))
	}
	c := northCourse(t)
	tr := testTerrain(c, Options{Elevation: func(lat, lon float64) (float64, bool) { return 10, true }})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	farm := scenery.Footprint{ID: 3, Kind: scenery.KindHouse, WallM: 5, RoofM: 4, Outline: pts}
	var p gltf.Primitive
	buildingMesh(&p, s, &styleFR, farm, nil, nil)
	top := math.Inf(-1)
	for k := 1; k < len(p.Positions); k += 3 {
		top = math.Max(top, float64(p.Positions[k]))
	}
	// The widest section is 12 m: its ridge 6·tan(22°) = 2.4 m over the
	// 3.5 m walls.
	if want := 10 + farmWallM + 6*math.Tan(farmPitchDeg*math.Pi/180); top > want+0.3 {
		t.Errorf("the farm's highest point at %.2f m, want low roofs up to about %.2f", top, want)
	}
}

// In a style with mountain farms a big pitched building out of town is a
// farm: walls farmWallM, a shallow gable (farmPitchDeg); the Dutch style
// keeps its walls (owner, 2026-10-18: the Ferme Auberge Breitzhousen).
func TestMountainFarmIsLowUnderABigRoof(t *testing.T) {
	c := northCourse(t)
	tr := testTerrain(c, Options{Elevation: func(lat, lon float64) (float64, bool) { return 10, true }})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	farm := scenery.Footprint{ID: 4, Kind: scenery.KindHouse, WallM: 6, RoofM: 3,
		Outline: [][2]float64{{-60, 400}, {-20, 400}, {-20, 412}, {-60, 412}}}
	height := func(st *style) (eaveAt, top float64) {
		var p gltf.Primitive
		buildingMesh(&p, s, st, farm, nil, nil)
		top = math.Inf(-1)
		for k := 1; k < len(p.Positions); k += 3 {
			top = math.Max(top, float64(p.Positions[k]))
		}
		// The wall's top along the south wall's middle.
		eaveAt = math.Inf(-1)
		for k := 0; k < len(p.Positions); k += 3 {
			if n := -float64(p.Positions[k+2]); math.Abs(n-400) < 0.01 {
				eaveAt = math.Max(eaveAt, float64(p.Positions[k+1]))
			}
		}
		return
	}
	ridge := 10 + farmWallM + 6*math.Tan(farmPitchDeg*math.Pi/180)
	if e, top := height(&styleFR); math.Abs(e-(10+farmWallM)) > 0.01 || math.Abs(top-ridge) > 0.01 {
		t.Errorf("French farm: eave %.2f m, ridge %.2f m; want %.2f and %.2f", e, top, 10+farmWallM, ridge)
	}
	if e, top := height(&styleNL); math.Abs(e-16) > 0.01 || math.Abs(top-19) > 0.01 {
		t.Errorf("Dutch: eave %.2f m, ridge %.2f m; want 16 and 19 as tagged", e, top)
	}
}

// A mountain farm on a slope is dug into it: its road-side wall is its
// height over the low ground, not a storey over the high (owner,
// 2026-10-18: the Ferme Auberge Breitzhousen's road side stood 8.5 m).
func TestMountainFarmOnASlopeIsDugIn(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	// The ground rises 6 m over the farm's 12 m depth (north).
	tr := testTerrain(c, Options{Elevation: func(lat, lon float64) (float64, bool) {
		n := (lat - 52) * 111195
		_ = kx
		return 10 + 0.5*math.Max(0, math.Min(12, n-400)), true
	}})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	farm := scenery.Footprint{ID: 5, Kind: scenery.KindHouse, WallM: farmWallM, RoofM: 4.7, FromLowest: true,
		Outline: [][2]float64{{-60, 400}, {-20, 400}, {-20, 412}, {-60, 412}}}
	var p gltf.Primitive
	buildingMesh(&p, s, &styleFR, farm, nil, nil)
	eave := math.Inf(-1)
	for k := 0; k < len(p.Positions); k += 3 {
		if n := -float64(p.Positions[k+2]); math.Abs(n-400) < 0.01 {
			eave = math.Max(eave, float64(p.Positions[k+1]))
		}
	}
	if low := s.at(-40, 400); eave-low > farmWallM+2.1 {
		t.Errorf("the downhill wall is %.1f m high, want about the farm's walls (%.1f m) on a slope dug into", eave-low, farmWallM)
	}
}
