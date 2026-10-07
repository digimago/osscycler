package hrm

import "testing"

func TestDecodeCurrentStrap(t *testing.T) {
	var d Decoder
	// Four messages with toggle 0, then the toggle flips: from then on the
	// page numbers are meaningful.
	p4 := [8]byte{0x04, 0xFF, 0x00, 0x10, 0x00, 0x14, 42, 145}
	for range 4 {
		if pg := d.Decode(p4); pg.Number != 0 || pg.HeartRate != 145 {
			t.Fatalf("before toggle: %+v", pg)
		}
	}
	if !d.Legacy() {
		t.Fatal("Legacy() = false before any toggle")
	}

	p4[0] |= 0x80
	pg := d.Decode(p4)
	if d.Legacy() {
		t.Fatal("Legacy() = true after toggle")
	}
	want := Page{Number: PagePreviousBeat, BeatTime: 0x1400, BeatCount: 42, HeartRate: 145, PreviousBeatTime: 0x1000}
	if pg != want {
		t.Fatalf("got %+v\nwant %+v", pg, want)
	}

	pg = d.Decode([8]byte{0x82, 1, 0x34, 0x12, 0, 0, 43, 146})
	if pg.Number != PageManufacturer || pg.ManufacturerID != 1 || pg.SerialUpper != 0x1234 {
		t.Errorf("manufacturer page: %+v", pg)
	}
	pg = d.Decode([8]byte{0x03, 5, 7, 9, 0, 0, 44, 147})
	if pg.Number != PageProduct || pg.HWVersion != 5 || pg.SWVersion != 7 || pg.ModelNumber != 9 {
		t.Errorf("product page: %+v", pg)
	}
}

func TestDecodeLegacyStrap(t *testing.T) {
	var d Decoder
	// Legacy byte 0 is not a page number, even if it looks like one.
	for range 10 {
		pg := d.Decode([8]byte{0x02, 0xFF, 0xFF, 0xFF, 0x00, 0x20, 7, 120})
		if pg.Number != 0 || pg.ManufacturerID != 0 || pg.HeartRate != 120 || pg.BeatCount != 7 {
			t.Fatalf("legacy decode: %+v", pg)
		}
	}
	if !d.Legacy() {
		t.Error("Legacy() = false for a strap that never toggles")
	}
}
