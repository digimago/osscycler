package scenery

import (
	"math"

	"github.com/digimago/osscycler/internal/course"
)

// Dunes (owner, 2026-10-10: dunes by the sea are sand with marram grass,
// sea buckthorn, brambles and other dune plants, not purple heather, green
// grass and juniper). OpenStreetMap tags coastal dunes as it tags inland
// heath and drift sand (the Amsterdam Water Supply Dunes: natural=sand,
// scrub, grassland, a little heath; no natural=dune areas), so the coast
// tells them apart: within DuneM of the coastline the classes become the
// dune's own.
//   - LandDuneSand: natural=sand, the white and drifting dunes: bare sand
//     with marram grass (Ammophila arenaria).
//   - LandDuneScrub: natural=scrub, the dune thickets: sea buckthorn
//     (Hippophae rhamnoides), brambles, wild privet, creeping willow,
//     elder; between them grey dune turf.
//   - LandDuneGrass: natural=grassland or heath, the grey dunes: short
//     grey-green turf and moss, marram here and there; grassland only
//     within duneNextM of dune sand or scrub (a dike meadow by an estuary
//     isn't a dune).
//   - LandBeach: natural=beach: bare sand.
//
// The dunes reach about 4 km inland at the Amsterdam Water Supply Dunes
// (owner, 2026-10-10), among the widest on the Dutch coast; the Veluwe's
// drift sands lie 60 km and more from the sea.
const (
	DuneM     = 4000.0
	duneNextM = 150.0
)

// ByTheSea turns the heath, sand, scrub and grassland areas within DuneM
// of the sea into dunes; dist is how far a point (lat, lon) is from the
// coastline (Coast.Distance). The map is in course c's frame.
func (m *LandMap) ByTheSea(c *course.Course, dist func(lat, lon float64) float64) {
	if m == nil || dist == nil {
		return
	}
	near := func(p *polygon) bool {
		test := func(x, y float64) bool {
			lat, lon := c.Unproject(x, y)
			return dist(lat, lon) <= DuneM
		}
		if test((p.minX+p.maxX)/2, (p.minY+p.maxY)/2) {
			return true
		}
		step := max(1, len(p.edges)/8)
		for i := 0; i < len(p.edges); i += step {
			if test(p.edges[i][0], p.edges[i][1]) {
				return true
			}
		}
		return false
	}
	var grass []*polygon
	var dunes []*polygon
	for _, p := range m.polys {
		switch p.natural {
		case "sand", "beach", "scrub", "heath", "grassland":
		default:
			continue
		}
		if !near(p) {
			continue
		}
		switch p.natural {
		case "sand":
			p.land = LandDuneSand
		case "beach":
			p.land = LandBeach
		case "scrub":
			p.land = LandDuneScrub
		case "heath":
			p.land = LandDuneGrass
		case "grassland":
			grass = append(grass, p)
			continue
		}
		dunes = append(dunes, p)
	}
	for _, g := range grass {
		for _, d := range dunes {
			if d.land != LandBeach && g.minX-duneNextM <= d.maxX && d.minX-duneNextM <= g.maxX &&
				g.minY-duneNextM <= d.maxY && d.minY-duneNextM <= g.maxY && polyGap(g, d) <= duneNextM {
				g.land = LandDuneGrass
				break
			}
		}
	}
}

// polyGap is about how far apart two areas' outlines come (from the
// points of one to the edges of the other, both ways); 0 if they touch.
func polyGap(a, b *polygon) float64 {
	best := math.Inf(1)
	for _, pair := range [2][2]*polygon{{a, b}, {b, a}} {
		p, q := pair[0], pair[1]
		step := max(1, len(p.edges)/64)
		for i := 0; i < len(p.edges); i += step {
			x, y := p.edges[i][0], p.edges[i][1]
			if q.contains(x, y) {
				return 0
			}
			for _, e := range q.edges {
				dx, dy := e[2]-e[0], e[3]-e[1]
				f := 0.0
				if l2 := dx*dx + dy*dy; l2 > 0 {
					f = math.Max(0, math.Min(1, ((x-e[0])*dx+(y-e[1])*dy)/l2))
				}
				best = math.Min(best, math.Hypot(x-e[0]-f*dx, y-e[1]-f*dy))
			}
		}
	}
	return best
}
