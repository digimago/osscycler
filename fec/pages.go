package fec

import (
	"encoding/binary"
	"fmt"
)

// Page is a decoded data page.
type Page interface {
	PageNumber() byte
}

// Decode decodes an 8-byte payload. Pages it doesn't know come back as Unknown.
func Decode(p [8]byte) Page {
	switch p[0] {
	case PageCalibration:
		return decodeCalibrationResponse(p)
	case PageCalibrationProgress:
		return decodeCalibrationProgress(p)
	case PageGeneral:
		return decodeGeneral(p)
	case PageTrainer:
		return decodeTrainer(p)
	case PageCommandStatus:
		return CommandStatus{LastCommand: p[1], Sequence: p[2], Status: CommandStatusCode(p[3]), Data: [4]byte(p[4:8])}
	case PageManufacturer:
		return Manufacturer{HWRevision: p[3], ManufacturerID: le16(p[4:]), ModelNumber: le16(p[6:])}
	case PageProduct:
		return Product{SWRevisionSupplemental: p[2], SWRevisionMain: p[3], SerialNumber: binary.LittleEndian.Uint32(p[4:])}
	}
	return Unknown(p)
}

// General is page 0x10, general FE data. FE profile table 8-7.
type General struct {
	EquipmentType   uint8  // byte 1 bits 0-4; 25 = trainer
	ElapsedTime     uint8  // 0.25 s units, rolls over at 64 s
	Distance        uint8  // metres, rolls over at 256 m; valid if DistanceEnabled
	Speed           uint16 // 0.001 m/s; 0xFFFF = invalid
	HeartRate       uint8  // bpm; 0xFF = invalid
	HRSource        uint8  // byte 7 bits 0-1
	DistanceEnabled bool
	VirtualSpeed    bool
	State           State
	Lap             bool
}

func (General) PageNumber() byte { return PageGeneral }

// SpeedMPS returns speed in m/s and false if the trainer reports it invalid.
func (g General) SpeedMPS() (float64, bool) {
	if g.Speed == 0xFFFF {
		return 0, false
	}
	return float64(g.Speed) / 1000, true
}

func decodeGeneral(p [8]byte) General {
	return General{
		EquipmentType:   p[1] & 0x1F,
		ElapsedTime:     p[2],
		Distance:        p[3],
		Speed:           le16(p[4:]),
		HeartRate:       p[6],
		HRSource:        p[7] & 0x03,
		DistanceEnabled: p[7]&0x04 != 0,
		VirtualSpeed:    p[7]&0x08 != 0,
		State:           State(p[7] >> 4 & 0x07),
		Lap:             p[7]&0x80 != 0,
	}
}

// TrainerStatus is the upper nibble of byte 6 of page 0x19. FE profile table 8-27.
type TrainerStatus uint8

const (
	PowerCalibrationRequired      TrainerStatus = 1 << 0
	ResistanceCalibrationRequired TrainerStatus = 1 << 1
	UserConfigRequired            TrainerStatus = 1 << 2
)

func (s TrainerStatus) String() string {
	if s == 0 {
		return "ok"
	}
	var out string
	for _, f := range []struct {
		bit  TrainerStatus
		name string
	}{
		{PowerCalibrationRequired, "power-cal"},
		{ResistanceCalibrationRequired, "resistance-cal"},
		{UserConfigRequired, "user-config"},
	} {
		if s&f.bit != 0 {
			if out != "" {
				out += ","
			}
			out += f.name
		}
	}
	return out + "-required"
}

// TargetPowerLimit reports whether the trainer can hold an ERG target.
// FE profile table 8-28.
type TargetPowerLimit uint8

const (
	AtTargetPower     TargetPowerLimit = 0 // or no target set
	SpeedTooLow       TargetPowerLimit = 1
	SpeedTooHigh      TargetPowerLimit = 2
	LimitUndetermined TargetPowerLimit = 3
)

func (l TargetPowerLimit) String() string {
	return [...]string{"at-target", "speed-too-low", "speed-too-high", "undetermined"}[l&3]
}

// Trainer is page 0x19, specific trainer / stationary bike data. FE profile
// table 8-25.
type Trainer struct {
	EventCount       uint8
	Cadence          uint8  // rpm; 0xFF = invalid
	AccumulatedPower uint16 // W, rolls over at 65536
	Power            uint16 // W, 12 bits; 0xFFF = invalid
	Status           TrainerStatus
	TargetPowerLimit TargetPowerLimit
	State            State
	Lap              bool
}

func (Trainer) PageNumber() byte { return PageTrainer }

func decodeTrainer(p [8]byte) Trainer {
	return Trainer{
		EventCount:       p[1],
		Cadence:          p[2],
		AccumulatedPower: le16(p[3:]),
		Power:            uint16(p[5]) | uint16(p[6]&0x0F)<<8,
		Status:           TrainerStatus(p[6] >> 4),
		TargetPowerLimit: TargetPowerLimit(p[7] & 0x03),
		State:            State(p[7] >> 4 & 0x07),
		Lap:              p[7]&0x80 != 0,
	}
}

// CommandStatusCode is byte 3 of page 0x47.
type CommandStatusCode uint8

const (
	CommandPass          CommandStatusCode = 0
	CommandFail          CommandStatusCode = 1
	CommandNotSupported  CommandStatusCode = 2
	CommandRejected      CommandStatusCode = 3
	CommandPending       CommandStatusCode = 4
	CommandUninitialized CommandStatusCode = 0xFF
)

func (c CommandStatusCode) String() string {
	switch c {
	case CommandPass:
		return "pass"
	case CommandFail:
		return "fail"
	case CommandNotSupported:
		return "not-supported"
	case CommandRejected:
		return "rejected"
	case CommandPending:
		return "pending"
	case CommandUninitialized:
		return "uninitialized"
	}
	return fmt.Sprintf("status(%d)", uint8(c))
}

// CommandStatus is page 0x47: the trainer's view of the last control page it
// received. Data echoes bytes 4-7 of that page. FE profile table 8-48.
//
// The Flux 2 (firmware 4.503) does not answer requests for this page, in
// broadcast or acknowledged mode, although the profile requires it.
type CommandStatus struct {
	LastCommand byte // page number; 0xFF = none received
	Sequence    uint8
	Status      CommandStatusCode
	Data        [4]byte
}

func (CommandStatus) PageNumber() byte { return PageCommandStatus }

// Manufacturer is common page 0x50. The Flux 2 reports manufacturer 89 (Tacx)
// and model 2980.
type Manufacturer struct {
	HWRevision     uint8
	ManufacturerID uint16
	ModelNumber    uint16
}

func (Manufacturer) PageNumber() byte { return PageManufacturer }

// Product is common page 0x51 (common data pages section 6.8).
type Product struct {
	SWRevisionSupplemental uint8 // 0xFF = not used
	SWRevisionMain         uint8
	SerialNumber           uint32 // 0xFFFFFFFF = none
}

func (Product) PageNumber() byte { return PageProduct }

// SWVersion decodes the software revision per common data pages equations
// 6-1 and 6-2: (main*100 + supplemental)/1000, or main/10 without a
// supplemental revision.
func (p Product) SWVersion() string {
	if p.SWRevisionSupplemental == 0xFF {
		return fmt.Sprintf("%.1f", float64(p.SWRevisionMain)/10)
	}
	return fmt.Sprintf("%.3f", float64(int(p.SWRevisionMain)*100+int(p.SWRevisionSupplemental))/1000)
}

// Unknown is any page this package does not decode.
type Unknown [8]byte

func (u Unknown) PageNumber() byte { return u[0] }

// Calibration bits shared by the request/response (byte 1 of page 0x01) and
// the status byte of page 0x02. FE profile tables 8-3 and 8-5.
const (
	calZeroOffset = 1 << 6
	calSpinDown   = 1 << 7
)

// CalibrationResponse is page 0x01 sent by the trainer when a calibration
// finishes. FE profile table 8-2.
type CalibrationResponse struct {
	SpinDownSuccess   bool
	ZeroOffsetSuccess bool
	Temperature       uint8  // see TemperatureC
	ZeroOffset        uint16 // raw; see ZeroOffsetValue
	SpinDownTime      uint16 // raw ms; see SpinDownMS
}

func (CalibrationResponse) PageNumber() byte { return PageCalibration }

// notReported is the lowest raw value treated as "no value". The profile
// only names 0xFFFF as invalid, but the Flux 2 (firmware 4.503) fills both
// fields with 0xFFFD, after successful and failed calibrations alike; no
// real spin-down lasts 65 s.
const notReported = 0xFFFD

// SpinDownMS returns the spin-down time, if the trainer reported one.
func (r CalibrationResponse) SpinDownMS() (uint16, bool) {
	return r.SpinDownTime, r.SpinDownTime < notReported
}

// ZeroOffsetValue returns the zero offset, if the trainer reported one.
func (r CalibrationResponse) ZeroOffsetValue() (uint16, bool) {
	return r.ZeroOffset, r.ZeroOffset < notReported
}

func (r CalibrationResponse) TemperatureC() (float64, bool) { return temperatureC(r.Temperature) }

func decodeCalibrationResponse(p [8]byte) CalibrationResponse {
	return CalibrationResponse{
		SpinDownSuccess:   p[1]&calSpinDown != 0,
		ZeroOffsetSuccess: p[1]&calZeroOffset != 0,
		Temperature:       p[3],
		ZeroOffset:        le16(p[4:]),
		SpinDownTime:      le16(p[6:]),
	}
}

// Condition is a calibration condition from page 0x02 (FE profile table 8-6).
type Condition uint8

const (
	ConditionNotApplicable Condition = 0
	ConditionTooLow        Condition = 1
	ConditionOK            Condition = 2
	ConditionTooHigh       Condition = 3 // temperature only; reserved for speed
)

func (c Condition) String() string {
	return [...]string{"n/a", "too-low", "ok", "too-high"}[c&3]
}

// CalibrationProgress is page 0x02, sent while a calibration runs. FE
// profile table 8-4.
type CalibrationProgress struct {
	SpinDownPending      bool
	ZeroOffsetPending    bool
	TemperatureCondition Condition
	SpeedCondition       Condition
	Temperature          uint8  // see TemperatureC
	TargetSpeed          uint16 // 0.001 m/s, 0xFFFF = invalid
	TargetSpinDownTime   uint16 // raw ms; see TargetSpinDownMS
}

// TargetSpinDownMS returns the ideal spin-down time, if reported.
func (c CalibrationProgress) TargetSpinDownMS() (uint16, bool) {
	return c.TargetSpinDownTime, c.TargetSpinDownTime < notReported
}

func (CalibrationProgress) PageNumber() byte { return PageCalibrationProgress }

func (c CalibrationProgress) TemperatureC() (float64, bool) { return temperatureC(c.Temperature) }

func (c CalibrationProgress) TargetSpeedMPS() (float64, bool) {
	if c.TargetSpeed == 0xFFFF {
		return 0, false
	}
	return float64(c.TargetSpeed) / 1000, true
}

func decodeCalibrationProgress(p [8]byte) CalibrationProgress {
	return CalibrationProgress{
		SpinDownPending:      p[1]&calSpinDown != 0,
		ZeroOffsetPending:    p[1]&calZeroOffset != 0,
		TemperatureCondition: Condition(p[2] >> 4 & 3),
		SpeedCondition:       Condition(p[2] >> 6 & 3),
		Temperature:          p[3],
		TargetSpeed:          le16(p[4:]),
		TargetSpinDownTime:   le16(p[6:]),
	}
}

// temperatureC decodes the calibration pages' temperature: 0.5 °C steps
// offset by -25 °C, 0xFF = invalid.
func temperatureC(raw uint8) (float64, bool) {
	if raw == 0xFF {
		return 0, false
	}
	return float64(raw)/2 - 25, true
}
