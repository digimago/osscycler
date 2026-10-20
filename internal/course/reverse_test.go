package course

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A reversed course is the same road the other way (owner, 2026-10-13):
// its profile the forward one's backwards, the climbs descents, and the
// profile the reversed GPX would give.
func TestReversed(t *testing.T) {
	pts := pointsAlong([2]float64{0, 0}, [2]float64{0, 800}, [2]float64{600, 800}, [2]float64{600, 1500})
	for i := range pts { // a hill in the middle, to tell the ends apart
		pts[i].Ele += 30 * math.Exp(-math.Pow(float64(i-90)/25, 2))
	}
	c, err := New("hill", "Hill", pts)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Reversed()
	if r.ID != "hill-reverse" || r.Name != "Hill (reverse)" || r.Distance != c.Distance {
		t.Fatalf("reversed %q %q %.1f m", r.ID, r.Name, r.Distance)
	}
	if !near(r.Gain, c.Loss, 1e-9) || !near(r.Loss, c.Gain, 1e-9) || r.MaxGrade != -c.MinGrade || r.MinGrade != -c.MaxGrade {
		t.Errorf("gain %.2f loss %.2f max %.2f min %.2f, forward gain %.2f loss %.2f max %.2f min %.2f",
			r.Gain, r.Loss, r.MaxGrade, r.MinGrade, c.Gain, c.Loss, c.MaxGrade, c.MinGrade)
	}
	for d := 0.0; d <= c.Distance; d += 7 {
		re, rg := r.At(d)
		fe, _ := c.At(c.Distance - d)
		if !near(re, fe, 1e-9) {
			t.Fatalf("at %.0f m the reverse is at %.3f m, the forward course there %.3f", d, re, fe)
		}
		// The grade the other way: the forward grade over the same sample.
		if d < c.Distance-c.Spacing {
			_, fg := c.At(c.Distance - d - c.Spacing/2)
			_, rg2 := r.At(d + c.Spacing/2)
			if !near(rg2, -fg, 1e-6) {
				t.Fatalf("at %.0f m grade %.3f %%, the forward course %.3f %% there", d, rg, fg)
			}
		}
		rlat, rlon := r.Position(d)
		flat, flon := c.Position(c.Distance - d)
		if math.Hypot((rlat-flat)*metresPerDegree, (rlon-flon)*metresPerDegree) > 0.01 {
			t.Fatalf("at %.0f m the reverse is at %.6f,%.6f, the forward course %.6f,%.6f", d, rlat, rlon, flat, flon)
		}
	}
	// As the reversed GPX: the same profile to within despiking's and
	// smoothing's rounding.
	back := slices.Clone(pts)
	slices.Reverse(back)
	g, err := New("back", "Back", back)
	if err != nil {
		t.Fatal(err)
	}
	for d := 0.0; d <= c.Distance; d += 10 {
		re, _ := r.At(d)
		ge, _ := g.At(d)
		if !near(re, ge, 0.01) {
			t.Fatalf("at %.0f m reversed %.3f m, from the reversed GPX %.3f", d, re, ge)
		}
	}
}

// A reversed course that comes back to its start is lapped the other way
// round, the same run-in and a lap as long; ForwardAt finds its points on
// the forward course.
func TestReversedLap(t *testing.T) {
	c, err := New("square", "Square", pointsAlong([2]float64{0, 0}, [2]float64{500, 0}, [2]float64{500, 500},
		[2]float64{1000, 500}, [2]float64{1000, 0}, [2]float64{500, 0}, [2]float64{0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	fwd, rev := withReverse(c)
	if rev.Forward != fwd || fwd.Forward != nil {
		t.Fatalf("forward of the reverse %p, want %p; of the forward %p", rev.Forward, fwd, fwd.Forward)
	}
	fl, rl := fwd.Lap, rev.Lap
	if rl == nil || !near(rl.StartM, fl.StartM, 25) || !near(rl.Length(), fl.Length(), 25) {
		t.Fatalf("reversed lap %+v, forward %+v", rl, fl)
	}
	at := func(c *Course, d float64) [2]float64 {
		e, n := fwd.Project(c.Position(d))
		return [2]float64{e, n}
	}
	// Into the lap the other way round: on east along the bottom (the
	// forward lap turns north there).
	if p := at(rev, rl.StartM+100); p[0] < 550 || math.Abs(p[1]) > 10 {
		t.Errorf("100 m into the reversed lap at %.0f,%.0f, want east along the bottom", p[0], p[1])
	}
	for d := 0.0; d <= rev.Distance; d += 50 {
		a, b := at(rev, d), at(fwd, rev.ForwardAt(d))
		// Within the laps' own detection: their ends lie up to lapMeetM
		// apart where the run-in meets the lap.
		if g := math.Hypot(a[0]-b[0], a[1]-b[1]); g > lapMeetM {
			t.Errorf("%.0f m along the reverse is %.1f m from the forward course's point ForwardAt gives (%.0f m)", d, g, rev.ForwardAt(d))
		}
	}
	// Opposite: the same spot the other way round, both ways; none on the
	// run-in out, whose way back neither lapped course rides.
	for _, pair := range [][2]*Course{{fwd, rev}, {rev, fwd}} {
		a, b := pair[0], pair[1]
		for d := a.Lap.StartM + 30; d < a.Lap.EndM-30; d += 50 {
			o, ok := a.Opposite(b, d)
			if !ok {
				t.Fatalf("%s at %.0f m: no opposite on the lap", a.ID, d)
			}
			if g := math.Hypot(at(a, d)[0]-at(b, o)[0], at(a, d)[1]-at(b, o)[1]); g > 0.5 {
				t.Errorf("%s at %.0f m: the opposite %.0f m along %s is %.1f m away", a.ID, d, o, b.ID, g)
			}
		}
		if _, ok := a.Opposite(b, a.Lap.StartM/2); ok {
			t.Errorf("%s: an opposite on the run-in out", a.ID)
		}
	}
	if fwd.ForwardAt(123) != 123 {
		t.Error("ForwardAt of a forward course isn't its own distance")
	}
}

// LoadDir gives each course its reverse after it, unless a file of its
// own has the reverse's ID; a reverse's file gets none.
func TestLoadDirReverses(t *testing.T) {
	dir := t.TempDir()
	write := func(id string, pts []Point) {
		var b strings.Builder
		b.WriteString(`<gpx><trk><name>` + id + `</name><trkseg>`)
		for _, p := range pts {
			fmt.Fprintf(&b, `<trkpt lat="%.10f" lon="%.10f"><ele>%.2f</ele></trkpt>`, p.Lat, p.Lon, p.Ele)
		}
		b.WriteString(`</trkseg></trk></gpx>`)
		if err := os.WriteFile(filepath.Join(dir, id+".gpx"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a", line(500, 10, 2))
	write("b", line(500, 10, 3))
	write("b-reverse", line(500, 10, -1))
	cs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	// By file name ("b-reverse.gpx" before "b.gpx"), each reverse after its
	// course.
	if want := []string{"a", "a-reverse", "b-reverse", "b"}; !slices.Equal(ids, want) {
		t.Fatalf("courses %v, want %v", ids, want)
	}
	if cs[1].Forward != cs[0] || cs[1].Name != "a (reverse)" {
		t.Errorf("a's reverse: forward %v, name %q", cs[1].Forward, cs[1].Name)
	}
	if cs[2].Forward != nil || cs[2].Name != "b-reverse" {
		t.Errorf("b-reverse is the derived one, not its file: forward %v, name %q", cs[2].Forward, cs[2].Name)
	}
}

// The included routes come both ways; the test tracks don't.
func TestIncludedReverses(t *testing.T) {
	byID := map[string]*Course{}
	for _, c := range Included() {
		byID[c.ID] = c
	}
	for _, tr := range Tracks() {
		if byID[tr.ID+ReverseSuffix] != nil {
			t.Errorf("track %s reversed", tr.ID)
		}
	}
	p, r := byID["posbank"], byID["posbank-reverse"]
	if r == nil || r.Forward != p || r.Name != "Posbank Loop (reverse)" || !r.Included {
		t.Fatalf("posbank's reverse: %+v", r)
	}
	// The same run-in from Rheden to the Diepesteeg, then the lap the other
	// way round, as long.
	if r.Lap == nil || !near(r.Lap.StartM, p.Lap.StartM, 100) || !near(r.Lap.Length(), p.Lap.Length(), 100) {
		t.Errorf("posbank-reverse's lap %+v, forward %+v", r.Lap, p.Lap)
	}
}
