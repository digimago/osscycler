// Package course turns a GPX track into a rideable course: distance along
// the route, smoothed elevation and grade.
package course

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// Step is the largest spacing of the resampled course. The actual
	// spacing divides the course into equal parts, so it can be a little
	// smaller.
	Step = 10.0 // m
	// SmoothWindow is the span of the elevation moving average. GPX
	// elevation is often quantised (0.2 m in Garmin exports), which over a
	// 10 m step alone would be ±2 % of grade noise.
	SmoothWindow = 60.0 // m
	// minPointGap drops points that don't move, such as standstills.
	minPointGap = 0.5 // m
)

// Point is a track point. Elevation is in metres.
type Point struct {
	Lat, Lon, Ele float64
}

// Course is a route resampled every Step metres.
type Course struct {
	ID, Name string
	Distance float64 // m
	Spacing  float64 // m between samples, at most Step
	Gain     float64 // m, from smoothed elevation
	Loss     float64 // m
	MaxGrade float64 // %
	MinGrade float64 // %

	dist  []float64 // sample positions, 0 .. Distance
	ele   []float64 // smoothed elevation at each sample
	grade []float64 // % from sample i to i+1; len(dist)-1
	// The track as recorded (after merging standstills), for positions:
	// distance along it and degrees.
	trackDist, lat, lon []float64
}

// New builds a course from track points, which need elevation.
func New(id, name string, pts []Point) (*Course, error) {
	// Distance along the track, skipping points that don't move.
	var d, e, la, lo []float64
	for i, p := range pts {
		if i == 0 {
			d, e, la, lo = append(d, 0), append(e, p.Ele), append(la, p.Lat), append(lo, p.Lon)
			continue
		}
		step := haversine(pts[i-1], p)
		if step < minPointGap && len(d) > 0 {
			// Merge into the previous point's distance; keep the newest elevation.
			e[len(e)-1] = p.Ele
			if len(d) > 1 {
				// The merged point moves on to this one; the start stays put.
				d[len(d)-1] += step
				la[len(la)-1], lo[len(lo)-1] = p.Lat, p.Lon
			}
			continue
		}
		d, e, la, lo = append(d, d[len(d)-1]+step), append(e, p.Ele), append(la, p.Lat), append(lo, p.Lon)
	}
	if len(d) < 2 || d[len(d)-1] < 2*Step {
		return nil, errors.New("course: track is too short")
	}
	total := d[len(d)-1]
	// Recordings often start (or stop) with an elevation glitch while the
	// altimeter settles: one point metres off its neighbours. The moving
	// average below narrows at the ends, so remove spikes first.
	e = despike(e)

	// Resample into equal segments. Even spacing keeps the moving average
	// unbiased right up to the finish.
	segments := int(math.Ceil(total/Step - 1e-6)) // tolerate float noise in the sum
	c := &Course{ID: id, Name: name, Distance: total, Spacing: total / float64(segments)}
	j := 0
	for i := 0; i <= segments; i++ {
		x := min(float64(i)*c.Spacing, total)
		for j < len(d)-2 && d[j+1] < x {
			j++
		}
		c.dist = append(c.dist, x)
		c.ele = append(c.ele, lerp(d[j], e[j], d[j+1], e[j+1], x))
	}
	c.ele = smooth(c.ele, int(math.Round(SmoothWindow/c.Spacing/2)))
	c.trackDist, c.lat, c.lon = d, la, lo

	c.MaxGrade, c.MinGrade = math.Inf(-1), math.Inf(1)
	for i := 1; i < len(c.dist); i++ {
		de := c.ele[i] - c.ele[i-1]
		g := de / (c.dist[i] - c.dist[i-1]) * 100
		c.grade = append(c.grade, g)
		c.MaxGrade, c.MinGrade = math.Max(c.MaxGrade, g), math.Min(c.MinGrade, g)
		if de > 0 {
			c.Gain += de
		} else {
			c.Loss -= de
		}
	}
	return c, nil
}

// At returns the elevation and grade at distance d from the start, clamped
// to the course.
func (c *Course) At(d float64) (ele, gradePct float64) {
	d = math.Max(0, math.Min(d, c.Distance))
	i := sort.SearchFloat64s(c.dist, d) // first sample >= d
	if i == 0 {
		return c.ele[0], c.grade[0]
	}
	if i >= len(c.dist) {
		i = len(c.dist) - 1
	}
	return lerp(c.dist[i-1], c.ele[i-1], c.dist[i], c.ele[i], d), c.grade[i-1]
}

// Position returns where on the map distance d from the start is, in
// degrees, clamped to the course. It follows the GPX track itself, not
// the resampled profile, so corners aren't cut; between track points it
// interpolates in a straight line, within centimetres of the great
// circle at any recording interval.
func (c *Course) Position(d float64) (lat, lon float64) {
	d = math.Max(0, math.Min(d, c.Distance))
	td := c.trackDist
	i := sort.SearchFloat64s(td, d)
	if i == 0 {
		return c.lat[0], c.lon[0]
	}
	if i >= len(td) {
		i = len(td) - 1
	}
	return lerp(td[i-1], c.lat[i-1], td[i], c.lat[i], d), lerp(td[i-1], c.lon[i-1], td[i], c.lon[i], d)
}

// Profile returns elevation and grade every Spacing metres from the start
// to the finish, for renderers to draw the course and what's coming. The
// grade at sample i holds from there to the next sample.
func (c *Course) Profile() (ele, gradePct []float64) {
	gradePct = append(append([]float64(nil), c.grade...), c.grade[len(c.grade)-1])
	return append([]float64(nil), c.ele...), gradePct
}

// metresPerDegree is the length of a degree of latitude on the haversine
// sphere.
const metresPerDegree = 6371000.0 * math.Pi / 180

// Track returns where each profile sample lies on the map, in metres east
// and north of the start, for renderers that draw the road's bends. It is
// a flat projection around the start: within about 1 % over any course
// that fits in a day's ride.
func (c *Course) Track() (east, north []float64) {
	east, north = make([]float64, len(c.dist)), make([]float64, len(c.dist))
	for i, d := range c.dist {
		east[i], north[i] = c.Project(c.Position(d))
	}
	return east, north
}

// Project maps a point to metres east and north of the start, as Track
// does.
func (c *Course) Project(lat, lon float64) (east, north float64) {
	kx := metresPerDegree * math.Cos(c.lat[0]*math.Pi/180)
	return (lon - c.lon[0]) * kx, (lat - c.lat[0]) * metresPerDegree
}

// despike applies a repeated running median: five points wide in the
// interior, three next to the ends, and Tukey's end-point rule for the first
// and last point. It removes spikes up to two points wide anywhere and
// leaves any straight slope, or a sustained step, exactly as it is.
func despike(v []float64) []float64 {
	if len(v) < 5 {
		return v
	}
	cur := append([]float64(nil), v...)
	next := make([]float64, len(v))
	for range 20 { // converges in a few passes
		changed := false
		n := len(cur) - 1
		for i := 2; i < n-1; i++ {
			next[i] = median5(cur[i-2], cur[i-1], cur[i], cur[i+1], cur[i+2])
		}
		next[1] = median3(cur[0], cur[1], cur[2])
		next[n-1] = median3(cur[n-2], cur[n-1], cur[n])
		// End-point rule: compare with the value extrapolated from the two
		// neighbours (already smoothed).
		next[0] = median3(cur[0], next[1], 3*next[1]-2*next[2])
		next[n] = median3(cur[n], next[n-1], 3*next[n-1]-2*next[n-2])
		for i := range cur {
			if next[i] != cur[i] {
				changed = true
			}
		}
		cur, next = next, cur
		if !changed {
			break
		}
	}
	return cur
}

func median3(a, b, c float64) float64 {
	return math.Max(math.Min(a, b), math.Min(math.Max(a, b), c))
}

func median5(a, b, c, d, e float64) float64 {
	v := []float64{a, b, c, d, e}
	sort.Float64s(v)
	return v[2]
}

// smooth is a centred moving average over 2k+1 samples, narrowing at the
// ends so the start and finish elevations stay put.
func smooth(v []float64, k int) []float64 {
	out := make([]float64, len(v))
	for i := range v {
		w := min(k, i, len(v)-1-i)
		var sum float64
		for j := i - w; j <= i+w; j++ {
			sum += v[j]
		}
		out[i] = sum / float64(2*w+1)
	}
	return out
}

func lerp(x0, y0, x1, y1, x float64) float64 {
	if x1 == x0 {
		return y1
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

func haversine(a, b Point) float64 {
	const r = 6371000.0
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla, dlo := la2-la1, (b.Lon-a.Lon)*math.Pi/180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// gpx covers the parts of GPX 1.1 we use: tracks, and routes as a fallback.
type gpx struct {
	Metadata struct {
		Name string `xml:"name"`
	} `xml:"metadata"`
	Trk []struct {
		Name string `xml:"name"`
		Seg  []struct {
			Pts []gpxPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
	Rte []struct {
		Name string     `xml:"name"`
		Pts  []gpxPoint `xml:"rtept"`
	} `xml:"rte"`
}

type gpxPoint struct {
	Lat float64  `xml:"lat,attr"`
	Lon float64  `xml:"lon,attr"`
	Ele *float64 `xml:"ele"`
}

// ParseGPX reads a GPX file into a course. All track segments are joined;
// without tracks the first route is used.
func ParseGPX(id string, r io.Reader) (*Course, error) {
	var g gpx
	if err := xml.NewDecoder(r).Decode(&g); err != nil {
		return nil, fmt.Errorf("course: parse GPX: %w", err)
	}
	name := g.Metadata.Name
	var raw []gpxPoint
	for _, t := range g.Trk {
		if name == "" || t.Name != "" {
			name = t.Name
		}
		for _, s := range t.Seg {
			raw = append(raw, s.Pts...)
		}
	}
	if len(raw) == 0 && len(g.Rte) > 0 {
		raw = g.Rte[0].Pts
		if g.Rte[0].Name != "" {
			name = g.Rte[0].Name
		}
	}
	if name == "" {
		name = id
	}
	pts := make([]Point, 0, len(raw))
	for _, p := range raw {
		if p.Ele == nil {
			return nil, errors.New("course: GPX has points without elevation")
		}
		pts = append(pts, Point{Lat: p.Lat, Lon: p.Lon, Ele: *p.Ele})
	}
	return New(id, name, pts)
}

// LoadDir loads every .gpx file in dir, sorted by name; the file name
// without extension is the course ID. Files that fail to load are reported
// in the error but don't stop the others.
func LoadDir(dir string) ([]*Course, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.gpx"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var (
		courses []*Course
		errs    []error
	)
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		f, err := os.Open(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c, err := ParseGPX(id, f)
		f.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		courses = append(courses, c)
	}
	return courses, errors.Join(errs...)
}
