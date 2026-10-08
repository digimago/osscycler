package sim

import "testing"

// BenchmarkStep is one ride tick of the simulation (0.25 s, 5 substeps).
func BenchmarkStep(b *testing.B) {
	r := Rider{Params: DefaultParams(75, 9), SpeedMPS: 9}
	b.ReportAllocs()
	for b.Loop() {
		r.Step(200, 4, 0.25)
	}
}
