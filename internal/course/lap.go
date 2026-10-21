package course

import (
	"math"
	"sort"
)

// Lap is the part of a course rides go round and round (owner,
// 2026-10-13: a course whose finish meets its start is lapped until the
// rider stops, each lap timed). StartM and EndM are where it starts and
// ends along the course; a lap is timed at EndM, and the ride goes on
// from StartM. On the test tracks (Loop) the lap is the whole course. On
// a course that comes back to where it started, the lap is what lies
// between the stretch its start and its end share (a run-in, ridden out
// and back: the Posbank Loop's from Rheden to the Diepesteeg) and the
// course ends TailM past EndM, the lap's first metres again: there the
// rider is where they were TailM into the lap, so a ride wraps back by the
// lap's length at the course's end without a jump, and a world built
// along the course has the turn into the lap at its end.
type Lap struct {
	StartM, EndM float64
}

// Length is a lap's distance.
func (l Lap) Length() float64 { return l.EndM - l.StartM }

// LapTailM is how far a lapped course runs on past its lap's end: into
// the lap's start again, far enough for a world's path to sweep the turn
// into it.
const LapTailM = 100.0

// Lap detection: the start and the end within lapMeetM of each other; the
// run-in the stretch both ends follow within lapMeetM; a lap at least
// lapMinM long.
// Out and back may run apart for a while on parallel roads (De Steeg's
// main road and its service road, 10-45 m apart for 700 m on the Posbank
// Loop's run-in): within lapApartM of each other for up to lapGapM, the
// run-in goes on where they meet again.
const (
	lapMeetM  = 20.0
	lapApartM = 60.0
	lapGapM   = 1000.0
	lapMinM   = 1000.0
	lapScanM  = 5.0
)

// DetectLap finds the lap of a course whose end comes back to its start:
// from the end of the run-in its start and its end share (0 when none) to
// where the end joins it again. A course that ends on itself elsewhere,
// within lapMeetM of an earlier point and going the same way there (owner,
// 2026-10-17: La Bresse to the Hohneck and back, the run-in from the start
// to the second roundabout, the lap from there up, round the summit's car
// park, down and round the roundabout, ending where the lap began), is
// lapped from that point: the run-in is what comes before. ok is false for
// a course that ends elsewhere, or whose lap would be shorter than lapMinM.
func (c *Course) DetectLap() (l Lap, ok bool) {
	n := int(c.Distance / lapScanM)
	if n < 2 {
		return Lap{}, false
	}
	pts := make([][2]float64, n+1)
	for k := range pts {
		pts[k][0], pts[k][1] = c.Project(c.Position(math.Min(float64(k)*lapScanM, c.Distance)))
	}
	near := func(a, b [2]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
	if near(pts[0], pts[n]) > lapMeetM {
		return c.lapOnItself(pts)
	}
	// Out along the run-in from the start and back along it from the end:
	// i and j keep step while the two stay within lapMeetM.
	i, j := 0, n
	window := int(3 * lapMeetM / lapScanM)
	// nearest is the point of the way back nearest pts[a], looking back
	// from j over reach samples.
	nearest := func(a, j, reach int) (float64, int) {
		best, bj := math.Inf(1), j
		for k := j; k >= max(a+1, j-reach); k-- {
			if d := near(pts[a], pts[k]); d < best {
				best, bj = d, k
			}
		}
		return best, bj
	}
	for i+1 < j {
		if best, bj := nearest(i+1, j, window); best <= lapMeetM {
			i, j = i+1, bj
			continue
		}
		// Apart: on, while within lapApartM, to where they meet again.
		met := false
		for a := i + 1; a < j && float64(a-i)*lapScanM <= lapGapM; a++ {
			best, bj := nearest(a, j, window+2*(a-i))
			if best > lapApartM {
				break
			}
			if best <= lapMeetM {
				i, j, met = a, bj, true
				break
			}
		}
		if !met {
			break
		}
	}
	l = Lap{StartM: float64(i) * lapScanM, EndM: math.Min(float64(j)*lapScanM, c.Distance)}
	// The lap's end exactly where the way back passes its start (to a
	// quarter metre): the course's tail is then the lap again to the
	// metre, and a ride wrapping back by a lap lands where it was (the
	// scan's pair lay 1.5 m apart along the road at the Posbank Loop's
	// Diepesteeg, a step forward at every lap).
	sx, sy := c.Project(c.Position(l.StartM))
	best := math.Inf(1)
	for e := math.Max(l.StartM, l.EndM-2*lapMeetM); e <= math.Min(c.Distance, l.EndM+2*lapMeetM); e += 0.25 {
		ex, ey := c.Project(c.Position(e))
		if d := math.Hypot(ex-sx, ey-sy); d < best {
			best, l.EndM = d, e
		}
	}
	if l.Length() < lapMinM {
		return Lap{}, false
	}
	return l, true
}

// lapOnItself is the lap of a course whose end lies on an earlier point
// of it (within lapMeetM, the nearest such), heading the same way there
// (within 60°: an out-and-back ends beside its own start stretch going
// the other way, a course crossing its own line ends across it; neither
// is a lap). The lap runs from that point to the end.
func (c *Course) lapOnItself(pts [][2]float64) (Lap, bool) {
	n := len(pts) - 1
	heading := func(k int) (float64, float64) {
		a, b := pts[max(0, k-2)], pts[min(n, k+2)]
		return b[0] - a[0], b[1] - a[1]
	}
	ex, ey := heading(n)
	best, at := lapMeetM, -1
	for a := 1; float64(n-a)*lapScanM >= lapMinM; a++ {
		d := math.Hypot(pts[a][0]-pts[n][0], pts[a][1]-pts[n][1])
		if d > best {
			continue
		}
		hx, hy := heading(a)
		if hx*ex+hy*ey < 0.5*math.Hypot(hx, hy)*math.Hypot(ex, ey) {
			continue // the way back beside it, or across it
		}
		best, at = d, a
	}
	if at < 0 {
		return Lap{}, false
	}
	l := Lap{StartM: float64(at) * lapScanM, EndM: c.Distance}
	// The start exactly where the course's end lies (the end is the lap's
	// end to the metre; the scan found it to lapScanM).
	ex, ey = c.Project(c.Position(c.Distance))
	best = math.Inf(1)
	for s := math.Max(0, l.StartM-2*lapScanM); s <= math.Min(c.Distance, l.StartM+2*lapScanM); s += 0.25 {
		sx, sy := c.Project(c.Position(s))
		if d := math.Hypot(sx-ex, sy-ey); d < best {
			best, l.StartM = d, s
		}
	}
	if l.Length() < lapMinM {
		return Lap{}, false
	}
	return l, true
}

// Lapped is the course to ride when it comes back to its start (DetectLap):
// its run-in, one lap and LapTailM of the lap's start again, with Lap set;
// what comes after the lap (the run-in back to the start) is left out.
// Otherwise (a one-way course, or already lapped) c itself.
func (c *Course) Lapped() *Course {
	if c.Lap != nil || c.Loop {
		return c
	}
	l, ok := c.DetectLap()
	if !ok {
		return c
	}
	// The profile: every Spacing to the lap's end and on through the tail,
	// the tail's elevations the lap's start's; the course ends on a sample.
	n := int(math.Ceil((l.EndM + LapTailM) / c.Spacing))
	lc := &Course{ID: c.ID, Name: c.Name, Spacing: c.Spacing, Builtin: c.Builtin, Included: c.Included, Credit: c.Credit,
		Forward: c.Forward, frame: c.frame, raw: c.rawLength()}
	lc.Distance = float64(n) * c.Spacing
	src := func(x float64) float64 { // where along c a point of the lapped course lies
		if x > l.EndM {
			return l.StartM + (x - l.EndM)
		}
		return x
	}
	for k := 0; k <= n; k++ {
		x := float64(k) * c.Spacing
		e, _ := c.At(src(x))
		lc.dist, lc.ele = append(lc.dist, x), append(lc.ele, e)
	}
	lc.MaxGrade, lc.MinGrade = math.Inf(-1), math.Inf(1)
	for k := 1; k < len(lc.dist); k++ {
		de := lc.ele[k] - lc.ele[k-1]
		g := de / (lc.dist[k] - lc.dist[k-1]) * 100
		lc.grade = append(lc.grade, g)
		lc.MaxGrade, lc.MinGrade = math.Max(lc.MaxGrade, g), math.Min(lc.MinGrade, g)
		if de > 0 {
			lc.Gain += de
		} else {
			lc.Loss -= de
		}
	}
	// The track: c's own points to the lap's end, then from the lap's start
	// on through the tail, distances shifted.
	add := func(d, lat, lon float64) {
		lc.trackDist, lc.lat, lc.lon = append(lc.trackDist, d), append(lc.lat, lat), append(lc.lon, lon)
	}
	for k, d := range c.trackDist {
		if d >= l.EndM {
			break
		}
		add(d, c.lat[k], c.lon[k])
	}
	lat, lon := c.Position(l.EndM)
	add(l.EndM, lat, lon)
	k := sort.SearchFloat64s(c.trackDist, l.StartM)
	for ; k < len(c.trackDist) && c.trackDist[k]-l.StartM < lc.Distance-l.EndM; k++ {
		if c.trackDist[k] > l.StartM {
			add(l.EndM+c.trackDist[k]-l.StartM, c.lat[k], c.lon[k])
		}
	}
	lat, lon = c.Position(src(lc.Distance))
	add(lc.Distance, lat, lon)
	lc.Lap = &l
	return lc
}

// lapWrap is d on a lapped course that isn't a closed loop: past the
// course's end, back by the lap's length as often as needed.
func (c *Course) lapWrap(d float64) float64 {
	if l := c.Lap; l != nil && l.Length() > 0 {
		for d > c.Distance {
			d -= l.Length()
		}
	}
	return d
}
