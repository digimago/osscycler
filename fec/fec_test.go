package fec

import "testing"

func TestDecodeGeneral(t *testing.T) {
	// Trainer, 10 s elapsed, 123 m, 8.333 m/s, no HR, distance enabled, in use, lap.
	p := [8]byte{0x10, 25, 40, 123, 0x8D, 0x20, 0xFF, 0x80 | 0x30 | 0x04}
	g, ok := Decode(p).(General)
	if !ok {
		t.Fatalf("Decode = %T", Decode(p))
	}
	want := General{EquipmentType: 25, ElapsedTime: 40, Distance: 123, Speed: 8333, HeartRate: 0xFF,
		DistanceEnabled: true, State: StateInUse, Lap: true}
	if g != want {
		t.Fatalf("got %+v\nwant %+v", g, want)
	}
	if v, ok := g.SpeedMPS(); !ok || v != 8.333 {
		t.Errorf("SpeedMPS = %v, %v", v, ok)
	}
}

func TestDecodeTrainer(t *testing.T) {
	// 250 W = 0x0FA, user config required, speed too low, ready.
	p := [8]byte{0x19, 7, 90, 0x34, 0x12, 0xFA, 0x40, 0x21} // byte 6: user-config bit, power MSN 0; byte 7: ready, speed too low
	tr := Decode(p).(Trainer)
	want := Trainer{EventCount: 7, Cadence: 90, AccumulatedPower: 0x1234, Power: 250,
		Status: UserConfigRequired, TargetPowerLimit: SpeedTooLow, State: StateReady}
	if tr != want {
		t.Fatalf("got %+v\nwant %+v", tr, want)
	}
	// 12-bit power uses the low nibble of byte 6.
	p[5], p[6] = 0xD0, 0x07
	if got := Decode(p).(Trainer).Power; got != 2000 {
		t.Errorf("Power = %d, want 2000", got)
	}
}

func TestDecodeCommonPages(t *testing.T) {
	m := Decode([8]byte{0x50, 0xFF, 0xFF, 3, 89, 0, 0x34, 0x12}).(Manufacturer)
	if m != (Manufacturer{HWRevision: 3, ManufacturerID: 89, ModelNumber: 0x1234}) {
		t.Errorf("Manufacturer = %+v", m)
	}
	pr := Decode([8]byte{0x51, 0xFF, 0xFF, 42, 0x78, 0x56, 0x34, 0x12}).(Product)
	if pr != (Product{SWRevisionSupplemental: 0xFF, SWRevisionMain: 42, SerialNumber: 0x12345678}) {
		t.Errorf("Product = %+v", pr)
	}
	if v := pr.SWVersion(); v != "4.2" {
		t.Errorf("SWVersion = %q, want 4.2", v)
	}
	// Flux 2 Smart firmware as reported on the dev trainer.
	if v := (Product{SWRevisionMain: 45, SWRevisionSupplemental: 3}).SWVersion(); v != "4.503" {
		t.Errorf("SWVersion = %q, want 4.503", v)
	}
	cs := Decode([8]byte{0x47, 0x33, 9, 0, 0xFF, 0x20, 0x4E, 0xFF}).(CommandStatus)
	if cs.LastCommand != PageTrackResistance || cs.Status != CommandPass || cs.Data != [4]byte{0xFF, 0x20, 0x4E, 0xFF} {
		t.Errorf("CommandStatus = %+v", cs)
	}
	if u, ok := Decode([8]byte{0xF0}).(Unknown); !ok || u.PageNumber() != 0xF0 {
		t.Errorf("unknown page = %T", Decode([8]byte{0xF0}))
	}
}

func TestControlPages(t *testing.T) {
	tests := []struct {
		name string
		got  [8]byte
		want [8]byte
	}{
		{"resistance 37.5%", BasicResistance(37.5), [8]byte{0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 75}},
		{"resistance clamps", BasicResistance(150), [8]byte{0x30, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 200}},
		{"erg 200 W", TargetPower(200), [8]byte{0x31, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x20, 0x03}},
		{"grade 0%", TrackResistance(0, 0), [8]byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x20, 0x4E, 0xFF}},
		{"grade 5.5% crr", TrackResistance(5.5, 0.004), [8]byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0x46, 0x50, 80}},
		{"grade -3%", TrackResistance(-3, 0), [8]byte{0x33, 0xFF, 0xFF, 0xFF, 0xFF, 0xF4, 0x4C, 0xFF}},
		{"request 0x51", RequestPage(0x51, 2), [8]byte{0x46, 0xFF, 0xFF, 0xFF, 0xFF, 2, 0x51, 0x01}},
		{"user config", UserConfig{UserWeightKg: 75, BikeWeightKg: 9, WheelDiameterM: 0.672, GearRatio: 2.4}.Page(),
			// 7500 = 0x1D4C; bike 180 = 0x0B4; wheel 672 mm = 67 + 2; gear 80.
			[8]byte{0x37, 0x4C, 0x1D, 0xFF, 0x42, 0x0B, 67, 80}},
		{"user config defaults", UserConfig{}.Page(), [8]byte{0x37, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got % X, want % X", tt.name, tt.got, tt.want)
		}
	}
}

func TestCalibrationPages(t *testing.T) {
	if got, want := CalibrationRequest(true, false), [8]byte{0x01, 0x80, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}; got != want {
		t.Errorf("spin-down request = % X, want % X", got, want)
	}
	if got := CalibrationRequest(false, false); got[1] != 0 {
		t.Errorf("cancel request byte 1 = %#x, want 0", got[1])
	}

	// In progress: spin-down pending, temperature ok, speed too low,
	// 21 degC (0x5C = 92 -> 46 - 25), target 10.000 m/s, target 3000 ms.
	pr := Decode([8]byte{0x02, 0x80, 0b0110_0000, 0x5C, 0x10, 0x27, 0xB8, 0x0B}).(CalibrationProgress)
	if !pr.SpinDownPending || pr.ZeroOffsetPending || pr.TemperatureCondition != ConditionOK || pr.SpeedCondition != ConditionTooLow {
		t.Errorf("progress flags: %+v", pr)
	}
	if v, ok := pr.TemperatureC(); !ok || v != 21 {
		t.Errorf("TemperatureC = %v, %v", v, ok)
	}
	if v, ok := pr.TargetSpeedMPS(); !ok || v != 10 {
		t.Errorf("TargetSpeedMPS = %v, %v", v, ok)
	}
	if pr.TargetSpinDownTime != 3000 {
		t.Errorf("TargetSpinDownTime = %d", pr.TargetSpinDownTime)
	}

	// The profile's own example: 0x10 means -17 degC.
	if v, _ := temperatureC(0x10); v != -17 {
		t.Errorf("temperatureC(0x10) = %v, want -17", v)
	}

	res := Decode([8]byte{0x01, 0x80, 0x00, 0xFF, 0xFF, 0xFF, 0xD2, 0x04}).(CalibrationResponse)
	if !res.SpinDownSuccess || res.ZeroOffsetSuccess || res.SpinDownTime != 1234 {
		t.Errorf("response: %+v", res)
	}
	if _, ok := res.TemperatureC(); ok {
		t.Error("invalid temperature decoded as valid")
	}
}

func TestCalibrationResultNotReported(t *testing.T) {
	// What the Flux 2 sends after a successful spin-down: success bit set,
	// 0xFFFD where a time would be.
	res := Decode([8]byte{0x01, 0x80, 0x00, 0xFF, 0xFD, 0xFF, 0xFD, 0xFF}).(CalibrationResponse)
	if !res.SpinDownSuccess {
		t.Fatal("success bit lost")
	}
	if v, ok := res.SpinDownMS(); ok {
		t.Errorf("SpinDownMS = %d, want not reported", v)
	}
	if _, ok := res.ZeroOffsetValue(); ok {
		t.Error("ZeroOffsetValue reported for 0xFFFD")
	}
	// A plausible time still comes through.
	res = Decode([8]byte{0x01, 0x80, 0x00, 0xFF, 0xFF, 0xFF, 0x70, 0x17}).(CalibrationResponse)
	if v, ok := res.SpinDownMS(); !ok || v != 6000 {
		t.Errorf("SpinDownMS = %d, %v, want 6000", v, ok)
	}
}
