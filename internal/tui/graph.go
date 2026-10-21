package tui

import (
	"context"
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/digimago/osscycler/gen/osscycler/v1"
)

// The ride graph (owner's list, item 4): power and heart rate over the
// whole activity being recorded, compressed to the screen's width, in a
// band above the footer. The core keeps the activity's samples, a second
// of riding time each; the TUI fetches what it lacks whenever the
// recording has run on graphFetchS past what it has. t shows or hides it,
// kept in the layout file.

const (
	graphRows   = 5  // terminal rows: twice as many pixels
	graphFetchS = 2  // fetch once the recording is this far ahead, s
	graphMinH   = 26 // the screen needs this many rows for it
	graphMinW   = 40
)

// rideHistory is the activity being recorded, as fetched so far.
type rideHistory struct {
	name      string
	power, hr []int32 // a sample a second of riding time; -1: no reading
}

type historyMsg struct {
	resp *pb.GetActivityHistoryResponse
	err  error
}

// graphOn reports whether the rider wants the graph (t).
func (m Model) graphOn() bool { return m.layout.Graph != "off" }

// needHistory fetches the seconds of the recording the graph lacks.
func (m Model) needHistory() (Model, tea.Cmd) {
	rec := m.st.GetRecording()
	if m.cmds == nil || !m.graphOn() || m.fetchingHistory || !rec.GetActive() {
		return m, nil
	}
	since := uint32(0)
	if m.history.name == rec.GetFile() {
		since = uint32(len(m.history.power))
		if rec.GetTimerS() < float64(since)+graphFetchS {
			return m, nil
		}
	}
	m.fetchingHistory = true
	cmds := m.cmds
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		resp, err := cmds.ActivityHistory(ctx, "", since)
		return historyMsg{resp, err}
	}
}

func (m Model) onHistory(msg historyMsg) Model {
	m.fetchingHistory = false
	r := msg.resp
	if msg.err != nil || r == nil {
		return m // tried again at the next state
	}
	if r.GetName() != m.history.name {
		if r.GetFromS() != 0 {
			return m // a newer activity: the next fetch starts it from 0
		}
		m.history = rideHistory{name: r.GetName()}
	}
	if int(r.GetFromS()) != len(m.history.power) {
		return m // overlaps or leaves a gap: fetch again from where it is
	}
	h := m.history
	h.power = append(append([]int32(nil), h.power...), r.GetPowerW()...)
	h.hr = append(append([]int32(nil), h.hr...), r.GetHeartRateBpm()...)
	m.history = h
	return m
}

// graphKey is t: the graph on or off, saved in the layout.
func (m Model) graphKey(key string) (Model, tea.Cmd, bool) {
	if key != "t" || m.draft != nil || m.input != nil || m.picking || m.menu != nil || m.onboarding != nil || m.arranging != nil || m.ending != nil {
		return m, nil, false
	}
	l := m.layout
	if l.Graph == "off" {
		l.Graph = ""
	} else {
		l.Graph = "off"
	}
	m.layout = l
	return m, m.saveLayout(), true
}

// graphStrip is the graph for the screen now, or "" when it's off, there
// is no room or nothing recorded yet.
func (m Model) graphStrip() string {
	if !m.graphOn() || m.width < graphMinW || m.height < graphMinH || !m.st.GetRecording().GetActive() ||
		m.countdown > 0 || m.calibrationActive() || m.showResult() ||
		m.history.name != m.st.GetRecording().GetFile() || len(m.history.power) < 2 {
		return ""
	}
	return rideGraph(m.history, m.profile().GetFtpW(), m.width, graphRows)
}

var (
	graphBack = rgb{22, 22, 26}
	graphHR   = rgb{255, 170, 200} // lighter than zone 5 and 6, which it crosses
)

// rideGraph draws power as bars coloured by zone (by ftp; one colour
// without it) and heart rate as a line over them, the whole activity in
// width columns, rows terminal rows high, with the scale on the left.
func rideGraph(h rideHistory, ftp float64, width, rows int) string {
	const label = 9
	w, px := width-label, rows*2
	n := len(h.power)
	type col struct{ p, hr float64 }
	cols := make([]col, w)
	maxP, minHR, maxHR := 0.0, math.Inf(1), math.Inf(-1)
	for c := range w {
		a, b := c*n/w, (c+1)*n/w
		if b <= a {
			b = a + 1
		}
		var ps, hs float64
		var pn, hn int
		for i := a; i < b && i < n; i++ {
			if v := h.power[i]; v >= 0 {
				ps += float64(v)
				pn++
			}
			if i < len(h.hr) && h.hr[i] > 0 {
				hs += float64(h.hr[i])
				hn++
			}
		}
		if pn > 0 {
			cols[c].p = ps / float64(pn)
			maxP = math.Max(maxP, cols[c].p)
		}
		if hn > 0 {
			cols[c].hr = hs / float64(hn)
			minHR, maxHR = math.Min(minHR, cols[c].hr), math.Max(maxHR, cols[c].hr)
		}
	}
	top := math.Max(100, math.Ceil(maxP/50)*50)
	grid := make([]rgb, w*px)
	for i := range grid {
		grid[i] = graphBack
	}
	for c, v := range cols {
		bar := int(math.Round(v.p / top * float64(px)))
		color := hexRGB("#ffcc3f")
		if ftp > 0 {
			color = zoneRGB(v.p / ftp)
		}
		for y := px - bar; y < px; y++ {
			grid[y*w+c] = color
		}
		if v.hr > 0 && maxHR > minHR {
			// Heart rate over its own range, in the band's upper part.
			f := (v.hr - minHR) / (maxHR - minHR)
			y := int(math.Round((1 - f) * float64(px-2)))
			grid[y*w+c] = graphHR
		}
	}
	var b strings.Builder
	for r := range rows {
		lbl := ""
		switch r {
		case 0:
			lbl = powerStyle.Render(fmt.Sprintf("%4.0f W", top))
		case 1:
			if maxHR > minHR {
				lbl = hrStyle.Render(fmt.Sprintf("%4.0f♥", maxHR))
			}
		case rows - 1:
			lbl = dimStyle.Render(clock(float64(n)))
		}
		b.WriteString(lbl + strings.Repeat(" ", max(0, label-1-lipgloss.Width(lbl))) + " ")
		var fg, bg rgb
		for x := range w {
			t, u := grid[2*r*w+x], grid[(2*r+1)*w+x]
			if x == 0 || t != fg {
				writeSGR(&b, 38, t)
				fg = t
			}
			if x == 0 || u != bg {
				writeSGR(&b, 48, u)
				bg = u
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// zoneRGB is zoneColor as pixels.
func zoneRGB(frac float64) rgb {
	for i, z := range zoneStops {
		if frac < z.upTo {
			return hexRGB(zoneHex[i])
		}
	}
	return hexRGB(zoneHex[len(zoneHex)-1])
}

// zoneHex are zoneStops' colours, and zone 6's last.
var zoneHex = []string{"#7f7f7f", "#338cff", "#59bf59", "#ffcc3f", "#ff6639", "#ff330c"}

func hexRGB(s string) rgb {
	var r, g, b uint8
	fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b)
	return rgb{r, g, b}
}
