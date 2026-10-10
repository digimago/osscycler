package world

import (
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// The riding line is settled on the road as drawn, last of all (owner,
// 2026-10-10: fix the riding line off road in the builder, for every
// course to come, not place by place). Every stage before it reasons about
// roads by their lines and widths (keepOnRoad, ridingLine, the roundabouts'
// ways through, limitDrift, evenPace), and some move the path again after
// keepOnRoad has looked: the Posbank Loop still had the rider on grass in
// 12 places (79 m), the Grebbeberg course in many more. settleOnRoad looks
// at what was drawn, the road surfaces' triangles, as World.Check does:
// where the riding line has no road under it (every metre), the lane narrows to what is on the road, and where even
// the path is off, it moves sideways onto the nearest road surface within
// settleM;
// the changes ease in over the points either side, and it looks again.
const (
	settleM      = 10.0 // the path moves at most this far to reach a road
	settleCellM  = 4.0  // the road triangles' index
	settleInsetM = 0.4  // a moved point lands this far inside the road
	settleRounds = 6
	settleSpan   = 3 // points either side a move eases over
)

// settleAt: where between two path points (pathStep apart) the riding
// line is looked at, every metre as World.Check does.
var settleAt = []float64{0, 0.2, 0.4, 0.6, 0.8}

// roadIndex is the road surfaces' triangles seen from above, in the
// builder's east and north.
type roadIndex struct {
	cells map[[2]int][][6]float64 // e0 n0 e1 n1 e2 n2
}

func newRoadIndex(prims []gltf.Primitive, isRoad func(mat int) bool) *roadIndex {
	ix := &roadIndex{cells: map[[2]int][][6]float64{}}
	for _, p := range prims {
		if !isRoad(p.Material) {
			continue
		}
		for ti := 0; ti+2 < len(p.Indices); ti += 3 {
			var t [6]float64
			for k, v := range p.Indices[ti : ti+3] {
				t[2*k], t[2*k+1] = float64(p.Positions[3*v]), -float64(p.Positions[3*v+2])
			}
			// Seen edge on (walls, kerb faces, skirts) it covers nothing.
			if math.Abs((t[2]-t[0])*(t[5]-t[1])-(t[4]-t[0])*(t[3]-t[1])) < 1e-6 {
				continue
			}
			e0, e1 := math.Min(t[0], math.Min(t[2], t[4])), math.Max(t[0], math.Max(t[2], t[4]))
			n0, n1 := math.Min(t[1], math.Min(t[3], t[5])), math.Max(t[1], math.Max(t[3], t[5]))
			for ce := int(math.Floor(e0 / settleCellM)); ce <= int(math.Floor(e1/settleCellM)); ce++ {
				for cn := int(math.Floor(n0 / settleCellM)); cn <= int(math.Floor(n1/settleCellM)); cn++ {
					k := [2]int{ce, cn}
					ix.cells[k] = append(ix.cells[k], t)
				}
			}
		}
	}
	return ix
}

func (ix *roadIndex) key(e, n float64) [2]int {
	return [2]int{int(math.Floor(e / settleCellM)), int(math.Floor(n / settleCellM))}
}

// on tells whether e, n lies on a road surface.
func (ix *roadIndex) on(e, n float64) bool {
	for _, t := range ix.cells[ix.key(e, n)] {
		if inTri2(t, e, n) {
			return true
		}
	}
	return false
}

// nearest is the nearest point on a road surface within settleM of e, n,
// moved settleInsetM into that triangle (towards its middle, as far as it
// reaches), and whether there is one.
func (ix *roadIndex) nearest(e, n float64) (float64, float64, bool) {
	best, be, bn := math.Inf(1), 0.0, 0.0
	var bt [6]float64
	k := ix.key(e, n)
	r := int(math.Ceil(settleM / settleCellM))
	for ce := k[0] - r; ce <= k[0]+r; ce++ {
		for cn := k[1] - r; cn <= k[1]+r; cn++ {
			for _, t := range ix.cells[[2]int{ce, cn}] {
				qe, qn := closestOnTri2(t, e, n)
				if d := math.Hypot(qe-e, qn-n); d < best {
					best, be, bn, bt = d, qe, qn, t
				}
			}
		}
	}
	if best > settleM {
		return 0, 0, false
	}
	ce, cn := (bt[0]+bt[2]+bt[4])/3, (bt[1]+bt[3]+bt[5])/3
	if l := math.Hypot(ce-be, cn-bn); l > 1e-9 {
		f := math.Min(settleInsetM, l*0.9) / l
		be, bn = be+(ce-be)*f, bn+(cn-bn)*f
	}
	return be, bn, true
}

// inTri2 tells whether e, n lies in triangle t, edges included.
func inTri2(t [6]float64, e, n float64) bool {
	d := func(ax, ay, bx, by float64) float64 { return (bx-ax)*(n-ay) - (by-ay)*(e-ax) }
	d0, d1, d2 := d(t[0], t[1], t[2], t[3]), d(t[2], t[3], t[4], t[5]), d(t[4], t[5], t[0], t[1])
	neg := d0 < 0 || d1 < 0 || d2 < 0
	pos := d0 > 0 || d1 > 0 || d2 > 0
	return !(neg && pos)
}

// closestOnTri2 is the point of triangle t nearest e, n.
func closestOnTri2(t [6]float64, e, n float64) (float64, float64) {
	if inTri2(t, e, n) {
		return e, n
	}
	best, be, bn := math.Inf(1), 0.0, 0.0
	for k := range 3 {
		ax, ay, bx, by := t[2*k], t[2*k+1], t[(2*k+2)%6], t[(2*k+3)%6]
		dx, dy := bx-ax, by-ay
		f := 0.0
		if l2 := dx*dx + dy*dy; l2 > 0 {
			f = math.Max(0, math.Min(1, ((e-ax)*dx+(n-ay)*dy)/l2))
		}
		qx, qy := ax+f*dx, ay+f*dy
		if d := math.Hypot(e-qx, n-qy); d < best {
			best, be, bn = d, qx, qy
		}
	}
	return be, bn
}

// settleOnRoad keeps the riding line (the path x, y and lane, metres to
// its right) on the road surfaces in ix; see above.
func settleOnRoad(x, y, lane []float64, loop bool, ix *roadIndex) {
	n := len(x)
	if n < 2 {
		return
	}
	right := func(i int) (float64, float64) {
		a, b := max(0, i-1), min(n-1, i+1)
		de, dn := x[b]-x[a], y[b]-y[a]
		l := math.Hypot(de, dn)
		if l == 0 {
			return 0, 0
		}
		return dn / l, -de / l
	}
	// at is the riding line and the path at i + f (0 ≤ f < 1).
	at := func(i int, f float64) (re, rn, pe, pn float64) {
		j := min(n-1, i+1)
		pe, pn = x[i]+f*(x[j]-x[i]), y[i]+f*(y[j]-y[i])
		ra, rb := right(i)
		off := lane[i] + f*(lane[j]-lane[i])
		return pe + ra*off, pn + rb*off, pe, pn
	}
	// offIn counts the riding line's samples off the road from point a to b.
	offIn := func(a, b int) int {
		c := 0
		for i := max(0, a); i <= min(n-1, b); i++ {
			for _, f := range settleAt {
				if f > 0 && i == n-1 {
					continue
				}
				if re, rn, _, _ := at(i, f); !ix.on(re, rn) {
					c++
				}
			}
		}
		return c
	}
	idx := func(k int) (int, bool) {
		if loop {
			return (k%n + n) % n, true
		}
		return k, k >= 0 && k < n
	}
	for range settleRounds {
		want := append([]float64(nil), lane...)
		move := make([][2]float64, n)
		moved := make([]bool, n)
		bad := 0
		for i := range n {
			for _, f := range settleAt {
				if f > 0 && i == n-1 {
					continue
				}
				re, rn, pe, pn := at(i, f)
				if ix.on(re, rn) {
					continue
				}
				bad++
				js := []int{i}
				if f > 0 {
					js = append(js, i+1)
				}
				// Narrower first: half the lane, then none.
				ra, rb := right(i)
				off := lane[i] + f*(lane[min(n-1, i+1)]-lane[i])
				switch {
				case ix.on(pe+ra*off/2, pn+rb*off/2):
					for _, j := range js {
						want[j] = math.Min(math.Abs(want[j]), math.Abs(off/2)) * sign(lane[j])
					}
				case ix.on(pe, pn):
					for _, j := range js {
						want[j] = 0
					}
				default:
					// Sideways only: a move along the path would bunch its
					// points at the end of a road before a gap it can't mend.
					if qe, qn, ok := ix.nearest(pe, pn); ok {
						s := (qe-pe)*ra + (qn-pn)*rb
						s += settleInsetM * sign(s)
						if math.Abs(s) > settleM || !ix.on(pe+ra*s, pn+rb*s) {
							continue
						}
						for _, j := range js {
							want[j] = 0
							if !moved[j] || math.Abs(s) > math.Hypot(move[j][0], move[j][1]) {
								move[j], moved[j] = [2]float64{ra * s, rb * s}, true
							}
						}
					}
				}
			}
		}
		if bad == 0 {
			return
		}
		// Eased in: a moved point's neighbours follow part of the way where
		// that keeps them on a road (else easing pulled them off: the
		// Posbank Loop at 30.7 km, two roads drawn touching at a point), and
		// their lanes narrow as far as the point's.
		dx, dy := make([]float64, n), make([]float64, n)
		for i := range n {
			if !moved[i] {
				continue
			}
			for k := -settleSpan; k <= settleSpan; k++ {
				j, ok := idx(i + k)
				if !ok {
					continue
				}
				w := 1 - math.Abs(float64(k))/float64(settleSpan+1)
				mx, my := move[i][0]*w, move[i][1]*w
				if k != 0 && !ix.on(x[j]+mx, y[j]+my) {
					continue
				}
				if math.Hypot(mx, my) > math.Hypot(dx[j], dy[j]) {
					dx[j], dy[j] = mx, my
				}
			}
		}
		nl := append([]float64(nil), lane...)
		for i := range n {
			for k := -1; k <= 1; k++ {
				if j, ok := idx(i + k); ok && math.Abs(want[j]) < math.Abs(nl[i]) {
					nl[i] = want[j]
				}
			}
		}
		// Each run of changed points is kept only where it leaves less of
		// the riding line off the road there (a move can make it worse
		// beside a gap between roads drawn).
		changed := func(i int) bool { return dx[i] != 0 || dy[i] != 0 || nl[i] != lane[i] }
		better := false
		for a := 0; a < n; {
			if !changed(a) {
				a++
				continue
			}
			b := a
			for b+1 < n && (changed(b+1) || (b+2 < n && changed(b+2))) {
				b++
			}
			lo, hi := a-2, b+2
			before := offIn(lo, hi)
			ox, oy, ol := append([]float64(nil), x[a:b+1]...), append([]float64(nil), y[a:b+1]...), append([]float64(nil), lane[a:b+1]...)
			for i := a; i <= b; i++ {
				x[i] += dx[i]
				y[i] += dy[i]
				lane[i] = nl[i]
			}
			if offIn(lo, hi) < before {
				better = true
			} else {
				copy(x[a:b+1], ox)
				copy(y[a:b+1], oy)
				copy(lane[a:b+1], ol)
			}
			a = b + 1
		}
		if !better {
			return
		}
	}
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
