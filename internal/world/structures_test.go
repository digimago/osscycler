package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// A bridge the map tags over ground the model leaves out (the AHN has no
// heights over water) is a deck (owner, 2026-11-17: the Erasmusbrug was a
// road on an embankment across the Maas, the land eased up to it); an
// untagged road there isn't.
func TestTaggedBridgeOverNoDataIsADeck(t *testing.T) {
	c := northCourse(t)
	ground := func(lat, lon float64) (float64, bool) {
		_, n := c.Project(lat, lon)
		if n > 400 && n < 600 {
			return 0, false // the river
		}
		return 10, true
	}
	tr := testTerrain(c, Options{Elevation: ground})
	line := func(way int) *roadLine {
		l := &roadLine{samples: append([]sample(nil), tr.fine...)}
		for i := range l.samples {
			l.samples[i].way = way
		}
		return l
	}
	bridge, road := line(1), line(0)
	tr.markAloft([]*roadLine{bridge, road}, func(s sample) bool { return s.way == 1 })
	for _, s := range bridge.samples {
		over := s.n > 400 && s.n < 600
		if over != s.aloft && math.Abs(s.n-400) > 1 && math.Abs(s.n-600) > 1 {
			t.Fatalf("the tagged bridge at %.0f m north: aloft %v, want %v", s.n, s.aloft, over)
		}
	}
	for _, s := range road.samples {
		if s.aloft {
			t.Fatalf("an untagged road over no ground model at %.0f m north: a deck", s.n)
		}
	}
}

// bridgeAlong is a deck running east from 0 to length at 10 m up over flat
// ground, its outline 20 m wide, with supports of the given sizes (side
// in metres, a square) at the given distances along.
func bridgeAlong(length float64, structure string, sups map[float64]float64) (scenery.Bridge, []scenery.Support, []*roadLine, *surface) {
	l := &roadLine{}
	for d := 0.0; d <= length; d += fineStep {
		l.samples = append(l.samples, sample{d: d, e: d, ele: 10, hw: 4, edge: 4, aloft: true})
	}
	b := scenery.Bridge{ID: 1, Structure: structure, Outline: [][2]float64{{-1, -10}, {length + 1, -10}, {length + 1, 10}, {-1, 10}}}
	var ss []scenery.Support
	for a, side := range sups {
		ss = append(ss, scenery.Support{ID: int64(a), Kind: "pier", Outline: square(a, 0, side/2)})
	}
	return b, ss, []*roadLine{l}, &surface{t: &terrain{landAt: func(e, n float64) float64 { return 0 }}}
}

// midSpan tells whether the bridge's structure reaches over its deck
// (more than 1.5 m: over its railings) between e0 and e1: a suspension bridge's cable and
// hangers at the middle of its span, where two pylons' stays leave off.
func midSpan(b scenery.Bridge, ss []scenery.Support, lines []*roadLine, s *surface, e0, e1 float64) bool {
	var p gltf.Primitive
	addStructures(func(e, n float64) *gltf.Primitive { return &p }, s, []scenery.Bridge{b}, ss, lines, [3]float32{1, 1, 1}, [3]float32{1, 1, 1})
	for v := 0; v+2 < len(p.Positions); v += 3 {
		if e := float64(p.Positions[v]); e > e0 && e < e1 && p.Positions[v+1] > 11.5 {
			return true
		}
	}
	return false
}

// tops are where (east) a bridge's structure stands more than 20 m over
// its deck, by 10 m along it.
func tops(b scenery.Bridge, ss []scenery.Support, lines []*roadLine, s *surface) map[int]bool {
	var p gltf.Primitive
	addStructures(func(e, n float64) *gltf.Primitive { return &p }, s, []scenery.Bridge{b}, ss, lines, [3]float32{1, 1, 1}, [3]float32{1, 1, 1})
	at := map[int]bool{}
	for v := 0; v+2 < len(p.Positions); v += 3 {
		if p.Positions[v+1] > 30 {
			at[int(math.Floor(float64(p.Positions[v])/10))] = true
		}
	}
	return at
}

// A cable-stayed bridge's pylon stands on the larger support at either end
// of its longest span (the Erasmusbrug's on its pylon's pier, not the
// bascule's); a "suspension" bridge with a short main span is taken for
// cable-stayed (the Erasmusbrug is tagged so).
func TestPylonStandsOnTheMainSpansLargerSupport(t *testing.T) {
	for _, kind := range []string{"cable-stayed", "suspension"} {
		b, ss, lines, s := bridgeAlong(400, kind, map[float64]float64{60: 20, 300: 8})
		got := tops(b, ss, lines, s)
		if !got[6] {
			t.Errorf("%s: no pylon on the large pier at 60 m (tall at %v)", kind, keys(got))
		}
		if got[30] {
			t.Errorf("%s: a pylon on the small pier at 300 m", kind)
		}
	}
	b, ss, lines, s := bridgeAlong(400, "beam", map[float64]float64{60: 20, 300: 8})
	if got := tops(b, ss, lines, s); len(got) != 0 {
		t.Errorf("a beam bridge: something tall at %v", keys(got))
	}
}

// Alike supports at both ends of the main span carry a pylon each (the
// Willemsbrug, tagged suspension); a suspension bridge's main span
// suspensionMinM or longer hangs from towers on both, its cables low at
// the middle.
func TestAlikeSupportsCarryTwoPylonsOrTowers(t *testing.T) {
	b, ss, lines, s := bridgeAlong(400, "suspension", map[float64]float64{100: 15, 300: 15})
	got := tops(b, ss, lines, s)
	if !got[10] || !got[30] || got[20] {
		t.Errorf("two pylons wanted at 100 and 300 m, none between: tall at %v", keys(got))
	}
	if midSpan(b, ss, lines, s, 197, 203) {
		t.Error("a 200 m span between alike supports hung from cables, want two pylons' stays")
	}
	b, ss, lines, s = bridgeAlong(800, "suspension", map[float64]float64{100: 15, 700: 15})
	got = tops(b, ss, lines, s)
	if !got[10] || !got[70] || got[40] {
		t.Errorf("towers wanted at 100 and 700 m, the cable low between: tall at %v", keys(got))
	}
	if !midSpan(b, ss, lines, s, 395, 405) {
		t.Error("a 600 m suspension span: no cable at its middle")
	}
}

func keys(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k*10)
	}
	return out
}

// A short bridge keeps the decks its roads make (owner, 2026-10-22/23, the
// N348's over the Posbank Loop's cycle underpass): its outline drew a
// second slab out over the banks, railed again.
func TestShortBridgeIsNoStructure(t *testing.T) {
	b, _, lines, _ := bridgeAlong(30, "beam", nil)
	if got := longBridges([]scenery.Bridge{b}, lines); len(got) != 0 {
		t.Errorf("a 30 m bridge drawn as a structure")
	}
	b, _, lines, _ = bridgeAlong(120, "beam", nil)
	if got := longBridges([]scenery.Bridge{b}, lines); len(got) != 1 {
		t.Errorf("a 120 m bridge not drawn as a structure")
	}
}
