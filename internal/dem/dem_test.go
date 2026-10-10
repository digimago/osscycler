package dem

import (
	"math"
	"os"
	"testing"
)

func TestToRD(t *testing.T) {
	// Pairs from PDOK's locatieserver, which transforms with RDNAPTRANS.
	for _, c := range []struct{ lat, lon, x, y float64 }{
		{52.15517440, 5.38720621, 155000, 463000}, // Amersfoort, the origin
		{52.02700666, 6.01568304, 198132.605, 448927.141},
		{52.05142357, 6.08533752, 202886.772, 451687.314},
		{52.02186267, 6.06002376, 201180.955, 448382.121},
		{52.09072066, 5.12160170, 136797.557, 455862.387},
		{52.43023775, 5.83437274, 185412.659, 493698.618},
	} {
		x, y := ToRD(c.lat, c.lon)
		if math.Hypot(x-c.x, y-c.y) > 1.5 {
			t.Errorf("ToRD(%v, %v) = %.1f, %.1f, want %.1f, %.1f", c.lat, c.lon, x, y, c.x, c.y)
		}
	}
}

func TestDecodeAHN(t *testing.T) {
	// A real AHN4 DTM tile from PDOK's WCS, 5 m cells (deflate, predictor 3):
	// the foot of the Diepesteeg in De Steeg.
	b, err := os.ReadFile("testdata/ahn-diepesteeg-5m.tif")
	if err != nil {
		t.Fatal(err)
	}
	g, err := DecodeGeoTIFF(b)
	if err != nil {
		t.Fatal(err)
	}
	if g.W != 40 || g.H != 40 || g.DX != 5 || g.DY != 5 || g.X0 != 201000 || g.Y0 != 448500 {
		t.Fatalf("grid %d×%d cells %v×%v at %v, %v", g.W, g.H, g.DX, g.DY, g.X0, g.Y0)
	}
	lo, hi, holes := math.Inf(1), math.Inf(-1), 0
	for _, z := range g.Z {
		if isNaN(z) {
			holes++
			continue
		}
		lo, hi = min(lo, float64(z)), max(hi, float64(z))
	}
	// The Veluwezoom's foot: ground between about 10 and 60 m above NAP.
	if lo < 5 || hi > 80 || hi-lo < 3 {
		t.Errorf("heights %.1f to %.1f m, not the foot of the Veluwe", lo, hi)
	}
	t.Logf("heights %.1f to %.1f m, %d holes", lo, hi, holes)
	if z, ok := g.At(201100, 448400); !ok || z < lo || z > hi {
		t.Errorf("At(centre) = %v, %v", z, ok)
	}
	if _, ok := g.At(200999, 448400); ok {
		t.Error("At outside the grid reported a height")
	}
}

func TestAtInterpolates(t *testing.T) {
	g := &Grid{X0: 0, Y0: 20, DX: 10, DY: 10, W: 2, H: 2, Z: []float32{0, 10, 20, 30}}
	for _, c := range []struct{ x, y, want float64 }{
		{5, 15, 0},   // first cell centre
		{15, 15, 10}, // second, same row
		{10, 15, 5},  // halfway along the row
		{10, 10, 15}, // middle of all four
		{1, 19, 0},   // corner: the edge value holds
		{15, 5, 30},  // last centre
		{19.9, 0.1, 30},
	} {
		if z, ok := g.At(c.x, c.y); !ok || math.Abs(z-c.want) > 1e-9 {
			t.Errorf("At(%v, %v) = %v, %v; want %v", c.x, c.y, z, ok, c.want)
		}
	}
	if _, ok := g.At(21, 5); ok {
		t.Error("At beyond the grid reported a height")
	}
}

func TestFill(t *testing.T) {
	nan := float32(math.NaN())
	g := &Grid{W: 3, H: 3, DX: 1, DY: 1, Z: []float32{
		1, 1, 1,
		1, nan, 1,
		1, 1, 1,
	}}
	g.Fill(5)
	if g.Z[4] != 1 {
		t.Errorf("hole filled with %v, want 1", g.Z[4])
	}
}
