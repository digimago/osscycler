package world

import (
	"fmt"
	"math"

	"github.com/digimago/osscycler/internal/gltf"
)

// Tunnels (owner, 2026-10-22: the figure 8 goes under its motorway's
// embankment through one): a road the map has in a tunnel (tunnel=yes;
// its heights straight from end to end on the raw ground, heightsStraight)
// runs under the land, which closes over it. Its surface is no part of
// the land's outline (roadPrims keeps it apart, as a deck's), and over it
// and its walls the land lies at least at the roof's top (tunnelFloors),
// whatever the roads beside it ease the land to. A tube carries it (a
// wall each side and a ceiling slab), and each open end is a portal: a
// headwall across over the opening, up past the land over the tube to a
// parapet, reaching past the walls into the banks either side, and a wing
// wall along each side of the road before it, holding the bank back
// where it falls away from the portal (eased to the open road, the land
// there had ramped up to the roof in a cell, a grass wedge in the
// opening).
const (
	// The tube's walls, its roof and the headwall are tunnelWallT of
	// concrete (owner, 2026-10-22: 30-50 cm), the walls' faces flush with
	// the wing walls' (wingInsetM in from the road's edge), so the opening
	// is the tube's own.
	tunnelRoofM = tunnelWallT // the ceiling slab, over the headroom
	// A tunnel's headroom is what its cover allows (the ground over it at
	// its highest, or a road over it, less the roof), up to layerClearM, at
	// least tunnelMinClearM: under a railway embankment 4 m over the road,
	// a full layerClearM stood the roof over the embankment. Lower cover
	// towards the portals is made up by the land over the roof.
	tunnelMinClearM = 3.0
	tunnelWallT     = 0.4
	// tubeShellM: the walls' outer faces and the roof reach this far out
	// from the inner faces (hidden in the land), past the road's edge: a
	// wall 40 cm thick flush with the wing walls stopped 5 cm inside it,
	// with land over the road's last strip.
	tubeShellM = wingInsetM + 0.1
	// tunnelPastM: the land holds the roof's height this far either side
	// past the walls, and portalPastM past a portal: its edge lies gTuckM
	// in over the open road's end, within the headwall.
	tunnelPastM   = headwallWingM + 0.3
	portalPastM   = gTuckM + 0.02
	headwallT     = tunnelWallT + 0.05 // a portal's headwall, out from the portal (past portalPastM)
	parapetM      = 0.5                // its top over the land over the tube
	headwallWingM = 2.0                // it reaches this far past the walls into the banks
	tunnelMinM    = 6.0                // a run shorter than this is a culvert under a track: no tube
	// A wing wall's face stands wingInsetM in from the road's edge, over
	// the land's edge tucked under it (gTuckM), wingT thick; the bank
	// behind it falls wingSlope metres along the road per metre down from
	// the roof's top, and it stands wingParapetM over the bank, which is
	// held up as far as wingReachM past it.
	wingInsetM   = gTuckM + 0.05
	wingT        = tunnelWallT
	wingSlope    = 1.5
	wingParapetM = 0.3
	wingReachM   = headwallWingM
	// Past those, the backfill falls away sideways wingSlope metres per
	// metre down to the land round it (owner, 2026-10-22: no trough beside
	// the portal, no headwall standing out of the bank).
	// tunnelHaunchM: the tube's top corners are chamfered this much (a
	// box culvert's haunches; flat, the dark ceiling read as a wedge
	// hanging into the opening, owner, 2026-10-22); lamps every
	// tunnelLampM along its ceiling.
	tunnelHaunchM = 0.35
	tunnelLampM   = 8.0
)

// wingFlare: the wing walls flare out from the road this much (the
// tangent of 10°) per metre from the portal (owner, 2026-10-22: so traffic
// is guided in rather than meeting a wall head-on); the widening strip
// between the road's edge and a wall is paved, an apron (aprons).
var wingFlare = math.Tan(10 * math.Pi / 180)

// wingFace is how far a wing wall's face stands from the line on a side
// whose road edge is edge, from metres from the portal.
func wingFace(edge, from float64) float64 { return edge - wingInsetM + from*wingFlare }

// inTunnel tells whether the segment from sample i to i+1 is in a tunnel.
func inTunnel(ss []sample, i int) bool { return ss[i].tunnel && ss[i+1].tunnel }

// tunnelRun is a tunnel's stretch of a line: samples i to j, and whether
// each end is a portal (the road goes on in the open; not where the line
// is cut off in the tunnel).
type tunnelRun struct {
	l      *roadLine
	i, j   int
	portal [2]bool
	clear  float64 // the headroom
}

// tunnelRuns are the lines' tunnels.
func tunnelRuns(lines []*roadLine) []tunnelRun {
	var out []tunnelRun
	for _, l := range lines {
		ss := l.samples
		for i := 0; i+1 < len(ss); {
			if !inTunnel(ss, i) {
				i++
				continue
			}
			j := i + 1
			for j+1 < len(ss) && inTunnel(ss, j) {
				j++
			}
			if ss[j].d-ss[i].d >= tunnelMinM {
				out = append(out, tunnelRun{l, i, j, [2]bool{i > 0, j < len(ss)-1}, layerClearM})
			}
			i = j
		}
	}
	return out
}

// headroom sets each run's headroom from its cover: cover(e, n) is the
// ground's height there as the ground model or profile has it; a road of
// lines over the run raises it.
func headroom(runs []tunnelRun, lines []*roadLine, cover func(e, n float64) float64) {
	if len(runs) == 0 {
		return
	}
	const cellM = 10.0
	type ref struct{ l, i int }
	grid := map[[2]int][]ref{}
	key := func(e, n float64) [2]int { return [2]int{int(math.Floor(e / cellM)), int(math.Floor(n / cellM))} }
	for li, l := range lines {
		for i, s := range l.samples {
			grid[key(s.e, s.n)] = append(grid[key(s.e, s.n)], ref{li, i})
		}
	}
	for k := range runs {
		r := &runs[k]
		ss := r.l.samples
		most := math.Inf(-1)
		for i := r.i; i <= r.j; i++ {
			s := ss[i]
			c := cover(s.e, s.n)
			g := key(s.e, s.n)
			for x := g[0] - 1; x <= g[0]+1; x++ {
				for y := g[1] - 1; y <= g[1]+1; y++ {
					for _, q := range grid[[2]int{x, y}] {
						o := lines[q.l].samples[q.i]
						if lines[q.l] != r.l && o.ele > s.ele+tunnelMinClearM && math.Hypot(o.e-s.e, o.n-s.n) <= o.edge+s.edge {
							c = math.Max(c, o.ele)
						}
					}
				}
			}
			most = math.Max(most, c-s.ele)
		}
		r.clear = math.Max(tunnelMinClearM, math.Min(layerClearM, most-tunnelRoofM))
	}
}

// roofTop is the height of the land over a tunnel whose road is at ele
// under headroom clear.
func roofTop(ele, clear float64) float64 { return ele + clear + tunnelRoofM }

// wingLen is how far a wing wall reaches from a portal whose road is at
// ele: until the bank behind it is down to the road.
func wingLen(ele, clear float64) float64 { return (roofTop(ele, clear) - ele) * wingSlope }

// approach is a portal's open road: the samples from the portal outward
// along line l as far as its wing walls reach, the portal first.
func approach(r tunnelRun, end int) []sample {
	ss := r.l.samples
	k, step := r.i, -1
	if end == 1 {
		k, step = r.j, 1
	}
	p := ss[k]
	out := []sample{p}
	for k += step; k >= 0 && k < len(ss); k += step {
		out = append(out, ss[k])
		if math.Abs(ss[k].d-p.d) >= wingLen(p.ele, r.clear) {
			break
		}
	}
	return out
}

// tunnelSeg is a piece of where the land lies over a tunnel or behind a
// portal's wing walls: the road from a to b; across it (right of a to b
// positive), from in to out on each side ([0] left, [1] right) at a, inB
// to outB at b (a wing flares); along it, how far it reaches before a and
// past b. Over the tube the land is at least the roof's top there; behind
// a wing, the portal's roof top (top) less the fall from it (fromA, fromB:
// how far a and b are from the portal).
type tunnelSeg struct {
	a, b         sample
	in, out      [2]float64
	inB, outB    [2]float64
	before, past float64
	clear        float64 // the tube's headroom
	wing         bool
	top          float64
	fromA, fromB float64
}

// tunnelFloors is where the land lies over a tunnel, and how high at least.
type tunnelFloors struct {
	segs []tunnelSeg
	grid map[[2]int][]int
}

const tunnelCellM = 10.0

func newTunnelFloors(runs []tunnelRun) *tunnelFloors {
	f := &tunnelFloors{grid: map[[2]int][]int{}}
	add := func(s tunnelSeg) {
		id := len(f.segs)
		f.segs = append(f.segs, s)
		reach := math.Max(math.Max(s.out[0], s.out[1]), math.Max(s.outB[0], s.outB[1])) + portalPastM + (layerClearM+tunnelRoofM+parapetM)*wingSlope
		a, b := s.a, s.b
		k0 := [2]int{int(math.Floor((math.Min(a.e, b.e) - reach) / tunnelCellM)), int(math.Floor((math.Min(a.n, b.n) - reach) / tunnelCellM))}
		k1 := [2]int{int(math.Floor((math.Max(a.e, b.e) + reach) / tunnelCellM)), int(math.Floor((math.Max(a.n, b.n) + reach) / tunnelCellM))}
		for x := k0[0]; x <= k1[0]; x++ {
			for y := k0[1]; y <= k1[1]; y++ {
				f.grid[[2]int{x, y}] = append(f.grid[[2]int{x, y}], id)
			}
		}
	}
	for _, r := range runs {
		ss := r.l.samples
		for k := r.i; k < r.j; k++ {
			fa, fb := frameOf(r.l, ss[k]), frameOf(r.l, ss[k+1])
			half := math.Max(math.Max(fa.left, fa.right), math.Max(fb.left, fb.right)) + tunnelWallT + tunnelPastM
			s := tunnelSeg{a: ss[k], b: ss[k+1], out: [2]float64{half, half}, outB: [2]float64{half, half}, clear: r.clear}
			if k == r.i && r.portal[0] {
				s.before = portalPastM
			}
			if k+1 == r.j && r.portal[1] {
				s.past = portalPastM
			}
			add(s)
		}
		for end := range 2 {
			if !r.portal[end] {
				continue
			}
			ap := approach(r, end)
			p := ap[0]
			for k := 0; k+1 < len(ap); k++ {
				a, b := ap[k], ap[k+1]
				fa, fb := frameOf(r.l, a), frameOf(r.l, b)
				s := tunnelSeg{a: a, b: b, wing: true, top: roofTop(p.ele, r.clear), fromA: math.Abs(a.d - p.d), fromB: math.Abs(b.d - p.d)}
				// Left and right as the line runs (a to b runs outward from
				// the end portal, inward to the start one).
				left, right := math.Min(fa.left, fb.left), math.Min(fa.right, fb.right)
				if end == 0 { // a to b runs against the line
					left, right = right, left
				}
				s.in = [2]float64{wingFace(left, s.fromA), wingFace(right, s.fromA)}
				s.inB = [2]float64{wingFace(left, s.fromB), wingFace(right, s.fromB)}
				for side := range 2 {
					s.out[side], s.outB[side] = s.in[side]+wingT+wingReachM, s.inB[side]+wingT+wingReachM
				}
				add(s)
			}
		}
	}
	return f
}

// at is the least height of the land at e, n over a tunnel or behind a
// wing wall, false where neither is.
func (f *tunnelFloors) at(e, n float64) (float64, bool) {
	if f == nil || len(f.segs) == 0 {
		return 0, false
	}
	floor, ok := math.Inf(-1), false
	for _, id := range f.grid[[2]int{int(math.Floor(e / tunnelCellM)), int(math.Floor(n / tunnelCellM))}] {
		s := f.segs[id]
		de, dn := s.b.e-s.a.e, s.b.n-s.a.n
		l := math.Hypot(de, dn)
		if l < 1e-9 {
			continue
		}
		along := ((e-s.a.e)*de + (n-s.a.n)*dn) / l
		right := ((e-s.a.e)*dn - (n-s.a.n)*de) / l
		side, across := 1, right
		if right < 0 {
			side, across = 0, -right
		}
		t := math.Max(0, math.Min(1, along/l))
		in, out := s.in[side]+t*(s.inB[side]-s.in[side]), s.out[side]+t*(s.outB[side]-s.out[side])
		if along < -s.before || along > l+s.past || across < in {
			continue
		}
		ele := s.a.ele + t*(s.b.ele-s.a.ele)
		h := roofTop(ele, s.clear)
		if s.wing {
			h = s.top - (s.fromA+t*(s.fromB-s.fromA))/wingSlope
		}
		if across > out {
			h -= (across - out) / wingSlope // the backfill's side slope
		}
		if h <= ele {
			continue // down to the road's level
		}
		if h > floor {
			floor, ok = h, true
		}
	}
	return floor, ok
}

// tunnelConcrete is the tunnels' concrete.
var tunnelConcrete = srgbLinear(0x9c988f)

// aprons are the paved strips before the portals, between the road's edge
// and the flaring wing walls (under the walls' faces by wingInsetM, so the
// land's edge, tucked under them, lies behind the walls), by terrain chunk:
// a surface the land goes round, as a road.
func aprons(runs []tunnelRun, chunkM float64, material int) map[[2]int][]gltf.Primitive {
	out := map[[2]int][]gltf.Primitive{}
	for _, r := range runs {
		for end := range 2 {
			if !r.portal[end] {
				continue
			}
			ap := approach(r, end)
			var p gltf.Primitive
			p.Material = material
			for k := 0; k+1 < len(ap); k++ {
				a, b := ap[k], ap[k+1]
				fa, fb := frameOf(r.l, a), frameOf(r.l, b)
				da, db := math.Abs(a.d-ap[0].d), math.Abs(b.d-ap[0].d)
				for side, sign := range [2]float64{-1, 1} {
					ea, eb := []float64{fa.left, fa.right}[side], []float64{fb.left, fb.right}[side]
					oa, ob := wingFace(ea, da)+wingInsetM, wingFace(eb, db)+wingInsetM
					if ob <= eb {
						continue // still within the road
					}
					up := [3]float64{0, 1, 0}
					quad(&p, frameAt(fa, sign*(ea-0.1), -0.005), frameAt(fb, sign*(eb-0.1), -0.005), frameAt(fb, sign*ob, -0.005), frameAt(fa, sign*math.Max(oa, ea), -0.005), up, tunnelConcrete)
				}
			}
			if len(p.Indices) == 0 {
				continue
			}
			k := [2]int{int(math.Floor(ap[0].e / chunkM)), int(math.Floor(ap[0].n / chunkM))}
			out[k] = append(out[k], p)
		}
	}
	return out
}

// frameAt is the point across to the right of f's centre (negative:
// left), up above its road.
func frameAt(f deckFrame, across, up float64) [3]float64 {
	return [3]float64{f.c[0] + f.r[0]*across, f.c[1] + up, f.c[2] + f.r[2]*across}
}

// addTunnels builds the tunnels' tubes and portals, a mesh per terrain
// chunk, in material mat; landAt is the land's height as laid.
func (w *World) addTunnels(t *terrain, runs []tunnelRun, mat int, landAt func(e, n float64) float64) error {
	if len(runs) == 0 {
		return nil
	}
	concrete, ceiling := tunnelConcrete, srgbLinear(0xdedbd3)
	lampMat := w.doc.AddMaterial(gltf.Material{Name: "tunnel lamp", Color: [4]float32{1, 1, 1, 1}, Roughness: 0.4, Emissive: [3]float32{1, 0.92, 0.75}})
	lampsBy := map[[2]int]*gltf.Primitive{}
	byChunk := map[[2]int]*gltf.Primitive{}
	for _, r := range runs {
		ss := r.l.samples
		mid := ss[(r.i+r.j)/2]
		k := [2]int{int(math.Floor(mid.e / t.o.ChunkM)), int(math.Floor(mid.n / t.o.ChunkM))}
		p := byChunk[k]
		if p == nil {
			p = &gltf.Primitive{Material: mat}
			byChunk[k] = p
			lampsBy[k] = &gltf.Primitive{Material: lampMat}
		}
		tube(p, lampsBy[k], r, landAt, concrete, ceiling)
	}
	for _, k := range chunkOrder(byChunk) {
		ps := []gltf.Primitive{*byChunk[k]}
		if len(lampsBy[k].Indices) > 0 {
			ps = append(ps, *lampsBy[k])
		}
		if err := w.add(fmt.Sprintf("tunnels %d %d", k[0], k[1]), map[string]any{"kind": "tunnels", "chunk": k}, ps...); err != nil {
			return err
		}
	}
	return nil
}

// tube builds run r's walls and ceiling, and its portals: the headwalls
// and the wing walls.
func tube(p, lamps *gltf.Primitive, r tunnelRun, landAt func(e, n float64) float64, concrete, ceiling [3]float32) {
	ss := r.l.samples
	// The tube's inner faces, flush with the wing walls'.
	frames := make([]deckFrame, 0, r.j-r.i+1)
	for k := r.i; k <= r.j; k++ {
		f := frameOf(r.l, ss[k])
		f.left, f.right = f.left-wingInsetM, f.right-wingInsetM
		frames = append(frames, f)
	}
	at := func(f deckFrame, across, up float64) [3]float64 {
		return [3]float64{f.c[0] + f.r[0]*across, f.c[1] + up, f.c[2] + f.r[2]*across}
	}
	clear := r.clear
	top := clear + tunnelRoofM
	for k := 0; k+1 < len(frames); k++ {
		a, b := frames[k], frames[k+1]
		// The walls: the inner face from under the road's edge up to the
		// ceiling, the outer down into the ground (hidden in the land).
		for _, side := range []struct {
			in, out [2]float64 // a's and b's
			sign    float64
		}{
			{[2]float64{-a.left, -b.left}, [2]float64{-a.left - tubeShellM, -b.left - tubeShellM}, -1},
			{[2]float64{a.right, b.right}, [2]float64{a.right + tubeShellM, b.right + tubeShellM}, 1},
		} {
			inN, outN := scale3(a.r, -side.sign), scale3(a.r, side.sign)
			h := tunnelHaunchM
			quad(p, at(a, side.in[0], -footM), at(b, side.in[1], -footM), at(b, side.in[1], clear-h), at(a, side.in[0], clear-h), inN, concrete)
			// The haunch, from the wall's top in to the ceiling.
			hn := [3]float64{inN[0] * math.Sqrt2 / 2, -math.Sqrt2 / 2, inN[2] * math.Sqrt2 / 2}
			quad(p, at(a, side.in[0], clear-h), at(b, side.in[1], clear-h), at(b, side.in[1]-side.sign*h, clear), at(a, side.in[0]-side.sign*h, clear), hn, concrete)
			quad(p, at(a, side.out[0], -footM), at(b, side.out[1], -footM), at(b, side.out[1], top), at(a, side.out[0], top), outN, concrete)
		}
		// The ceiling: its underside over the road between the haunches,
		// its top over the walls.
		h := tunnelHaunchM
		quad(p, at(a, -a.left+h, clear), at(a, a.right-h, clear), at(b, b.right-h, clear), at(b, -b.left+h, clear), [3]float64{0, -1, 0}, ceiling)
		quad(p, at(a, -a.left-tubeShellM, top), at(a, a.right+tubeShellM, top), at(b, b.right+tubeShellM, top), at(b, -b.left-tubeShellM, top), [3]float64{0, 1, 0}, concrete)
	}
	// The lamps: a fitting on the ceiling's middle every tunnelLampM,
	// from half that in.
	run := 0.0
	next := tunnelLampM / 2
	for k := 1; k < len(frames); k++ {
		run += ss[r.i+k].d - ss[r.i+k-1].d
		if run < next || k == len(frames)-1 {
			continue
		}
		next += tunnelLampM
		f := frames[k]
		box(lamps, f, -0.15, 0.15, -0.6, 0.6, f.c[1]+clear-0.1, clear-0.005, [3]float32{1, 1, 1})
	}
	for e, end := range []struct {
		f   deckFrame
		out float64 // along the road, away from the tube
	}{{frames[0], -1}, {frames[len(frames)-1], 1}} {
		if !r.portal[e] {
			continue
		}
		f := end.f
		// The headwall: over the opening, a lintel from the ceiling up past
		// the land to the parapet, at least wingParapetM over the land just
		// behind it (owner, 2026-10-22: the bank eased up towards the road
		// above poked through its top); either side, a jamb from the ground
		// up as high, as wide as the tube's wall, where the wing wall goes
		// on (reaching past it into the bank, its ends stood out where the
		// bank falls away).
		up := top + parapetM
		for x := -f.left - tubeShellM; x <= f.right+tubeShellM+1e-9; x += 0.5 {
			q := at(f, x, 0)
			b := [3]float64{q[0] - f.along[0]*end.out*0.6, 0, q[2] - f.along[2]*end.out*0.6}
			up = math.Max(up, landAt(b[0], -b[2])-f.c[1]+wingParapetM)
		}
		l0, r0 := f.left, f.right
		box(p, f, -l0, r0, 0, end.out*headwallT, f.c[1]+clear, up, concrete)
		for _, x := range [2][2]float64{{-f.left - tubeShellM, -l0}, {r0, f.right + tubeShellM}} {
			box(p, f, x[0], x[1], 0, end.out*headwallT, f.c[1]-footM, up, concrete)
		}
		wings(p, r, e, landAt, concrete)
	}
}

// wings builds the wing walls before portal end of run r: along each side
// of the open road from the headwall out, their face just inside the
// road's edge, their top over the bank behind them as laid (held up to
// the wing's fall from the roof's top, tunnelFloors, or higher where the
// land is).
func wings(p *gltf.Primitive, r tunnelRun, end int, landAt func(e, n float64) float64, concrete [3]float32) {
	ap := approach(r, end)
	if len(ap) < 2 {
		return
	}
	portal := ap[0]
	type cut struct {
		f      deckFrame
		in     [2]float64 // the faces' offsets, left and right of the line
		top    [2]float64 // the walls' tops (absolute)
		inWall bool
	}
	var cs []cut
	for k, a := range ap {
		f := frameOf(r.l, a)
		from := math.Abs(a.d - portal.d)
		if from < headwallT && k > 0 {
			continue // within the headwall
		}
		at := from
		if k == 0 {
			at = headwallT // the wall begins at the headwall's face
		}
		c := cut{f: f, in: [2]float64{-wingFace(f.left, at), wingFace(f.right, at)}}
		fall := roofTop(portal.ele, r.clear) - from/wingSlope
		for side, sign := range [2]float64{-1, 1} {
			o := c.in[side] + sign*(wingT+0.3)
			q := [3]float64{f.c[0] + f.r[0]*o, 0, f.c[2] + f.r[2]*o}
			c.top[side] = math.Max(fall, landAt(q[0], -q[2])) + wingParapetM
		}
		c.inWall = fall > a.ele || c.top[0] > a.ele+wingParapetM+0.2 || c.top[1] > a.ele+wingParapetM+0.2
		if k == 0 {
			// From the headwall's outer face.
			c.f.c = [3]float64{f.c[0] + f.along[0]*float64(2*end-1)*headwallT, f.c[1], f.c[2] + f.along[2]*float64(2*end-1)*headwallT}
		}
		cs = append(cs, c)
		if !c.inWall {
			break
		}
	}
	at := func(f deckFrame, across, h float64) [3]float64 {
		return [3]float64{f.c[0] + f.r[0]*across, h, f.c[2] + f.r[2]*across}
	}
	for k := 0; k+1 < len(cs); k++ {
		a, b := cs[k], cs[k+1]
		for side, sign := range [2]float64{-1, 1} {
			ia, ib := a.in[side], b.in[side]
			oa, ob := ia+sign*wingT, ib+sign*wingT
			low := math.Min(a.f.c[1], b.f.c[1]) - footM
			toRoad := scale3(a.f.r, -sign)
			quad(p, at(a.f, ia, low), at(b.f, ib, low), at(b.f, ib, b.top[side]), at(a.f, ia, a.top[side]), toRoad, concrete)
			quad(p, at(a.f, ia, a.top[side]), at(b.f, ib, b.top[side]), at(b.f, ob, b.top[side]), at(a.f, oa, a.top[side]), [3]float64{0, 1, 0}, concrete)
			quad(p, at(a.f, oa, low), at(b.f, ob, low), at(b.f, ob, b.top[side]), at(a.f, oa, a.top[side]), scale3(toRoad, -1), concrete)
			// Its backing, hidden just under the bank, out past the road's
			// edge (as the tube's, tubeShellM): the bank isn't over the
			// road's last strip with nothing between.
			ba, bb := ia+sign*tubeShellM, ib+sign*tubeShellM
			ya, yb := a.top[side]-wingParapetM-0.05, b.top[side]-wingParapetM-0.05
			quad(p, at(a.f, oa, ya), at(b.f, ob, yb), at(b.f, bb, yb), at(a.f, ba, ya), [3]float64{0, 1, 0}, concrete)
		}
	}
	// The far end's face, across each wall.
	last := cs[len(cs)-1]
	away := scale3(last.f.along, float64(2*end-1))
	for side, sign := range [2]float64{-1, 1} {
		i, o := last.in[side], last.in[side]+sign*wingT
		low := last.f.c[1] - footM
		quad(p, at(last.f, i, low), at(last.f, o, low), at(last.f, o, last.top[side]), at(last.f, i, last.top[side]), away, concrete)
	}
}
