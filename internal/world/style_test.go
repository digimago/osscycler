package world

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Worlds outside a region of their own take the Dutch style, the one the
// builder had before styles, and it is made of the very tables the Dutch
// worlds were built from (so they stay byte-identical).
func TestDutchStyleIsTheDefault(t *testing.T) {
	for _, r := range []string{"", RegionNL, "de"} {
		if styleFor(r) != &styleNL {
			t.Errorf("styleFor(%q) isn't the Dutch style", r)
		}
	}
	if styleFor(RegionFR) != &styleFR {
		t.Error("styleFor(fr) isn't the French style")
	}
	same := func(name string, a, b any) {
		if reflect.ValueOf(a).Pointer() != reflect.ValueOf(b).Pointer() {
			t.Errorf("the Dutch style's %s isn't the builder's own table", name)
		}
	}
	same("species", styleNL.species, speciesByLand)
	same("town", styleNL.town, townMix)
	same("bushes", styleNL.bushes, bushesByLand)
	same("avenue", styleNL.avenue, avenueMix)
	same("walls", styleNL.walls, brickWalls)
	if len(styleNL.bands) != 0 || styleNL.dryAboveM != 0 || styleNL.gardens != gardenMix || styleNL.pasture.leanDeg != 0 {
		t.Error("the Dutch style has altitude bands, dry grass, other gardens or leaning fences")
	}
}

// Every species a style plants exists and belongs to its region, and a
// world carries templates only for its own region's species.
func TestStylesPlantTheirOwnSpecies(t *testing.T) {
	check := func(st *style, mix map[string]float64, where string) {
		for name, w := range mix {
			i := speciesIndex(name)
			if i < 0 {
				t.Errorf("%s: %s %s isn't a species", st.name, where, name)
				continue
			}
			if r := allSpecies[i].region; w > 0 && r != "" && r != st.name {
				t.Errorf("%s: %s %s is %s's", st.name, where, name, r)
			}
		}
	}
	for _, st := range []*style{&styleNL, &styleFR} {
		lands := []map[scenery.Land]speciesMix{st.species}
		for _, b := range st.bands {
			lands = append(lands, b.species)
		}
		for _, ls := range lands {
			for l, m := range ls {
				check(st, m.broad, fmt.Sprint(l, " broad"))
				check(st, m.conifer, fmt.Sprint(l, " conifer"))
			}
		}
		for _, m := range []map[string]float64{st.town, st.townWater, st.avenue, st.countryAvenue, st.canalAvenue, {st.pioneer: 1}} {
			check(st, m, "street or pioneer")
		}
		for l, m := range st.bushes {
			for name := range m {
				found := false
				for _, b := range allBushes {
					found = found || b.name == name
				}
				if !found {
					t.Errorf("%s: %v bush %s isn't a bush", st.name, l, name)
				}
			}
		}
	}

	templates := func(region string) map[string]bool {
		got := map[string]bool{}
		for _, k := range templateKinds(region) {
			got["template "+plantKinds[k].name] = true
		}
		return got
	}
	nl, fr := templates(RegionNL), templates(RegionFR)
	for _, name := range []string{"spruce", "silver fir", "lime", "mountain beech"} {
		if nl["template "+name+" 1"] {
			t.Errorf("a Dutch world carries the %s, a French species", name)
		}
		if !fr["template "+name+" 1"] {
			t.Errorf("a French world lacks the %s", name)
		}
	}
	if !nl["template oak 1"] || !fr["template oak 1"] {
		t.Error("the oak, every region's, is missing")
	}
}

// In the Vosges the trees follow the height (owner, 2026-10-15): a wood
// at 300 m is oak and beech with spruce and fir, at 1300 m stunted
// mountain beech and spruce; on the crest's pastures a tree is a bush.
func TestFrenchTreesFollowTheHeight(t *testing.T) {
	// The test terrain's ground is the course's profile: a course 1 km
	// north at the height asked.
	at := func(ele float64) *course.Course {
		var pts []course.Point
		for i := range 101 {
			pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: ele})
		}
		c, err := course.New("north", "North", pts)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := at(0)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest"}, Geometry: ring(-300, -100, 0, 1100)},
		{Type: "way", ID: 2, Tags: map[string]string{"landuse": "meadow"}, Geometry: ring(0, -100, 300, 1100)},
	}})
	species := func(height float64) (forest, meadow map[string]int, meadowMax float64) {
		o := Options{Land: lm, Region: RegionFR}
		tr := testTerrain(at(height), o)
		s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
		forest, meadow = map[string]int{}, map[string]int{}
		for _, p := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
			if p.kind >= speciesKinds {
				continue
			}
			sp := allSpecies[p.kind/(treeVariants+1)]
			if p.x < 0 {
				forest[sp.name]++
			} else {
				meadow[sp.name]++
				meadowMax = math.Max(meadowMax, sp.h*float64(p.s))
			}
		}
		return forest, meadow, meadowMax
	}

	low, _, _ := species(300)
	if low["oak"] == 0 || low["beech"] == 0 || low["spruce"]+low["silver fir"] == 0 {
		t.Errorf("a wood at 300 m: %v, want oak, beech, spruce and fir", low)
	}
	if low["mountain beech"] > 0 {
		t.Errorf("a wood at 300 m has %d mountain beech", low["mountain beech"])
	}
	high, meadow, tallest := species(1300)
	for name, n := range high {
		// Beech kept to the crest's canopy, wind-shorn beech, rowan, the
		// odd spruce (owner, 2026-10-18: the woods along the Route des
		// Crêtes are a 12-15 m beech canopy).
		if name != "beech" && name != "mountain beech" && name != "spruce" && name != "rowan" {
			t.Errorf("a wood at 1300 m has %d %s", n, name)
		}
	}
	if high["mountain beech"] == 0 || high["beech"] == 0 {
		t.Errorf("a wood at 1300 m: %v, want beech and mountain beech", high)
	}
	if len(meadow) == 0 || tallest > 4.5 {
		t.Errorf("the crest's pasture: %v, the tallest %.1f m; want a few bushy trees under 4.5 m", meadow, tallest)
	}
}

// The summer pastures' fences are sparse and leaning, one wire; a Dutch
// farmer's stand straight every 3.5 m with three.
func TestFrenchPastureFencesLean(t *testing.T) {
	c := northCourse(t)
	tr := testTerrain(c, Options{})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	f := scenery.Fence{Line: [][2]float64{{10, 100}, {10, 155}}, Wire: true}
	posts := func(pf pastureFence) (n int, maxLean float64) {
		var p gltf.Primitive
		wireFence(&p, s, f, pf, srgbLinear(0xb4bcc2))
		// A beam is five quads of four vertices: four sides, each its
		// foot's two corners, then its top's, and the cap; posts stand,
		// wires lie.
		pos := p.Positions
		for v := 0; v+20*3 <= len(pos); v += 20 * 3 {
			var foot, top [3]float64
			for q := range 16 {
				end := &foot
				if q%4 >= 2 {
					end = &top
				}
				for a := range 3 {
					end[a] += float64(pos[v+3*q+a]) / 8
				}
			}
			if top[1]-foot[1] < 0.5 {
				continue // a wire
			}
			n++
			maxLean = math.Max(maxLean, math.Hypot(top[0]-foot[0], top[2]-foot[2]))
		}
		return n, maxLean
	}
	nlPosts, nlLean := posts(styleNL.pasture)
	frPosts, frLean := posts(styleFR.pasture)
	if nlPosts != 17 || nlLean > 0.001 {
		t.Errorf("Dutch: %d posts leaning up to %.2f m, want 17 straight", nlPosts, nlLean)
	}
	if frPosts != 11 || frLean < 0.05 || frLean > 0.25 {
		t.Errorf("French: %d posts leaning up to %.2f m, want 11 leaning 5-25 cm", frPosts, frLean)
	}
}

// Conifers at the road's level have their branches cut back where they
// reach over it; on a bank above the road they keep them low; clear of
// the road they grow as they are (owner, 2026-10-15, the D13B).
func TestTreesOverTheRoadArePruned(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest", "leaf_type": "needleleaved"}, Geometry: ring(-300, -100, 300, 1100)},
	}})
	tr := testTerrain(c, Options{Land: lm, Region: RegionFR})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	var near, natural, far int
	{
		for _, p := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
			sp := kindSpecies(p.kind)
			if sp < 0 || prunedKind[sp] < 0 || p.z > -100 || p.z < -900 {
				continue
			}
			off := math.Abs(float64(p.x)) - tr.fine[0].edge
			switch {
			case p.kind == prunedKind[sp] && off > allSpecies[sp].crownReach()*float64(p.s)+0.01:
				far++
			case p.kind == prunedKind[sp]:
				near++
			case off < 3:
				natural++
			}
		}
	}
	if near < 20 || far > 0 {
		t.Errorf("at the road's level: %d pruned over the road, %d pruned clear of it; want many and none", near, far)
	}
	if natural > near/4 {
		t.Errorf("at the road's level: %d conifers within 3 m of the road unpruned, %d pruned", natural, near)
	}
	// On a bank: the test terrain's ground is the road's profile, so the
	// heights are asked directly.
	spruce := allSpecies[speciesIndex("spruce")]
	if !belowClearance(spruce, formForest, 1, 10, 10) {
		t.Error("a spruce at the road's level clears it unpruned")
	}
	if belowClearance(spruce, formForest, 1, 16, 10) {
		t.Error("a spruce on a 6 m bank above the road needs pruning")
	}
}

// The meso relief (owner, 2026-10-15: a detail terrain over the coarse
// one, mountains first): hummocks on slopes in a style with reliefM,
// none on the flat, none by the road, none in a Dutch world.
func TestReliefOnSlopesOnly(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	sloped := func(lat, lon float64) (float64, bool) { return 10 + 0.3*(lon-5)*kx, true } // 30 % east-west
	flat := func(lat, lon float64) (float64, bool) { return 10, true }
	most := func(region string, elev func(lat, lon float64) (float64, bool), gap float64) float64 {
		tr := testTerrain(c, Options{Region: region, Elevation: elev})
		g := &gland{t: tr}
		m := 0.0
		for n := 100.0; n < 900; n += 7 {
			for e := 20.0; e < 200; e += 7 {
				m = math.Max(m, math.Abs(g.relief(e, n, gap, tr.coarse)))
			}
		}
		return m
	}
	if m := most(RegionFR, sloped, 30); m < 0.5 || m > 2*styleFR.reliefM {
		t.Errorf("a 30 %% slope in the Vosges: relief up to %.2f m, want 0.5-%.1f", m, 2*styleFR.reliefM)
	}
	if m := most(RegionFR, flat, 30); m > 0 {
		t.Errorf("flat ground: relief %.2f m", m)
	}
	if m := most(RegionFR, sloped, gBlendM-0.1); m > 0 {
		t.Errorf("within the road's blend: relief %.2f m", m)
	}
	if m := most(RegionNL, sloped, 30); m > 0 {
		t.Errorf("a Dutch world: relief %.2f m", m)
	}
}

// A property's wall (owner, 2026-10-16): pillars at both ends, the top
// level in steps with a pillar at each, the base never more than
// wallRaiseM over the ground nor wallSinkM under it; a gate between two
// pillars in some.
func TestPropertyWallsAreLevelWithPillars(t *testing.T) {
	// 20 m of frontage climbing 8 % (1.6 m).
	var run []wallSpot
	for i := range 21 {
		run = append(run, wallSpot{e: float64(i), n: 0, de: 1, dn: 0, ground: 0.08 * float64(i)})
	}
	gates := 0
	for seed := uint64(0); seed < 40; seed++ {
		ps := propertyWall(run, splitmix(seed))
		var pillars, walls []wallPiece
		for _, p := range ps {
			switch p.kind {
			case hPillar:
				pillars = append(pillars, p)
			case hWall:
				walls = append(walls, p)
			case hGate:
				gates++
			}
		}
		if len(pillars) < 3 || pillars[0].e != 0 || pillars[len(pillars)-1].e != 20 {
			t.Fatalf("seed %d: pillars %v, want one at each end and steps between", seed, pillars)
		}
		levels := map[float64]bool{}
		for _, w := range walls {
			g := 0.08 * w.e
			if w.y-g > wallRaiseM+0.05 || g-w.y > wallSinkM+0.05 {
				t.Fatalf("seed %d: a segment at %.1f m stands at %.2f over ground %.2f", seed, w.e, w.y, g)
			}
			levels[w.y] = true
		}
		if len(levels) < 2 || len(levels) > 6 {
			t.Errorf("seed %d: %d levels on a 1.6 m climb", seed, len(levels))
		}
	}
	if gates < 10 || gates > 35 {
		t.Errorf("%d of 40 properties with a gate, want about half", gates)
	}
	if ps := propertyWall(run[:3], 1); ps != nil {
		t.Errorf("a 2 m frontage got a wall: %v", ps)
	}
}

func TestResampleRing(t *testing.T) {
	sq := [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	pts := resampleRing(sq, 2)
	if len(pts) != 20 {
		t.Fatalf("a 40 m ring every 2 m: %d points, want 20", len(pts))
	}
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		if d := math.Hypot(q[0]-p[0], q[1]-p[1]); d > 2.01 {
			t.Errorf("points %d and %d %.2f m apart", i, i+1, d)
		}
	}
}

// A mountain forest the map tags as one kind has stands of the other
// (owner, 2026-10-18: the forêt domaniale on the Route des Américains is
// tagged needleleaved and is patches of fir and spruce among beech from
// above); a Dutch forest stays pure, as tagged.
func TestPureForestHasStandsOfTheOtherKind(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest", "leaf_type": "needleleaved"}, Geometry: ring(-300, -100, 300, 1100)},
	}})
	share := func(region string) (broad float64, patches int) {
		tr := testTerrain(c, Options{Land: lm, Region: region})
		s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
		var nb, nc int
		cells := map[[2]int][2]int{} // broadleaved, coniferous grown trees per 50 m cell
		for _, p := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
			sp := kindSpecies(p.kind)
			if sp < 0 || float64(p.s) < 0.6 { // grown trees only
				continue
			}
			k := [2]int{int(math.Floor(float64(p.x) / 50)), int(math.Floor(float64(p.z) / 50))}
			c := cells[k]
			if allSpecies[sp].fir || allSpecies[sp].pine {
				nc++
				c[1]++
			} else {
				nb++
				c[0]++
			}
			cells[k] = c
		}
		// Patches: cells that are nearly all one kind.
		pure, all := 0, 0
		for _, c := range cells {
			if c[0]+c[1] < 6 {
				continue
			}
			all++
			if f := float64(c[0]) / float64(c[0]+c[1]); f < 0.15 || f > 0.85 {
				pure++
			}
		}
		return float64(nb) / float64(nb+nc), 100 * pure / all
	}
	if b, _ := share(RegionNL); b > 0.12 {
		t.Errorf("Dutch needleleaved forest: %.0f %% broadleaved trees, want under 12 %%", 100*b)
	}
	if b, pure := share(RegionFR); b < 0.2 || b > 0.45 || pure < 60 {
		// 0.7 of the stands conifer: 20-45 % of the trees broadleaved, in
		// patches (most 50 m cells nearly all one kind, not a pepper mix).
		t.Errorf("French needleleaved forest: %.0f %% broadleaved trees, %d %% of 50 m cells one kind; want 20-45 %% in patches (over 60 %%)", 100*b, pure)
	}
}

// Trees are kept to their altitude band's canopy (owner, 2026-10-18: on
// the Route des Crêtes at 1200 m spruce stood 25-37 m over beech that is
// a 12-15 m canopy there with the odd spruce a few metres above it).
func TestTreesKeepToTheCanopy(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	ring := func(x0, y0, x1, y1 float64) []scenery.LatLon {
		p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
		return []scenery.LatLon{p(x0, y0), p(x1, y0), p(x1, y1), p(x0, y1), p(x0, y0)}
	}
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"landuse": "forest", "leaf_type": "mixed"}, Geometry: ring(-300, -100, 300, 1100)},
	}})
	heights := func(z float64) (tallest, median float64, n int) {
		// The test terrain's ground is the course's profile: a course at z.
		var pts []course.Point
		for i := range 101 {
			pts = append(pts, course.Point{Lat: 52 + float64(i)*10/111195, Lon: 5, Ele: z})
		}
		c, err := course.New("north", "North", pts)
		if err != nil {
			t.Fatal(err)
		}
		tr := testTerrain(c, Options{Land: lm, Region: RegionFR})
		s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
		var hs []float64
		for _, p := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
			sp := kindSpecies(p.kind)
			if sp < 0 || !allSpecies[sp].fir {
				continue
			}
			hs = append(hs, allSpecies[sp].h*float64(p.s)) // young trees too
		}
		sort.Float64s(hs)
		if len(hs) == 0 {
			return 0, 0, 0
		}
		return hs[len(hs)-1], hs[len(hs)/2], len(hs)
	}
	if tall, med, n := heights(1250); n < 100 || tall > 21 || med < 8 || med > 18 {
		t.Errorf("conifers on the crest: %d, tallest %.0f m, median %.0f m; want many, at most 21 m, median 8-18 m", n, tall, med)
	}
	if tall, med, n := heights(500); n < 100 || tall < 36 || med < 18 {
		t.Errorf("conifers in the valley: %d, tallest %.0f m, median %.0f m; want many, over 36 m, median over 18 m", n, tall, med)
	}
}
