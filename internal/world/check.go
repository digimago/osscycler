package world

import (
	"math"
	"runtime"
	"sort"
	"sync"
)

// The builder's own check of a world (owner, 2026-10-09): seen from above
// within checkReachM of the rider's path, every checkCellM, whether
// anything is drawn there (holes: the sky shows through the ground) and
// whether land (terrain and verges) lies over a road surface (grass
// through the asphalt). And along the riding line (the path, lane_m to its
// right, where renderers put the rider) every checkRideM, whether a road
// surface is there at all: off road (2026-10-10: an 8.7 m road between two
// junctions in Rhenen was left out, and the rider crossed the grass under
// it; the terrain was whole, so neither holes nor land over road saw it).
// riddenMaterials are the surfaces a rider can be on (settleOnRoad keeps
// the riding line on them).
var riddenMaterials = map[string]bool{"road": true, "cycle lane": true, "sidewalk": true, "kerb": true,
	"roundabout centre": true, "start line": true, "running track": true}

const (
	checkReachM = 40.0
	checkCellM  = 0.25
	checkTileM  = 64.0
	checkOverM  = 0.02 // land this much above the road counts
	checkSpotM  = 200.0
	checkRideM  = 1.0 // the riding line is checked this often
)

// CheckResult is what the check found: areas in m², and the worst
// stretches by distance along the course (checkSpotM each, the most first).
type CheckResult struct {
	HolesM2, LandOverRoadM2 float64
	Holes, LandOverRoad     []CheckSpot
	// OffRoadM is how much of the riding line has no road surface under
	// it, in metres (OffRoad: by stretch, Area in metres too).
	OffRoadM float64
	OffRoad  []CheckSpot
}

// CheckSpot is a stretch of the course from D metres, with Area m² found.
type CheckSpot struct {
	D, Area float64
}

// Check looks the world over from above (see CheckResult).
func (w *World) Check() CheckResult {
	const (
		other = iota
		land
		road
	)
	class := func(mat int) int {
		switch w.doc.MaterialName(mat) {
		case "terrain":
			return land
		}
		if riddenMaterials[w.doc.MaterialName(mat)] {
			return road
		}
		return other
	}
	path := w.Manifest.Path.XYZ
	n := len(path) / 3
	if n == 0 {
		return CheckResult{}
	}
	tileOf := func(x, z float64) [2]int {
		return [2]int{int(math.Floor(x / checkTileM)), int(math.Floor(z / checkTileM))}
	}
	// The tiles near the path, with the path points that reach into each.
	near := map[[2]int][]int{}
	for i := range n {
		x, z := path[3*i], path[3*i+2]
		t0, t1 := tileOf(x-checkReachM, z-checkReachM), tileOf(x+checkReachM, z+checkReachM)
		for tx := t0[0]; tx <= t1[0]; tx++ {
			for tz := t0[1]; tz <= t1[1]; tz++ {
				k := [2]int{tx, tz}
				near[k] = append(near[k], i)
			}
		}
	}
	// The riding line every checkRideM, by tile: the path's point moved
	// lane_m to the right of the way it goes (x east, z south: right of
	// (dx, dz) is (-dz, dx)).
	type ride struct {
		x, z float64
		spot int
	}
	rides := map[[2]int][]ride{}
	lane := w.Manifest.Path.LaneM
	step := w.Manifest.Path.StepM
	for i := 0; i+1 < n; i++ {
		ax, az, bx, bz := path[3*i], path[3*i+2], path[3*i+3], path[3*i+5]
		dx, dz := bx-ax, bz-az
		l := math.Hypot(dx, dz)
		if l < 1e-9 {
			continue
		}
		dx, dz = dx/l, dz/l
		// At the middle of each checkRideM: not on the road's very end
		// where an open course starts.
		for k := checkRideM / 2; k < step; k += checkRideM {
			f := k / step
			off := 0.0
			if len(lane) == n {
				off = lane[i] + f*(lane[i+1]-lane[i])
			}
			x, z := ax+f*(bx-ax)-dz*off, az+f*(bz-az)+dx*off
			t := tileOf(x, z)
			if _, ok := near[t]; ok {
				rides[t] = append(rides[t], ride{x, z, int((float64(i)*step + k) / checkSpotM)})
			}
		}
	}
	// Triangles by tile.
	type tri struct{ p, i int32 }
	bins := map[[2]int][]tri{}
	for pi, p := range w.prims {
		for ti := 0; ti+2 < len(p.Indices); ti += 3 {
			minX, maxX, minZ, maxZ := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
			for _, v := range p.Indices[ti : ti+3] {
				x, z := float64(p.Positions[3*v]), float64(p.Positions[3*v+2])
				minX, maxX, minZ, maxZ = math.Min(minX, x), math.Max(maxX, x), math.Min(minZ, z), math.Max(maxZ, z)
			}
			t0, t1 := tileOf(minX, minZ), tileOf(maxX, maxZ)
			for tx := t0[0]; tx <= t1[0]; tx++ {
				for tz := t0[1]; tz <= t1[1]; tz++ {
					k := [2]int{tx, tz}
					if _, ok := near[k]; ok {
						bins[k] = append(bins[k], tri{int32(pi), int32(ti)})
					}
				}
			}
		}
	}
	keys := make([][2]int, 0, len(near))
	for k := range near {
		keys = append(keys, k)
	}
	const cells = int(checkTileM / checkCellM)
	spots := int(math.Ceil(float64(n)*w.Manifest.Path.StepM/checkSpotM)) + 1
	var mu sync.Mutex
	holes, over, off := make([]float64, spots), make([]float64, spots), make([]float64, spots)
	next := 0
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			top := make([][3]float32, cells*cells) // highest other, land, road
			nearest := make([]int32, cells*cells)
			for {
				mu.Lock()
				if next == len(keys) {
					mu.Unlock()
					return
				}
				k := keys[next]
				next++
				mu.Unlock()
				x0, z0 := float64(k[0])*checkTileM, float64(k[1])*checkTileM
				// Cells within reach of the path, and the nearest point.
				for c := range nearest {
					nearest[c] = -1
					top[c] = [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
				}
				best := make([]float64, cells*cells)
				for _, i := range near[k] {
					px, pz := path[3*i], path[3*i+2]
					i0 := max(0, int((px-checkReachM-x0)/checkCellM))
					i1 := min(cells-1, int((px+checkReachM-x0)/checkCellM))
					j0 := max(0, int((pz-checkReachM-z0)/checkCellM))
					j1 := min(cells-1, int((pz+checkReachM-z0)/checkCellM))
					for j := j0; j <= j1; j++ {
						for ii := i0; ii <= i1; ii++ {
							cx, cz := x0+(float64(ii)+0.5)*checkCellM, z0+(float64(j)+0.5)*checkCellM
							d := (cx-px)*(cx-px) + (cz-pz)*(cz-pz)
							c := j*cells + ii
							if d <= checkReachM*checkReachM && (nearest[c] < 0 || d < best[c]) {
								nearest[c], best[c] = int32(i), d
							}
						}
					}
				}
				for _, t := range bins[k] {
					p := &w.prims[t.p]
					cl := class(p.Material)
					var vx, vy, vz [3]float64
					for a := range 3 {
						v := p.Indices[int(t.i)+a]
						vx[a], vy[a], vz[a] = float64(p.Positions[3*v]), float64(p.Positions[3*v+1]), float64(p.Positions[3*v+2])
					}
					den := (vz[1]-vz[2])*(vx[0]-vx[2]) + (vx[2]-vx[1])*(vz[0]-vz[2])
					if math.Abs(den) < 1e-9 {
						continue // vertical: seen edge-on from above
					}
					minX, maxX := math.Min(vx[0], math.Min(vx[1], vx[2])), math.Max(vx[0], math.Max(vx[1], vx[2]))
					minZ, maxZ := math.Min(vz[0], math.Min(vz[1], vz[2])), math.Max(vz[0], math.Max(vz[1], vz[2]))
					i0 := max(0, int(math.Ceil((minX-x0)/checkCellM-0.5)))
					i1 := min(cells-1, int(math.Floor((maxX-x0)/checkCellM-0.5)))
					j0 := max(0, int(math.Ceil((minZ-z0)/checkCellM-0.5)))
					j1 := min(cells-1, int(math.Floor((maxZ-z0)/checkCellM-0.5)))
					for j := j0; j <= j1; j++ {
						cz := z0 + (float64(j)+0.5)*checkCellM
						for ii := i0; ii <= i1; ii++ {
							c := j*cells + ii
							if nearest[c] < 0 {
								continue
							}
							cx := x0 + (float64(ii)+0.5)*checkCellM
							a := ((vz[1]-vz[2])*(cx-vx[2]) + (vx[2]-vx[1])*(cz-vz[2])) / den
							b := ((vz[2]-vz[0])*(cx-vx[2]) + (vx[0]-vx[2])*(cz-vz[2])) / den
							const eps = -1e-9
							if a < eps || b < eps || 1-a-b < eps {
								continue
							}
							y := float32(a*vy[0] + b*vy[1] + (1-a-b)*vy[2])
							if y > top[c][cl] {
								top[c][cl] = y
							}
						}
					}
				}
				h, o, f := map[int]float64{}, map[int]float64{}, map[int]float64{}
				ninf := float32(math.Inf(-1))
				for _, rd := range rides[k] {
					ii, j := int((rd.x-x0)/checkCellM), int((rd.z-z0)/checkCellM)
					if ii >= 0 && ii < cells && j >= 0 && j < cells && top[j*cells+ii][2] == ninf {
						f[rd.spot] += checkRideM
					}
				}
				for c, i := range nearest {
					if i < 0 {
						continue
					}
					s := int(float64(i) * w.Manifest.Path.StepM / checkSpotM)
					t := top[c]
					switch {
					case t[0] == ninf && t[1] == ninf && t[2] == ninf:
						h[s] += checkCellM * checkCellM
					case t[1] != ninf && t[2] != ninf && t[1] > t[2]+checkOverM:
						o[s] += checkCellM * checkCellM
					}
				}
				mu.Lock()
				for s, a := range h {
					holes[s] += a
				}
				for s, a := range o {
					over[s] += a
				}
				for s, a := range f {
					off[s] += a
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var r CheckResult
	list := func(a []float64, total *float64) []CheckSpot {
		var out []CheckSpot
		for s, v := range a {
			if v > 0 {
				*total += v
				out = append(out, CheckSpot{D: float64(s) * checkSpotM, Area: v})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Area > out[j].Area })
		return out
	}
	r.Holes = list(holes, &r.HolesM2)
	r.LandOverRoad = list(over, &r.LandOverRoadM2)
	r.OffRoad = list(off, &r.OffRoadM)
	return r
}
