package worlds

import (
	"encoding/json"
	"math"
	"os"
	"sync"
	"time"
)

// Progress is how far a build is (owner, 2026-10-10: a ride on a course
// whose world was being built never said how long it would take): its
// phase, a count within it where there is one (tiles, stretches), how far
// the phase is, and an estimate of the time left for the whole build.
type Progress struct {
	Phase       string
	Done, Total int     // 0, 0: no count
	Frac        float64 // of the phase, 0 to 1
	LeftS       float64 // seconds left for the build, estimated; < 0 unknown
}

// Estimates until a build has measured its own: a map data stretch fetched
// (Overpass, 5 km of course), and the build itself per km of course on
// this machine (learned: Timing).
const (
	stretchFetchS   = 15.0
	defaultBuildSKm = 1.5
	checkS          = 2.0
)

// Timing keeps how fast this machine builds worlds (seconds per km of
// course), learned from its builds, for the next build's estimate. File
// empty: not kept.
type Timing struct {
	File string
	mu   sync.Mutex
}

type timingData struct {
	BuildSPerKm float64 `json:"build_s_per_km"`
}

func (t *Timing) perKm() float64 {
	if t == nil || t.File == "" {
		return defaultBuildSKm
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, err := os.ReadFile(t.File)
	var d timingData
	if err != nil || json.Unmarshal(raw, &d) != nil || d.BuildSPerKm <= 0 {
		return defaultBuildSKm
	}
	return d.BuildSPerKm
}

// learn folds a build's seconds per km into the kept rate.
func (t *Timing) learn(sPerKm float64) {
	if t == nil || t.File == "" || !(sPerKm > 0) {
		return
	}
	old := t.perKm()
	t.mu.Lock()
	defer t.mu.Unlock()
	d := timingData{BuildSPerKm: 0.5*old + 0.5*sPerKm}
	if old == defaultBuildSKm {
		d.BuildSPerKm = sPerKm
	}
	if raw, err := json.Marshal(d); err == nil {
		tmp := t.File + ".tmp"
		if os.WriteFile(tmp, raw, 0o644) == nil {
			_ = os.Rename(tmp, t.File)
		}
	}
}

// tracker turns a build's steps into Progress with an estimate of the time
// left: the current phase's from its own rate where it has one, the later
// phases' from what is known ahead (stretches to fetch, km to build).
type tracker struct {
	mu          sync.Mutex
	report      func(Progress)
	km          float64
	buildSPerKm float64
	missing     int // map data stretches still to fetch
	fetchS      float64
	phase       string
	phaseAt     time.Time
}

func (t *tracker) start(phase string) {
	t.mu.Lock()
	t.phase, t.phaseAt = phase, time.Now()
	t.mu.Unlock()
	t.step(0, 0, 0)
}

// later is the estimate for the phases after the current one.
func (t *tracker) later() float64 {
	left := checkS
	switch t.phase {
	case PhaseGround:
		left += float64(t.missing)*t.fetchS + t.km*t.buildSPerKm
	case PhaseMaps:
		left += t.km * t.buildSPerKm
	}
	return left
}

// step reports the phase frac done (done of total where counted).
func (t *tracker) step(done, total int, frac float64) {
	if t == nil || t.report == nil {
		return
	}
	t.mu.Lock()
	el := time.Since(t.phaseAt).Seconds()
	var now float64
	switch t.phase {
	case PhaseMaps:
		now = float64(t.missing) * t.fetchS
	case PhaseBuild:
		now = t.km * t.buildSPerKm * (1 - frac)
		if frac > 0.15 { // its own rate, once there is one
			now = el / frac * (1 - frac)
		}
	case PhaseCheck:
		now = 0
	default:
		if frac > 0.05 && el > 1 {
			now = el / frac * (1 - frac)
		} else {
			now = -1
		}
	}
	p := Progress{Phase: t.phase, Done: done, Total: total, Frac: frac, LeftS: -1}
	if now >= 0 {
		p.LeftS = math.Round(now + t.later())
	}
	t.mu.Unlock()
	t.report(p)
}

// FormatProgress is p as osscycler-world -progress prints it:
// ProgressPrefix and JSON (phase, done, total, frac, left_s).
func FormatProgress(p Progress) string {
	raw, _ := json.Marshal(struct {
		Phase string  `json:"phase"`
		Done  int     `json:"done"`
		Total int     `json:"total"`
		Frac  float64 `json:"frac"`
		LeftS float64 `json:"left_s"`
	}{p.Phase, p.Done, p.Total, math.Round(p.Frac*1000) / 1000, p.LeftS})
	return ProgressPrefix + string(raw)
}
