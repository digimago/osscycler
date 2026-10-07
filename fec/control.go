package fec

import "math"

// Control pages are sent to the trainer as acknowledged messages. Unused
// bytes are 0xFF (reserved). FE profile section 8.8 and table 8-50.

func reserved(page byte) [8]byte {
	return [8]byte{page, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
}

// BasicResistance is page 0x30: total resistance as a percentage of maximum,
// in 0.5% steps.
func BasicResistance(percent float64) [8]byte {
	p := reserved(PageBasicResistance)
	p[7] = byte(math.Round(clamp(percent, 0, 100) * 2))
	return p
}

// TargetPower is page 0x31: ERG mode target in 0.25 W steps.
func TargetPower(watts float64) [8]byte {
	p := reserved(PageTargetPower)
	put16(p[6:], uint16(math.Round(clamp(watts, 0, 4000)*4)))
	return p
}

// TrackResistance is page 0x33: simulation mode grade in 0.01% steps, offset
// by -200%, plus the coefficient of rolling resistance in 5e-5 steps. A crr
// of 0 or less sends 0xFF so the trainer uses its default.
//
// This encodes the grade it is given. Clamping to what the trainer can
// render (0..16% on a Flux 2) is the sim core's job.
func TrackResistance(gradePercent, crr float64) [8]byte {
	p := reserved(PageTrackResistance)
	put16(p[5:], uint16(math.Round((clamp(gradePercent, -200, 200)+200)*100)))
	if crr > 0 {
		p[7] = byte(math.Round(clamp(crr, 0, 254*5e-5) / 5e-5))
	}
	return p
}

// UserConfig is page 0x37. The trainer uses it in simulation mode. Zero
// fields are sent as "invalid" so the trainer keeps its defaults.
type UserConfig struct {
	UserWeightKg   float64 // 0.01 kg steps, up to 655.34
	BikeWeightKg   float64 // 0.05 kg steps, up to 50
	WheelDiameterM float64 // 1 mm resolution, up to 2.54 m
	GearRatio      float64 // 0.03 steps, front/rear
}

func (u UserConfig) Page() [8]byte {
	p := [8]byte{PageUserConfig}

	uw := uint16(0xFFFF)
	if u.UserWeightKg > 0 {
		uw = uint16(math.Round(clamp(u.UserWeightKg, 0, 655.34) * 100))
	}
	put16(p[1:], uw)
	p[3] = 0xFF

	bw := uint16(0xFFF)
	if u.BikeWeightKg > 0 {
		bw = uint16(math.Round(clamp(u.BikeWeightKg, 0, 50) / 0.05))
	}
	wheel, wheelOffset := byte(0xFF), byte(0x0F)
	if u.WheelDiameterM > 0 {
		mm := int(math.Round(clamp(u.WheelDiameterM, 0, 2.54) * 1000))
		wheel, wheelOffset = byte(mm/10), byte(mm%10)
	}
	p[4] = wheelOffset | byte(bw&0x0F)<<4
	p[5] = byte(bw >> 4)
	p[6] = wheel

	if u.GearRatio > 0 {
		p[7] = byte(math.Round(clamp(u.GearRatio, 0.03, 255*0.03) / 0.03))
	}
	return p
}

// RequestPage is common page 0x46 asking the trainer to send page `page`.
// response is the "requested transmission response" byte: bits 0-6 are the
// number of transmissions, bit 7 asks for acknowledged messages, and 0x80
// means "until acknowledged". The trainer may ignore requests entirely.
func RequestPage(page, response byte) [8]byte {
	return [8]byte{PageRequest, 0xFF, 0xFF, 0xFF, 0xFF, response, page, 0x01}
}

// CalibrationRequest is page 0x01 asking the trainer to calibrate. With
// neither kind requested it cancels a calibration in progress. The trainer
// fields are sent as invalid, as the profile requires of displays.
func CalibrationRequest(spinDown, zeroOffset bool) [8]byte {
	var bits byte
	if spinDown {
		bits |= calSpinDown
	}
	if zeroOffset {
		bits |= calZeroOffset
	}
	return [8]byte{PageCalibration, bits, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
}
