package ant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

// CommandTimeout bounds how long a command waits for its response when the
// caller's context has no earlier deadline.
const CommandTimeout = 2 * time.Second

var (
	ErrClosed     = errors.New("ant: node closed")
	ErrTxFailed   = errors.New("ant: acknowledged transfer failed")
	ErrChannelUse = errors.New("ant: channel number already in use")
)

// Direction says which way a traced frame travelled.
type Direction int

const (
	Rx Direction = iota
	Tx
)

func (d Direction) String() string {
	if d == Tx {
		return "tx"
	}
	return "rx"
}

type Options struct {
	Logger *slog.Logger
	// Trace, if set, receives every valid frame read from and written to the
	// stick. It is called from the read loop and from senders concurrently.
	Trace func(dir Direction, frame []byte)
}

// Stats are cumulative transport counters.
type Stats struct {
	Rx, Tx       uint64 // valid messages
	DroppedBytes uint64 // bytes discarded while resynchronising
	BadChecksums uint64
	Overflows    uint64 // inbound channel messages dropped because the consumer was slow
	Channels     map[byte]ChannelStats
}

// ChannelStats count one open channel's inbound traffic.
type ChannelStats struct {
	Data     uint64               // broadcast, acknowledged and burst messages
	RFEvents map[EventCode]uint64 // e.g. EVENT_RX_FAIL for each missed message period
}

// Node drives one ANT stick over a byte-stream transport.
type Node struct {
	rw    io.ReadWriteCloser
	dec   *Decoder
	log   *slog.Logger
	trace func(Direction, []byte)

	wmu sync.Mutex // serialises writes

	mu       sync.Mutex
	waiters  map[*waiter]struct{}
	channels map[byte]*Channel
	chStats  map[byte]*ChannelStats // kept after a channel closes

	done chan struct{}
	err  error // read loop exit reason, valid once done is closed

	rx, tx, overflows atomic.Uint64
}

type waiter struct {
	match func(Message) bool
	c     chan Message
}

func NewNode(rw io.ReadWriteCloser, opts Options) *Node {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	n := &Node{
		rw:       rw,
		dec:      NewDecoder(rw),
		log:      log,
		trace:    opts.Trace,
		waiters:  make(map[*waiter]struct{}),
		channels: make(map[byte]*Channel),
		chStats:  make(map[byte]*ChannelStats),
		done:     make(chan struct{}),
	}
	go n.readLoop()
	return n
}

// Close closes the transport and waits for the read loop to stop.
func (n *Node) Close() error {
	err := n.rw.Close()
	<-n.done
	return err
}

// Done is closed when the read loop stops; Err then returns the reason.
func (n *Node) Done() <-chan struct{} { return n.done }

func (n *Node) Err() error {
	select {
	case <-n.done:
		return n.err
	default:
		return nil
	}
}

func (n *Node) Stats() Stats {
	st := Stats{
		Rx:           n.rx.Load(),
		Tx:           n.tx.Load(),
		DroppedBytes: n.dec.Dropped.Load(),
		BadChecksums: n.dec.BadChecksums.Load(),
		Overflows:    n.overflows.Load(),
		Channels:     make(map[byte]ChannelStats),
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch, cs := range n.chStats {
		st.Channels[ch] = ChannelStats{Data: cs.Data, RFEvents: maps.Clone(cs.RFEvents)}
	}
	return st
}

// Reset resets the stick and waits for its startup message.
func (n *Node) Reset(ctx context.Context) error {
	w := newWaiter(func(m Message) bool { return m.ID == MsgStartup })
	if _, err := n.exchange(ctx, Message{ID: MsgResetSystem, Data: []byte{0}}, w); err != nil {
		return fmt.Errorf("ant: reset: %w", err)
	}
	return nil
}

func (n *Node) SetNetworkKey(ctx context.Context, network byte, key NetworkKey) error {
	return n.command(ctx, MsgSetNetworkKey, append([]byte{network}, key[:]...)...)
}

// Request asks the stick for a channel-scoped message such as MsgChannelID
// and returns the reply.
func (n *Node) Request(ctx context.Context, ch, id byte) (Message, error) {
	w := newWaiter(func(m Message) bool {
		if m.ID == id && len(m.Data) > 0 && m.Data[0] == ch {
			return true
		}
		e, ok := ParseChannelEvent(m)
		return ok && e.Channel == ch && e.MsgID == MsgRequest
	})
	m, err := n.exchange(ctx, Message{ID: MsgRequest, Data: []byte{ch, id}}, w)
	if err != nil {
		return Message{}, fmt.Errorf("ant: request %s: %w", msgName(id), err)
	}
	if e, ok := ParseChannelEvent(m); ok && id != MsgChannelEvent {
		return Message{}, &ResponseError{Msg: MsgRequest, Code: e.Code}
	}
	return m, nil
}

// ChannelConfig describes a channel to assign and open.
type ChannelConfig struct {
	Number       byte
	Network      byte
	Type         ChannelType
	DeviceNumber uint16 // 0 = wildcard
	DeviceType   byte
	TransType    byte   // 0 = wildcard
	Period       uint16 // message rate is 32768/Period Hz
	RFFrequency  byte   // MHz above 2400
	// SearchTimeout is in 2.5 s units; 0xFF searches forever and 0 disables
	// high-priority search (it does not mean "default").
	SearchTimeout byte
	// LowPrioritySearchTimeout, if set, makes the channel search in low
	// priority first, which does not disturb other open channels, for this
	// many 2.5 s units (0xFF = forever, 0 = skip) before falling back to
	// high priority for SearchTimeout. Section 9.5.2.15.
	LowPrioritySearchTimeout *byte
}

// OpenChannel assigns, configures and opens a channel.
func (n *Node) OpenChannel(ctx context.Context, cfg ChannelConfig) (*Channel, error) {
	c := &Channel{node: n, num: cfg.Number, in: make(chan Message, 64)}
	if err := n.addChannel(c); err != nil {
		return nil, err
	}
	steps := []Message{
		{ID: MsgAssignChannel, Data: []byte{cfg.Number, byte(cfg.Type), cfg.Network}},
		{ID: MsgChannelID, Data: []byte{cfg.Number, byte(cfg.DeviceNumber), byte(cfg.DeviceNumber >> 8), cfg.DeviceType, cfg.TransType}},
		{ID: MsgSearchTimeout, Data: []byte{cfg.Number, cfg.SearchTimeout}},
	}
	if t := cfg.LowPrioritySearchTimeout; t != nil {
		steps = append(steps, Message{ID: MsgLowPrioritySearchTimeout, Data: []byte{cfg.Number, *t}})
	}
	steps = append(steps,
		Message{ID: MsgChannelPeriod, Data: []byte{cfg.Number, byte(cfg.Period), byte(cfg.Period >> 8)}},
		Message{ID: MsgRFFrequency, Data: []byte{cfg.Number, cfg.RFFrequency}},
		Message{ID: MsgOpenChannel, Data: []byte{cfg.Number}},
	)
	for _, s := range steps {
		if err := n.command(ctx, s.ID, s.Data...); err != nil {
			n.removeChannel(c)
			return nil, err
		}
	}
	return c, nil
}

// command sends a configuration message and waits for its channel response.
// data[0] is the channel (or network) number the response echoes.
func (n *Node) command(ctx context.Context, id byte, data ...byte) error {
	ch := data[0]
	w := newWaiter(func(m Message) bool {
		e, ok := ParseChannelEvent(m)
		return ok && e.Channel == ch && e.MsgID == id
	})
	m, err := n.exchange(ctx, Message{ID: id, Data: data}, w)
	if err != nil {
		return fmt.Errorf("ant: %s: %w", msgName(id), err)
	}
	if e, _ := ParseChannelEvent(m); e.Code != ResponseNoError {
		return &ResponseError{Msg: id, Code: e.Code}
	}
	return nil
}

func newWaiter(match func(Message) bool) *waiter {
	return &waiter{match: match, c: make(chan Message, 1)}
}

// exchange registers w, sends out and waits for the message w matches.
func (n *Node) exchange(ctx context.Context, out Message, w *waiter) (Message, error) {
	n.register(w)
	if err := n.send(out); err != nil {
		n.unregister(w)
		return Message{}, err
	}
	return n.await(ctx, w)
}

func (n *Node) await(ctx context.Context, w *waiter) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	select {
	case m := <-w.c:
		return m, nil
	case <-ctx.Done():
		n.unregister(w)
		return Message{}, ctx.Err()
	case <-n.done:
		return Message{}, fmt.Errorf("%w: %v", ErrClosed, n.err)
	}
}

func (n *Node) send(m Message) error {
	frame := m.Encode()
	n.wmu.Lock()
	defer n.wmu.Unlock()
	if n.trace != nil {
		n.trace(Tx, frame)
	}
	if _, err := n.rw.Write(frame); err != nil {
		return fmt.Errorf("ant: write: %w", err)
	}
	n.tx.Add(1)
	return nil
}

func (n *Node) register(w *waiter) {
	n.mu.Lock()
	n.waiters[w] = struct{}{}
	n.mu.Unlock()
}

func (n *Node) unregister(w *waiter) {
	n.mu.Lock()
	delete(n.waiters, w)
	n.mu.Unlock()
}

func (n *Node) addChannel(c *Channel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.channels == nil {
		return ErrClosed
	}
	if _, ok := n.channels[c.num]; ok {
		return ErrChannelUse
	}
	n.channels[c.num] = c
	return nil
}

func (n *Node) removeChannel(c *Channel) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.channels != nil && n.channels[c.num] == c {
		delete(n.channels, c.num)
		close(c.in)
	}
}

func (n *Node) readLoop() {
	defer func() {
		n.mu.Lock()
		for _, c := range n.channels {
			close(c.in)
		}
		n.channels = nil
		n.mu.Unlock()
		close(n.done)
	}()
	for {
		m, err := n.dec.Next()
		if err != nil {
			n.err = err
			return
		}
		n.rx.Add(1)
		if n.trace != nil {
			n.trace(Rx, m.Encode())
		}
		if n.wake(m) {
			continue
		}
		n.route(m)
	}
}

// wake hands m to the first waiter that matches it.
func (n *Node) wake(m Message) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for w := range n.waiters {
		if w.match(m) {
			delete(n.waiters, w)
			w.c <- m
			return true
		}
	}
	return false
}

// route delivers channel traffic to its Channel without blocking the read loop.
func (n *Node) route(m Message) {
	var ch byte
	switch m.ID {
	case MsgBroadcastData, MsgAcknowledgedData, MsgChannelEvent:
		if len(m.Data) == 0 {
			return
		}
		ch = m.Data[0]
	case MsgBurstData:
		if len(m.Data) == 0 {
			return
		}
		ch = m.Data[0] & 0x1F // upper bits carry the burst sequence number
	default:
		n.log.Debug("unhandled ANT message", "id", msgName(m.ID), "data", fmt.Sprintf("% X", m.Data))
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	c := n.channels[ch]
	if c == nil {
		n.log.Debug("message for unknown channel", "channel", ch, "id", msgName(m.ID))
		return
	}
	cs := n.chStats[ch]
	if cs == nil {
		cs = &ChannelStats{RFEvents: make(map[EventCode]uint64)}
		n.chStats[ch] = cs
	}
	if e, ok := ParseChannelEvent(m); ok {
		if e.IsRF() {
			cs.RFEvents[e.Code]++
		}
	} else {
		cs.Data++
	}
	select {
	case c.in <- m:
	default:
		n.overflows.Add(1)
	}
}

// Channel is an open ANT channel.
type Channel struct {
	node  *Node
	num   byte
	in    chan Message
	ackMu sync.Mutex
}

func (c *Channel) Number() byte { return c.num }

// Messages delivers data messages and channel events not consumed as command
// responses. It is closed when the channel is closed or the node stops.
func (c *Channel) Messages() <-chan Message { return c.in }

// ChannelID identifies the device a channel is paired with.
type ChannelID struct {
	DeviceNumber uint16
	DeviceType   byte // bit 7 is the pairing bit
	TransType    byte
}

// ID asks the stick which device the channel is talking to. After a wildcard
// search this reports the device that was found.
func (c *Channel) ID(ctx context.Context) (ChannelID, error) {
	m, err := c.node.Request(ctx, c.num, MsgChannelID)
	if err != nil {
		return ChannelID{}, err
	}
	if len(m.Data) < 5 {
		return ChannelID{}, fmt.Errorf("ant: short channel ID reply: % X", m.Data)
	}
	return ChannelID{
		DeviceNumber: uint16(m.Data[1]) | uint16(m.Data[2])<<8,
		DeviceType:   m.Data[3],
		TransType:    m.Data[4],
	}, nil
}

// SendAcknowledged sends an 8-byte payload and waits until the device
// acknowledges it (nil) or the transfer fails (ErrTxFailed). Only one
// acknowledged transfer per channel is in flight at a time.
func (c *Channel) SendAcknowledged(ctx context.Context, payload [8]byte) error {
	c.ackMu.Lock()
	defer c.ackMu.Unlock()
	w := newWaiter(func(m Message) bool {
		e, ok := ParseChannelEvent(m)
		if !ok || e.Channel != c.num {
			return false
		}
		if e.IsRF() {
			return e.Code == EventTransferTxCompleted || e.Code == EventTransferTxFailed
		}
		return e.MsgID == MsgAcknowledgedData && e.Code != ResponseNoError
	})
	m, err := c.node.exchange(ctx, Message{ID: MsgAcknowledgedData, Data: append([]byte{c.num}, payload[:]...)}, w)
	if err != nil {
		return fmt.Errorf("ant: acknowledged data: %w", err)
	}
	e, _ := ParseChannelEvent(m)
	switch {
	case !e.IsRF():
		return &ResponseError{Msg: MsgAcknowledgedData, Code: e.Code}
	case e.Code == EventTransferTxFailed:
		return ErrTxFailed
	}
	return nil
}

// Close closes and unassigns the channel.
func (c *Channel) Close(ctx context.Context) error {
	n := c.node
	defer n.removeChannel(c)
	closed := newWaiter(func(m Message) bool {
		e, ok := ParseChannelEvent(m)
		return ok && e.Channel == c.num && e.IsRF() && e.Code == EventChannelClosed
	})
	n.register(closed)
	if err := n.command(ctx, MsgCloseChannel, c.num); err != nil {
		n.unregister(closed)
		return err
	}
	if _, err := n.await(ctx, closed); err != nil {
		return fmt.Errorf("ant: waiting for channel close: %w", err)
	}
	return n.command(ctx, MsgUnassignChannel, c.num)
}
