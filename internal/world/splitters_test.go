package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/scenery"
)

// dualRoad is a two-way road from the south to node 1 at 0, 0, where it
// parts into two one-way carriageways 7 m apart (centre to centre) by 15 m
// north, on to 100 m: northbound on the west, southbound on the east.
// southbound reverses the east one's traffic (both then go north).
func dualRoad(southbound bool) []scenery.Way {
	ow := 1
	if !southbound {
		ow = -1 // drawn southwards, traffic against: northwards
	}
	return []scenery.Way{
		{Line: [][2]float64{{0, -50}, {0, 0}}, Nodes: []int64{10, 1}, Name: "Oranjeweg", Class: "secondary", WidthM: 7},
		{Line: [][2]float64{{0, 0}, {-3.5, 15}, {-3.5, 100}}, Nodes: []int64{1, 2, 3}, Name: "Oranjeweg", Class: "secondary", WidthM: 3.5, Oneway: 1},
		{Line: [][2]float64{{3.5, 100}, {3.5, 15}, {0, 0}}, Nodes: []int64{4, 5, 1}, Name: "Oranjeweg", Class: "secondary", WidthM: 3.5, Oneway: ow},
	}
}

func islandsOf(t *testing.T, ws []scenery.Way) []splitIsland {
	t.Helper()
	ls := networkLines(ws, nil, func(e, n float64, _ bool) bool { return true })
	for _, l := range ls {
		finishRoad(l.samples, 1)
	}
	return splitterIslands(ls, nil)
}

// Where the map draws a road as two one-way carriageways, the strip
// between them is a raised island from where it is splitNoseW wide, its
// nose rounded (owner, 2026-10-24: the Velp loop's roundabouts).
func TestSplitRoadGetsAnIsland(t *testing.T) {
	isls := islandsOf(t, dualRoad(true))
	if len(isls) != 1 {
		t.Fatalf("%d islands, want 1", len(isls))
	}
	isl := isls[0]
	// The carriageways' centres part at 7/15 m per metre north: the strip
	// is splitNoseW wide 10.07 m north of the node.
	if isl.lo[1] < 9.5 || isl.lo[1] > 11.5 {
		t.Errorf("the nose's tip at %.2f m north, want about 10.1", isl.lo[1])
	}
	if isl.hi[1] < 99 {
		t.Errorf("the island ends %.1f m north, want to the carriageways' end at 100", isl.hi[1])
	}
	// Between the carriageways' edges (1.75 m from their centres), over
	// them by at most splitOverlapM.
	for _, q := range isl.poly {
		if q[1] > 16 && math.Abs(q[0]) > 3.5-1.75+splitOverlapM+1e-6 {
			t.Errorf("outline point %.2f, %.2f outside the strip", q[0], q[1])
			break
		}
	}
	paved, grass := 0, 0
	for _, p := range isl.paved {
		if p {
			paved++
		} else {
			grass++
		}
	}
	if paved == 0 || grass == 0 {
		t.Errorf("%d paved and %d grass triangles: want paved noses, grass between (3.5 m wide)", paved, grass)
	}
	ix := newIslandIndex(isls)
	if top, hedge, on := ix.at(0, 50); !on || !hedge || math.Abs(top-kerbM) > 1e-6 {
		t.Errorf("mid-island: on %v, hedge %v, top %.3f; want on grass a hedge may take, kerbM up", on, hedge, top)
	}
	if _, hedge, on := ix.at(0, 14); !on || hedge {
		t.Errorf("by the nose: on %v, hedge %v; want on the island, no hedge", on, hedge)
	}
}

// Carriageways side by side with traffic the same way aren't a split road.
func TestNoIslandBetweenRoadsTheSameWay(t *testing.T) {
	if isls := islandsOf(t, dualRoad(false)); len(isls) != 0 {
		t.Errorf("%d islands between two northbound roads, want none", len(isls))
	}
}

// A road across the strip breaks the island; a carriageway inside it
// (a slip road between the two) leaves no island across it.
func TestIslandBreaksForARoadAcross(t *testing.T) {
	ws := append(dualRoad(true), scenery.Way{Line: [][2]float64{{-40, 60}, {40, 60}}, Nodes: []int64{20, 21}, Name: "Dwarsweg", Class: "residential", WidthM: 5})
	isls := islandsOf(t, ws)
	if len(isls) != 2 {
		t.Fatalf("%d islands, want 2: either side of the road across", len(isls))
	}
	for _, isl := range isls {
		if isl.lo[1] < 62.5 && isl.hi[1] > 57.5 {
			t.Errorf("an island from %.1f to %.1f m north reaches over the road across at 60", isl.lo[1], isl.hi[1])
		}
	}
	ws = append(dualRoad(true), scenery.Way{Line: [][2]float64{{0, 30}, {0, 90}}, Nodes: []int64{30, 31}, Name: "Busbaan", Class: "service", WidthM: 3, Oneway: 1})
	for _, isl := range islandsOf(t, ws) {
		if isl.hi[1] > 31 && isl.lo[1] < 89 {
			t.Errorf("an island from %.1f to %.1f m north over the lane between the carriageways (30-90)", isl.lo[1], isl.hi[1])
		}
	}
}

// A nose is round and the island narrows to it, bullet-shaped, rather
// than ending the strip's full width with its corners rounded; at a
// roundabout it stops short of the ring's edge (owner, 2026-10-25: the
// noses stood out into the Velp loop's rings).
func TestIslandNoseIsBulletShapedAndClearOfTheRing(t *testing.T) {
	isl := islandsOf(t, dualRoad(true))[0]
	// The strip is 3.5 m wide where the carriageways end at 100 m north:
	// a metre from the end the island is no wider than the nose and a
	// metre of narrowing.
	for _, q := range isl.poly {
		if q[1] > isl.hi[1]-1 && math.Abs(q[0]) > splitNoseR+splitTaperK+1e-6 {
			t.Errorf("outline point %.2f, %.2f: %.2f m from the middle within a metre of the nose, want at most %.2f", q[0], q[1], math.Abs(q[0]), splitNoseR+splitTaperK)
			break
		}
	}
	// A roundabout north of 100 m: the carriageways run into it.
	ws := dualRoad(true)
	ws[1].Line[2], ws[2].Line[0] = [2]float64{-3.5, 110}, [2]float64{3.5, 110}
	ls := networkLines(ws, nil, func(e, n float64, _ bool) bool { return true })
	for _, l := range ls {
		finishRoad(l.samples, 1)
	}
	ring := patch{poly: [][2]float64{{-20, 100}, {20, 100}, {20, 140}, {-20, 140}}, rb: &roundabout{}}
	isls := splitterIslands(ls, []patch{ring})
	if len(isls) != 1 {
		t.Fatalf("%d islands, want 1", len(isls))
	}
	if hi := isls[0].hi[1]; hi > 100-splitRingBackM+0.15 {
		t.Errorf("the nose reaches %.2f m north, want no further than %.2f (the ring's edge at 100, less splitRingBackM)", hi, 100-splitRingBackM)
	}
	if far := isls[0].strip[len(isls[0].strip)-1]; (far.a[1]+far.b[1])/2 < 100 {
		t.Errorf("the strip's asphalt ends %.2f m north, short of the ring's edge at 100", (far.a[1]+far.b[1])/2)
	}
}
