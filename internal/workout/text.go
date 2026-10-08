package workout

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// The text format is osscycler's native way to write a workout, one line
// per block. Powers are percentages of FTP:
//
//	name     Sweet spot 3x10
//	author   me
//	# comments start with #
//	warmup   10m 40-75%
//	steady   5m 88% @90rpm
//	> 10s Settle in               (message 10 s into the block above)
//	3x 10m 90% / 5m 55%            (intervals: repeat x on / off)
//	ramp     2m 60-100%
//	free     3m
//	max      30s
//	cooldown 5m 70-30%
//
// Durations: 45s, 10m, 1h, 1m30s, 1:30 (m:ss) or 1:02:03 (h:mm:ss).
// The ramp blocks keep the order written, as .zwo does.
const TextHelp = `name <text> · author <text> · description <text>
warmup|ramp|cooldown <dur> <from>-<to>%   steady <dur> <pct>%
<n>x <dur> <pct>% [/ <dur> <pct>%]        free <dur> · max <dur>
optional @<rpm>rpm on a block · "> <offset> <message>" adds a message
durations: 45s 10m 1h 1m30s 1:30 · lines starting with # are comments`

// LineError points at the offending line of a text workout.
type LineError struct {
	Line int
	Err  error
}

func (e *LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }
func (e *LineError) Unwrap() error { return e.Err }

// ParseText reads the text format. The result is validated.
func ParseText(r io.Reader) (*Workout, error) {
	w := &Workout{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := parseLine(w, line); err != nil {
			return nil, &LineError{Line: n, Err: err}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return w, nil
}

func parseLine(w *Workout, line string) error {
	if rest, ok := strings.CutPrefix(line, ">"); ok {
		if len(w.Blocks) == 0 {
			return fmt.Errorf("message before any block")
		}
		f := strings.Fields(rest)
		if len(f) < 2 {
			return fmt.Errorf(`message needs "> <offset> <text>"`)
		}
		off, err := ParseDuration(f[0])
		if err != nil {
			return err
		}
		b := &w.Blocks[len(w.Blocks)-1]
		b.Texts = append(b.Texts, Text{Offset: off, Message: strings.Join(f[1:], " ")})
		return nil
	}

	word, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(word) {
	case "name":
		w.Name = rest
		return nil
	case "author":
		w.Author = rest
		return nil
	case "description":
		w.Description = rest
		return nil
	}

	f := strings.Fields(line)
	b := Block{}
	// A trailing @90rpm or @90 is the cadence.
	if last := f[len(f)-1]; strings.HasPrefix(last, "@") {
		rpm, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(last, "@"), "rpm"))
		if err != nil || rpm <= 0 {
			return fmt.Errorf("cadence %q: want e.g. @90rpm", last)
		}
		b.Cadence = rpm
		f = f[:len(f)-1]
	}
	args := f[1:]
	need := func(k int, form string) error {
		if len(args) != k {
			return fmt.Errorf("want %q", form)
		}
		return nil
	}
	var err error
	switch kw := strings.ToLower(f[0]); {
	case kw == "steady":
		if err = need(2, "steady <dur> <pct>%"); err != nil {
			return err
		}
		b.Type = SteadyState
		if b.Duration, err = ParseDuration(args[0]); err != nil {
			return err
		}
		b.Power, err = parsePercent(args[1])
	case kw == "warmup" || kw == "ramp" || kw == "cooldown":
		if err = need(2, kw+" <dur> <from>-<to>%"); err != nil {
			return err
		}
		b.Type = map[string]BlockType{"warmup": Warmup, "ramp": Ramp, "cooldown": Cooldown}[kw]
		if b.Duration, err = ParseDuration(args[0]); err != nil {
			return err
		}
		lo, hi, ok := strings.Cut(strings.TrimSuffix(args[1], "%"), "-")
		if !ok {
			return fmt.Errorf("%q: want <from>-<to>%%, e.g. 40-75%%", args[1])
		}
		if b.PowerLow, err = parsePercent(lo); err != nil {
			return err
		}
		b.PowerHigh, err = parsePercent(hi)
	case kw == "free" || kw == "max":
		if err = need(1, kw+" <dur>"); err != nil {
			return err
		}
		b.Type = FreeRide
		if kw == "max" {
			b.Type = MaxEffort
		}
		b.Duration, err = ParseDuration(args[0])
	case strings.HasSuffix(kw, "x"):
		reps, convErr := strconv.Atoi(strings.TrimSuffix(kw, "x"))
		if convErr != nil || reps < 1 {
			return fmt.Errorf("%q: intervals start with a repeat count like 3x", f[0])
		}
		b.Type, b.Repeat = IntervalsT, reps
		switch {
		case len(args) == 2:
		case len(args) == 5 && args[2] == "/":
			if b.OffDuration, err = ParseDuration(args[3]); err != nil {
				return err
			}
			if b.OffPower, err = parsePercent(args[4]); err != nil {
				return err
			}
		default:
			return errors.New(`want "<n>x <dur> <pct>% / <dur> <pct>%"`)
		}
		if b.OnDuration, err = ParseDuration(args[0]); err != nil {
			return err
		}
		b.OnPower, err = parsePercent(args[1])
	default:
		return fmt.Errorf("unknown block %q (warmup, steady, <n>x, ramp, free, max, cooldown)", f[0])
	}
	if err != nil {
		return err
	}
	if err := b.validate(); err != nil {
		return err
	}
	w.Blocks = append(w.Blocks, b)
	return nil
}

func parsePercent(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if err != nil {
		return 0, fmt.Errorf("power %q: want a percentage of FTP like 88%%", s)
	}
	return v / 100, nil
}

// ParseDuration reads 45s, 10m, 1m30s, 1:30 (m:ss) or 1:02:03 (h:mm:ss).
func ParseDuration(s string) (time.Duration, error) {
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) > 3 {
			return 0, fmt.Errorf("duration %q", s)
		}
		var total float64
		for _, p := range parts {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("duration %q: want m:ss or h:mm:ss", s)
			}
			total = total*60 + v
		}
		return time.Duration(total * float64(time.Second)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("duration %q: want e.g. 45s, 10m, 1m30s or 1:30", s)
	}
	return d, nil
}

// FormatText writes the workout in the text format. ParseText of the
// result gives back the same workout.
func FormatText(w *Workout) string {
	var b strings.Builder
	if w.Name != "" {
		fmt.Fprintf(&b, "name        %s\n", w.Name)
	}
	if w.Author != "" {
		fmt.Fprintf(&b, "author      %s\n", w.Author)
	}
	if w.Description != "" {
		// One line: the text format has no multi-line values.
		fmt.Fprintf(&b, "description %s\n", strings.Join(strings.Fields(w.Description), " "))
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	for _, bl := range w.Blocks {
		var line string
		switch bl.Type {
		case SteadyState:
			line = fmt.Sprintf("steady   %s %s", FormatDuration(bl.Duration), fmtPercent(bl.Power))
		case Warmup, Ramp, Cooldown:
			line = fmt.Sprintf("%-8s %s %s-%s", strings.ToLower(string(bl.Type)), FormatDuration(bl.Duration),
				strings.TrimSuffix(fmtPercent(bl.PowerLow), "%"), fmtPercent(bl.PowerHigh))
		case IntervalsT:
			line = fmt.Sprintf("%dx %s %s", bl.Repeat, FormatDuration(bl.OnDuration), fmtPercent(bl.OnPower))
			if bl.OffDuration > 0 {
				line += fmt.Sprintf(" / %s %s", FormatDuration(bl.OffDuration), fmtPercent(bl.OffPower))
			}
		case FreeRide:
			line = "free     " + FormatDuration(bl.Duration)
		case MaxEffort:
			line = "max      " + FormatDuration(bl.Duration)
		}
		if bl.Cadence > 0 {
			line += fmt.Sprintf(" @%drpm", bl.Cadence)
		}
		b.WriteString(line + "\n")
		for _, t := range bl.Texts {
			fmt.Fprintf(&b, "> %s %s\n", FormatDuration(t.Offset), t.Message)
		}
	}
	return b.String()
}

// FormatDuration writes the shortest form ParseText reads back exactly.
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Second != 0 {
		return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
	}
	h, m, s := int(d/time.Hour), int(d%time.Hour/time.Minute), int(d%time.Minute/time.Second)
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	return b.String()
}

// fmtPercent writes up to one decimal, which is exact for .zwo powers
// (stored to three decimals of FTP).
func fmtPercent(v float64) string {
	return strconv.FormatFloat(math.Round(v*1000)/10, 'f', -1, 64) + "%"
}
