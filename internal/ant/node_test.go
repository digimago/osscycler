package ant

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// fakeStick answers commands the way an ANT stick does, on the far end of a
// net.Pipe. The handler returns the replies for each message it receives.
type fakeStick struct {
	t    *testing.T
	conn net.Conn
	got  chan Message
}

func newFakeStick(t *testing.T, handle func(Message) []Message) (*Node, *fakeStick) {
	t.Helper()
	a, b := net.Pipe()
	s := &fakeStick{t: t, conn: b, got: make(chan Message, 64)}
	go func() {
		dec := NewDecoder(b)
		for {
			m, err := dec.Next()
			if err != nil {
				return
			}
			s.got <- m
			for _, r := range handle(m) {
				if _, err := b.Write(r.Encode()); err != nil {
					return
				}
			}
		}
	}()
	n := NewNode(a, Options{})
	t.Cleanup(func() { n.Close(); b.Close() })
	return n, s
}

// push sends an unsolicited message from the stick to the node.
func (s *fakeStick) push(m Message) {
	if _, err := s.conn.Write(m.Encode()); err != nil {
		s.t.Fatal(err)
	}
}

func ok(m Message) []Message {
	return []Message{{ID: MsgChannelEvent, Data: []byte{m.Data[0], m.ID, byte(ResponseNoError)}}}
}

func TestOpenChannelSequence(t *testing.T) {
	n, s := newFakeStick(t, func(m Message) []Message {
		if m.ID == MsgResetSystem {
			return []Message{{ID: MsgStartup, Data: []byte{0}}}
		}
		return ok(m)
	})
	ctx := context.Background()
	if err := n.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.SetNetworkKey(ctx, 0, NetworkKey{1, 2, 3, 4, 5, 6, 7, 8}); err != nil {
		t.Fatal(err)
	}
	lowPriority := byte(0xFF)
	cfg := ChannelConfig{Number: 0, DeviceType: 17, Period: 8192, RFFrequency: 57, SearchTimeout: 0, DeviceNumber: 0x1234,
		LowPrioritySearchTimeout: &lowPriority}
	c, err := n.OpenChannel(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	want := []Message{
		{ID: MsgResetSystem, Data: []byte{0}},
		{ID: MsgSetNetworkKey, Data: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		{ID: MsgAssignChannel, Data: []byte{0, 0x00, 0}},
		{ID: MsgChannelID, Data: []byte{0, 0x34, 0x12, 17, 0}},
		{ID: MsgSearchTimeout, Data: []byte{0, 0}},
		{ID: MsgLowPrioritySearchTimeout, Data: []byte{0, 0xFF}},
		{ID: MsgChannelPeriod, Data: []byte{0, 0x00, 0x20}},
		{ID: MsgRFFrequency, Data: []byte{0, 57}},
		{ID: MsgOpenChannel, Data: []byte{0}},
	}
	for i, w := range want {
		g := <-s.got
		if g.ID != w.ID || !bytes.Equal(g.Data, w.Data) {
			t.Fatalf("command %d = %s % X, want %s % X", i, msgName(g.ID), g.Data, msgName(w.ID), w.Data)
		}
	}

	// Broadcast data and RF events are routed to the channel.
	bcast := Message{ID: MsgBroadcastData, Data: []byte{0, 0x19, 1, 2, 3, 4, 5, 6, 7}}
	s.push(bcast)
	s.push(Message{ID: MsgChannelEvent, Data: []byte{0, msgRFEvent, byte(EventRxFail)}})
	if g := <-c.Messages(); g.ID != MsgBroadcastData {
		t.Fatalf("got %s, want broadcast", msgName(g.ID))
	}
	if e, _ := ParseChannelEvent(<-c.Messages()); e.Code != EventRxFail {
		t.Fatalf("got %v, want EVENT_RX_FAIL", e.Code)
	}
}

func TestCommandRejected(t *testing.T) {
	n, _ := newFakeStick(t, func(m Message) []Message {
		return []Message{{ID: MsgChannelEvent, Data: []byte{m.Data[0], m.ID, byte(ChannelInWrongState)}}}
	})
	_, err := n.OpenChannel(context.Background(), ChannelConfig{})
	var re *ResponseError
	if !errors.As(err, &re) || re.Msg != MsgAssignChannel || re.Code != ChannelInWrongState {
		t.Fatalf("err = %v, want AssignChannel CHANNEL_IN_WRONG_STATE", err)
	}
	// The failed channel number is free again.
	if err := n.addChannel(&Channel{num: 0, in: make(chan Message)}); err != nil {
		t.Fatalf("channel 0 still reserved: %v", err)
	}
}

func TestSendAcknowledged(t *testing.T) {
	fail := true
	n, _ := newFakeStick(t, func(m Message) []Message {
		switch m.ID {
		case MsgAcknowledgedData:
			code := EventTransferTxCompleted
			if fail {
				code = EventTransferTxFailed
				fail = false
			}
			// Interleave unrelated traffic before the result, as the radio does.
			return []Message{
				{ID: MsgBroadcastData, Data: []byte{0, 0x10, 0, 0, 0, 0, 0, 0, 0}},
				{ID: MsgChannelEvent, Data: []byte{0, msgRFEvent, byte(code)}},
			}
		}
		return ok(m)
	})
	ctx := context.Background()
	c, err := n.OpenChannel(ctx, ChannelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendAcknowledged(ctx, [8]byte{0x33}); !errors.Is(err, ErrTxFailed) {
		t.Fatalf("first send: err = %v, want ErrTxFailed", err)
	}
	if err := c.SendAcknowledged(ctx, [8]byte{0x33}); err != nil {
		t.Fatalf("second send: %v", err)
	}
}

func TestChannelIDAndClose(t *testing.T) {
	n, _ := newFakeStick(t, func(m Message) []Message {
		switch m.ID {
		case MsgRequest:
			return []Message{{ID: MsgChannelID, Data: []byte{m.Data[0], 0xCD, 0xAB, 17, 5}}}
		case MsgCloseChannel:
			return append(ok(m), Message{ID: MsgChannelEvent, Data: []byte{m.Data[0], msgRFEvent, byte(EventChannelClosed)}})
		}
		return ok(m)
	})
	ctx := context.Background()
	c, err := n.OpenChannel(ctx, ChannelConfig{Number: 2})
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.ID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id != (ChannelID{DeviceNumber: 0xABCD, DeviceType: 17, TransType: 5}) {
		t.Fatalf("ID = %+v", id)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, open := <-c.Messages(); open {
		t.Fatal("Messages still open after Close")
	}
}

func TestCommandTimeoutAndNodeClose(t *testing.T) {
	n, _ := newFakeStick(t, func(Message) []Message { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := n.Reset(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	n.Close()
	if err := n.Reset(context.Background()); err == nil {
		t.Fatal("Reset after Close succeeded")
	}
}

func TestChannelStats(t *testing.T) {
	n, s := newFakeStick(t, ok)
	ctx := context.Background()
	c, err := n.OpenChannel(ctx, ChannelConfig{Number: 0, DeviceType: 17, Period: 8192, RFFrequency: 57})
	if err != nil {
		t.Fatal(err)
	}
	data := Message{ID: MsgBroadcastData, Data: []byte{0, 0x10, 0, 0, 0, 0, 0, 0, 0}}
	rxFail := Message{ID: MsgChannelEvent, Data: []byte{0, 0x01, byte(EventRxFail)}}
	for _, m := range []Message{data, data, rxFail, data, rxFail} {
		s.push(m)
	}
	for range 5 {
		<-c.Messages()
	}
	st := n.Stats().Channels[0]
	if st.Data != 3 || st.RFEvents[EventRxFail] != 2 || len(st.RFEvents) != 1 {
		t.Errorf("channel stats %+v", st)
	}
	// Command responses on the channel are not RF events.
	if _, ok := n.Stats().Channels[0].RFEvents[ResponseNoError]; ok {
		t.Error("a command response counted as an RF event")
	}
}
