package course

import (
	"math"
	"strings"
	"testing"
)

func TestIncluded(t *testing.T) {
	cs := Included()
	byID := map[string]*Course{}
	for _, c := range cs {
		if byID[c.ID] != nil {
			t.Errorf("two included courses are called %q", c.ID)
		}
		byID[c.ID] = c
		if !c.Builtin && (!c.Included || !strings.Contains(c.Credit, "OpenStreetMap")) {
			t.Errorf("%s: included %v, credit %q", c.ID, c.Included, c.Credit)
		}
	}
	for _, tr := range Tracks() {
		if byID[tr.ID] == nil {
			t.Errorf("track %s missing", tr.ID)
		}
	}
	// The Posbank Loop: Rheden, De Steeg, up the Diepesteeg to the Posbank,
	// over the Veluwe to Eerbeek, Dieren and back.
	p := byID["posbank"]
	if p == nil {
		t.Fatal("no posbank")
	}
	if p.Name != "Posbank Loop" || p.Loop || p.Builtin ||
		p.Distance < 32000 || p.Distance > 34000 || p.Gain < 140 || p.Gain > 200 || p.MaxGrade < 6 {
		t.Errorf("posbank: %q loop %v builtin %v, %.0f m, gain %.0f m, max %.1f %%",
			p.Name, p.Loop, p.Builtin, p.Distance, p.Gain, p.MaxGrade)
	}
}

func TestIncludedRoutesHaveNoUTurns(t *testing.T) {
	// A router sent to a waypoint just off its road goes there and back: a
	// U-turn in the middle of a ride, the road folding over itself on
	// screen. Real corners stay well under 135° over 30 m.
	for _, c := range Included() {
		if c.Builtin {
			continue
		}
		if at, turn := sharpestTurn(c); turn > 135 {
			t.Errorf("%s turns %.0f° within 30 m at %.2f km", c.ID, turn, at/1000)
		}
	}
}

// sharpestTurn is the largest change of heading over three steps of the
// resampled track (about 30 m), and where it is.
func sharpestTurn(c *Course) (at, turn float64) {
	east, north := c.Track()
	var heading []float64
	for i := 0; i+1 < len(east); i++ {
		heading = append(heading, math.Atan2(east[i+1]-east[i], north[i+1]-north[i])*180/math.Pi)
	}
	for i := 0; i+3 < len(heading); i++ {
		sum := 0.0
		for k := i; k < i+3; k++ {
			d := math.Mod(heading[k+1]-heading[k]+540, 360) - 180
			sum += d
		}
		if math.Abs(sum) > turn {
			at, turn = float64(i+1)*c.Spacing, math.Abs(sum)
		}
	}
	return at, turn
}
