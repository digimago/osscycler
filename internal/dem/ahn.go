package dem

import (
	"context"
	"fmt"
	"net/url"
)

const (
	// AHNURL is PDOK's WCS for the AHN.
	AHNURL = "https://service.pdok.nl/rws/ahn/wcs/v1_0"
	// AHNAttribution credits the source. AHN is CC0, so this is courtesy,
	// not a condition.
	AHNAttribution = "elevation: AHN (Actueel Hoogtebestand Nederland), CC0"

	ahnCoverage = "dtm_05m" // bare ground, 0.5 m cells
	ahnNative   = 0.5       // m
)

// AHN's extent in RD New, from the WCS's coverage description.
var ahnMinX, ahnMinY, ahnMaxX, ahnMaxY = 10000.0, 306250.0, 280000.0, 618750.0

// AHN fetches the Dutch AHN ground model in 1 km tiles of RD New.
type AHN Source

var ahnTiles = &tileset{
	name:        "AHN",
	native:      ahnNative,
	endpoint:    AHNURL,
	contentType: "image/tiff",
	project:     ToRD,
	inside:      inAHN,
	file: func(tx, ty int, cell float64) string {
		return fmt.Sprintf("ahn-%s-%gm-%d-%d.tif", ahnCoverage, cell, tx, ty)
	},
	url: func(endpoint string, tx, ty int, cell float64) string {
		q := url.Values{
			"service":     {"WCS"},
			"version":     {"2.0.1"},
			"request":     {"GetCoverage"},
			"CoverageId":  {ahnCoverage},
			"format":      {"image/tiff"},
			"scalefactor": {fmt.Sprint(ahnNative / cell)},
		}
		return endpoint + "?" + q.Encode() +
			fmt.Sprintf("&subset=x(%d,%d)&subset=y(%d,%d)", tx*tileM, (tx+1)*tileM, ty*tileM, (ty+1)*tileM)
	},
}

// Load reads the tiles within radius metres of any of pts.
func (a *AHN) Load(ctx context.Context, pts []LatLon, radius float64) (*Mosaic, error) {
	return (*Source)(a).load(ctx, ahnTiles, pts, radius)
}

func inAHN(tx, ty int) bool {
	x, y := float64(tx*tileM), float64(ty*tileM)
	return x+tileM > ahnMinX && x < ahnMaxX && y+tileM > ahnMinY && y < ahnMaxY
}
