// Package ant implements ANT serial message framing and a minimal node that
// drives ANT channels on a USB stick.
//
// Constants follow "ANT Message Protocol and Usage" rev 5.1 (section 9.5),
// kept locally under _antdocs/ and not redistributable.
package ant

import (
	"bytes"
	"io"
	"sync/atomic"
)

// Sync is the first byte of every ANT serial message.
const Sync = 0xA4

// MaxPayload bounds the length byte the Decoder accepts. Standard and
// extended messages fit well inside it; a larger value means the decoder
// locked onto a 0xA4 that is not a real sync byte.
const MaxPayload = 64

// Message is one ANT serial message without sync, length and checksum.
type Message struct {
	ID   byte
	Data []byte
}

// Encode returns the framed message: SYNC LEN ID DATA... CHECKSUM.
func (m Message) Encode() []byte {
	b := make([]byte, 0, len(m.Data)+4)
	b = append(b, Sync, byte(len(m.Data)), m.ID)
	b = append(b, m.Data...)
	return append(b, checksum(b))
}

// Payload returns the 8-byte application payload of a broadcast,
// acknowledged or burst data message (Data[1:9]; Data[0] is the channel).
func (m Message) Payload() ([8]byte, bool) {
	var p [8]byte
	switch m.ID {
	case MsgBroadcastData, MsgAcknowledgedData, MsgBurstData:
	default:
		return p, false
	}
	if len(m.Data) < 9 {
		return p, false
	}
	copy(p[:], m.Data[1:9])
	return p, true
}

func checksum(b []byte) byte {
	var c byte
	for _, x := range b {
		c ^= x
	}
	return c
}

// Decoder reads framed messages from a byte stream. It resynchronises after
// garbage or corrupted frames instead of failing, and counts what it discards.
type Decoder struct {
	r   io.Reader
	buf []byte
	rd  []byte
	err error

	Dropped      atomic.Uint64 // bytes discarded while hunting for sync
	BadChecksums atomic.Uint64
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r, rd: make([]byte, 512)}
}

// Next returns the next valid message. Errors from the underlying reader are
// returned once all complete messages buffered before them are consumed.
func (d *Decoder) Next() (Message, error) {
	for {
		if m, ok := d.parse(); ok {
			return m, nil
		}
		if d.err != nil {
			return Message{}, d.err
		}
		n, err := d.r.Read(d.rd)
		d.buf = append(d.buf, d.rd[:n]...)
		d.err = err
	}
}

func (d *Decoder) parse() (Message, bool) {
	for {
		i := bytes.IndexByte(d.buf, Sync)
		if i < 0 {
			d.drop(len(d.buf))
			return Message{}, false
		}
		d.drop(i)
		if len(d.buf) < 2 {
			return Message{}, false
		}
		n := int(d.buf[1])
		if n > MaxPayload {
			d.drop(1)
			continue
		}
		total := n + 4
		if len(d.buf) < total {
			return Message{}, false
		}
		if checksum(d.buf[:total-1]) != d.buf[total-1] {
			d.BadChecksums.Add(1)
			d.drop(1)
			continue
		}
		m := Message{ID: d.buf[2], Data: bytes.Clone(d.buf[3 : total-1])}
		d.consume(total)
		return m, true
	}
}

func (d *Decoder) drop(n int) {
	if n == 0 {
		return
	}
	d.Dropped.Add(uint64(n))
	d.consume(n)
}

func (d *Decoder) consume(n int) {
	d.buf = d.buf[:copy(d.buf, d.buf[n:])]
}
