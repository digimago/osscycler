// Package fec encodes and decodes ANT+ Fitness Equipment Control (FE-C) data
// pages for a trainer.
//
// Layouts follow "ANT+ Device Profile - Fitness Equipment" rev 5.0 and
// "ANT+ Common Data Pages" rev 3.1 (section numbers cited per type), and were
// exercised against a Tacx Flux 2 Smart. Those documents are licensed under
// the ANT+ adopter agreement and must not be committed to this repo.
package fec

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ANT channel parameters for an FE-C slave (FE profile table 7-1).
const (
	DeviceType  = 17   // fitness equipment
	Period      = 8192 // 4 Hz
	RFFrequency = 57   // 2457 MHz
)

// Page numbers.
const (
	PageCalibration         = 0x01
	PageCalibrationProgress = 0x02
	PageGeneral             = 0x10
	PageTrainer             = 0x19
	PageBasicResistance     = 0x30
	PageTargetPower         = 0x31
	PageTrackResistance     = 0x33
	PageUserConfig          = 0x37
	PageRequest             = 0x46
	PageCommandStatus       = 0x47
	PageManufacturer        = 0x50
	PageProduct             = 0x51
)

// State is the FE state carried in the upper nibble of byte 7 of pages 0x10
// and 0x19 (bits 4-6; bit 7 is the lap toggle). FE profile table 8-10.
type State uint8

const (
	StateReserved State = 0
	StateAsleep   State = 1
	StateReady    State = 2
	StateInUse    State = 3
	StateFinished State = 4 // finished or paused
)

func (s State) String() string {
	switch s {
	case StateAsleep:
		return "asleep"
	case StateReady:
		return "ready"
	case StateInUse:
		return "in-use"
	case StateFinished:
		return "finished"
	}
	return fmt.Sprintf("state(%d)", uint8(s))
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }

func put16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
