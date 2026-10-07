// Package replay re-rides recorded course rides: each finished ride in a
// recording directory's results file, with the power from its lap of the
// FIT activity, through the same simulation and clock as a live ride.
// Replaying with the recorded parameters should give the recorded time;
// changed parameters show what they would have done to it.
package replay

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/fit"
	"github.com/digimago/osscycler/record"
	"github.com/digimago/osscycler/ride"
	"github.com/digimago/osscycler/sim"
)

// Ride is a finished course ride from the results file.
type Ride struct {
	record.Result
	// Power is the rider's power through the ride's lap, a sample a
	// second; nil when Err says why it couldn't be read.
	Power []ride.PowerSample
	Err   error
}

// Load reads every ride in dir's results file. A ride whose FIT file
// can't be read is returned with Err set; the others are still usable.
func Load(dir string) ([]Ride, error) {
	results, err := record.ReadResults(dir)
	if err != nil {
		return nil, err
	}
	rides := make([]Ride, 0, len(results))
	for _, res := range results {
		r := Ride{Result: res}
		if r.File == "" {
			r.Err = errors.New("no activity file recorded with this result")
		} else {
			r.Power, r.Err = LapPower(filepath.Join(dir, r.File), r.Lap)
		}
		rides = append(rides, r)
	}
	return rides, nil
}

// LapPower reads the power records of one lap of a FIT activity, timed
// from the lap's first record. Records without power count as 0 W.
func LapPower(path string, lap int) ([]ride.PowerSample, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	msgs, err := fit.Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	const lapNum, recordNum = 19, 20 // FIT global message numbers
	var start, end int64 = -1, -1
	n := 0
	for _, m := range msgs {
		if m.Num != lapNum {
			continue
		}
		if n == lap {
			start, _ = m.Get(2) // start_time
			end, _ = m.Get(253) // timestamp: the lap's end
			break
		}
		n++
	}
	if start < 0 {
		return nil, fmt.Errorf("%s has no lap %d", filepath.Base(path), lap)
	}
	var ps []ride.PowerSample
	var first int64 = -1
	for _, m := range msgs {
		ts, ok := m.Get(253)
		if m.Num != recordNum || !ok || ts < start || ts > end {
			continue
		}
		if first < 0 {
			first = ts
		}
		w, _ := m.Get(7) // power; absent reads as 0
		ps = append(ps, ride.PowerSample{At: time.Duration(ts-first) * time.Second, PowerW: float64(w)})
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("%s: lap %d has no records", filepath.Base(path), lap)
	}
	return ps, nil
}

// Params are what the ride was simulated with, or the defaults for a
// result recorded before parameters were kept.
func (r Ride) Params() sim.Params {
	if p, ok := r.Sim.Params(); ok {
		return p
	}
	return sim.DefaultParams(75, 9)
}

// Replay re-rides r on its course c with params p.
func (r Ride) Replay(c *course.Course, p sim.Params) ride.ReplayResult {
	return ride.Replay(c, p, r.StartM, r.StartSpeedMPS, r.Power)
}
