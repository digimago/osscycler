package ant

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func TestEncode(t *testing.T) {
	// Open channel 0: A4 01 4B 00, checksum A4^01^4B^00 = EE.
	got := Message{ID: MsgOpenChannel, Data: []byte{0}}.Encode()
	want := []byte{0xA4, 0x01, 0x4B, 0x00, 0xEE}
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode = % X, want % X", got, want)
	}
}

func TestDecoderResync(t *testing.T) {
	a := Message{ID: MsgBroadcastData, Data: []byte{0, 0x10, 25, 1, 2, 3, 4, 0xFF, 0x30}}
	b := Message{ID: MsgChannelEvent, Data: []byte{0, 1, 2}}
	corrupt := a.Encode()
	corrupt[5] ^= 0xFF

	var stream []byte
	stream = append(stream, 0x00, 0x00, 0x12)  // leading junk
	stream = append(stream, corrupt...)        // bad checksum
	stream = append(stream, a.Encode()...)     // good
	stream = append(stream, 0xA4, 0xF0, 0x00)  // fake sync with absurd length
	stream = append(stream, b.Encode()...)     // good
	stream = append(stream, b.Encode()[:3]...) // truncated tail

	// OneByteReader exercises partial frames across reads.
	d := NewDecoder(iotest.OneByteReader(bytes.NewReader(stream)))
	for i, want := range []Message{a, b} {
		got, err := d.Next()
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		if got.ID != want.ID || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("message %d = %+v, want %+v", i, got, want)
		}
	}
	if _, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("tail: err = %v, want EOF", err)
	}
	if d.BadChecksums.Load() != 1 {
		t.Errorf("BadChecksums = %d, want 1", d.BadChecksums.Load())
	}
	if d.Dropped.Load() == 0 {
		t.Error("Dropped = 0, want junk counted")
	}
}

func TestPayload(t *testing.T) {
	m := Message{ID: MsgBroadcastData, Data: []byte{3, 1, 2, 3, 4, 5, 6, 7, 8}}
	p, ok := m.Payload()
	if !ok || p != [8]byte{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Fatalf("Payload = %v, %v", p, ok)
	}
	if _, ok := (Message{ID: MsgChannelEvent, Data: m.Data}).Payload(); ok {
		t.Fatal("Payload accepted a channel event")
	}
}

func TestParseNetworkKey(t *testing.T) {
	want := NetworkKey{1, 2, 3, 4, 5, 6, 7, 8}
	for _, s := range []string{
		"0102030405060708",
		"01 02 03 04 05 06 07 08",
		"0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08",
		"01:02:03:04:05:06:07:08",
	} {
		k, err := ParseNetworkKey(s)
		if err != nil || k != want {
			t.Errorf("ParseNetworkKey(%q) = %v, %v", s, k[:], err)
		}
	}
	for _, s := range []string{"", "0102", "zz02030405060708", "010203040506070809"} {
		if _, err := ParseNetworkKey(s); err == nil {
			t.Errorf("ParseNetworkKey(%q) succeeded", s)
		}
	}
	if s := want.String(); bytes.Contains([]byte(s), []byte("01")) {
		t.Errorf("String leaks key: %q", s)
	}
}
