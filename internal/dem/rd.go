package dem

// ToRD converts WGS84 latitude and longitude in degrees to RD New
// (EPSG:28992) x, y in metres, with the approximation formulas of
// Schreutelkamp and Strang van Hees (polynomials around Amersfoort,
// within about a metre over the Netherlands). That is well inside a
// terrain grid's cell, and needs no projection library.
func ToRD(lat, lon float64) (x, y float64) {
	dp := 0.36 * (lat - 52.15517440)
	dl := 0.36 * (lon - 5.38720621)
	dp2, dl2 := dp*dp, dl*dl
	x = 155000 +
		190094.945*dl +
		-11832.228*dp*dl +
		-114.221*dp2*dl +
		-32.391*dl2*dl +
		-0.705*dp +
		-2.340*dp2*dp*dl +
		-0.608*dp*dl2*dl +
		-0.008*dl2 +
		0.148*dp2*dl2*dl
	y = 463000 +
		309056.544*dp +
		3638.893*dl2 +
		73.077*dp2 +
		-157.984*dp*dl2 +
		59.788*dp2*dp +
		0.433*dl +
		-6.439*dp2*dl2 +
		-0.032*dp*dl +
		0.092*dl2*dl2 +
		-0.054*dp*dl2*dl2
	return x, y
}
