package record

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/digimago/osscycler/fit"
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
	Err       error // the file doesn't decode; the rest is empty
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
	for _, m := range msgs {
		switch m.Num {
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
	return a
}
