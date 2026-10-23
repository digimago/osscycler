package world

import (
	"fmt"
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// Motorways (owner, 2026-10-22: the figure 8's motorway): drawn as roads
// (network.go: never ridden, met only on other levels, layers.go), each
// carriageway with its markings and a crash barrier along both edges.
// The markings as on Dutch motorways: a solid line along each edge of the
// lanes, the hard shoulder outside the right one, and between the lanes
// a dashed line, 3 m on, 9 m off.
const (
	motorwayLaneM     = 3.5  // a lane
	motorwayEdgeInM   = 0.5  // the left edge line, in from the carriageway's left edge
	motorwayLineW     = 0.15 // the markings' width
	motorwayDashOn    = 3.0
	motorwayDashPer   = 12.0
	motorwayRailOutM  = 0.6 // the barrier, outside the carriageway's edge
	motorwayRailMinHW = 4.0 // a slip road narrower than this has no lanes marked
	// motorwayClearM: nothing is planted this close to a motorway's edge,
	// the central reservation included (the clear zone a motorway keeps
	// beside its barriers); motorwayReachM: a carriageway's half width at
	// most, for the search.
	motorwayClearM = 10.0
	motorwayReachM = 12.0
)

// materialNamed is the world's material of m's name, added if it has none.
func (w *World) materialNamed(m gltf.Material) int {
	for i := 0; ; i++ {
		name := w.doc.MaterialName(i)
		if name == "" {
			break
		}
		if name == m.Name {
			return i
		}
	}
	return w.doc.AddMaterial(m)
}

// addMotorways builds the motorways' markings and barriers, a mesh of each
// per terrain chunk.
func (w *World) addMotorways(t *terrain, lines []*roadLine) error {
	var ms []*roadLine
	for _, l := range lines {
		if l.motorway && len(l.samples) >= 2 {
			ms = append(ms, l)
		}
	}
	if len(ms) == 0 {
		return nil
	}
	markMat := w.materialNamed(gltf.Material{Name: "road marking", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.6})
	railMat := w.materialNamed(gltf.Material{Name: "roadside", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.75})
	marks, rails := map[[2]int]*gltf.Primitive{}, map[[2]int]*gltf.Primitive{}
	prim := func(m map[[2]int]*gltf.Primitive, e, n float64, material int) *gltf.Primitive {
		k := [2]int{int(math.Floor(e / t.o.ChunkM)), int(math.Floor(n / t.o.ChunkM))}
		if m[k] == nil {
			m[k] = &gltf.Primitive{Material: material}
		}
		return m[k]
	}
	white := srgbLinear(0xe9e8e2)
	steel, post := srgbLinear(0xb9bfc4), srgbLinear(0x8d9398)
	for _, l := range ms {
		ss := l.samples
		n := len(ss)
		dist := make([]float64, n)
		right := make([][2]float64, n) // to the right of traffic
		for k := range ss {
			if k > 0 {
				dist[k] = dist[k-1] + math.Hypot(ss[k].e-ss[k-1].e, ss[k].n-ss[k-1].n)
			}
			a, b := ss[max(0, k-1)], ss[min(n-1, k+1)]
			de, dn := b.e-a.e, b.n-a.n
			if ll := math.Hypot(de, dn); ll > 0 {
				de, dn = de/ll, dn/ll
			}
			if ss[k].oneway < 0 {
				de, dn = -de, -dn // traffic against the line
			}
			right[k] = [2]float64{dn, -de}
		}
		pt := func(k int, f, off, up float64) [3]float64 {
			a, b := ss[k], ss[min(n-1, k+1)]
			re := right[k][0]*(1-f) + right[min(n-1, k+1)][0]*f
			rn := right[k][1]*(1-f) + right[min(n-1, k+1)][1]*f
			e, nn := a.e+f*(b.e-a.e)+re*off, a.n+f*(b.n-a.n)+rn*off
			return [3]float64{e, a.ele + f*(b.ele-a.ele) + up, -nn}
		}
		open := func(k int) bool { return !ss[k].onPatch && !ss[k].again && !ss[k].tunnel }
		// The lines, from the carriageway's left edge (traffic's left):
		// solid, dashed between each two lanes, solid before the shoulder.
		line := func(off float64, on, per float64) {
			for k := 0; k+1 < n; k++ {
				if !open(k) || !open(k+1) || ss[k].hw < motorwayRailMinHW {
					continue
				}
				s0, s1 := dist[k], dist[k+1]
				if s1 <= s0 {
					continue
				}
				for c := math.Floor(s0/per) * per; c < s1; c += per {
					lo, hi := math.Max(s0, c), math.Min(s1, c+on)
					if hi <= lo+0.01 {
						continue
					}
					f0, f1 := (lo-s0)/(s1-s0), (hi-s0)/(s1-s0)
					p := prim(marks, ss[k].e, ss[k].n, markMat)
					quad(p, pt(k, f0, off-motorwayLineW/2, markLift), pt(k, f0, off+motorwayLineW/2, markLift),
						pt(k, f1, off+motorwayLineW/2, markLift), pt(k, f1, off-motorwayLineW/2, markLift), [3]float64{0, 1, 0}, white)
				}
			}
		}
		hw := ss[n/2].hw
		left := -hw + motorwayEdgeInM
		lanes := max(1, int((2*hw-motorwayEdgeInM-motorwayLaneM)/motorwayLaneM)) // a hard shoulder a lane wide
		line(left, 1, 1)
		for q := 1; q < lanes; q++ {
			line(left+float64(q)*motorwayLaneM, motorwayDashOn, motorwayDashPer)
		}
		line(left+float64(lanes)*motorwayLaneM, 1, 1)
		// The barriers, on posts every barrierPostM, along both edges
		// (on a deck its own railing stands there; in a tunnel its walls).
		for _, side := range []float64{-1, 1} {
			last := math.Inf(-1)
			for k := 0; k+1 < n; k++ {
				if !open(k) || !open(k+1) || ss[k].aloft || ss[k+1].aloft {
					continue
				}
				off := side * (ss[k].hw + motorwayRailOutM)
				p := prim(rails, ss[k].e, ss[k].n, railMat)
				if dist[k]-last >= barrierPostM {
					beam(p, pt(k, 0, off, -0.3), pt(k, 0, off, 0.72), 0.1, 0.12, post)
					last = dist[k]
				}
				offB := side * (ss[k+1].hw + motorwayRailOutM)
				a, b := pt(k, 0, off, 0.6), pt(k+1, 0, offB, 0.6)
				beam(p, a, b, 0.06, 0.31, steel)
			}
		}
	}
	for _, k := range chunkOrder(marks) {
		if err := w.add(fmt.Sprintf("motorway markings %d %d", k[0], k[1]), map[string]any{"kind": "markings", "chunk": k}, *marks[k]); err != nil {
			return err
		}
	}
	for _, k := range chunkOrder(rails) {
		if err := w.add(fmt.Sprintf("motorway barriers %d %d", k[0], k[1]), map[string]any{"kind": "roadside", "chunk": k}, *rails[k]); err != nil {
			return err
		}
	}
	return nil
}
