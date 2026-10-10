package scenery

import (
	"math"

	"github.com/digimago/osscycler/internal/course"
)

// Countryside is made-up farmland along any course, for a preview world
// while the real one is built (owner, 2026-10-09: ride a road along the
// GPX with generic terrain meanwhile): fields and meadows in long parcels,
// blocks of forest, rows of trees along some parcels, and now and then a
// farmstead beside the road. It goes through the same land use and
// building code as map data, as the test tracks' landscapes do.
//
// Everything is keyed by place (a fixed 200 m grid east and north of the
// course's start, the farmsteads by the grid cell beside the road), never
// drawn from one random sequence, so a longer GPX changes only what it
// adds.
func Countryside(c *course.Course) *Track {
	g := newTrackGen(c)
	const (
		tileM           = 200.0
		reach           = 340.0 // past the route: the world's corridor and a bit
		parcelM, ditchM = 50.0, 2.5
	)
	// The tiles the corridor reaches, in the order the route meets them.
	seen := map[[2]int]bool{}
	var tiles [][2]int
	r := int(math.Ceil(reach / tileM))
	for _, p := range g.route {
		k := [2]int{int(math.Floor(p[0] / tileM)), int(math.Floor(p[1] / tileM))}
		for dx := -r; dx <= r; dx++ {
			for dy := -r; dy <= r; dy++ {
				t := [2]int{k[0] + dx, k[1] + dy}
				if seen[t] {
					continue
				}
				centre := [2]float64{(float64(t[0]) + 0.5) * tileM, (float64(t[1]) + 0.5) * tileM}
				if math.Hypot(centre[0]-p[0], centre[1]-p[1]) > reach+tileM*math.Sqrt2/2 {
					continue
				}
				seen[t] = true
				tiles = append(tiles, t)
			}
		}
	}
	leaf := []string{"needleleaved", "broadleaved", "mixed"}
	for _, t := range tiles {
		x0, y0 := float64(t[0])*tileM, float64(t[1])*tileM
		// Forest comes in patches of a few tiles: a coarse cell decides.
		if hashUnit(t[0]>>2, t[1]>>2, 1) < 0.22 {
			g.area([][2]float64{{x0, y0}, {x0 + tileM, y0}, {x0 + tileM, y0 + tileM}, {x0, y0 + tileM}},
				tags("landuse", "forest", "leaf_type", leaf[int(hashUnit(t[0]>>2, t[1]>>2, 2)*3)]))
			continue
		}
		// Parcels across the tile, north to south or east to west.
		ns := hashUnit(t[0], t[1], 3) < 0.5
		for k := range int(tileM / parcelM) {
			off := (float64(k) + 0.5) * parcelM
			c := [2]float64{x0 + off, y0 + tileM/2}
			h := math.Pi / 2
			if !ns {
				c, h = [2]float64{x0 + tileM/2, y0 + off}, 0
			}
			use := tags("landuse", "farmland")
			if hashUnit(t[0], t[1], 10+k) < 0.45 {
				use = tags("landuse", "meadow")
			}
			g.area(trackRect(c, h, tileM-ditchM, parcelM-ditchM), use)
			if hashUnit(t[0], t[1], 20+k) < 0.12 {
				// A row of trees along the parcel's edge.
				e := [2]float64{c[0] - parcelM/2 + 3, c[1]}
				if !ns {
					e = [2]float64{c[0], c[1] - parcelM/2 + 3}
				}
				g.area(trackRect(e, h, tileM-20, 6), tags("landuse", "forest", "leaf_type", "broadleaved"))
			}
		}
	}
	// Farmsteads beside the road: per 100 m of route, the grid cell there
	// decides, on the side it says; not two within 600 m.
	last := math.Inf(-1)
	for d := 50.0; d+10 < c.Distance; d += 100 {
		p, q := g.at(d), g.at(d+5)
		k := [2]int{int(math.Floor(p[0] / 100)), int(math.Floor(p[1] / 100))}
		if d-last < 600 || hashUnit(k[0], k[1], 30) > 0.12 {
			continue
		}
		last = d
		h := math.Atan2(q[1]-p[1], q[0]-p[0])
		side := 1.0
		if hashUnit(k[0], k[1], 31) < 0.5 {
			side = -1
		}
		nx, ny := -math.Sin(h)*side, math.Cos(h)*side
		off := func(across, along float64) [2]float64 {
			return [2]float64{p[0] + nx*across + math.Cos(h)*along, p[1] + ny*across + math.Sin(h)*along}
		}
		g.area(trackRect(off(45, 0), h, 70, 50), tags("landuse", "farmyard"))
		g.building(off(25, -12), h, 13, 9, "building", "farmhouse", "building:levels", "1")
		g.building(off(45, 8), h, 32, 16, "building", "barn")
		g.building(off(30, 22), h, 10, 6, "building", "shed")
		g.area(trackRect(off(45, -45), h, 40, 40), tags("landuse", "orchard"))
	}
	return &Track{Data: g.data}
}

// hashUnit is a number in [0, 1) for grid cell x, y and a salt: the same
// for the same place every time.
func hashUnit(x, y, salt int) float64 {
	z := uint64(int64(x))*0x9e3779b97f4a7c15 ^ uint64(int64(y))*0xc2b2ae3d27d4eb4f ^ uint64(salt)*0x165667b19e3779f9
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	z ^= z >> 31
	return float64(z>>11) / (1 << 53)
}
