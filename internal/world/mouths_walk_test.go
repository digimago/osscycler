package world

import (
	"math"
	"testing"

	"github.com/digimago/osscycler/internal/gltf"
	"github.com/digimago/osscycler/internal/scenery"
)

// mouthX is where the side road joins the main road in frenchMouth: off
// the 250 m chunk edge at 0, where a strip's pieces meet and each closes
// its end inside the slab.
const mouthX = 50.0

// frenchMouth builds, through the French junction rules, a main road with
// sidewalks running east and a side road of class joining it from the
// south at x = mouthX, and returns the road meshes by chunk, the
// materials and the junction.
func frenchMouth(t *testing.T, class string) (map[[2]int][]gltf.Primitive, roadMaterials, patch) {
	t.Helper()
	return frenchFork(t, class, [][2]float64{{mouthX, 0}, {mouthX, -100}})
}

// frenchFork is frenchMouth with the side road along side.
func frenchFork(t *testing.T, class string, side [][2]float64) (map[[2]int][]gltf.Primitive, roadMaterials, patch) {
	t.Helper()
	ways := []scenery.Way{
		{Line: [][2]float64{{mouthX - 100, 0}, {mouthX, 0}, {mouthX + 100, 0}}, Nodes: []int64{1, 2, 3}, Name: "Main", Class: "secondary", WidthM: 6, Sidewalk: [2]bool{true, true}},
		{Line: side, Nodes: []int64{2, 4}, Class: class, WidthM: 4},
	}
	ls := networkLines(ways, nil, func(e, n float64, _ bool) bool { return true })
	for _, l := range ls {
		finishRoad(l.samples, 5)
	}
	ps := networkPatches(ls, ways, 0)
	if len(ps) != 1 {
		t.Fatalf("%d junctions, want one at the mouth", len(ps))
	}
	cutRoads(ls, ps)
	walksOnSpans(ls, ps, ways)
	m := addRoadMaterials(gltf.New("test"), &styleFR)
	by, _, _ := roadPrims(ls, ps, 250, m, nil)
	return by, m, ps[0]
}

// tris calls f with each triangle's vertices of every primitive in by of
// material mat.
func tris(by map[[2]int][]gltf.Primitive, mat int, f func(v [3][3]float64)) {
	for _, k := range chunkOrder(by) {
		for _, p := range by[k] {
			if p.Material != mat {
				continue
			}
			for i := 0; i+2 < len(p.Indices); i += 3 {
				var v [3][3]float64
				for j := range 3 {
					q := p.Indices[i+j]
					v[j] = [3]float64{float64(p.Positions[3*q]), float64(p.Positions[3*q+1]), float64(p.Positions[3*q+2])}
				}
				f(v)
			}
		}
	}
}

func centroid(v [3][3]float64) (x, y, z float64) {
	return (v[0][0] + v[1][0] + v[2][0]) / 3, (v[0][1] + v[1][1] + v[2][1]) / 3, (v[0][2] + v[1][2] + v[2][2]) / 3
}

// alongRoad tells whether a vertical triangle faces along the main road
// (east or west): a face across a sidewalk.
func alongRoad(v [3][3]float64) bool {
	n := cross([3]float64{v[1][0] - v[0][0], v[1][1] - v[0][1], v[1][2] - v[0][2]}, [3]float64{v[2][0] - v[0][0], v[2][1] - v[0][1], v[2][2] - v[0][2]})
	l := math.Sqrt(dot3(n, n))
	return l > 0 && math.Abs(n[0])/l > 0.9
}

// At a service road's mouth (a parking aisle, a driveway) the French
// road's sidewalk ramps down to it over dropRampM either side and leaves
// the mouth to the other road's asphalt, as a driveway entrance (owner,
// 2026-10-18: a dropped kerb for service roads and driveways; a slab left
// across lay under a climbing drive's asphalt and flickered, 1.55 km):
// full height away from it, no slab over it, its ends low.
func TestServiceMouthHasADroppedKerb(t *testing.T) {
	by, m, p := frenchMouth(t, "service")
	if !p.dropWalks {
		t.Fatal("the junction doesn't know the sidewalk runs on across it")
	}
	// The south (right) sidewalk, hw 3 to 4.8 m.
	var atMouth, before, ramp int
	var beforeY, rampY float64
	for _, mat := range []int{m.walk, m.walkDrop} {
		tris(by, mat, func(v [3][3]float64) {
			x, y, z := centroid(v)
			if z < 3.2 || z > 4.6 {
				return
			}
			switch dx := math.Abs(x - mouthX); {
			case dx < 1:
				atMouth++
			case math.Abs(x-mouthX+30) < 1:
				before++
				beforeY = y
			case dx < 15 && y < kerbM-0.01:
				ramp++
				rampY = y
			}
		})
	}
	if atMouth > 0 {
		t.Errorf("%d slab triangles over the mouth's middle, want none: the mouth is the drive's", atMouth)
	}
	if before == 0 || math.Abs(beforeY-kerbM) > 0.005 {
		t.Errorf("30 m before the mouth: %d slab triangles at %.3f m; want the full kerb, %.2f m", before, beforeY, kerbM)
	}
	if ramp == 0 {
		t.Error("no lowered slab beside the mouth: the kerb should ramp down to it")
	}
	_ = rampY
	var tall int
	tris(by, m.kerb, func(v [3][3]float64) {
		x, _, z := centroid(v)
		if z > 2.5 && z < 5.5 && math.Abs(x-mouthX) < 15 && alongRoad(v) {
			if top := math.Max(v[0][1], math.Max(v[1][1], v[2][1])); top > dropKerbM+0.05 {
				tall++
			}
		}
	})
	if tall > 0 {
		t.Errorf("%d tall kerb faces across the south sidewalk by the mouth: its ends should come down low", tall)
	}
	// No ordinary slab over a carriageway.
	var over int
	tris(by, m.walk, func(v [3][3]float64) {
		if x, _, z := centroid(v); overCarriageway(p, x, z) {
			over++
		}
	})
	if over > 0 {
		t.Errorf("%d sidewalk triangles of the ordinary slab lie over a carriageway, where the clipper would cut them", over)
	}
	// Across from the mouth the road's sidewalk runs on alone: no second
	// slab at its height (it flickered).
	var north int
	px, pz := mouthX+0.13, -4.07
	for _, mat := range []int{m.walk, m.walkDrop} {
		tris(by, mat, func(v [3][3]float64) {
			d1 := (v[1][0]-v[0][0])*(pz-v[0][2]) - (v[1][2]-v[0][2])*(px-v[0][0])
			d2 := (v[2][0]-v[1][0])*(pz-v[1][2]) - (v[2][2]-v[1][2])*(px-v[1][0])
			d3 := (v[0][0]-v[2][0])*(pz-v[2][2]) - (v[0][2]-v[2][2])*(px-v[2][0])
			if d1 >= 0 && d2 >= 0 && d3 >= 0 || d1 <= 0 && d2 <= 0 && d3 <= 0 {
				north++
			}
		})
	}
	if north != 1 {
		t.Errorf("%d slabs over a point of the sidewalk across from the mouth, want the road's alone (1)", north)
	}
}

// overCarriageway tells whether x, z (glTF) lies over the junction's
// surface, the main road's carriageway (6 m, along z = 0) or the side
// road's (4 m, along x = 0, south).
func overCarriageway(p patch, x, z float64) bool {
	return inPoly(p.poly, x, -z) || math.Abs(z) < 2.9 || math.Abs(x-mouthX) < 1.9 && z > 0
}

// At a side street the French road's sidewalk ends square at the
// junction's edge and the junction's own takes it round the corner to the
// street's edge: nothing of it lies over the junction's surface, so
// nothing is clipped (owner, 2026-10-18, La Bresse: the slab ran into the
// junction and was cut along the corner's arc, its faces showing).
func TestSideStreetSidewalkRoundsTheCorner(t *testing.T) {
	by, m, p := frenchMouth(t, "residential")
	if p.dropWalks {
		t.Fatal("a residential street's mouth taken for a service road's")
	}
	var onJunction, corner, caps int
	for _, mat := range []int{m.walk, m.walkDrop} {
		tris(by, mat, func(v [3][3]float64) {
			x, _, z := centroid(v)
			if inPoly(p.poly, x, -z) {
				onJunction++
			}
			if z > 3.5 && z < 12 && x > mouthX-12 && x < mouthX-2 {
				corner++
			}
		})
	}
	if onJunction > 0 {
		t.Errorf("%d sidewalk triangles lie over the junction's surface", onJunction)
	}
	if corner == 0 {
		t.Error("no sidewalk round the south-west corner, from the main road's edge to the side street's")
	}
	tris(by, m.kerb, func(v [3][3]float64) {
		if x, _, z := centroid(v); z > 2.5 && z < 5.5 && x < mouthX && x > mouthX-20 && alongRoad(v) {
			caps++
		}
	})
	if caps < 2 {
		t.Errorf("%d kerb faces across the south sidewalk before the mouth, want its square end closed (2)", caps)
	}
	var over int
	for _, mat := range []int{m.walk, m.walkDrop} {
		tris(by, mat, func(v [3][3]float64) {
			if x, _, z := centroid(v); overCarriageway(p, x, z) {
				over++
			}
		})
	}
	if over > 0 {
		t.Errorf("%d sidewalk triangles lie over a carriageway, want none", over)
	}
}

// A drive leaving at a shallow angle lies under the main road's sidewalk
// for metres past the junction's span: the dropped kerb runs on over it,
// so no ordinary slab lies on its asphalt to be clipped into slivers
// (owner, 2026-10-18, 1.55 km of the Route des Américains: a spike where
// the drive parted).
func TestShallowDriveKeepsTheDroppedKerb(t *testing.T) {
	by, m, _ := frenchFork(t, "service", [][2]float64{{mouthX, 0}, {mouthX + 60, -9}})
	slab := func() map[[3]int]bool {
		out := map[[3]int]bool{}
		tris(by, m.walk, func(v [3][3]float64) {
			x, y, z := centroid(v)
			out[[3]int{int(math.Round(x * 100)), int(math.Round(y * 100)), int(math.Round(z * 100))}] = true
		})
		return out
	}
	before := slab()
	carriageway := func(mat int) bool { return mat == m.surface[scenery.SurfaceAsphalt] }
	clearWalksOffRoads(by, m.walk, m.kerb, carriageway)
	after := slab()
	changed := 0
	for k := range before {
		if !after[k] {
			changed++
		}
	}
	for k := range after {
		if !before[k] {
			changed++
		}
	}
	if changed > 0 {
		t.Errorf("%d triangles of the ordinary slab clipped off the drive's carriageway, want none", changed)
	}
}
