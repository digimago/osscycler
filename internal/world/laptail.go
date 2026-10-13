package world

import (
	"math"

	"github.com/digimago/osscycler/internal/course"
)

// lapTailTurnM: a lapped course's tail keeps its own path this far past
// the lap's end (the turn into the lap: the lap's own start turns in from
// the run-in, another way).
const lapTailTurnM = 40.0

// tailOntoLap eases a lapped course's tail (course.Lap: the course runs on
// past the lap's end into the lap's start again) onto the lap's own path:
// its points, heights and riding line, from lapTailTurnM past the lap's
// end to the course's end, where it is the lap's. A ride wrapping back by
// a lap there then lands where it was (owner, 2026-10-13: the tail's turn
// was rounded otherwise than the lap's start, its path 1.5 m along the
// road behind the lap's, and the 3D view hopped forward at each lap).
// pd are the path's distances every step along the course; lanes may be
// nil. A course without a lap, or a closed loop, is left as it is.
func tailOntoLap(c *course.Course, pd, px, py, pz, lanes []float64) {
	l := c.Lap
	if l == nil || c.Loop || len(pd) < 2 {
		return
	}
	step := pd[1] - pd[0]
	from, to := l.EndM+lapTailTurnM, pd[len(pd)-1]
	if to <= from || step <= 0 {
		return
	}
	lerp := func(v []float64, d float64) float64 {
		f := math.Max(0, math.Min(d/step, float64(len(v)-1)))
		i := min(int(f), len(v)-2)
		return v[i] + (f-float64(i))*(v[i+1]-v[i])
	}
	// The lap's values first: the tail's own are overwritten as it goes.
	type at struct{ x, y, z, lane float64 }
	lap := make([]at, len(pd))
	for i, d := range pd {
		if d <= from {
			continue
		}
		d0 := d - l.Length()
		lap[i] = at{lerp(px, d0), lerp(py, d0), lerp(pz, d0), 0}
		if lanes != nil {
			lap[i].lane = lerp(lanes, d0)
		}
	}
	for i, d := range pd {
		if d <= from {
			continue
		}
		w := math.Min(1, (d-from)/(to-from))
		w = w * w * (3 - 2*w)
		px[i] += w * (lap[i].x - px[i])
		py[i] += w * (lap[i].y - py[i])
		pz[i] += w * (lap[i].z - pz[i])
		if lanes != nil {
			lanes[i] += w * (lap[i].lane - lanes[i])
		}
	}
}
