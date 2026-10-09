package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// cadenceWindow is what the CADENCE tile averages over during rides and
// workouts: steadier to read than the trainer's 4 Hz reading.
const cadenceWindow = 5 * time.Second

// cadenceSample is the trainer's cadence as one state reported it.
type cadenceSample struct {
	at  time.Time
	rpm float64
	ok  bool // the trainer reported a valid cadence
}

// withCadence adds the cadence in the latest state, keeping the readings
// that still count for the window (the one before it holds into it).
// It builds a new slice: models are copied by value.
func (m Model) withCadence(now time.Time) Model {
	tr := m.st.GetTrainer()
	s := cadenceSample{at: now, rpm: float64(tr.GetCadenceRpm()), ok: tr != nil && tr.CadenceRpm != nil}
	from := now.Add(-cadenceWindow)
	keep := make([]cadenceSample, 0, len(m.cadence)+1)
	for i, c := range m.cadence {
		next := now
		if i+1 < len(m.cadence) {
			next = m.cadence[i+1].at
		}
		if next.After(from) {
			keep = append(keep, c)
		}
	}
	m.cadence = append(keep, s)
	return m
}

// cadenceAvg is the cadence over the last cadenceWindow, each reading
// weighted by how long it held; ok is false without valid readings.
func (m Model) cadenceAvg() (float64, bool) {
	now := m.now()
	from := now.Add(-cadenceWindow)
	var sum, weight float64
	for i, c := range m.cadence {
		to := now
		if i+1 < len(m.cadence) {
			to = m.cadence[i+1].at
		}
		start := c.at
		if start.Before(from) {
			start = from
		}
		if d := to.Sub(start).Seconds(); d > 0 && c.ok {
			sum, weight = sum+c.rpm*d, weight+d
		}
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}

// cadenceTile is the averaged cadence; aim is the workout's cadence for
// this part, 0 for none.
func (m Model) cadenceTile(aim uint32) metric {
	v := "--"
	if rpm, ok := m.cadenceAvg(); ok {
		v = fmt.Sprintf("%.0f", rpm)
	}
	unit := "rpm"
	if aim > 0 {
		unit = fmt.Sprintf("rpm · aim %d", aim)
	}
	return metric{"CADENCE 5s", v, unit, cadenceStyle, nil}
}

// tileGrid lays metrics out as big tiles in two rows (three over two for
// five); empty when they don't fit, so callers fall back to a list.
func tileGrid(ms []metric, width, height int, size digitSize) string {
	top := (len(ms) + 1) / 2
	rowH := height / 2
	if rowH < size.height()+3 {
		return ""
	}
	row := func(ms []metric, h int) string {
		w := width / len(ms)
		tiles := make([]string, len(ms))
		for i, mt := range ms {
			tw := w
			if i == len(ms)-1 {
				tw = width - w*(len(ms)-1)
			}
			tiles[i] = bigTile(mt, tw, h, size)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, tiles...)
	}
	for i, mt := range ms {
		cols := top
		if i >= top {
			cols = len(ms) - top
		}
		if w := width / cols; lipgloss.Width(size.render(mt.value))+2 > w || lipgloss.Width(mt.label)+2 > w || lipgloss.Width(mt.unit)+2 > w {
			return ""
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, row(ms[:top], rowH), row(ms[top:], height-rowH))
}

// tileList is the compact fallback: one metric per line.
func tileList(ms []metric, labelW, width, height int) string {
	var b strings.Builder
	for _, mt := range ms {
		fmt.Fprintf(&b, "%s %s %s\n", labelStyle.Render(fmt.Sprintf("%-*s", labelW, truncate(mt.label, labelW))),
			mt.style.Bold(true).Render(fmt.Sprintf("%8s", mt.value)), unitStyle.Render(mt.unit))
	}
	return lipgloss.Place(width, max(height, len(ms)), lipgloss.Center, lipgloss.Center, b.String())
}
