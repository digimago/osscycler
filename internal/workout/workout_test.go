package workout

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sample uses every block type, mixed attribute case and a text event, the
// way .zwo files in the wild do.
const sample = `<workout_file>
    <author>osscycler</author>
    <name>Test &amp; Tune</name>
    <description>Every block type</description>
    <sportType>bike</sportType>
    <tags><tag name="TEST"/></tags>
    <workout>
        <Warmup Duration="600" PowerLow="0.25" PowerHigh="0.75"/>
        <SteadyState Duration="300" Power="0.88" Cadence="90">
            <textevent timeoffset="10" message="Settle in"/>
        </SteadyState>
        <IntervalsT Repeat="3" OnDuration="60" OffDuration="30" OnPower="1.2" OffPower="0.5"/>
        <Ramp duration="120" powerlow="0.6" powerhigh="1.0"/>
        <FreeRide Duration="180" FlatRoad="1"/>
        <Cooldown Duration="300" PowerLow="0.25" PowerHigh="0.7"/>
    </workout>
</workout_file>`

func parse(t *testing.T, s string) *Workout {
	t.Helper()
	w, err := ParseZWO(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestParseZWO(t *testing.T) {
	w := parse(t, sample)
	if w.Name != "Test & Tune" || w.Author != "osscycler" || len(w.Blocks) != 6 {
		t.Fatalf("workout %q by %q with %d blocks", w.Name, w.Author, len(w.Blocks))
	}
	want := []Block{
		{Type: Warmup, Duration: 600 * time.Second, PowerLow: 0.25, PowerHigh: 0.75},
		{Type: SteadyState, Duration: 300 * time.Second, Power: 0.88, Cadence: 90,
			Texts: []Text{{Offset: 10 * time.Second, Message: "Settle in"}}},
		{Type: IntervalsT, Repeat: 3, OnDuration: 60 * time.Second, OffDuration: 30 * time.Second, OnPower: 1.2, OffPower: 0.5},
		{Type: Ramp, Duration: 120 * time.Second, PowerLow: 0.6, PowerHigh: 1.0},
		{Type: FreeRide, Duration: 180 * time.Second},
		{Type: Cooldown, Duration: 300 * time.Second, PowerLow: 0.25, PowerHigh: 0.7},
	}
	for i := range want {
		if !reflect.DeepEqual(w.Blocks[i], want[i]) {
			t.Errorf("block %d:\n got %+v\nwant %+v", i, w.Blocks[i], want[i])
		}
	}
}

func TestTimeline(t *testing.T) {
	w := parse(t, sample)
	segs := w.Timeline()
	// warmup, steady, 3 x (on, off), ramp, free, cooldown
	if len(segs) != 1+1+6+1+1+1 {
		t.Fatalf("%d segments", len(segs))
	}
	if total := w.Duration(); total != (600+300+3*90+120+180+300)*time.Second {
		t.Errorf("duration %v", total)
	}
	for _, tc := range []struct {
		at    time.Duration
		label string
		want  float64
		free  bool
	}{
		{0, "Warmup", 0.25, false},
		{300 * time.Second, "Warmup", 0.5, false}, // halfway up the ramp
		{650 * time.Second, "Steady", 0.88, false},
		{900 * time.Second, "Interval 1/3 on", 1.2, false},
		{965 * time.Second, "Interval 1/3 off", 0.5, false},
		{1150 * time.Second, "Interval 3/3 off", 0.5, false},
		{1230 * time.Second, "Ramp", 0.8, false},
		{1300 * time.Second, "Free ride", 0, true},
		{1470 * time.Second, "Cooldown", 0.7, false}, // cooldowns ride downwards
		{1769 * time.Second, "Cooldown", 0.25, false},
	} {
		i, got, ok := At(segs, tc.at)
		if !ok || segs[i].Label != tc.label || (segs[i].Kind == KindFree) != tc.free || abs(got-tc.want) > 0.002 {
			t.Errorf("At(%v) = %s %.3f (ok %v), want %s %.3f", tc.at, segs[i].Label, got, ok, tc.label, tc.want)
		}
	}
	if _, _, ok := At(segs, w.Duration()); ok {
		t.Error("At past the end reported ok")
	}
}

func TestRoundTrip(t *testing.T) {
	w := parse(t, sample)
	var buf bytes.Buffer
	if err := w.WriteZWO(&buf); err != nil {
		t.Fatal(err)
	}
	back := parse(t, buf.String())
	if !reflect.DeepEqual(w, back) {
		t.Errorf("round trip changed the workout:\n%s", buf.String())
	}
}

func TestRejects(t *testing.T) {
	for name, doc := range map[string]string{
		"run":       `<workout_file><sportType>run</sportType><workout><SteadyState Duration="60" Power="0.5"/></workout></workout_file>`,
		"unknown":   `<workout_file><workout><SolidState Duration="60" Power="0.5"/></workout></workout_file>`,
		"empty":     `<workout_file><workout></workout></workout_file>`,
		"bad num":   `<workout_file><workout><SteadyState Duration="sixty" Power="0.5"/></workout></workout_file>`,
		"no time":   `<workout_file><workout><SteadyState Power="0.5"/></workout></workout_file>`,
		"too hard":  `<workout_file><workout><SteadyState Duration="60" Power="9"/></workout></workout_file>`,
		"not zwo":   `<gpx><trk/></gpx>`,
		"no repeat": `<workout_file><workout><IntervalsT OnDuration="60" OnPower="1"/></workout></workout_file>`,
	} {
		if _, err := ParseZWO(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWatts(t *testing.T) {
	if got := Watts(0.88, 250, 1); got != 220 {
		t.Errorf("Watts(0.88, 250, 1) = %v", got)
	}
	if got := Watts(1.2, 250, 0.95); got != 285 {
		t.Errorf("Watts at 95 %% intensity = %v", got)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

const textSample = `# a comment
name     Sweet spot 3x10
author   me
warmup   10m 40-75%
3x 10m 90% / 5m 55% @88rpm
> 0:10 Hold it steady
steady   5m 88%
ramp     1m30s 60-100%
free     3m
max      30s
cooldown 5m 70-30%
`

func TestParseText(t *testing.T) {
	w, err := ParseText(strings.NewReader(textSample))
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Sweet spot 3x10" || w.Author != "me" || len(w.Blocks) != 7 {
		t.Fatalf("%q by %q, %d blocks", w.Name, w.Author, len(w.Blocks))
	}
	iv := w.Blocks[1]
	if iv.Type != IntervalsT || iv.Repeat != 3 || iv.OnDuration != 10*time.Minute || iv.OnPower != 0.9 ||
		iv.OffDuration != 5*time.Minute || iv.OffPower != 0.55 || iv.Cadence != 88 ||
		len(iv.Texts) != 1 || iv.Texts[0] != (Text{Offset: 10 * time.Second, Message: "Hold it steady"}) {
		t.Errorf("intervals: %+v", iv)
	}
	if r := w.Blocks[3]; r.Duration != 90*time.Second || r.PowerLow != 0.6 || r.PowerHigh != 1 {
		t.Errorf("ramp: %+v", r)
	}
	if w.Blocks[5].Type != MaxEffort || w.Blocks[6].Type != Cooldown {
		t.Errorf("types: %v %v", w.Blocks[5].Type, w.Blocks[6].Type)
	}
	if got := w.Duration(); got != (10+3*15+5)*time.Minute+90*time.Second+3*time.Minute+30*time.Second+5*time.Minute {
		t.Errorf("duration %v", got)
	}
}

func TestTextRoundTrips(t *testing.T) {
	// Text → workout → text is stable, and .zwo → text → workout is lossless.
	w, err := ParseText(strings.NewReader(textSample))
	if err != nil {
		t.Fatal(err)
	}
	text := FormatText(w)
	again, err := ParseText(strings.NewReader(text))
	if err != nil {
		t.Fatalf("formatted text doesn't parse: %v\n%s", err, text)
	}
	if FormatText(again) != text {
		t.Errorf("format not stable:\n%s\nvs\n%s", text, FormatText(again))
	}
	if !reflect.DeepEqual(w, again) {
		t.Errorf("text round trip changed the workout:\n%s", text)
	}

	z := parse(t, sample)
	fromText, err := ParseText(strings.NewReader(FormatText(z)))
	if err != nil {
		t.Fatalf(".zwo as text doesn't parse: %v\n%s", err, FormatText(z))
	}
	if !reflect.DeepEqual(z, fromText) {
		t.Errorf(".zwo → text → workout changed it:\n%s", FormatText(z))
	}
}

func TestTextErrors(t *testing.T) {
	for _, tc := range []struct {
		text string
		line int
	}{
		{"name x\nsteady 5m", 2},
		{"steady 5m 88%\nwobble 3m", 2},
		{"> 0:10 orphan message", 1},
		{"steady five 88%", 1},
		{"3x 1m 120% 1m 50%", 1},
		{"steady 5m 88% @fast", 1},
		{"ramp 2m 60%", 1},
		{"0x 1m 100%", 1},
	} {
		_, err := ParseText(strings.NewReader(tc.text))
		var le *LineError
		if !errors.As(err, &le) || le.Line != tc.line {
			t.Errorf("%q: err %v, want an error on line %d", tc.text, err, tc.line)
		}
	}
	if _, err := ParseText(strings.NewReader("name only a name\n")); err == nil {
		t.Error("accepted a workout without blocks")
	}
}

func TestDurations(t *testing.T) {
	for s, want := range map[string]time.Duration{
		"45s": 45 * time.Second, "10m": 10 * time.Minute, "1h": time.Hour, "1m30s": 90 * time.Second,
		"1:30": 90 * time.Second, "1:02:03": time.Hour + 2*time.Minute + 3*time.Second, "12.5s": 12500 * time.Millisecond,
	} {
		got, err := ParseDuration(s)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v", s, got, err)
		}
		if back, _ := ParseDuration(FormatDuration(got)); back != got {
			t.Errorf("FormatDuration(%v) = %q doesn't parse back", got, FormatDuration(got))
		}
	}
}
