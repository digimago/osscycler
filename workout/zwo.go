package workout

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// zwoFile mirrors a .zwo document. Blocks are read generically so their
// order is kept and attribute names can be matched case-insensitively.
type zwoFile struct {
	XMLName     xml.Name `xml:"workout_file"`
	Author      string   `xml:"author"`
	Name        string   `xml:"name"`
	Description string   `xml:"description"`
	SportType   string   `xml:"sportType"`
	Workout     struct {
		Blocks []zwoBlock `xml:",any"`
	} `xml:"workout"`
}

type zwoBlock struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Texts   []struct {
		Attrs []xml.Attr `xml:",any,attr"`
	} `xml:"textevent"`
}

// ParseZWO reads a Zwift workout. Unknown block types are an error, so a
// workout is never silently ridden with parts missing.
func ParseZWO(r io.Reader) (*Workout, error) {
	var f zwoFile
	if err := xml.NewDecoder(r).Decode(&f); err != nil {
		return nil, fmt.Errorf("workout: parse .zwo: %w", err)
	}
	if s := strings.ToLower(f.SportType); s != "" && s != "bike" {
		return nil, fmt.Errorf("workout: sport type %q is not a bike workout", f.SportType)
	}
	w := &Workout{
		Name:        strings.TrimSpace(f.Name),
		Author:      strings.TrimSpace(f.Author),
		Description: strings.TrimSpace(f.Description),
	}
	for i, zb := range f.Workout.Blocks {
		b, err := zb.block()
		if err != nil {
			return nil, fmt.Errorf("workout: block %d <%s>: %w", i+1, zb.XMLName.Local, err)
		}
		w.Blocks = append(w.Blocks, b)
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return w, nil
}

func (zb zwoBlock) block() (Block, error) {
	a := attrs(zb.Attrs)
	b := Block{Type: canonicalType(zb.XMLName.Local)}
	var err error
	num := func(name string) float64 {
		v, e := a.float(name)
		if e != nil && err == nil {
			err = e
		}
		return v
	}
	dur := func(name string) time.Duration {
		return time.Duration(num(name) * float64(time.Second))
	}
	switch b.Type {
	case SteadyState:
		b.Duration, b.Power = dur("duration"), num("power")
		if !a.has("power") && a.has("powerlow") {
			// Some editors write steady blocks with equal low/high.
			b.Power = num("powerlow")
		}
	case Warmup, Ramp, Cooldown:
		b.Duration, b.PowerLow, b.PowerHigh = dur("duration"), num("powerlow"), num("powerhigh")
	case IntervalsT:
		b.Repeat = int(num("repeat"))
		b.OnDuration, b.OffDuration = dur("onduration"), dur("offduration")
		b.OnPower, b.OffPower = num("onpower"), num("offpower")
	case FreeRide, MaxEffort:
		b.Duration = dur("duration")
	default:
		return b, fmt.Errorf("unsupported block type")
	}
	if a.has("cadence") {
		b.Cadence = int(num("cadence"))
	}
	for _, t := range zb.Texts {
		ta := attrs(t.Attrs)
		off, e := ta.float("timeoffset")
		if e != nil {
			return b, e
		}
		b.Texts = append(b.Texts, Text{Offset: time.Duration(off * float64(time.Second)), Message: ta.get("message")})
	}
	return b, err
}

// canonicalType maps element names case-insensitively onto block types.
func canonicalType(name string) BlockType {
	for _, t := range []BlockType{Warmup, SteadyState, Ramp, Cooldown, IntervalsT, FreeRide, MaxEffort} {
		if strings.EqualFold(name, string(t)) {
			return t
		}
	}
	return BlockType(name)
}

type attrs []xml.Attr

func (a attrs) get(name string) string {
	for _, x := range a {
		if strings.EqualFold(x.Name.Local, name) {
			return x.Value
		}
	}
	return ""
}

func (a attrs) has(name string) bool {
	for _, x := range a {
		if strings.EqualFold(x.Name.Local, name) {
			return true
		}
	}
	return false
}

func (a attrs) float(name string) (float64, error) {
	s := a.get(name)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("attribute %s=%q is not a number", name, s)
	}
	return v, nil
}

// WriteZWO writes the workout as a .zwo file that Zwift can import.
func (w *Workout) WriteZWO(out io.Writer) error {
	if err := w.Validate(); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("<workout_file>\n")
	fmt.Fprintf(&b, "    <author>%s</author>\n", esc(w.Author))
	fmt.Fprintf(&b, "    <name>%s</name>\n", esc(w.Name))
	fmt.Fprintf(&b, "    <description>%s</description>\n", esc(w.Description))
	b.WriteString("    <sportType>bike</sportType>\n    <tags/>\n    <workout>\n")
	for _, bl := range w.Blocks {
		var at []string
		add := func(name, value string) { at = append(at, fmt.Sprintf("%s=%q", name, value)) }
		switch bl.Type {
		case SteadyState:
			add("Duration", secs(bl.Duration))
			add("Power", frac(bl.Power))
		case Warmup, Ramp, Cooldown:
			add("Duration", secs(bl.Duration))
			add("PowerLow", frac(bl.PowerLow))
			add("PowerHigh", frac(bl.PowerHigh))
		case IntervalsT:
			add("Repeat", strconv.Itoa(bl.Repeat))
			add("OnDuration", secs(bl.OnDuration))
			add("OffDuration", secs(bl.OffDuration))
			add("OnPower", frac(bl.OnPower))
			add("OffPower", frac(bl.OffPower))
		case FreeRide:
			add("Duration", secs(bl.Duration))
			add("FlatRoad", "1")
		case MaxEffort:
			add("Duration", secs(bl.Duration))
		}
		if bl.Cadence > 0 {
			add("Cadence", strconv.Itoa(bl.Cadence))
		}
		fmt.Fprintf(&b, "        <%s %s", bl.Type, strings.Join(at, " "))
		if len(bl.Texts) == 0 {
			b.WriteString("/>\n")
			continue
		}
		b.WriteString(">\n")
		for _, t := range bl.Texts {
			fmt.Fprintf(&b, "            <textevent timeoffset=%q message=%q/>\n", secs(t.Offset), esc(t.Message))
		}
		fmt.Fprintf(&b, "        </%s>\n", bl.Type)
	}
	b.WriteString("    </workout>\n</workout_file>\n")
	_, err := io.WriteString(out, b.String())
	return err
}

func secs(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) }

func frac(v float64) string { return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64) }

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
