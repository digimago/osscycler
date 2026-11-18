package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// crossing is a road 10 m up running east over one on flat ground running
// north, its decks marked, with the ground at 0.
func crossing() ([]*roadLine, *surface) {
	line := func(e0, n0, de, dn, ele float64) *roadLine {
		l := &roadLine{}
		for i := -50; i <= 50; i++ {
			d := float64(i) * fineStep
			l.samples = append(l.samples, sample{d: d + 100, e: e0 + de*d, n: n0 + dn*d, ele: ele, hw: 2.5, edge: 2.5})
		}
		return l
	}
	lines := []*roadLine{line(0, 0, 1, 0, 10), line(0, 0, 0, 1, 0)}
	markDecks(lines, 0, false, false)
	return lines, &surface{t: &terrain{landAt: func(e, n float64) float64 { return 0 }}}
}

func square(e, n, half float64) [][2]float64 {
	return [][2]float64{{e - half, n - half}, {e + half, n - half}, {e + half, n + half}, {e - half, n + half}}
}

// A building under a deck stands below it (owner, 2026-11-17: under the
// Erasmusbrug's north ramp one stood through the deck and walled the road
// off): cut down under the deck's underside, flat; left out when too low
// to stand there; one beside the deck as it was.
func TestBuildingsUnderADeckStandBelowIt(t *testing.T) {
	lines, s := crossing()
	cover := newDeckCover(lines)
	under := scenery.Footprint{ID: 1, Outline: square(-5, 0, 1.5), Kind: scenery.KindHouse, WallM: 9, RoofM: 4}
	beside := scenery.Footprint{ID: 2, Outline: square(-4, 30, 3), Kind: scenery.KindHouse, WallM: 9, RoofM: 4}
	got := underDecks(s, cover, []scenery.Footprint{under, beside})
	if len(got) != 2 {
		t.Fatalf("%d buildings kept, want both", len(got))
	}
	want := 10 - deckDepthM - deckGapM
	if f := got[0]; f.Kind != scenery.KindFlat || math.Abs(f.WallM-want) > 1e-9 || f.RoofM != 0 {
		t.Errorf("under the deck: kind %v, walls %.2f m, roof %.2f m; want flat, %.2f m, none", f.Kind, f.WallM, f.RoofM, want)
	}
	if got[1].WallM != 9 || got[1].RoofM != 4 {
		t.Errorf("beside the deck: walls %.1f m, roof %.1f m; want as it was, 9 and 4", got[1].WallM, got[1].RoofM)
	}
	// The deck 2 m up: no room for a building under it.
	for _, l := range lines {
		for i := range l.samples {
			if l.samples[i].aloft {
				l.samples[i].ele = 2
			}
		}
	}
	if got := underDecks(s, newDeckCover(lines), []scenery.Footprint{under}); len(got) != 0 {
		t.Errorf("under a deck 2 m up: %d buildings kept, want none", len(got))
	}
}

// A deck standing on supports the map draws gets no piers of its own, and
// the supports are drawn up to its underside: the Erasmusbrug got a pier
// every 24 m across the Maas, beside the piers the map has.
func TestDeckStandsOnTheMapsSupports(t *testing.T) {
	lines, s := crossing()
	upper := lines[0]
	i, j := -1, -1
	for k, sm := range upper.samples {
		if sm.aloft {
			if i < 0 {
				i = k
			}
			j = k
		}
	}
	pier := scenery.Support{ID: 3, Kind: "pier", Outline: square(6, 0, 1.5)}
	away := scenery.Support{ID: 4, Kind: "pier", Outline: square(6, 40, 1.5)}
	if !supported([]scenery.Support{pier}, upper, i, j) {
		t.Error("a deck over a mapped pier: not supported")
	}
	if supported([]scenery.Support{away}, upper, i, j) {
		t.Error("a deck nowhere near a mapped pier: supported")
	}
	var p gltf.Primitive
	prim := func(e, n float64) *gltf.Primitive { return &p }
	cover := newDeckCover(lines)
	supportMesh(prim, s, cover, pier, [3]float32{1, 1, 1})
	if len(p.Positions) == 0 {
		t.Fatal("the pier under the deck: nothing drawn")
	}
	top, low := math.Inf(-1), math.Inf(1)
	for v := 1; v < len(p.Positions); v += 3 {
		top, low = math.Max(top, float64(p.Positions[v])), math.Min(low, float64(p.Positions[v]))
	}
	if math.Abs(top-(10-deckDepthM)) > 1e-3 || low > -footM+1e-3 {
		t.Errorf("the pier from %.2f to %.2f m, want from %.2f (in the ground) to the deck's underside %.2f", low, top, -footM, 10-deckDepthM)
	}
	var q gltf.Primitive
	supportMesh(func(e, n float64) *gltf.Primitive { return &q }, s, cover, away, [3]float32{1, 1, 1})
	if len(q.Positions) != 0 {
		t.Error("a pier under no deck drawn")
	}
}

// The check measures a building standing on the riding line (owner,
// 2026-11-17: underground metro stations stood on Rotterdam's streets and
// the riding line passed through them; holes, land over road and off road
// all stayed clean); one beside the line doesn't count.
func TestBlockedRideMeasuresBuildingsOnTheRidingLine(t *testing.T) {
	_, s := crossing()
	m := &Manifest{}
	m.Path.StepM = 5
	for k := 0; k <= 20; k++ { // 100 m east along n = 0, the ground at 0
		m.Path.XYZ = append(m.Path.XYZ, float64(k)*5, 0, 0)
		m.Path.LaneM = append(m.Path.LaneM, 0)
	}
	on := scenery.Footprint{ID: 1, Outline: square(50, 0, 5), WallM: 8}
	beside := scenery.Footprint{ID: 2, Outline: square(50, 20, 5), WallM: 8}
	got := 0.0
	for _, v := range blockedRide(m, s, []scenery.Footprint{on, beside}) {
		got += v
	}
	if math.Abs(got-10) > 1.5 {
		t.Errorf("%.0f m of the riding line blocked, want the 10 m through the building", got)
	}
	got = 0
	for _, v := range blockedRide(m, s, []scenery.Footprint{beside}) {
		got += v
	}
	if got != 0 {
		t.Errorf("a building beside the line: %.0f m blocked, want none", got)
	}
}
