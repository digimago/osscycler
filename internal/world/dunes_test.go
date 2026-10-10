package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// Dunes by the sea grow marram on the sand and thickets of sea buckthorn,
// brambles and the like, never heather, broom or juniper (owner,
// 2026-10-10: the Amsterdam Water Supply Dunes looked like a Veluwe
// heath).
func TestDunePlants(t *testing.T) {
	c := northCourse(t)
	kx := 111195 * math.Cos(52*math.Pi/180)
	p := func(x, y float64) scenery.LatLon { return scenery.LatLon{Lat: 52 + y/111195, Lon: 5 + x/kx} }
	lm := scenery.NewLandMap(c, &scenery.Data{Elements: []scenery.Element{
		{Type: "way", ID: 1, Tags: map[string]string{"natural": "sand"}, Geometry: []scenery.LatLon{p(-300, -100), p(-8, -100), p(-8, 1100), p(-300, 1100), p(-300, -100)}},
		{Type: "way", ID: 2, Tags: map[string]string{"natural": "scrub"}, Geometry: []scenery.LatLon{p(8, -100), p(300, -100), p(300, 1100), p(8, 1100), p(8, -100)}},
	}})
	sea := func(float64, float64) float64 { return 800 }
	lm.ByTheSea(c, sea)
	tr := testTerrain(c, Options{Land: lm, SeaDistance: sea})
	s := &surface{t: tr, lists: map[[2]int]chunkLists{}}
	count := map[string][2]int{} // west (sand), east (scrub)
	for _, q := range plants(tr, s, lm, tr.chunks(), nil, nil, nil, newRouteGrid(tr.coarse, 400)) {
		name := plantKinds[q.kind].name
		side := 0
		if q.x > 0 {
			side = 1
		}
		n := count[name]
		n[side]++
		count[name] = n
	}
	for _, not := range []string{"heather", "bush broom", "bush juniper", "bush holly", "bush laurel"} {
		if n := count[not]; n != [2]int{} {
			t.Errorf("%s in the dunes: %v", not, n)
		}
	}
	m, sb := count["marram"], count["bush sea buckthorn"]
	if m[0] < 5000 || m[0] < 5*m[1] {
		t.Errorf("marram: %d on the sand, %d in the thicket", m[0], m[1])
	}
	if sb[1] < 1000 || sb[1] < 5*sb[0] {
		t.Errorf("sea buckthorn: %d on the sand, %d in the thicket", sb[0], sb[1])
	}
	if count["bush bramble"][1] == 0 || count["bush privet"][1] == 0 || count["bush creeping willow"][1] == 0 {
		t.Errorf("the thicket's other bushes: %v", count)
	}

	// The templates in their real sizes: marram 0.6-1 m (its spikes to 1.2), bramble and
	// creeping willow low and wider than tall, sea buckthorn a shorn
	// mound wider than tall, elder the tallest.
	type dims struct{ h, w float64 }
	measure := func(build func(*gltf.Primitive)) dims {
		var p gltf.Primitive
		build(&p)
		var d dims
		for i := 0; i < len(p.Positions); i += 3 {
			d.h = math.Max(d.h, float64(p.Positions[i+1]))
			d.w = math.Max(d.w, 2*math.Hypot(float64(p.Positions[i]), float64(p.Positions[i+2])))
		}
		if tris := len(p.Indices) / 3; tris > 600 {
			t.Errorf("%d triangles; keep them low-poly", tris)
		}
		return d
	}
	size := map[string]dims{"marram": measure(marramTemplate)}
	for _, b := range allBushes {
		size[b.name] = measure(func(p *gltf.Primitive) { bushTemplate(p, b) })
	}
	if d := size["marram"]; d.h < 0.6 || d.h > 1.2 || d.w > 1.4 {
		t.Errorf("marram %.2f m high, %.2f wide", d.h, d.w)
	}
	for _, low := range []string{"bramble", "creeping willow"} {
		if d := size[low]; d.h > 1.1 || d.w < 1.5*d.h {
			t.Errorf("%s %.2f m high, %.2f wide", low, d.h, d.w)
		}
	}
	if d := size["sea buckthorn"]; d.h < 1.5 || d.h > 2.5 || d.w < d.h {
		t.Errorf("sea buckthorn %.2f m high, %.2f wide", d.h, d.w)
	}
	if d := size["elder"]; d.h < 3.5 {
		t.Errorf("elder %.2f m high", d.h)
	}
}
