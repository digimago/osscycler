package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/digimago/osscycler/api"
	"github.com/digimago/osscycler/workout"
)

// The workout editor is a form: one row per detail (name, author,
// description) and per block, one cell per value. Arrows move, + / -
// step the value under the cursor, typing replaces it. E hands the same
// workout to $EDITOR in the text format and brings the result back into
// the form.

// editedMsg returns from $EDITOR with the file's new contents.
type editedMsg struct {
	text string
	err  error
}

type savedMsg struct {
	id  string
	err error
}

// draft is a workout being edited.
type draft struct {
	id       string // "" for a new workout
	w        *workout.Workout
	row, col int     // cursor; rows below headerRows are blocks
	buf      *string // value being typed; nil when not typing
	dirty    bool
	// discardUntil: a second esc before this discards the changes.
	discardUntil time.Time
	// text and err keep $EDITOR output that didn't parse, so the next
	// edit starts from it rather than from the form.
	text, err string
}

type fieldID int

const (
	fName fieldID = iota
	fAuthor
	fDescription
	fType
	fDuration
	fPower
	fLow
	fHigh
	fRepeat
	fOn
	fOnPower
	fOff
	fOffPower
	fCadence
)

const headerRows = 3 // name, author, description

// blockTypes is the order t cycles through.
var blockTypes = []workout.BlockType{workout.Warmup, workout.SteadyState, workout.IntervalsT,
	workout.Ramp, workout.FreeRide, workout.MaxEffort, workout.Cooldown}

var typeNames = map[workout.BlockType]string{
	workout.Warmup: "Warmup", workout.SteadyState: "Steady", workout.IntervalsT: "Intervals",
	workout.Ramp: "Ramp", workout.FreeRide: "Free ride", workout.MaxEffort: "Max effort", workout.Cooldown: "Cooldown",
}

// newWorkoutText is the starting point for a new workout.
const newWorkoutText = `name     My workout
warmup   10m 40-75%
3x 5m 95% / 3m 55%
cooldown 5m 65-40%
`

func newWorkout() *workout.Workout {
	w, err := workout.ParseText(strings.NewReader(newWorkoutText))
	if err != nil {
		panic("tui: newWorkoutText: " + err.Error())
	}
	return w
}

func newDraft(id string, w *workout.Workout) *draft {
	return &draft{id: id, w: w, row: min(headerRows, headerRows+len(w.Blocks)-1)}
}

func (d *draft) rows() int { return headerRows + len(d.w.Blocks) }

// block is the block under the cursor, or -1 on a detail row.
func (d *draft) block() int {
	if d.row < headerRows {
		return -1
	}
	return d.row - headerRows
}

func (d *draft) fields(row int) []fieldID {
	if row < headerRows {
		return []fieldID{fName + fieldID(row)}
	}
	switch d.w.Blocks[row-headerRows].Type {
	case workout.SteadyState:
		return []fieldID{fType, fDuration, fPower, fCadence}
	case workout.Warmup, workout.Ramp, workout.Cooldown:
		return []fieldID{fType, fDuration, fLow, fHigh, fCadence}
	case workout.IntervalsT:
		return []fieldID{fType, fRepeat, fOn, fOnPower, fOff, fOffPower, fCadence}
	}
	return []fieldID{fType, fDuration}
}

func (d *draft) field() fieldID {
	fs := d.fields(d.row)
	return fs[min(d.col, len(fs)-1)]
}

func (d *draft) move(drow, dcol int) {
	d.row = max(0, min(d.rows()-1, d.row+drow))
	n := len(d.fields(d.row))
	d.col = max(0, min(n-1, min(d.col, n-1)+dcol))
}

// change applies f to a copy of the workout, so earlier models (and the
// workout list they came from) never see the edit.
func (d *draft) change(f func(w *workout.Workout)) {
	w := *d.w
	w.Blocks = slices.Clone(d.w.Blocks)
	f(&w)
	d.w, d.dirty, d.text, d.err = &w, true, "", ""
}

func pctText(v float64) string { return strconv.FormatFloat(math.Round(v*1000)/10, 'f', -1, 64) }

// value is a field as typed: the starting text when editing it.
func value(w *workout.Workout, b workout.Block, f fieldID) string {
	switch f {
	case fName:
		return w.Name
	case fAuthor:
		return w.Author
	case fDescription:
		return w.Description
	case fType:
		return typeNames[b.Type]
	case fDuration:
		return workout.FormatDuration(b.Duration)
	case fOn:
		return workout.FormatDuration(b.OnDuration)
	case fOff:
		return workout.FormatDuration(b.OffDuration)
	case fPower:
		return pctText(b.Power)
	case fLow:
		return pctText(b.PowerLow)
	case fHigh:
		return pctText(b.PowerHigh)
	case fOnPower:
		return pctText(b.OnPower)
	case fOffPower:
		return pctText(b.OffPower)
	case fRepeat:
		return strconv.Itoa(b.Repeat)
	case fCadence:
		if b.Cadence == 0 {
			return ""
		}
		return strconv.Itoa(b.Cadence)
	}
	return ""
}

func isText(f fieldID) bool { return f <= fDescription }

// set parses s into field f of the workout or block.
func set(w *workout.Workout, b *workout.Block, f fieldID, s string) error {
	s = strings.TrimSpace(s)
	switch f {
	case fName:
		w.Name = s
		return nil
	case fAuthor:
		w.Author = s
		return nil
	case fDescription:
		w.Description = s
		return nil
	case fDuration, fOn, fOff:
		var d time.Duration
		if f != fOff || strings.Trim(s, "0s:") != "" {
			var err error
			if d, err = workout.ParseDuration(s); err != nil {
				if _, numErr := strconv.ParseFloat(s, 64); numErr == nil {
					return fmt.Errorf("%s: add a unit, like %ss or %sm", s, s, s)
				}
				return err
			}
		}
		*map[fieldID]*time.Duration{fDuration: &b.Duration, fOn: &b.OnDuration, fOff: &b.OffDuration}[f] = d
	case fPower, fLow, fHigh, fOnPower, fOffPower:
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil || v < 0 || v > 500 {
			return fmt.Errorf("power %q: want a percentage of FTP from 0 to 500", s)
		}
		*map[fieldID]*float64{fPower: &b.Power, fLow: &b.PowerLow, fHigh: &b.PowerHigh,
			fOnPower: &b.OnPower, fOffPower: &b.OffPower}[f] = v / 100
	case fRepeat:
		n, err := strconv.Atoi(strings.TrimSuffix(s, "x"))
		if err != nil || n < 1 || n > 99 {
			return fmt.Errorf("repeat %q: want 1 to 99", s)
		}
		b.Repeat = n
	case fCadence:
		n, err := strconv.Atoi(strings.TrimSuffix(s, "rpm"))
		switch {
		case s == "" || (err == nil && n == 0):
			b.Cadence = 0
		case err != nil || n < 30 || n > 200:
			return fmt.Errorf("cadence %q: want 30 to 200 rpm, or 0 for none", s)
		default:
			b.Cadence = n
		}
	}
	return nil
}

// durationStep is 5 s under a minute, 30 s under ten, then a minute.
func durationStep(d time.Duration, up bool) time.Duration {
	if !up {
		d-- // stepping down from 1m uses the finer step below it
	}
	switch {
	case d < time.Minute:
		return 5 * time.Second
	case d < 10*time.Minute:
		return 30 * time.Second
	}
	return time.Minute
}

// step nudges field f up or down.
func step(b *workout.Block, f fieldID, up bool) {
	sign := 1.0
	if !up {
		sign = -1
	}
	switch f {
	case fType:
		i := slices.Index(blockTypes, b.Type)
		*b = retype(*b, blockTypes[(i+len(blockTypes)+int(sign))%len(blockTypes)])
	case fDuration, fOn, fOff:
		p := map[fieldID]*time.Duration{fDuration: &b.Duration, fOn: &b.OnDuration, fOff: &b.OffDuration}[f]
		lowest := 5 * time.Second
		if f == fOff {
			lowest = 0
		}
		next := *p + time.Duration(sign)*durationStep(*p, up)
		*p = max(lowest, next.Truncate(durationStep(*p, up)))
	case fPower, fLow, fHigh, fOnPower, fOffPower:
		p := map[fieldID]*float64{fPower: &b.Power, fLow: &b.PowerLow, fHigh: &b.PowerHigh,
			fOnPower: &b.OnPower, fOffPower: &b.OffPower}[f]
		*p = math.Max(0, math.Min(5, math.Round(*p*100+sign)/100))
	case fRepeat:
		b.Repeat = max(1, min(99, b.Repeat+int(sign)))
	case fCadence:
		switch {
		case b.Cadence == 0 && up:
			b.Cadence = 85
		case b.Cadence <= 50 && !up:
			b.Cadence = 0
		case b.Cadence > 0:
			b.Cadence = min(200, b.Cadence+int(sign)*5)
		}
	}
}

// retype changes a block's type. Block keeps every field, so cycling
// through the types keeps what was set; fields the new type needs and
// that are still unset come from the block's overall level and length.
func retype(b workout.Block, to workout.BlockType) workout.Block {
	level, length := 0.75, 5*time.Minute
	switch b.Type {
	case workout.SteadyState:
		level, length = b.Power, b.Duration
	case workout.Warmup, workout.Ramp, workout.Cooldown:
		level, length = (b.PowerLow+b.PowerHigh)/2, b.Duration
	case workout.IntervalsT:
		level, length = b.OnPower, time.Duration(b.Repeat)*(b.OnDuration+b.OffDuration)
	case workout.FreeRide, workout.MaxEffort:
		length = b.Duration
	}
	level = math.Round(level*100) / 100
	b.Type = to
	if b.Duration == 0 {
		b.Duration = length
	}
	switch to {
	case workout.SteadyState:
		if b.Power == 0 {
			b.Power = level
		}
	case workout.Warmup, workout.Ramp, workout.Cooldown:
		if b.PowerLow == 0 && b.PowerHigh == 0 {
			b.PowerLow, b.PowerHigh = math.Round(level*60)/100, level
		}
		// Cooldowns are written as ridden, high to low.
		if (to == workout.Cooldown) != (b.PowerLow > b.PowerHigh) {
			b.PowerLow, b.PowerHigh = b.PowerHigh, b.PowerLow
		}
	case workout.IntervalsT:
		if b.Repeat == 0 {
			b.Repeat, b.OnDuration, b.OnPower = 3, 5*time.Minute, level
			b.OffDuration, b.OffPower = 3*time.Minute, 0.55
		}
	}
	return b
}

// tidy drops the fields a block's type doesn't use, which retype keeps
// around while editing.
func tidy(w *workout.Workout) *workout.Workout {
	t := *w
	t.Blocks = make([]workout.Block, len(w.Blocks))
	for i, b := range w.Blocks {
		n := workout.Block{Type: b.Type, Cadence: b.Cadence, Texts: b.Texts}
		switch b.Type {
		case workout.SteadyState:
			n.Duration, n.Power = b.Duration, b.Power
		case workout.Warmup, workout.Ramp, workout.Cooldown:
			n.Duration, n.PowerLow, n.PowerHigh = b.Duration, b.PowerLow, b.PowerHigh
		case workout.IntervalsT:
			n.Repeat, n.OnDuration, n.OnPower = b.Repeat, b.OnDuration, b.OnPower
			if b.OffDuration > 0 {
				n.OffDuration, n.OffPower = b.OffDuration, b.OffPower
			}
		default:
			n.Duration = b.Duration
		}
		t.Blocks[i] = n
	}
	return &t
}

// typed turns a key into the character it types, if any.
func typed(key string) (rune, bool) {
	if key == "space" {
		return ' ', true
	}
	r, n := utf8.DecodeRuneInString(key)
	return r, n == len(key) && unicode.IsPrint(r)
}

// numeric reports whether r can appear in a number or duration.
func numeric(r rune) bool { return unicode.IsDigit(r) || strings.ContainsRune(".:hms%x", r) }

func (m Model) draftKey(key string) (Model, tea.Cmd, bool) {
	d := *m.draft
	m.draft = &d
	if d.buf != nil {
		return m.typingKey(key)
	}
	bi := d.block()
	switch key {
	case "up":
		d.move(-1, 0)
	case "down":
		d.move(1, 0)
	case "left", "shift+tab":
		d.move(0, -1)
	case "right", "tab":
		d.move(0, 1)
	case "enter":
		if f := d.field(); f != fType {
			s := value(d.w, d.blockOrZero(), f)
			d.buf = &s
		}
	case "+", "=", "-", "_", "t", "T":
		if bi < 0 {
			break
		}
		f, up := d.field(), key == "+" || key == "=" || key == "t"
		if key == "t" || key == "T" {
			f = fType
		}
		d.change(func(w *workout.Workout) { step(&w.Blocks[bi], f, up) })
	case "a", "c":
		at := bi + 1
		nb := workout.Block{Type: workout.SteadyState, Duration: 5 * time.Minute, Power: 0.65}
		if key == "c" {
			if bi < 0 {
				break
			}
			nb = d.w.Blocks[bi]
		}
		d.change(func(w *workout.Workout) { w.Blocks = slices.Insert(w.Blocks, at, nb) })
		d.row = headerRows + at
	case "d", "delete":
		if bi < 0 {
			break
		}
		d.change(func(w *workout.Workout) { w.Blocks = slices.Delete(w.Blocks, bi, bi+1) })
		d.move(0, 0)
	case "K", "shift+up", "J", "shift+down":
		to := bi - 1
		if key == "J" || key == "shift+down" {
			to = bi + 1
		}
		if bi < 0 || to < 0 || to >= len(d.w.Blocks) {
			break
		}
		d.change(func(w *workout.Workout) { w.Blocks[bi], w.Blocks[to] = w.Blocks[to], w.Blocks[bi] })
		d.row = headerRows + to
	case "E":
		return m.openEditor(&d)
	case "s":
		w := tidy(d.w)
		if err := w.Validate(); err != nil {
			m.notice = "can't save: " + err.Error()
			break
		}
		cmds, id, def := m.cmds, d.id, api.WorkoutToProto(d.id, w)
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			defer cancel()
			saved, err := cmds.SaveWorkout(ctx, id, def)
			return savedMsg{id: saved, err: err}
		}, true
	case "esc":
		if d.dirty && !m.now().Before(d.discardUntil) {
			d.discardUntil = m.now().Add(abortConfirm)
			m.notice = "press esc again to discard your changes"
			break
		}
		m.draft = nil
		m.notice = "edit discarded"
	default:
		// Typing a number on a number field starts replacing it.
		if r, ok := typed(key); ok && unicode.IsDigit(r) && !isText(d.field()) && d.field() != fType {
			d.buf = &key
		}
	}
	return m, nil, true
}

func (d *draft) blockOrZero() workout.Block {
	if bi := d.block(); bi >= 0 {
		return d.w.Blocks[bi]
	}
	return workout.Block{}
}

func (m Model) typingKey(key string) (Model, tea.Cmd, bool) {
	d := m.draft
	buf := *d.buf
	switch key {
	case "esc":
		d.buf = nil
		return m, nil, true
	case "backspace":
		if _, n := utf8.DecodeLastRuneInString(buf); n > 0 {
			buf = buf[:len(buf)-n]
		}
	case "enter", "tab":
		f, bi := d.field(), d.block()
		var err error
		d.change(func(w *workout.Workout) {
			if bi < 0 {
				err = set(w, nil, f, buf)
			} else {
				err = set(w, &w.Blocks[bi], f, buf)
			}
		})
		if err != nil {
			m.notice = err.Error()
			return m, nil, true
		}
		d.buf, m.notice = nil, ""
		if key == "tab" {
			d.move(0, 1)
		}
		return m, nil, true
	default:
		r, ok := typed(key)
		limit := 12
		if isText(d.field()) {
			limit = 80
		}
		if ok && (isText(d.field()) || numeric(r)) && utf8.RuneCountInString(buf) < limit {
			buf += string(r)
		}
	}
	d.buf = &buf
	return m, nil, true
}

// openEditor hands the terminal to $EDITOR with the draft as text and a
// help header; the result comes back as editedMsg.
func (m Model) openEditor(d *draft) (Model, tea.Cmd, bool) {
	f, err := os.CreateTemp("", "osscycler-*.workout")
	if err != nil {
		m.notice = "editor: " + err.Error()
		return m, nil, true
	}
	var help strings.Builder
	for _, l := range strings.Split(workout.TextHelp, "\n") {
		help.WriteString("# " + l + "\n")
	}
	text := workout.FormatText(tidy(d.w))
	if d.err != "" {
		help.WriteString("# ERROR: " + d.err + "\n")
		text = d.text
	}
	help.WriteString("\n")
	f.WriteString(help.String() + stripHelp(text))
	f.Close()

	m.draft = d
	path := f.Name()
	cmd := exec.Command(editor(), path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editedMsg{err: err}
		}
		b, rerr := os.ReadFile(path)
		return editedMsg{text: string(b), err: rerr}
	}), true
}

// stripHelp removes the comment header openEditor adds, so it doesn't
// pile up across edits.
func stripHelp(text string) string {
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && (strings.HasPrefix(lines[i], "#") || (strings.TrimSpace(lines[i]) == "" && i < 8)) {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

func editor() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(v); e != "" {
			return e
		}
	}
	for _, e := range []string{"nano", "vi"} {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return "vi"
}

// onEdited brings the editor's text back into the form, or keeps it
// with the error so the next E starts from it.
func (m Model) onEdited(msg editedMsg) (Model, tea.Cmd) {
	if m.draft == nil {
		return m, nil
	}
	if msg.err != nil {
		m.notice = "editor: " + msg.err.Error()
		return m, nil
	}
	d := *m.draft
	m.draft = &d
	w, err := workout.ParseText(strings.NewReader(msg.text))
	if err != nil {
		d.text, d.err = stripHelp(msg.text), err.Error()
		return m, nil
	}
	if workout.FormatText(w) != workout.FormatText(tidy(d.w)) {
		d.w, d.dirty = w, true
	}
	d.text, d.err = "", ""
	d.move(0, 0)
	return m, nil
}

func (m Model) onSaved(msg savedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.notice = "saving failed: " + friendlyErr(msg.err)
		return m, nil
	}
	m.draft = nil
	m.notice = "saved " + msg.id
	m.picking, m.tab = true, tabWorkouts
	return m, m.fetchWorkouts(false)
}

var (
	cellStyle   = lipgloss.NewStyle().Reverse(true).Bold(true)
	typingStyle = lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("#000000"))
)

// cell shows one value, padded to width, highlighted under the cursor.
func (d *draft) cell(row int, f fieldID, text string, width int) string {
	here := row == d.row && f == d.field()
	if here && d.buf != nil {
		text = *d.buf + "▏"
	}
	if w := utf8.RuneCountInString(text); w < width {
		text += strings.Repeat(" ", width-w)
	}
	switch {
	case here && d.buf != nil:
		return typingStyle.Render(text)
	case here:
		return cellStyle.Render(text)
	}
	return text
}

// formRow draws one row of the form.
func (d *draft) formRow(row int) string {
	cursor := "  "
	if row == d.row {
		cursor = warnStyle.Render("▸ ")
	}
	if row < headerRows {
		f := fName + fieldID(row)
		label := []string{"name", "author", "description"}[row]
		v := value(d.w, workout.Block{}, f)
		if v == "" && !(row == d.row && d.buf != nil) {
			v = "–"
		}
		return cursor + labelStyle.Render(fmt.Sprintf("%-12s", label)) + d.cell(row, f, truncate(v, 60), 1)
	}
	b := d.w.Blocks[row-headerRows]
	var s strings.Builder
	fmt.Fprintf(&s, "%s%s ", cursor, dimStyle.Render(fmt.Sprintf("%2d", row-headerRows+1)))
	sep := func(t string) { s.WriteString(dimStyle.Render(t)) }
	dur := func(f fieldID) string { return d.cell(row, f, value(d.w, b, f), 6) }
	pct := func(f fieldID) string { return d.cell(row, f, value(d.w, b, f)+"%", 5) }
	s.WriteString(d.cell(row, fType, typeNames[b.Type], 10) + " ")
	switch b.Type {
	case workout.SteadyState:
		s.WriteString(dur(fDuration))
		sep(" @ ")
		s.WriteString(pct(fPower))
	case workout.Warmup, workout.Ramp, workout.Cooldown:
		s.WriteString(dur(fDuration) + " ")
		s.WriteString(pct(fLow))
		sep(" → ")
		s.WriteString(pct(fHigh))
	case workout.IntervalsT:
		s.WriteString(d.cell(row, fRepeat, value(d.w, b, fRepeat)+"×", 3) + " ")
		s.WriteString(dur(fOn))
		sep(" @ ")
		s.WriteString(pct(fOnPower))
		sep("  / ")
		s.WriteString(dur(fOff))
		sep(" @ ")
		s.WriteString(pct(fOffPower))
	default:
		s.WriteString(dur(fDuration))
	}
	if slices.Contains(d.fields(row), fCadence) {
		c := "– rpm"
		if b.Cadence > 0 {
			c = value(d.w, b, fCadence) + " rpm"
		}
		s.WriteString("  " + d.cell(row, fCadence, c, 7))
	}
	if n := len(b.Texts); n > 0 {
		sep(fmt.Sprintf("  ✉ %d", n))
	}
	return s.String()
}

// formPanel is the workout editor.
func (m Model) formPanel(width, height int) string {
	d := m.draft
	title := "New workout"
	if d.id != "" {
		title = "Edit " + d.id
	}
	w := tidy(d.w)
	verr := w.Validate()

	profileH := 5
	if height < 26 {
		profileH = 0
	}
	// Everything but the block rows: title, details, gaps, status,
	// profile and two hint lines.
	fixed := 2 + headerRows + 1 + 2 + 2 + 2
	if profileH > 0 {
		fixed += profileH + 1
	}
	visible := max(3, height-fixed)

	lines := []string{titleStyle.Render(title), ""}
	for r := range headerRows {
		lines = append(lines, d.formRow(r))
	}
	lines = append(lines, "")
	n := len(d.w.Blocks)
	first := 0
	if n > visible {
		first = max(0, min(n-visible, d.block()-visible/2))
	}
	for i := first; i < min(n, first+visible); i++ {
		switch {
		case i == first && first > 0:
			lines = append(lines, dimStyle.Render(fmt.Sprintf("     ↑ %d more", first)))
		case i == first+visible-1 && i < n-1:
			lines = append(lines, dimStyle.Render(fmt.Sprintf("     ↓ %d more", n-i)))
		default:
			lines = append(lines, d.formRow(headerRows+i))
		}
	}
	if n == 0 {
		lines = append(lines, dimStyle.Render("     no blocks yet: press a to add one"))
	}
	lines = append(lines, "")

	var status string
	switch {
	case d.err != "":
		status = badStyle.Render("✕ the text didn't parse: " + d.err + " · E to fix it")
	case verr != nil:
		status = badStyle.Render("✕ " + verr.Error())
	default:
		def := api.WorkoutToProto(d.id, w)
		ftp := m.wk().GetFtpW()
		avg := fmt.Sprintf("~%.0f%% FTP average", avgTarget(def)*100)
		if ftp > 0 {
			avg = fmt.Sprintf("~%.0f W average at %.0f W FTP", avgTarget(def)*ftp, ftp)
		}
		status = fmt.Sprintf("%s · %d blocks · %s", clock(def.GetDurationS()), n, avg)
		if profileH > 0 {
			status += "\n\n" + workoutProfile(def, -1, min(width-4, 100), profileH)
		}
	}
	lines = append(lines, status, "")
	if d.buf != nil {
		lines = append(lines, dimStyle.Render("enter set · tab set and next · esc cancel"),
			dimStyle.Render("durations 45s 5m 1m30s 1:30 · powers in % of FTP"))
	} else {
		lines = append(lines, dimStyle.Render("↑↓←→ move · +/- adjust · type or enter to edit · t type · a add · c copy · d delete · J/K move"),
			dimStyle.Render("E edit as text in $EDITOR · s save · esc discard"))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Left, lines...))
}
