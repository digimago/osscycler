package course

import (
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
