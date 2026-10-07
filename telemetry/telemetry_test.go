package telemetry

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digimago/osscycler/ant"
	"github.com/digimago/osscycler/fec"
)

func TestHubConflatesAndDiscards(t *testing.T) {
	h := NewHub()
	st, changed := h.Latest()
	if st.Seq != 0 {
		t.Fatalf("initial Seq = %d", st.Seq)
	}
	h.Update(func(s *State) bool { s.Trainer.PowerW = Some[uint16](100); return true })
	h.Update(func(s *State) bool { s.Trainer.PowerW = Some[uint16](200); return true })
	h.Update(func(*State) bool { return false }) // discarded
	select {
	case <-changed:
	default:
		t.Fatal("changed not closed after Update")
	}
	st, changed = h.Latest()
	if st.Seq != 2 || st.Trainer.PowerW.V != 200 {
		t.Fatalf("got Seq %d power %v, want 2 and 200", st.Seq, st.Trainer.PowerW)
	}
	select {
	case <-changed:
		t.Fatal("changed closed without a new Update")
	default:
	}
}

func TestRollover(t *testing.T) {
	var r rollover
	for _, v := range []uint8{250, 255, 3, 10} {
		r.add(v)
	}
	if r.total != 16 { // 5 + 4 + 7
		t.Fatalf("total = %d, want 16", r.total)
	}
}

// fakeStick plays the stick side of a net.Pipe: it acknowledges every
// command, answers channel ID requests and lets the test inject traffic.
type fakeStick struct {
	conn net.Conn
	mu   sync.Mutex
	ids  map[byte][]byte // channel -> device number LSB, MSB, type, trans type
	acks chan [8]byte    // payloads of acknowledged messages the node sent
}

func newFakeStick(t *testing.T) (*ant.Node, *fakeStick) {
	a, b := net.Pipe()
	f := &fakeStick{conn: b, acks: make(chan [8]byte, 32), ids: map[byte][]byte{
		trainerChannel: {0x94, 0xB9, fec.DeviceType, 5}, // 47508
		hrmChannel:     {0x39, 0x30, 120, 1},            // 12345
	}}
	go func() {
		dec := ant.NewDecoder(b)
		for {
			m, err := dec.Next()
			if err != nil {
				return
			}
			var replies []ant.Message
			switch m.ID {
			case ant.MsgResetSystem:
				replies = append(replies, ant.Message{ID: ant.MsgStartup, Data: []byte{0}})
			case ant.MsgRequest:
				replies = append(replies, ant.Message{ID: ant.MsgChannelID, Data: append([]byte{m.Data[0]}, f.ids[m.Data[0]]...)})
			case ant.MsgAcknowledgedData:
				var p [8]byte
				copy(p[:], m.Data[1:])
				select {
				case f.acks <- p:
				default:
				}
				replies = append(replies, ant.Message{ID: ant.MsgChannelEvent, Data: []byte{m.Data[0], 1, byte(ant.EventTransferTxCompleted)}})
			case ant.MsgCloseChannel:
				replies = append(replies,
					ant.Message{ID: ant.MsgChannelEvent, Data: []byte{m.Data[0], m.ID, 0}},
					ant.Message{ID: ant.MsgChannelEvent, Data: []byte{m.Data[0], 1, byte(ant.EventChannelClosed)}})
			default:
				replies = append(replies, ant.Message{ID: ant.MsgChannelEvent, Data: []byte{m.Data[0], m.ID, 0}})
			}
			for _, r := range replies {
				f.write(r)
			}
		}
	}()
	n := ant.NewNode(a, ant.Options{})
	t.Cleanup(func() { n.Close(); b.Close() })
	return n, f
}

func (f *fakeStick) write(m ant.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conn.Write(m.Encode())
}

func (f *fakeStick) broadcast(ch byte, p [8]byte) {
	f.write(ant.Message{ID: ant.MsgBroadcastData, Data: append([]byte{ch}, p[:]...)})
}

// waitFor polls the hub until cond holds or the deadline passes.
func waitFor(t *testing.T, hub *Hub, what string, cond func(State) bool) State {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		st, changed := hub.Latest()
		if cond(st) {
			return st
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatalf("timed out waiting for %s; state: %+v", what, st)
		}
	}
}

func TestRunPairsTrainerAndHRM(t *testing.T) {
	node, stick := newFakeStick(t)
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- NewService(node, hub, Config{StaleAfter: time.Hour}, log).Run(ctx) }()

	waitFor(t, hub, "both sensors searching", func(s State) bool {
		return s.Trainer.Sensor.Status == StatusSearching && s.HeartRate.Sensor.Status == StatusSearching
	})

	// Trainer: 250 W at 90 rpm, 8.333 m/s, in use.
	stick.broadcast(trainerChannel, [8]byte{0x19, 1, 90, 0, 0, 0xFA, 0x00, 0x30})
	stick.broadcast(trainerChannel, [8]byte{0x10, 25, 4, 10, 0x8D, 0x20, 0xFF, 0x34})
	// Current HR strap: page toggles, 142 bpm.
	stick.broadcast(hrmChannel, [8]byte{0x04, 0xFF, 0, 0, 0, 0, 1, 140})
	stick.broadcast(hrmChannel, [8]byte{0x84, 0xFF, 0, 0, 0, 0, 2, 142})

	st := waitFor(t, hub, "readings and device numbers", func(s State) bool {
		return s.Trainer.PowerW.OK && s.Trainer.SpeedMPS.OK && s.HeartRate.BPM.V == 142 &&
			s.Trainer.Sensor.DeviceNumber != 0 && s.HeartRate.Sensor.DeviceNumber != 0
	})
	if st.Trainer.Sensor.Status != StatusConnected || st.HeartRate.Sensor.Status != StatusConnected {
		t.Errorf("statuses: trainer %v, hrm %v", st.Trainer.Sensor.Status, st.HeartRate.Sensor.Status)
	}
	if st.Trainer.PowerW.V != 250 || st.Trainer.CadenceRPM.V != 90 || st.Trainer.State != fec.StateInUse {
		t.Errorf("trainer: %+v", st.Trainer)
	}
	if st.Trainer.Sensor.DeviceNumber != 47508 || st.HeartRate.Sensor.DeviceNumber != 12345 {
		t.Errorf("device numbers: trainer %d, hrm %d", st.Trainer.Sensor.DeviceNumber, st.HeartRate.Sensor.DeviceNumber)
	}
	if st.HeartRate.Legacy {
		t.Error("toggling strap reported as legacy")
	}

	// Losing the HR strap clears its reading.
	stick.write(ant.Message{ID: ant.MsgChannelEvent, Data: []byte{hrmChannel, 1, byte(ant.EventRxFailGoToSearch)}})
	st = waitFor(t, hub, "hrm lost", func(s State) bool { return s.HeartRate.Sensor.Status == StatusLost })
	if st.HeartRate.BPM.OK {
		t.Error("lost strap still reports a heart rate")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunWithoutHRM(t *testing.T) {
	node, _ := newFakeStick(t)
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- NewService(node, hub, Config{DisableHRM: true}, log).Run(ctx) }()
	waitFor(t, hub, "hrm disabled", func(s State) bool {
		return s.HeartRate.Sensor.Status == StatusDisabled && s.Trainer.Sensor.Status == StatusSearching
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestWatchStaleMarksLost(t *testing.T) {
	hub := NewHub()
	hub.Update(func(s *State) bool {
		s.Trainer.Sensor.Status = StatusConnected
		s.Trainer.PowerW = Some[uint16](123)
		return true
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchStale(ctx, hub, time.Millisecond)
	st := waitFor(t, hub, "trainer lost", func(s State) bool { return s.Trainer.Sensor.Status == StatusLost })
	if st.Trainer.PowerW.OK {
		t.Error("stale trainer still reports power")
	}
}

// nextAck returns the next acknowledged page with the given page number.
func (f *fakeStick) nextAck(t *testing.T, page byte) [8]byte {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case p := <-f.acks:
			if p[0] == page {
				return p
			}
		case <-timeout:
			t.Fatalf("no acknowledged page 0x%02X sent", page)
		}
	}
}

func TestSpinDownCalibration(t *testing.T) {
	node, stick := newFakeStick(t)
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(node, hub, Config{DisableHRM: true, StaleAfter: time.Hour}, log)
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	waitFor(t, hub, "trainer searching", func(s State) bool { return s.Trainer.Sensor.Status == StatusSearching })
	if err := svc.StartSpinDown(ctx); err != ErrTrainerUnavailable {
		t.Fatalf("StartSpinDown before pairing: err = %v, want ErrTrainerUnavailable", err)
	}

	stick.broadcast(trainerChannel, [8]byte{0x19, 1, 90, 0, 0, 0xFA, 0x20, 0x30}) // resistance cal required
	waitFor(t, hub, "trainer connected", func(s State) bool { return s.Trainer.Sensor.Status == StatusConnected })

	if err := svc.StartSpinDown(ctx); err != nil {
		t.Fatalf("StartSpinDown: %v", err)
	}
	if got, want := stick.nextAck(t, 0x01), fec.CalibrationRequest(true, false); got != want {
		t.Fatalf("request page % X, want % X", got, want)
	}
	if st, _ := hub.Latest(); st.Trainer.Calibration.Phase != CalRequested {
		t.Fatalf("phase %v, want requested", st.Trainer.Calibration.Phase)
	}
	if err := svc.StartSpinDown(ctx); err != ErrCalibrationActive {
		t.Fatalf("second StartSpinDown: err = %v, want ErrCalibrationActive", err)
	}

	// Speed too low, target 10 m/s.
	stick.broadcast(trainerChannel, [8]byte{0x02, 0x80, 0b0110_0000, 0x5C, 0x10, 0x27, 0xB8, 0x0B})
	st := waitFor(t, hub, "in progress", func(s State) bool { return s.Trainer.Calibration.Phase == CalInProgress })
	c := st.Trainer.Calibration
	if c.SpeedCondition != fec.ConditionTooLow || c.TargetSpeedMPS != Some(10.0) || c.TemperatureC != Some(21.0) {
		t.Errorf("progress: %+v", c)
	}

	// Result, sent three times as the profile asks.
	for range 3 {
		stick.broadcast(trainerChannel, [8]byte{0x01, 0x80, 0x00, 0xFF, 0xFF, 0xFF, 0xD2, 0x04})
	}
	st = waitFor(t, hub, "succeeded", func(s State) bool { return s.Trainer.Calibration.Phase == CalSucceeded })
	if st.Trainer.Calibration.SpinDownMS != Some[uint16](1234) {
		t.Errorf("spin-down time %v, want 1234 ms", st.Trainer.Calibration.SpinDownMS)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestCancelCalibration(t *testing.T) {
	node, stick := newFakeStick(t)
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(node, hub, Config{DisableHRM: true, StaleAfter: time.Hour}, log)
	go svc.Run(ctx)

	waitFor(t, hub, "trainer searching", func(s State) bool { return s.Trainer.Sensor.Status == StatusSearching })
	stick.broadcast(trainerChannel, [8]byte{0x19, 1, 90, 0, 0, 0xFA, 0x20, 0x30})
	waitFor(t, hub, "trainer connected", func(s State) bool { return s.Trainer.Sensor.Status == StatusConnected })
	if err := svc.StartSpinDown(ctx); err != nil {
		t.Fatal(err)
	}
	stick.nextAck(t, 0x01)
	if err := svc.CancelCalibration(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stick.nextAck(t, 0x01); got[1] != 0 {
		t.Errorf("cancel page byte 1 = %#x, want 0", got[1])
	}
	// A failure response after the cancel doesn't overwrite it.
	stick.broadcast(trainerChannel, [8]byte{0x01, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	stick.broadcast(trainerChannel, [8]byte{0x19, 2, 90, 0, 0, 0xFA, 0x20, 0x30})
	waitFor(t, hub, "still cancelled", func(s State) bool { return s.Trainer.Calibration.Phase == CalCancelled })
}

func TestFakeCalibration(t *testing.T) {
	hub := NewHub()
	f := NewFake(hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	waitFor(t, hub, "fake running", func(s State) bool { return s.Trainer.SpeedMPS.OK })
	if err := f.StartSpinDown(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, hub, "fake in progress", func(s State) bool { return s.Trainer.Calibration.Phase == CalInProgress })
	if err := f.CancelCalibration(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, hub, "fake cancelled", func(s State) bool { return s.Trainer.Calibration.Phase == CalCancelled })
	// A new calibration can start after a cancel.
	if err := f.StartSpinDown(ctx); err != nil {
		t.Fatalf("restart after cancel: %v", err)
	}
}

// TestCalibrationEndsWithoutResult replays the Flux 2: it ignores the
// cancel, keeps sending progress pages, then stops without a result.
func TestCalibrationEndsWithoutResult(t *testing.T) {
	old := progressTimeout
	progressTimeout = 300 * time.Millisecond
	defer func() { progressTimeout = old }()

	for _, cancelFirst := range []bool{true, false} {
		node, stick := newFakeStick(t)
		hub := NewHub()
		ctx, cancel := context.WithCancel(context.Background())
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		svc := NewService(node, hub, Config{DisableHRM: true, StaleAfter: time.Hour}, log)
		go svc.Run(ctx)

		waitFor(t, hub, "trainer searching", func(s State) bool { return s.Trainer.Sensor.Status == StatusSearching })
		stick.broadcast(trainerChannel, [8]byte{0x19, 1, 90, 0, 0, 0xFA, 0x20, 0x30})
		waitFor(t, hub, "trainer connected", func(s State) bool { return s.Trainer.Sensor.Status == StatusConnected })
		if err := svc.StartSpinDown(ctx); err != nil {
			t.Fatal(err)
		}
		// The page the Flux 2 actually sends: speed too low, target 32 km/h.
		progress := [8]byte{0x02, 0x80, 0x40, 0xFF, 0xB8, 0x22, 0xFF, 0xFF}
		stick.broadcast(trainerChannel, progress)
		waitFor(t, hub, "in progress", func(s State) bool { return s.Trainer.Calibration.Phase == CalInProgress })

		if cancelFirst {
			if err := svc.CancelCalibration(ctx); err != nil {
				t.Fatal(err)
			}
			stick.broadcast(trainerChannel, progress) // cancel ignored
			st := waitFor(t, hub, "cancel-ignored advice", func(s State) bool { return s.Trainer.Calibration.Message == cancelIgnored })
			if st.Trainer.Calibration.Phase != CalInProgress {
				t.Errorf("phase %v while the trainer still calibrates", st.Trainer.Calibration.Phase)
			}
		}

		// Progress pages stop; the watchdog ends the calibration.
		want := CalFailed
		if cancelFirst {
			want = CalCancelled
		}
		st := waitFor(t, hub, want.String(), func(s State) bool { return s.Trainer.Calibration.Phase == want })
		if !cancelFirst && st.Trainer.Calibration.Message == "" {
			t.Error("failure without a message")
		}
		cancel()
	}
}

func TestLogEvents(t *testing.T) {
	hub := NewHub()
	var buf syncBuffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { LogEvents(ctx, hub, log); close(done) }()

	step := func(f func(*State)) {
		hub.Update(func(s *State) bool { f(s); return true })
		time.Sleep(10 * time.Millisecond) // let the logger observe each step
	}
	step(func(s *State) {
		s.Trainer.Sensor.Status = StatusConnected
		s.Trainer.Flags = fec.ResistanceCalibrationRequired
	})
	step(func(s *State) { s.Trainer.Calibration.setPhase(CalRequested, s.Time, "") })
	step(func(s *State) {
		c := &s.Trainer.Calibration
		c.setPhase(CalInProgress, s.Time, "")
		c.SpeedCondition, c.TargetSpeedMPS = fec.ConditionTooLow, Some(8.888)
	})
	step(func(s *State) { s.Trainer.Calibration.SpeedCondition = fec.ConditionOK })
	step(func(s *State) { s.Trainer.PowerW = Some[uint16](0) }) // unrelated change: no log line
	step(func(s *State) {
		s.Trainer.Calibration.SpinDownMS = Some[uint16](2980)
		s.Trainer.Calibration.setPhase(CalSucceeded, s.Time, "")
		s.Trainer.Flags = 0
	})
	cancel()
	<-done

	want := []string{
		`level=INFO msg="trainer status" flags=resistance-cal-required`,
		`level=INFO msg=calibration phase=requested`,
		`level=INFO msg=calibration phase=in-progress speed=too-low target_kmh=32`,
		`level=INFO msg=calibration phase=in-progress speed=ok target_kmh=32`,
		`level=INFO msg=calibration phase=succeeded spin_down_ms=2980`,
		`level=INFO msg="trainer status" flags=ok`,
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// syncBuffer is a bytes.Buffer safe for a logger goroutine and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestFakeERG(t *testing.T) {
	hub := NewHub()
	f := NewFake(hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	f.SetTargetPower(ctx, 180)
	waitFor(t, hub, "fake holding 180 W", func(s State) bool {
		return s.Trainer.PowerW.OK && s.Trainer.PowerW.V >= 175 && s.Trainer.PowerW.V <= 185
	})
	f.SetGrade(ctx, 0) // back to simulation mode
	waitFor(t, hub, "fake leaves ERG", func(s State) bool {
		return s.Trainer.PowerW.OK && (s.Trainer.PowerW.V < 175 || s.Trainer.PowerW.V > 185)
	})
}
