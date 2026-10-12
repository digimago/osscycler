package scenery

import "math"

// Arable fields in the 3D world (owner, 2026-10-12: farmland shown as
// fields, freshly ploughed or under a crop, not one flat green; the crop
// itself needn't be the real one): each farmland area becomes one of four,
// picked by its map element's ID, so a field is one crop throughout and
// the same in every build. Shares as a Dutch arable landscape in summer
// roughly has them: maize and wheat the most, potatoes, and some just
// ploughed. The real crops are in RVO's crop parcel register (BRP
// Gewaspercelen, open data on PDOK) should they ever matter.
var fieldShares = []struct {
	land  Land
	share float64
}{{LandMaize, 0.3}, {LandWheat, 0.3}, {LandPotatoes, 0.2}, {LandPloughed, 0.2}}

// Arable tells whether l is arable land: farmland, or a field the 3D
// world has sown (Fields).
func Arable(l Land) bool {
	return l == LandFarmland || l >= LandPloughed && l <= LandPotatoes
}

// Fields sows the farmland areas: each one ploughed, maize, wheat or
// potatoes.
func (m *LandMap) Fields() {
	if m == nil {
		return
	}
	for _, p := range m.polys {
		if p.land != LandFarmland {
			continue
		}
		p.rows = rowDir(p)
		u := float64(mix64(uint64(p.osm))>>11) / (1 << 53)
		for _, f := range fieldShares {
			p.land = f.land
			if u < f.share {
				break
			}
			u -= f.share
		}
	}
}

// RowDir is the direction a sown field's (area id's) rows and furrows
// run, radians anticlockwise from east, in [0, π).
func (m *LandMap) RowDir(id int) float64 {
	if id < 0 || id >= len(m.polys) {
		return 0
	}
	return m.polys[id].rows
}

// rowDir is along p's longest edge: a parcel is worked along its length.
func rowDir(p *polygon) float64 {
	best, a := -1.0, 0.0
	for _, e := range p.edges {
		if l := math.Hypot(e[2]-e[0], e[3]-e[1]); l > best {
			best, a = l, math.Atan2(e[3]-e[1], e[2]-e[0])
		}
	}
	if a < 0 {
		a += math.Pi
	}
	if a >= math.Pi {
		a -= math.Pi
	}
	return a
}

// mix64 is splitmix64's finaliser.
func mix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ x>>30) * 0xbf58476d1ce4e5b9
	x = (x ^ x>>27) * 0x94d049bb133111eb
	return x ^ x>>31
}
