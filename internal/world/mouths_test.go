package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/scenery"
)

// An unridden lane running into the main road at a shallow angle is
// redrawn to meet it square over its last metres (owner, 2026-10-16, the
// lane off the Rue du Hohneck); one meeting square already, one the route
// rides, and one joining a road of its own class are left as they are.
func TestSquareMouths(t *testing.T) {
	main := scenery.Way{Line: [][2]float64{{-100, 0}, {0, 0}, {100, 0}}, Nodes: []int64{1, 2, 3}, Class: "secondary"}
	shallow := scenery.Way{Line: [][2]float64{{0, 0}, {20, 5}, {40, 10}, {60, 15}}, Nodes: []int64{2, 10, 11, 12}, Class: "residential"}
	square := scenery.Way{Line: [][2]float64{{100, 0}, {100, 20}, {100, 40}}, Nodes: []int64{3, 20, 21}, Class: "residential"}
	peer := scenery.Way{Line: [][2]float64{{60, 15}, {80, 20}, {100, 25}}, Nodes: []int64{12, 30, 31}, Class: "residential"}
	ways := []scenery.Way{main, shallow, square, peer}
	out := squareMouths(ways, map[int]bool{})
	got := out[1].Line
	if len(got) < 3 || got[0] != [2]float64{0, 0} {
		t.Fatalf("the shallow lane's line: %v", got)
	}
	// Its first segment stands square to the main road, mouthStubM long.
	if dx, dy := got[1][0]-got[0][0], got[1][1]-got[0][1]; math.Abs(dx) > 1e-9 || math.Abs(dy-mouthStubM) > 1e-9 {
		t.Errorf("the lane leaves the main road along (%.2f, %.2f), want (0, %.0f)", dx, dy, mouthStubM)
	}
	if len(out[1].Nodes) != len(got) {
		t.Errorf("%d nodes for %d points", len(out[1].Nodes), len(got))
	}
	// Its far end, joining a road of its own class, is as it was.
	if last := got[len(got)-1]; last != [2]float64{60, 15} {
		t.Errorf("the lane's far end moved to %v", last)
	}
	for i, name := range map[int]string{2: "a square mouth", 3: "a road joining its own class"} {
		if len(out[i].Line) != len(ways[i].Line) {
			t.Errorf("%s was redrawn: %v", name, out[i].Line)
		}
	}
	// Ridden, the lane keeps its line (the course rides it as mapped).
	if r := squareMouths(ways, map[int]bool{1: true}); len(r[1].Line) != len(shallow.Line) {
		t.Errorf("a ridden lane was redrawn: %v", r[1].Line)
	}
	// The shared map data is untouched.
	if len(ways[1].Line) != 4 {
		t.Error("the input was changed")
	}
}
