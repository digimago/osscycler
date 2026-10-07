// Package hrm decodes ANT+ heart rate monitor data pages.
//
// Layouts follow "ANT+ Device Profile - Heart Rate" rev 2.5, kept locally
// under _antdocs/ and not redistributable.
package hrm

import "encoding/binary"

// ANT channel parameters for a heart rate display (profile table 4).
const (
	DeviceType  = 120
	Period      = 8070 // 4.06 Hz
	RFFrequency = 57
)

// Page numbers (byte 0 bits 0-6).
const (
	PageDefault      = 0x00
	PageCumulative   = 0x01
	PageManufacturer = 0x02
	PageProduct      = 0x03
	PagePreviousBeat = 0x04
)

// Page is one decoded message. Bytes 4-7 are the same on every page; the
// page-specific fields are only set when Number says so.
type Page struct {
	// Number is the data page number. Legacy straps don't use page
	// numbers; their messages decode as PageDefault.
	Number byte

	BeatTime  uint16 // time of last beat, 1/1024 s, rolls over at 64 s
	BeatCount uint8  // rolls over at 256
	HeartRate uint8  // bpm, 0 = invalid

	PreviousBeatTime uint16 // PagePreviousBeat

	ManufacturerID uint8  // PageManufacturer
	SerialUpper    uint16 // PageManufacturer: upper 16 bits of the serial

	HWVersion, SWVersion, ModelNumber uint8 // PageProduct
}

// Decoder tells current straps from legacy ones. Current straps toggle
// bit 7 of byte 0 every four messages; legacy straps never do, and their
// byte 0 is not a page number (profile section 6.1.1).
type Decoder struct {
	seen    bool
	last    bool
	toggled bool
}

// Legacy reports whether no page toggle has been seen yet.
func (d *Decoder) Legacy() bool { return !d.toggled }

func (d *Decoder) Decode(p [8]byte) Page {
	toggle := p[0]&0x80 != 0
	if d.seen && toggle != d.last {
		d.toggled = true
	}
	d.seen, d.last = true, toggle

	pg := Page{
		BeatTime:  binary.LittleEndian.Uint16(p[4:]),
		BeatCount: p[6],
		HeartRate: p[7],
	}
	if !d.toggled {
		return pg
	}
	pg.Number = p[0] & 0x7F
	switch pg.Number {
	case PageManufacturer:
		pg.ManufacturerID = p[1]
		pg.SerialUpper = binary.LittleEndian.Uint16(p[2:])
	case PageProduct:
		pg.HWVersion, pg.SWVersion, pg.ModelNumber = p[1], p[2], p[3]
	case PagePreviousBeat:
		pg.PreviousBeatTime = binary.LittleEndian.Uint16(p[2:])
	}
	return pg
}
