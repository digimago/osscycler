package fit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestCRC(t *testing.T) {
	// The FIT CRC is CRC-16/ARC; this is its standard check value.
	if got := CRC(0, []byte("123456789")); got != 0xBB3D {
		t.Errorf("CRC = %#04x, want 0xbb3d", got)
	}
}

func TestTime(t *testing.T) {
	// 2026-10-07 12:00 UTC is 1 175 515 200 Unix seconds; the FIT epoch is 631 065 600.
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if got := Time(at); got != at.Unix()-631065600 {
		t.Errorf("Time = %d", got)
	}
	if !FromTime(Time(at)).Equal(at) {
		t.Error("FromTime(Time(t)) != t")
	}
}

var testMsg = &Message{Num: 20, Local: 3, Fields: []Field{
	{253, Uint32}, {7, Uint16}, {3, Uint8}, {9, Sint16}, {2, Uint16}, {21, Uint16z},
}}

func encode(t *testing.T, rows ...[]int64) []byte {
	t.Helper()
	var body bytes.Buffer
	e := NewEncoder(&body)
	for _, r := range rows {
		if err := e.Write(testMsg, r...); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	if e.Written() != int64(body.Len()) {
		t.Errorf("Written %d, buffer %d", e.Written(), body.Len())
	}
	file := append(Header(uint32(body.Len())), body.Bytes()...)
	return binary.LittleEndian.AppendUint16(file, CRC(0, body.Bytes()))
}

func TestRoundTrip(t *testing.T) {
	b := encode(t,
		[]int64{1000, 250, 151, -350, Invalid, 7},
		[]int64{1001, 0xFFFF, 300, 0x7FFF, 0, 0}, // out of range or a marker: invalid
	)
	// Definition: 1+5+6*3 bytes; two data messages of 1+4+2+1+2+2+2.
	if want := HeaderSize + 24 + 2*14 + 2; len(b) != want {
		t.Errorf("file is %d bytes, want %d", len(b), want)
	}
	msgs, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Num != 20 {
		t.Fatalf("messages %+v", msgs)
	}
	want := map[uint8]int64{253: 1000, 7: 250, 3: 151, 9: -350, 21: 7}
	for k, v := range want {
		if got, ok := msgs[0].Get(k); !ok || got != v {
			t.Errorf("field %d = %d, %v; want %d", k, got, ok, v)
		}
	}
	if _, ok := msgs[0].Get(2); ok {
		t.Error("Invalid came back as a value")
	}
	if len(msgs[1].Fields) != 2 || msgs[1].Fields[2] != 0 { // timestamp and the uint16 zero
		t.Errorf("second message %v: markers and overflows should be absent", msgs[1].Fields)
	}

	b[HeaderSize+30] ^= 1
	if _, err := Decode(b); !errors.Is(err, ErrCRC) {
		t.Errorf("corrupt data: %v, want ErrCRC", err)
	}
}

func TestDecodePartial(t *testing.T) {
	full := encode(t, []int64{1, 2, 3, 4, 5, 6}, []int64{2, 2, 3, 4, 5, 6})
	body := full[HeaderSize : len(full)-2]
	partial := append(Header(0), body[:len(body)-5]...) // cut inside the second message
	msgs, end, err := DecodePartial(partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || end != HeaderSize+24+14 {
		t.Errorf("got %d messages ending at %d", len(msgs), end)
	}
	if _, err := Decode(partial); err == nil {
		t.Error("Decode accepted an unfinished file")
	}
}

func TestEncoderRedefines(t *testing.T) {
	other := &Message{Num: 21, Local: 3, Fields: []Field{{0, Enum}}}
	var body bytes.Buffer
	e := NewEncoder(&body)
	for _, m := range []*Message{testMsg, testMsg, other, testMsg} {
		vals := make([]int64, len(m.Fields))
		if err := e.Write(m, vals...); err != nil {
			t.Fatal(err)
		}
	}
	e.Flush()
	msgs, _, err := parse(body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	var nums []uint16
	for _, m := range msgs {
		nums = append(nums, m.Num)
	}
	if len(nums) != 4 || nums[2] != 21 || nums[3] != 20 {
		t.Errorf("messages %v: local type reuse needs a fresh definition", nums)
	}
	if err := e.Write(testMsg, 1); err == nil {
		t.Error("wrong value count accepted")
	}
}
