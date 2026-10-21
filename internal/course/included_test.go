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
		p.Distance < 29000 || p.Distance > 31000 || p.Gain < 140 || p.Gain > 200 || p.MaxGrade < 6 {
		t.Errorf("posbank: %q loop %v builtin %v, %.0f m, gain %.0f m, max %.1f %%",
			p.Name, p.Loop, p.Builtin, p.Distance, p.Gain, p.MaxGrade)
	}
	// Lapped (owner, 2026-10-13): the run-in from Rheden to the Diepesteeg,
	// then the lap round and back along the main road to it, the course
	// ending LapTailM up the Diepesteeg again.
	if l := p.Lap; l == nil || l.StartM < 2200 || l.StartM > 2450 || l.EndM < 29400 || l.EndM > 29800 ||
		math.Abs(p.Distance-l.EndM-LapTailM) > p.Spacing {
		t.Errorf("posbank's lap: %+v, course %.0f m", p.Lap, p.Distance)
	}
	// It passes the Posbank on the Beekhuizenseweg, not through the car park
	// north of it, where the first version's waypoint lay (a jog of two
	// 80° turns at 6 km).
	carPark := [2]float64{52.02942, 6.02377}
	nearest := math.Inf(1)
	for d := 0.0; d <= p.Distance; d += 2 {
		lat, lon := p.Position(d)
		nearest = math.Min(nearest, math.Hypot((lat-carPark[0])*111195, (lon-carPark[1])*111195*math.Cos(carPark[0]*math.Pi/180)))
	}
	if nearest < 15 {
		t.Errorf("posbank passes %.0f m from the Posbank car park's middle: through it", nearest)
	}
}

func TestIncludedRoutesHaveNoUTurns(t *testing.T) {
	// A router sent to a waypoint just off its road goes there and back: a
	// U-turn in the middle of a ride, the road folding over itself on
	// screen. A mountain road's hairpins turn as sharply, but onto another
	// stretch of road (the Hohneck loop has some twenty, and turns round
	// the summit's car park).
	for _, c := range Included() {
		if c.Builtin {
			continue
		}
		if u := uTurns(c); len(u) > 0 {
			t.Errorf("%s goes there and back at %.2f km", c.ID, u[0]/1000)
		}
	}
}

func TestUTurnsAreThereAndBack(t *testing.T) {
	// North 400 m with a 40 m spur east and back at 200 m: a U-turn. Then
	// a hairpin: 300 m north, a tight turn, 300 m back south 15 m west.
	mk := func(pts [][2]float64) *Course {
		var ps []Point
		for _, q := range pts {
			ps = append(ps, Point{Lat: 52 + q[1]/111195, Lon: 5 + q[0]/(111195*math.Cos(52*math.Pi/180)), Ele: 10})
		}
		c, err := New("t", "T", ps)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	spur := mk([][2]float64{{0, 0}, {0, 200}, {40, 200}, {0, 200.5}, {0, 400}})
	if u := uTurns(spur); len(u) == 0 {
		t.Error("a spur out and back isn't a U-turn")
	}
	hairpin := mk([][2]float64{{0, 0}, {0, 300}, {-5, 308}, {-10, 308}, {-15, 300}, {-15, 0}})
	if at, turn := sharpTurns(hairpin, 135), uTurns(hairpin); len(at) == 0 || len(turn) > 0 {
		t.Errorf("a hairpin: %d sharp turns (want some), %d U-turns (want none)", len(at), len(turn))
	}
}

// uTurns are the sharp turns (over 135° within 30 m) whose way on runs
// back over the way in: within 4 m of it 15-30 m either side.
func uTurns(c *Course) []float64 {
	east, north := c.Track()
	at := func(d float64) (float64, float64) {
		i := max(0, min(len(east)-1, int(math.Round(d/c.Spacing))))
		return east[i], north[i]
	}
	var out []float64
	for _, tr := range sharpTurns(c, 135) {
		for o := 15.0; o <= 30; o += 5 {
			e0, n0 := at(tr[0] - o)
			e1, n1 := at(tr[0] + o)
			if math.Hypot(e1-e0, n1-n0) < 4 {
				out = append(out, tr[0])
				break
			}
		}
	}
	return out
}

// sharpTurns are the places where the heading changes by more than limit
// over three steps of the resampled track (about 30 m): distance, turn.
func sharpTurns(c *Course, limit float64) [][2]float64 {
	var out [][2]float64
	for _, h := range turnsOver3(c) {
		if math.Abs(h[1]) > limit {
			out = append(out, [2]float64{h[0], math.Abs(h[1])})
		}
	}
	return out
}

// turnsOver3 is the change of heading over three steps from each step.
func turnsOver3(c *Course) [][2]float64 {
	east, north := c.Track()
	var heading []float64
	for i := 0; i+1 < len(east); i++ {
		heading = append(heading, math.Atan2(east[i+1]-east[i], north[i+1]-north[i])*180/math.Pi)
	}
	var out [][2]float64
	for i := 0; i+3 < len(heading); i++ {
		sum := 0.0
		for k := i; k < i+3; k++ {
			sum += math.Mod(heading[k+1]-heading[k]+540, 360) - 180
		}
		out = append(out, [2]float64{float64(i+1) * c.Spacing, sum})
	}
	return out
}
