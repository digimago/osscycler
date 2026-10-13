package world

import (
	"math"

	"github.com/digimago/osscycler/internal/scenery"
)

// Meadow fences (owner, 2026-10-12: fence posts and wire round meadows):
// a meadow by a road is fenced along it with wooden posts and strands of
// wire, a steel gate in a longer stretch. Not where a ditch lies between
// the road and the meadow: in the Dutch polder the ditch is the fence (a
// wet fence) and the cattle stay on their side of it. The map has next to
// no fences (barrier=fence is rarely mapped along pastures), so they are
// guessed, as the front-garden hedges are.
const (
	meadowFenceOffM = 1.2  // off the road's edge (its sidewalk's): the verge before it
	meadowDeepM     = 4.0  // the land this far beyond the fence is the meadow
	meadowMinM      = 15.0 // shorter stretches are left out
	meadowGateM     = 40.0 // a stretch this long has a gate
	meadowGateWideM = 4.0
	// meadowNearM: only within this of the route (thin wire further off
	// is lost in the haze, and the Posbank Loop's corridor had 13 km of it).
	meadowNearM = 60.0
	// meadowDitchM: water this far from the road's edge on a side is its
	// ditch, and the meadow behind it needs no fence (owner, 2026-10-12:
	// fences only where there is no ditch; a roadside ditch lies a few
	// metres beyond the verge).
	meadowDitchM = 8.0
)

// meadowFences are the fences along the roads where a meadow lies beside
// them: land, the buildings and the areas kept clear (car parks, lawns,
// field entrances) as for the plants.
func meadowFences(t *terrain, land *scenery.LandMap, fps []scenery.Footprint, parks []scenery.Parking) []scenery.Fence {
	if land == nil || len(t.fine) < 2 {
		return nil
	}
	sp := newSpots(t, land, fps, parks)
	route := newRouteGrid(t.coarse, meadowNearM)
	nearRoute := func(e, n float64) bool {
		hit := false
		route.within(e, n, meadowNearM, func(int) { hit = true })
		return hit
	}
	is := func(e, n float64, l scenery.Land) bool {
		got, _ := sp.landAt(e, n)
		return got == l
	}
	var out []scenery.Fence
	for side, sign := range []float64{-1, 1} {
		var run [][2]float64
		flush := func() {
			if len(run) > 1 && polyLength(run) >= meadowMinM {
				out = append(out, meadowFence(run))
			}
			run = nil
		}
		for i := 0; i+1 < len(t.fine); i++ {
			a, b := t.fine[i], t.fine[i+1]
			if a.line != b.line || a.onPatch || a.aloft || a.again || a.open[side] {
				flush()
				continue
			}
			de, dn := b.e-a.e, b.n-a.n
			l := math.Hypot(de, dn)
			if l == 0 {
				continue
			}
			de, dn = de/l, dn/l
			rx, rn := sign*dn, -sign*de // to this side
			edge := a.hw + a.wide[side]
			if a.walk[side] {
				edge += a.walkWidth(side)
			}
			off := edge + meadowFenceOffM
			e, n := a.e+rx*off, a.n+rn*off
			ok := is(e+rx*meadowDeepM, n+rn*meadowDeepM, scenery.LandMeadow) && is(e+rx, n+rn, scenery.LandMeadow)
			// No ditch along the road on this side (the fence it is), no
			// building, car park or entrance, no other road.
			for d := edge + 0.3; ok && d <= edge+meadowDitchM; d += 0.6 {
				ok = !is(a.e+rx*d, a.n+rn*d, scenery.LandWater)
			}
			ok = ok && nearRoute(e, n) && !sp.inArea(e, n) && sp.roadNear(e, n, 0.5, a.line) < 0 &&
				(t.surfaces == nil || t.surfaces.dist(e, -n, 0.5) >= 0.5)
			if !ok {
				flush()
				continue
			}
			run = append(run, [2]float64{e, n})
		}
		flush()
	}
	return out
}

// meadowFence is the fence along run, with a gate in a long one, where
// its place's seed puts it.
func meadowFence(run [][2]float64) scenery.Fence {
	f := scenery.Fence{Line: run, Wire: true}
	if l := polyLength(run); l >= meadowGateM {
		h := splitmix(uint64(int64(math.Round(run[0][0]*10)))<<32 ^ uint64(int64(math.Round(run[0][1]*10))) ^ 0x6a7e)
		at := l * (0.25 + 0.5*unit(h))
		f.GateFromM, f.GateToM = at-meadowGateWideM/2, at+meadowGateWideM/2
	}
	return f
}

// polyLength is a polyline's length.
func polyLength(p [][2]float64) float64 {
	var l float64
	for i := 1; i < len(p); i++ {
		l += math.Hypot(p[i][0]-p[i-1][0], p[i][1]-p[i-1][1])
	}
	return l
}

// polyAt is the point d along polyline p (clamped to its ends).
func polyAt(p [][2]float64, d float64) [2]float64 {
	for i := 1; i < len(p); i++ {
		l := math.Hypot(p[i][0]-p[i-1][0], p[i][1]-p[i-1][1])
		if d <= l || i == len(p)-1 {
			f := 0.0
			if l > 0 {
				f = math.Max(0, math.Min(1, d/l))
			}
			return [2]float64{p[i-1][0] + (p[i][0]-p[i-1][0])*f, p[i-1][1] + (p[i][1]-p[i-1][1])*f}
		}
		d -= l
	}
	return p[0]
}
