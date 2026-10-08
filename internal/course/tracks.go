package course

import (
	"math"
	"sort"
)

// The built-in test tracks: loops to ride without a course of your own,
// in free ride or during a workout. They are made here rather than read
// from GPX, lie in the open South Atlantic (no map data, nobody's home
// nearby), and are never sent to a map server.

// trackOrigin is where the tracks lie: open ocean, about 1000 km from
// any land.
const trackOriginLat, trackOriginLon = -25.5, -20.0

// trackStep is the spacing of the generated track points.
const trackStep = 2.0 // m

// Tracks returns the built-in test tracks.
func Tracks() []*Course {
	return []*Course{oval(), figureEight()}
}

// oval is a 400 m loop shaped like an athletics track: two 84.39 m
// straights joined by bends of 36.80 m radius, flat. It starts at the
// beginning of the home straight and runs anticlockwise.
func oval() *Course {
	const straight = 84.39
	r := (400 - 2*straight) / (2 * math.Pi)
	length := 2*straight + 2*math.Pi*r
	at := func(s float64) (x, y float64) {
		switch bend := math.Pi * r; {
		case s < straight: // home straight, eastwards along y = -r
			return s, -r
		case s < straight+bend: // east bend
			a := -math.Pi/2 + (s-straight)/r
			return straight + r*math.Cos(a), r * math.Sin(a)
		case s < 2*straight+bend: // back straight, westwards along y = r
			return straight - (s - straight - bend), r
		default: // west bend
			a := math.Pi/2 + (s-2*straight-bend)/r
			return r * math.Cos(a), r * math.Sin(a)
		}
	}
	return track("oval-400", "Oval 400 m", length, at, func(float64) float64 { return 2 })
}

// figureEight is a 5 km figure of eight (a lemniscate) whose road climbs
// 40 m from its lowest to its highest point. The two roads cross in the
// middle at right angles, a quarter and three quarters of the way round;
// the second pass is 10 m higher, on a bridge over the first.
func figureEight() *Course {
	const length = 5000.0
	// The lemniscate of Bernoulli is about 5.2441 times its half-width
	// round; sample it finely and measure it to get its shape by distance.
	const a = length / 5.244115108584239
	const n = 20000
	xs, ys, ss := make([]float64, n+1), make([]float64, n+1), make([]float64, n+1)
	for i := 0; i <= n; i++ {
		t := 2 * math.Pi * float64(i) / n
		d := 1 + math.Sin(t)*math.Sin(t)
		xs[i], ys[i] = a*math.Cos(t)/d, a*math.Sin(t)*math.Cos(t)/d
		if i > 0 {
			ss[i] = ss[i-1] + math.Hypot(xs[i]-xs[i-1], ys[i]-ys[i-1])
		}
	}
	scale := length / ss[n] // land on exactly 5 km
	at := func(s float64) (x, y float64) {
		s /= scale
		i := min(max(sort.SearchFloat64s(ss, s), 1), n)
		x = lerp(ss[i-1], xs[i-1], ss[i], xs[i], s) * scale
		y = lerp(ss[i-1], ys[i-1], ss[i], ys[i], s) * scale
		return x, y
	}
	// Elevation: eased between heights at these distances, so the grade
	// is level at each and steepest midway (π/2 times the average).
	keys := [][2]float64{
		{0, 5}, {600, 9}, {1250, 12}, // rolling out to the crossing, under the bridge
		{1700, 10}, {2000, 14}, {2900, 45}, // the climb: 3.4 % on average, 5.4 % at most
		{3200, 44},             // over the top
		{3750, 22}, {4300, 12}, // down across the bridge
		{5000, 5},
	}
	ele := func(s float64) float64 {
		for i := 1; i < len(keys); i++ {
			if s <= keys[i][0] {
				k0, k1 := keys[i-1], keys[i]
				f := (s - k0[0]) / (k1[0] - k0[0])
				return k0[1] + (k1[1]-k0[1])*(1-math.Cos(math.Pi*f))/2
			}
		}
		return keys[len(keys)-1][1]
	}
	return track("figure-8", "Figure 8 · 5 km", length, at, ele)
}

// track builds a loop course from a shape in metres east (x) and north
// (y) of the track origin, by distance along it.
func track(id, name string, length float64, at func(s float64) (x, y float64), ele func(s float64) float64) *Course {
	kx := metresPerDegree * math.Cos(trackOriginLat*math.Pi/180)
	n := int(math.Ceil(length / trackStep))
	pts := make([]Point, 0, n+1)
	for i := 0; i <= n; i++ {
		s := min(float64(i)*length/float64(n), length)
		x, y := at(math.Mod(s, length))
		if i == n {
			x, y = at(0) // close the loop exactly
		}
		pts = append(pts, Point{Lat: trackOriginLat + y/metresPerDegree, Lon: trackOriginLon + x/kx, Ele: ele(s)})
	}
	c, err := New(id, name, pts)
	if err != nil {
		panic("course: built-in track " + id + ": " + err.Error())
	}
	c.Loop, c.Builtin = true, true
	return c
}
