package dem

import (
	"context"
	"fmt"
	"math"
	"net/url"
)

const (
	// RGEAltiURL is the Géoplateforme's raster WMS, which serves IGN's
	// ground models as float32 GeoTIFF.
	RGEAltiURL = "https://data.geopf.fr/wms-r/wms"
	// RGEAltiAttribution credits the source, as the Licence Ouverte 2.0
	// (Etalab) asks: the source named.
	RGEAltiAttribution = "elevation: IGN RGE ALTI, Licence Ouverte 2.0 (Etalab)"

	rgeAltiLayer  = "ELEVATION.ELEVATIONGRIDCOVERAGE.HIGHRES" // the RGE ALTI DTM
	rgeAltiNative = 1                                         // m
)

// Lambert-93's area of use (EPSG:2154: metropolitan France, Corsica and
// the seas round them) in its own coordinates. The layer has data a few
// km past the land border, nodata beyond (Source.load).
var l93MinX, l93MinY, l93MaxX, l93MaxY = -378305.81, 6005281.20, 1320649.57, 7235612.72

// RGEAlti fetches IGN's RGE ALTI, France's bare-ground model, in 1 km
// tiles of Lambert-93 (EPSG:2154), heights above NGF-IGN69.
type RGEAlti Source

var rgeAltiTiles = &tileset{
	name:        "RGE ALTI",
	native:      rgeAltiNative,
	endpoint:    RGEAltiURL,
	contentType: "image/geotiff",
	project:     ToLambert93,
	inside: func(tx, ty int) bool {
		x, y := float64(tx*tileM), float64(ty*tileM)
		return x+tileM > l93MinX && x < l93MaxX && y+tileM > l93MinY && y < l93MaxY
	},
	file: func(tx, ty int, cell float64) string {
		return fmt.Sprintf("rgealti-%gm-%d-%d.tif", cell, tx, ty)
	},
	url: func(endpoint string, tx, ty int, cell float64) string {
		n := fmt.Sprint(int(math.Round(tileM / cell)))
		q := url.Values{
			"SERVICE": {"WMS"},
			"VERSION": {"1.3.0"},
			"REQUEST": {"GetMap"},
			"LAYERS":  {rgeAltiLayer},
			"STYLES":  {""},
			"CRS":     {"EPSG:2154"}, // easting, northing
			"BBOX":    {fmt.Sprintf("%d,%d,%d,%d", tx*tileM, ty*tileM, (tx+1)*tileM, (ty+1)*tileM)},
			"WIDTH":   {n},
			"HEIGHT":  {n},
			"FORMAT":  {"image/geotiff"},
		}
		return endpoint + "?" + q.Encode()
	},
}

// Load reads the tiles within radius metres of any of pts.
func (r *RGEAlti) Load(ctx context.Context, pts []LatLon, radius float64) (*Mosaic, error) {
	return (*Source)(r).load(ctx, rgeAltiTiles, pts, radius)
}

// Lambert-93: Lambert conformal conic on two standard parallels (EPSG
// method 9802), GRS80, parallels 44° and 49°, origin 46.5° N 3° E at
// 700000, 6600000.
var l93 = func() (p struct{ e, n, aF, r0 float64 }) {
	const a, f = 6378137.0, 1 / 298.257222101
	p.e = math.Sqrt(2*f - f*f)
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	m := func(phi float64) float64 { s := p.e * math.Sin(phi); return math.Cos(phi) / math.Sqrt(1-s*s) }
	t := lccT(p.e)
	phi1, phi2 := rad(44), rad(49)
	p.n = (math.Log(m(phi1)) - math.Log(m(phi2))) / (math.Log(t(phi1)) - math.Log(t(phi2)))
	p.aF = a * m(phi1) / (p.n * math.Pow(t(phi1), p.n))
	p.r0 = p.aF * math.Pow(t(rad(46.5)), p.n)
	return p
}()

func lccT(e float64) func(phi float64) float64 {
	return func(phi float64) float64 {
		s := e * math.Sin(phi)
		return math.Tan(math.Pi/4-phi/2) / math.Pow((1-s)/(1+s), e/2)
	}
}

// ToLambert93 converts WGS84 latitude and longitude in degrees to
// Lambert-93 (EPSG:2154) x, y in metres. Its datum, RGF93, is ETRS89,
// within a metre of WGS84: well inside a terrain grid's cell.
func ToLambert93(lat, lon float64) (x, y float64) {
	r := l93.aF * math.Pow(lccT(l93.e)(lat*math.Pi/180), l93.n)
	th := l93.n * (lon - 3) * math.Pi / 180
	return 700000 + r*math.Sin(th), 6600000 + l93.r0 - r*math.Cos(th)
}
