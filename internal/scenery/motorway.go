package scenery

import "math"

// The figure 8's motorway (owner, 2026-10-22: the demo track should show
// the route crossing a motorway both ways): a dual carriageway runs
// straight north to south through the forest lobe, 300 m west of the
// figure's middle, where there is room for it to cross both arcs. Over the
// low southern arc it rides an embankment and the course goes under it
// through a tunnel; under the high northern arc it lies in a cutting and
// the course crosses it on a bridge. Both crossings are mapped stretches
// of road the course's own way joins and leaves, as the Rivierdijk over
// the river (village.go): the map says which lies over which (layer,
// bridge, tunnel), and the world builder lifts and lowers to match where
// no ground model does.

const (
	motorwayX      = -300.0 // the motorway's axis, east of the figure's middle
	motorwayApartM = 16.0   // between the two carriageways' centre lines
	motorwayBankM  = 60.0   // the embankment reaches this far either side of the tunnel
	motorwayCutM   = 80.0   // and the cutting either side of the bridge
	crossHalfM     = 20.0   // the bridge reaches this far either side of the axis, along the course
	// tunnelHalfM: the tunnel this far, so its portals lie clear of the
	// carriageways' edges and barriers (16 m along the course at its 60°
	// to the motorway) and their verges; at 20 m the barrier stood over
	// the portal (owner, 2026-10-22).
	tunnelHalfM  = 27.0
	crossMappedM = 60.0 // the mapped road reaches this far either side
)

// motorway lays the motorway and the two mapped crossings. s and n are the
// land's southern and northern ends; mid the figure's middle (east).
func (g *trackGen) motorway(mid, s, n float64) {
	snap := func(d float64) float64 { return math.Round(d/5) * 5 }
	x := mid + motorwayX
	// Where the course crosses the axis: the southern arc on its way west,
	// the northern on its way back.
	south := g.riverAt(x, 1250, 2500)
	north := g.riverAt(x, 2500, 3750)
	ys, yn := g.at(south)[1], g.at(north)[1]

	// Each carriageway in pieces: on the embankment over the tunnel, in the
	// cutting under the bridge, at grade between and beyond. Traffic keeps
	// right: northbound on the east carriageway.
	cuts := []float64{s, ys - motorwayBankM, ys + motorwayBankM, yn - motorwayCutM, yn + motorwayCutM, n}
	kind := []string{"", "embankment", "", "cutting", ""}
	for _, side := range []float64{1, -1} {
		cx := x + side*motorwayApartM/2
		for k := 0; k+1 < len(cuts); k++ {
			var pts [][2]float64
			for y := cuts[k]; y < cuts[k+1]; y += 10 {
				pts = append(pts, [2]float64{cx, y})
			}
			pts = append(pts, [2]float64{cx, cuts[k+1]})
			if side < 0 { // southbound: drawn north to south
				for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
					pts[i], pts[j] = pts[j], pts[i]
				}
			}
			kv := []string{"highway", "motorway", "oneway", "yes", "lanes", "2"}
			if kind[k] != "" {
				kv = append(kv, kind[k], "yes")
			}
			g.way(pts, kv...)
		}
	}

	// The course's crossings: through the tunnel under the embankment, over
	// the bridge above the cutting.
	for _, c := range []struct {
		d                 float64
		name, kind, layer string
	}{
		{south, "Bosweg", "tunnel", "-1"},
		{north, "Heideweg", "bridge", "1"},
	} {
		road := []string{"highway", "unclassified", "name", c.name}
		half := crossHalfM
		if c.kind == "tunnel" {
			half = tunnelHalfM
		}
		from, a, b, to := snap(c.d-crossMappedM), snap(c.d-half), snap(c.d+half), snap(c.d+crossMappedM)
		g.way(g.along(from, a), road...)
		g.way(g.along(a, b), append(road, c.kind, "yes", "layer", c.layer)...)
		g.way(g.along(b, to), road...)
	}
}
