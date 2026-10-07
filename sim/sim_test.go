package sim

import (
	"math"
	"testing"
)

func settle(r *Rider, powerW, gradePct float64) {
	for range 600 { // five minutes at 0.5 s steps
		r.Step(powerW, gradePct, 0.5)
	}
}

func TestReachesSteadySpeed(t *testing.T) {
	p := DefaultParams(87, 9)
	for _, tc := range []struct{ power, grade, lo, hi float64 }{
		{200, 0, 30, 36},  // km/h, a plausible flat-road range
		{300, 10, 9, 12},  // a steep climb
		{150, -5, 50, 65}, // light pedalling downhill
		{0, -8, 55, 75},   // coasting a steep descent
	} {
		r := Rider{Params: p}
		settle(&r, tc.power, tc.grade)
		want := p.SteadySpeed(tc.power, tc.grade)
		got := r.SpeedMPS
		if math.Abs(got-want) > 0.01*want+0.01 {
			t.Errorf("%v W on %v %%: settled at %.3f m/s, steady state is %.3f", tc.power, tc.grade, got, want)
		}
		if kmh := got * 3.6; kmh < tc.lo || kmh > tc.hi {
			t.Errorf("%v W on %v %%: %.1f km/h, expected %v..%v", tc.power, tc.grade, kmh, tc.lo, tc.hi)
		}
	}
}

func TestStopsOnClimbWithoutPower(t *testing.T) {
	r := Rider{Params: DefaultParams(87, 9), SpeedMPS: 5}
	settle(&r, 0, 8)
	if r.SpeedMPS != 0 {
		t.Fatalf("speed %.3f, want 0: the bike must not roll backwards", r.SpeedMPS)
	}
	d := r.DistanceM
	settle(&r, 0, 8)
	if r.DistanceM != d {
		t.Fatal("distance changed while stopped")
	}
}

func TestStartsFromStandstill(t *testing.T) {
	r := Rider{Params: DefaultParams(87, 9)}
	for range 20 { // 5 s at 0.25 s
		r.Step(250, 5, 0.25)
	}
	if r.SpeedMPS < 2 || r.DistanceM < 5 {
		t.Fatalf("after 5 s at 250 W on 5 %%: %.2f m/s, %.1f m", r.SpeedMPS, r.DistanceM)
	}
}

func TestDistanceIsIntegratedSpeed(t *testing.T) {
	p := DefaultParams(87, 9)
	r := Rider{Params: p, SpeedMPS: p.SteadySpeed(200, 0)}
	r.Step(200, 0, 60)
	if want := r.SpeedMPS * 60; math.Abs(r.DistanceM-want) > 1 {
		t.Fatalf("60 s at steady speed: %.1f m, want %.1f", r.DistanceM, want)
	}
}

func TestTrainerGrade(t *testing.T) {
	for _, tc := range []struct{ grade, difficulty, want float64 }{
		{5, 1, 5},
		{5, 0.5, 2.5},
		{-10, 1, -5},     // descents are halved, as in Zwift
		{-10, 0.5, -2.5}, // and then scaled: a quarter at the 50 % default
		{20, 1, 16},      // capped at the trainer's maximum
		{8, 0, 0},        // 0 % difficulty feels flat
		{0, 1, 0},
	} {
		if got := TrainerGrade(tc.grade, tc.difficulty, 16); got != tc.want {
			t.Errorf("TrainerGrade(%v, %v) = %v, want %v", tc.grade, tc.difficulty, got, tc.want)
		}
	}
}
