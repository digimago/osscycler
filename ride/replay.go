package ride

import (
	"context"
	"io"
	"log/slog"
	"math"
	"time"

	"github.com/digimago/osscycler/course"
	"github.com/digimago/osscycler/sim"
	"github.com/digimago/osscycler/telemetry"
)

// PowerSample is the rider's power from At on, until the next sample.
type PowerSample struct {
	At     time.Duration // from the first sample
	PowerW float64
}

// ReplayResult is how a re-ridden course turned out.
type ReplayResult struct {
	Finished  bool
	Elapsed   time.Duration // the ride clock: to the line, or to the end of the power
	DistanceM float64       // where the rider got to on the course
	AvgPowerW float64
	ClimbedM  float64
	// Trace is the position over the ride clock, every tick from the start
	// (0 at the start point) to the finish or the end of the power.
	Trace []TracePoint
}

// maxSampleGap: power older than this counts as not pedalling. Recordings
// have a sample a second; a longer gap is a pause (auto-pause stops the
// records).
const maxSampleGap = 2 * time.Second

// Replay re-rides course c with recorded power, through the same session
// and ticks as a live ride: armed at the start (or a rolling start at
// startM; at startSpeed when that is above 0), the clock from the first
// pedal stroke, stopped at the line. On a loop that is one lap. Without
// the trainer, nothing feels the grade; riding time never depends on it
// anyway.
func Replay(c *course.Course, params sim.Params, startM, startSpeed float64, power []PowerSample) ReplayResult {
	hub := telemetry.NewHub()
	cfg := DefaultConfig(params)
	cfg.StartDistanceM, cfg.StartSpeedMPS = startM, startSpeed
	s := NewSession(hub, nopTrainer{}, []*course.Course{c}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(c.ID); err != nil {
		return ReplayResult{} // only fails for an unknown course, and c is the one
	}

	t0 := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) // any fixed instant: replays are deterministic
	var end time.Duration
	if n := len(power); n > 0 {
		end = power[n-1].At + time.Second
	}
	i := 0
	trace := []TracePoint{{0, math.Max(0, math.Min(startM, c.Distance-minRideM))}}
	for at := time.Duration(0); at <= end; at += tickInterval {
		for i+1 < len(power) && power[i+1].At <= at {
			i++
		}
		w := 0.0
		if len(power) > 0 && power[i].At <= at && at-power[i].At <= maxSampleGap {
			w = math.Max(0, power[i].PowerW)
		}
		hub.Update(func(st *telemetry.State) bool {
			st.Trainer.PowerW = telemetry.Some(uint16(math.Round(w)))
			return true
		})
		s.tick(t0.Add(at))
		st, _ := hub.Latest()
		if l := st.Ride.LastLap; c.Loop && l.N > 0 {
			trace = append(trace, TracePoint{l.Elapsed, c.Distance})
			return ReplayResult{Finished: true, Elapsed: l.Elapsed, DistanceM: c.Distance, AvgPowerW: l.AvgPowerW, ClimbedM: l.ClimbedM, Trace: trace}
		}
		if st.Ride.Phase == telemetry.RideRiding || st.Ride.Phase == telemetry.RideFinished {
			if last := trace[len(trace)-1]; st.Ride.Elapsed > last.At {
				trace = append(trace, TracePoint{st.Ride.Elapsed, st.Ride.DistanceM})
			}
		}
		if st.Ride.Phase == telemetry.RideFinished {
			break
		}
	}
	st, _ := hub.Latest()
	return ReplayResult{
		Finished:  st.Ride.Phase == telemetry.RideFinished,
		Elapsed:   st.Ride.Elapsed,
		DistanceM: st.Ride.DistanceM,
		AvgPowerW: st.Ride.AvgPowerW,
		ClimbedM:  st.Ride.ClimbedM,
		Trace:     trace,
	}
}

type nopTrainer struct{}

func (nopTrainer) SetGrade(context.Context, float64) error { return nil }
