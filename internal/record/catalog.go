package record

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/effort"
	"github.com/digimago/osscycler/internal/fit"
)

// Activity is a finished recording, summarised from its session message.
type Activity struct {
	Name      string // file name in the recording directory
	Size      int64
	Start     time.Time
	Elapsed   time.Duration
	Timer     time.Duration // riding time, pauses left out
	DistanceM float64
	AvgPowerW float64 // 0 when unknown
	Virtual   bool    // has course laps
	Laps      int
	// HeldMaxHR is the highest heart rate held for HeldHRS seconds (0:
	// none read): what the rider has shown, not their maximum, and over a
	// spike of a strap that misreads.
	HeldMaxHR int
	// HeldThresholdHR is 95 % of the highest heart rate averaged over
	// ThresholdHRS seconds (0: none that long): Friel's threshold from a
	// 20-minute all-out effort, so a lower bound on the rider's threshold
	// unless the activity held one.
	HeldThresholdHR int
	Err             error // the file doesn't decode; the rest is empty
}

// activityName is how the recorder names files; anything else (paths,
// "..", hidden files) is never served.
var activityName = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{6}\.fit$`)

// ErrNoActivity: no finished recording by that name.
var ErrNoActivity = errors.New("no such activity")

// Catalog lists and opens the finished recordings in a directory. It keeps
// each file's summary until the file changes, so listing stays quick as
// rides pile up.
type Catalog struct {
	Dir string

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	size  int64
	mtime time.Time
	a     Activity
}

// List returns the finished recordings, newest first. Files being written
// (.fit.part) aren't listed.
func (c *Catalog) List() ([]Activity, error) {
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]cached{}
	}
	seen := map[string]bool{}
	var out []Activity
	for _, e := range entries {
		name := e.Name()
		if !activityName.MatchString(name) || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		seen[name] = true
		if k, ok := c.cache[name]; ok && k.size == info.Size() && k.mtime.Equal(info.ModTime()) {
			out = append(out, k.a)
			continue
		}
		a := summarise(filepath.Join(c.Dir, name))
		a.Name, a.Size = name, info.Size()
		c.cache[name] = cached{info.Size(), info.ModTime(), a}
		out = append(out, a)
	}
	for name := range c.cache {
		if !seen[name] {
			delete(c.cache, name)
		}
	}
	// Names are start times, so they sort like the rides.
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Open opens a finished recording for reading, with its size. The name
// must be one List returns: no paths, no unfinished files.
func (c *Catalog) Open(name string) (io.ReadCloser, int64, error) {
	if !activityName.MatchString(name) {
		return nil, 0, fmt.Errorf("%w: %q", ErrNoActivity, name)
	}
	f, err := os.Open(filepath.Join(c.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("%w: %s", ErrNoActivity, name)
	}
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// summarise reads a recording's session message.
func summarise(path string) Activity {
	b, err := os.ReadFile(path)
	if err != nil {
		return Activity{Err: err}
	}
	msgs, err := fit.Decode(b)
	if err != nil {
		return Activity{Err: err}
	}
	var a Activity
	found := false
	var hr []int
	for _, m := range msgs {
		switch m.Num {
		case recordMsg.Num:
			v, ok := m.Get(3)
			if !ok {
				v = 0
			}
			hr = append(hr, int(v))
		case sessionMsg.Num:
			found = true
			if v, ok := m.Get(2); ok {
				a.Start = fit.FromTime(v)
			}
			if v, ok := m.Get(7); ok {
				a.Elapsed = time.Duration(v) * time.Millisecond
			}
			if v, ok := m.Get(8); ok {
				a.Timer = time.Duration(v) * time.Millisecond
			}
			if v, ok := m.Get(9); ok {
				a.DistanceM = float64(v) / 100
			}
			if v, ok := m.Get(20); ok {
				a.AvgPowerW = float64(v)
			}
			if v, ok := m.Get(26); ok {
				a.Laps = int(v)
			}
			if v, ok := m.Get(6); ok {
				a.Virtual = v == subVirtual
			}
		}
	}
	if !found {
		return Activity{Err: errors.New("no session summary")}
	}
	a.HeldMaxHR = heldMax(hr, HeldHRS)
	if best := bestAverage(hr, ThresholdHRS); best > 0 {
		a.HeldThresholdHR = int(math.Round(thresholdShare * best))
	}
	return a
}

// ThresholdHRS is how long a threshold effort lasts (Friel's 20-minute
// test), and thresholdShare the share of its average heart rate that is
// the threshold.
const (
	ThresholdHRS   = 1200
	thresholdShare = 0.95
)

// bestAverage is the highest average of n values in a row (0 when there
// are fewer).
func bestAverage(v []int, n int) float64 {
	if n <= 0 || len(v) < n {
		return 0
	}
	sum := 0
	for _, x := range v[:n] {
		sum += x
	}
	best := sum
	for i := n; i < len(v); i++ {
		sum += v[i] - v[i-n]
		best = max(best, sum)
	}
	return float64(best) / float64(n)
}

// HeldHRS: a heart rate counts as reached once held this long (seconds of
// records); a strap that misreads spikes for a second or two.
const HeldHRS = 10

// heldMax is the highest value held for n consecutive samples (the
// highest of each window's lowest); 0 for none (no reading counts as 0).
func heldMax(v []int, n int) int {
	best := 0
	for i := 0; i+n <= len(v); i++ {
		low := v[i]
		for _, x := range v[i+1 : i+n] {
			low = min(low, x)
		}
		best = max(best, low)
	}
	return best
}

// Samples reads a finished recording's samples, one a second of riding
// time, and the FTP it was ridden with (0 when it has none). The name is
// checked as Open checks it.
func (c *Catalog) Samples(name string) ([]effort.Sample, float64, error) {
	f, _, err := c.Open(name)
	if err != nil {
		return nil, 0, err
	}
	b, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, 0, err
	}
	msgs, err := fit.Decode(b)
	if err != nil {
		return nil, 0, err
	}
	var out []effort.Sample
	ftp := 0.0
	reading := func(m fit.Msg, num uint8) int {
		if v, ok := m.Get(num); ok {
			return int(v)
		}
		return -1
	}
	for _, m := range msgs {
		switch m.Num {
		case recordMsg.Num:
			x := effort.Sample{T: float64(len(out)), PowerW: reading(m, 7), HR: reading(m, 3), Cadence: reading(m, 4)}
			if v, ok := m.Get(6); ok {
				x.SpeedMS = float64(v) / 1000
			}
			if v, ok := m.Get(5); ok {
				x.DistM = float64(v) / 100
			}
			out = append(out, x)
		case zonesTargetMsg.Num:
			if v, ok := m.Get(3); ok {
				ftp = float64(v)
			}
		}
	}
	return out, ftp, nil
}
