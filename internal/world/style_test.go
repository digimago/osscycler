package world

import (
	"fmt"
	"math"
	"reflect"
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
		if name != "mountain beech" && name != "spruce" && name != "rowan" {
			t.Errorf("a wood at 1300 m has %d %s", n, name)
		}
	}
	if high["mountain beech"] == 0 {
		t.Errorf("a wood at 1300 m: %v, want mountain beech", high)
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
