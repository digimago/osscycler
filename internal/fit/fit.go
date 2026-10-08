// Package fit writes and reads the Garmin FIT file format: enough of it
// for activity files with numeric fields. It has no profile of its own;
// callers describe their messages with global message and field numbers
// from the FIT profile.
//
// Layout (FIT protocol 2.0): a 14-byte header, then definition and data
// messages with normal (not compressed-timestamp) headers, then a CRC-16
// over everything after the header. All values are little endian.
package fit

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// BaseType is a FIT base type number.
type BaseType uint8

const (
	Enum    BaseType = 0x00
	Sint8   BaseType = 0x01
	Uint8   BaseType = 0x02
	Sint16  BaseType = 0x83
	Uint16  BaseType = 0x84
	Sint32  BaseType = 0x85
	Uint32  BaseType = 0x86
	Uint8z  BaseType = 0x0A
	Uint16z BaseType = 0x8B
	Uint32z BaseType = 0x8C
)

// Size is the type's width in bytes, or 0 if this package doesn't
// support it.
func (t BaseType) Size() int {
	switch t {
	case Enum, Sint8, Uint8, Uint8z:
		return 1
	case Sint16, Uint16, Uint16z:
		return 2
	case Sint32, Uint32, Uint32z:
		return 4
	}
	return 0
}

func (t BaseType) signed() bool { return t == Sint8 || t == Sint16 || t == Sint32 }

// invalid is the raw value meaning "no value" for the type.
func (t BaseType) invalid() uint64 {
	switch t {
	case Uint8z, Uint16z, Uint32z:
		return 0
	case Sint8:
		return 0x7F
	case Sint16:
		return 0x7FFF
	case Sint32:
		return 0x7FFFFFFF
	}
	return 1<<(8*t.Size()) - 1
}

// Invalid as a value writes the type's "no value" marker.
const Invalid = math.MinInt64

// Field is one field of a message definition.
type Field struct {
	Num  uint8
	Type BaseType
}

// Message describes a message as written: its global number from the
// profile, the local message type (0-15) it is written under, and its
// fields in order.
type Message struct {
	Num    uint16
	Local  uint8
	Fields []Field
}

const (
	HeaderSize      = 14
	protocolVersion = 0x20          // 2.0
	profileVersion  = 21*1000 + 218 // 21.218, as the FIT SDK encodes it
)

// epoch is the FIT time origin, 1989-12-31 00:00:00 UTC.
var epoch = time.Date(1989, 12, 31, 0, 0, 0, 0, time.UTC)

// Time converts a time to a FIT date_time (seconds since the FIT epoch).
func Time(t time.Time) int64 { return int64(t.Sub(epoch) / time.Second) }

// FromTime converts a FIT date_time back.
func FromTime(v int64) time.Time { return epoch.Add(time.Duration(v) * time.Second) }

// Header returns a file header for dataSize bytes of messages.
func Header(dataSize uint32) []byte {
	h := make([]byte, HeaderSize)
	h[0] = HeaderSize
	h[1] = protocolVersion
	binary.LittleEndian.PutUint16(h[2:], profileVersion)
	binary.LittleEndian.PutUint32(h[4:], dataSize)
	copy(h[8:], ".FIT")
	binary.LittleEndian.PutUint16(h[12:], CRC(0, h[:12]))
	return h
}

// Encoder writes definition and data messages; the header and the final
// CRC are up to the caller (see Header and CRC), so a file can be
// written incrementally and finished later.
type Encoder struct {
	w       *bufio.Writer
	defined [16]*Message
	n       int64 // bytes written
}

func NewEncoder(w io.Writer) *Encoder { return &Encoder{w: bufio.NewWriter(w)} }

// Written is the number of bytes encoded so far, flushed or not.
func (e *Encoder) Written() int64 { return e.n }

// Flush writes buffered messages to the underlying writer.
func (e *Encoder) Flush() error { return e.w.Flush() }

// Write writes a data message with one value per field, preceded by the
// definition if the local type last held a different message. Signed
// fields take negative values as is; Invalid writes "no value"; values
// out of a field's range are written as invalid too.
func (e *Encoder) Write(m *Message, values ...int64) error {
	if len(values) != len(m.Fields) {
		return fmt.Errorf("fit: message %d: %d values for %d fields", m.Num, len(values), len(m.Fields))
	}
	if m.Local > 15 {
		return fmt.Errorf("fit: message %d: local type %d", m.Num, m.Local)
	}
	if e.defined[m.Local] != m {
		if err := e.define(m); err != nil {
			return err
		}
	}
	buf := []byte{m.Local}
	for i, f := range m.Fields {
		u := raw(f.Type, values[i])
		for k := range f.Type.Size() {
			buf = append(buf, byte(u>>(8*k)))
		}
	}
	return e.put(buf)
}

func (e *Encoder) define(m *Message) error {
	buf := []byte{0x40 | m.Local, 0, 0} // header, reserved, little endian
	buf = binary.LittleEndian.AppendUint16(buf, m.Num)
	buf = append(buf, byte(len(m.Fields)))
	for _, f := range m.Fields {
		if f.Type.Size() == 0 {
			return fmt.Errorf("fit: message %d field %d: unsupported base type %#x", m.Num, f.Num, f.Type)
		}
		buf = append(buf, f.Num, byte(f.Type.Size()), byte(f.Type))
	}
	if err := e.put(buf); err != nil {
		return err
	}
	e.defined[m.Local] = m
	return nil
}

func (e *Encoder) put(b []byte) error {
	n, err := e.w.Write(b)
	e.n += int64(n)
	return err
}

// raw encodes v for type t, or the invalid marker when v doesn't fit.
func raw(t BaseType, v int64) uint64 {
	bits := 8 * t.Size()
	if v == Invalid {
		return t.invalid()
	}
	if t.signed() {
		lo, hi := -int64(1)<<(bits-1), int64(1)<<(bits-1)-1
		if v < lo || v >= hi { // hi itself is the invalid marker
			return t.invalid()
		}
		return uint64(v) & (1<<bits - 1)
	}
	if v < 0 || uint64(v) > 1<<bits-1 || (uint64(v) == t.invalid() && t.invalid() != 0) {
		return t.invalid()
	}
	return uint64(v)
}

// CRC continues a FIT CRC-16 over b.
func CRC(crc uint16, b []byte) uint16 {
	table := [16]uint16{
		0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401,
		0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400,
	}
	for _, c := range b {
		tmp := table[crc&0xF]
		crc = (crc>>4)&0x0FFF ^ tmp ^ table[c&0xF]
		tmp = table[crc&0xF]
		crc = (crc>>4)&0x0FFF ^ tmp ^ table[c>>4]
	}
	return crc
}

// Msg is a decoded data message. Fields holds the values that are
// present (not invalid), signed types sign-extended.
type Msg struct {
	Num    uint16
	Fields map[uint8]int64
	Offset int // in the file, where the message (its data, not its definition) starts
}

// Get returns a field and whether it is present.
func (m Msg) Get(num uint8) (int64, bool) {
	v, ok := m.Fields[num]
	return v, ok
}

var (
	ErrHeader = errors.New("fit: bad header")
	ErrCRC    = errors.New("fit: CRC mismatch")
)

// Decode reads a complete file, checking the header, the data size and
// both CRCs.
func Decode(b []byte) ([]Msg, error) {
	size, err := header(b)
	if err != nil {
		return nil, err
	}
	end := HeaderSize + int(size)
	if size == 0 || len(b) < end+2 {
		return nil, fmt.Errorf("fit: file shorter than its header says (%d of %d bytes)", len(b), end+2)
	}
	if CRC(0, b[HeaderSize:end]) != binary.LittleEndian.Uint16(b[end:]) {
		return nil, ErrCRC
	}
	msgs, n, err := parse(b[HeaderSize:end])
	shift(msgs)
	if err == nil && n != end-HeaderSize {
		err = errors.New("fit: data ends inside a message")
	}
	return msgs, err
}

// DecodePartial reads an unfinished file (header data size 0, no CRC) as
// far as it holds complete messages. end is the offset just past the last
// complete message: where writing can resume.
func DecodePartial(b []byte) (msgs []Msg, end int, err error) {
	if _, err := header(b); err != nil {
		return nil, 0, err
	}
	msgs, n, _ := parse(b[HeaderSize:])
	shift(msgs)
	return msgs, HeaderSize + n, nil
}

func shift(msgs []Msg) {
	for i := range msgs {
		msgs[i].Offset += HeaderSize
	}
}

func header(b []byte) (uint32, error) {
	if len(b) < HeaderSize || b[0] != HeaderSize || string(b[8:12]) != ".FIT" {
		return 0, ErrHeader
	}
	if c := binary.LittleEndian.Uint16(b[12:]); c != 0 && c != CRC(0, b[:12]) {
		return 0, ErrHeader
	}
	return binary.LittleEndian.Uint32(b[4:]), nil
}

// parse decodes messages until data runs out or doesn't parse; n is the
// length of the complete messages.
func parse(data []byte) (msgs []Msg, n int, err error) {
	type def struct {
		num    uint16
		big    bool
		fields []Field
		sizes  []int
	}
	var defs [16]*def
	for n < len(data) {
		h := data[n]
		if h&0x80 != 0 {
			return msgs, n, errors.New("fit: compressed timestamp headers are not supported")
		}
		local := h & 0x0F
		p := n + 1
		if h&0x40 != 0 { // definition
			if p+5 > len(data) {
				return msgs, n, io.ErrUnexpectedEOF
			}
			d := &def{big: data[p+1] == 1}
			if d.big {
				d.num = binary.BigEndian.Uint16(data[p+2:])
			} else {
				d.num = binary.LittleEndian.Uint16(data[p+2:])
			}
			nf := int(data[p+4])
			p += 5
			if p+3*nf > len(data) {
				return msgs, n, io.ErrUnexpectedEOF
			}
			for i := range nf {
				d.fields = append(d.fields, Field{Num: data[p+3*i], Type: BaseType(data[p+3*i+2])})
				d.sizes = append(d.sizes, int(data[p+3*i+1]))
			}
			p += 3 * nf
			if h&0x20 != 0 { // developer fields
				if p >= len(data) {
					return msgs, n, io.ErrUnexpectedEOF
				}
				ndev := int(data[p])
				p++
				for range ndev {
					if p+3 > len(data) {
						return msgs, n, io.ErrUnexpectedEOF
					}
					d.fields = append(d.fields, Field{Num: 255, Type: 0xFF})
					d.sizes = append(d.sizes, int(data[p+1]))
					p += 3
				}
			}
			defs[local] = d
			n = p
			continue
		}
		d := defs[local]
		if d == nil {
			return msgs, n, fmt.Errorf("fit: data message for undefined local type %d", local)
		}
		m := Msg{Num: d.num, Fields: map[uint8]int64{}, Offset: n}
		for i, f := range d.fields {
			sz := d.sizes[i]
			if p+sz > len(data) {
				return msgs, n, io.ErrUnexpectedEOF
			}
			if f.Type.Size() == sz && f.Num != 255 {
				var u uint64
				for k := range sz {
					shift := 8 * k
					if d.big {
						shift = 8 * (sz - 1 - k)
					}
					u |= uint64(data[p+k]) << shift
				}
				if u != f.Type.invalid() {
					v := int64(u)
					if f.Type.signed() && u>>(8*sz-1) == 1 {
						v -= 1 << (8 * sz)
					}
					m.Fields[f.Num] = v
				}
			}
			p += sz
		}
		msgs = append(msgs, m)
		n = p
	}
	return msgs, n, nil
}
