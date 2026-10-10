package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/fec"
	"github.com/digimago/osscycler/internal/hrm"
)

// ChannelNames names the ANT channels the service opens, by number.
var ChannelNames = map[byte]string{trainerChannel: "trainer", hrmChannel: "hrm"}

const (
	trainerChannel = 0
	hrmChannel     = 1
	ackAttempts    = 3
)

type Config struct {
	NetworkKey    ant.NetworkKey
	TrainerDevice uint16 // 0 = pair with the first trainer found
	HRMDevice     uint16 // 0 = pair with the first heart rate monitor found
	DisableHRM    bool
	// User is sent to the trainer after pairing if UserWeightKg is set.
	User fec.UserConfig
	// StaleAfter marks a connected sensor lost when nothing arrives for this
	// long. Default 5 s.
	StaleAfter time.Duration
}

var (
	ErrTrainerUnavailable = errors.New("telemetry: trainer not connected")
	ErrCalibrationActive  = errors.New("telemetry: a calibration is already running")
)

// requestTimeout fails a calibration request the trainer never starts.
const requestTimeout = 10 * time.Second

// progressTimeout ends a calibration whose in-progress pages stopped
// without a result page. The Flux 2 does this when it gives up on a
// spin-down, about a minute after the request. A var for tests.
var progressTimeout = 5 * time.Second

// cancelIgnored is shown when the trainer keeps calibrating after a cancel;
// the Flux 2 (firmware 4.503) acknowledges the cancel page but ignores it.
const cancelIgnored = "the trainer ignored the cancel: finish the spin-down, or wait about a minute for it to give up"

// Calibrator starts and cancels trainer calibrations. Both the ANT service
// and the fake implement it.
type Calibrator interface {
	StartSpinDown(ctx context.Context) error
	CancelCalibration(ctx context.Context) error
}

// Service drives the ANT stick: it tracks the trainer and heart rate monitor
// in a Hub and sends trainer commands.
type Service struct {
	node *ant.Node
	hub  *Hub
	cfg  Config
	log  *slog.Logger

	mu      sync.Mutex
	trainer *ant.Channel // nil until open
}

func NewService(node *ant.Node, hub *Hub, cfg Config, log *slog.Logger) *Service {
	if cfg.StaleAfter == 0 {
		cfg.StaleAfter = 5 * time.Second
	}
	return &Service{node: node, hub: hub, cfg: cfg, log: log}
}

// Run configures the stick, opens the trainer and heart rate channels and
// feeds the hub until ctx is cancelled (returns nil) or the stick goes away.
//
// The trainer searches at high priority. The heart rate monitor searches
// in low priority forever, which per the ANT protocol does not interrupt the
// trainer channel, so a strap put on mid-ride is still picked up.
func (s *Service) Run(ctx context.Context) error {
	s.mu.Lock() // SetUser changes cfg.User meanwhile
	node, hub, cfg, log := s.node, s.hub, s.cfg, s.log
	s.mu.Unlock()
	if err := node.Reset(ctx); err != nil {
		log.Warn("no startup message after reset", "err", err)
	}
	if err := node.SetNetworkKey(ctx, 0, cfg.NetworkKey); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		channels []*ant.Channel
	)

	tch, err := node.OpenChannel(ctx, ant.ChannelConfig{
		Number:        trainerChannel,
		DeviceNumber:  cfg.TrainerDevice,
		DeviceType:    fec.DeviceType,
		Period:        fec.Period,
		RFFrequency:   fec.RFFrequency,
		SearchTimeout: 0xFF,
	})
	if err != nil {
		return fmt.Errorf("trainer channel: %w", err)
	}
	channels = append(channels, tch)
	s.mu.Lock()
	s.trainer = tch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.trainer = nil
		s.mu.Unlock()
	}()
	trainer := newTrainerLoop(tch, hub, s.user, log.With("sensor", "trainer"))
	setStatus(hub, trainer.sensor, StatusSearching)
	wg.Go(func() { trainer.run(ctx) })

	hrmSensor := func(s *State) *Sensor { return &s.HeartRate.Sensor }
	if cfg.DisableHRM {
		setStatus(hub, hrmSensor, StatusDisabled)
	} else {
		lowPriorityForever := byte(0xFF)
		hch, err := node.OpenChannel(ctx, ant.ChannelConfig{
			Number:                   hrmChannel,
			DeviceNumber:             cfg.HRMDevice,
			DeviceType:               hrm.DeviceType,
			Period:                   hrm.Period,
			RFFrequency:              hrm.RFFrequency,
			SearchTimeout:            0, // no high-priority search
			LowPrioritySearchTimeout: &lowPriorityForever,
		})
		if err != nil {
			log.Warn("heart rate channel unavailable", "err", err)
			setStatus(hub, hrmSensor, StatusDisabled)
		} else {
			channels = append(channels, hch)
			h := newHRMLoop(hch, hub, log.With("sensor", "hrm"))
			setStatus(hub, h.sensor, StatusSearching)
			wg.Go(func() { h.run(ctx) })
		}
	}
	wg.Go(func() { watchStale(ctx, hub, cfg.StaleAfter) })

	var runErr error
	select {
	case <-ctx.Done():
	case <-node.Done():
		runErr = fmt.Errorf("ANT stick stopped: %w", node.Err())
	}
	cancel()
	wg.Wait()
	if runErr == nil {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancelClose()
		for _, ch := range channels {
			if err := ch.Close(closeCtx); err != nil {
				log.Warn("closing channel", "channel", ch.Number(), "err", err)
			}
		}
	}
	return runErr
}

// user is the rider's config as it is now; it changes with SetUser.
func (s *Service) user() fec.UserConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.User
}

// SetUser changes the rider's weight and bike for the trainer (FE page
// 0x37): sent now if the trainer is connected, and after every pairing.
func (s *Service) SetUser(ctx context.Context, u fec.UserConfig) error {
	s.mu.Lock()
	s.cfg.User = u
	ch := s.trainer
	s.mu.Unlock()
	st, _ := s.hub.Latest()
	if ch == nil || st.Trainer.Sensor.Status != StatusConnected || u.UserWeightKg <= 0 {
		return nil // sent at the next pairing
	}
	if err := sendAck(ctx, ch, s.log, "user config", u.Page()); err != nil {
		return err
	}
	s.log.Info("user config sent", "rider_kg", u.UserWeightKg, "bike_kg", u.BikeWeightKg)
	return nil
}

func (s *Service) trainerChannel() *ant.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trainer
}

// StartSpinDown asks the trainer for a spin-down calibration. It returns
// once the trainer has acknowledged the request; progress and the result
// arrive in State.Trainer.Calibration.
func (s *Service) StartSpinDown(ctx context.Context) error {
	ch := s.trainerChannel()
	st, _ := s.hub.Latest()
	switch {
	case ch == nil || st.Trainer.Sensor.Status != StatusConnected:
		return ErrTrainerUnavailable
	case st.Trainer.Calibration.Phase.Active():
		return ErrCalibrationActive
	}
	s.hub.Update(func(st *State) bool {
		st.Trainer.Calibration = Calibration{}
		st.Trainer.Calibration.setPhase(CalRequested, st.Time, "")
		return true
	})
	s.log.Info("spin-down calibration requested")
	if err := sendAck(ctx, ch, s.log, "calibration request", fec.CalibrationRequest(true, false)); err != nil {
		s.hub.Update(func(st *State) bool {
			st.Trainer.Calibration.setPhase(CalFailed, st.Time, "trainer did not receive the request")
			return true
		})
		return err
	}
	return nil
}

// CancelCalibration asks the trainer to abandon a calibration in progress.
func (s *Service) CancelCalibration(ctx context.Context) error {
	ch := s.trainerChannel()
	if ch == nil {
		return ErrTrainerUnavailable
	}
	if err := sendAck(ctx, ch, s.log, "calibration cancel", fec.CalibrationRequest(false, false)); err != nil {
		return err
	}
	// Only a request the trainer hasn't started is cancelled outright; a
	// running calibration ends when its progress pages stop (watchStale).
	s.hub.Update(func(st *State) bool {
		c := &st.Trainer.Calibration
		if !c.Phase.Active() {
			return false
		}
		c.CancelRequested = true
		if c.Phase == CalRequested {
			c.setPhase(CalCancelled, st.Time, "")
		}
		return true
	})
	s.log.Info("calibration cancel sent")
	return nil
}

// SetGrade puts the trainer in simulation mode at gradePct (FE page 0x33,
// trainer-default rolling resistance).
func (s *Service) SetGrade(ctx context.Context, gradePct float64) error {
	ch := s.trainerChannel()
	st, _ := s.hub.Latest()
	switch {
	case ch == nil || st.Trainer.Sensor.Status != StatusConnected:
		return ErrTrainerUnavailable
	case st.Trainer.Calibration.Phase.Active():
		return ErrCalibrationActive
	}
	return sendAck(ctx, ch, s.log, "track resistance", fec.TrackResistance(gradePct, 0))
}

// SetResistance sets a fixed brake level, percent of maximum (FE page
// 0x30, basic resistance).
func (s *Service) SetResistance(ctx context.Context, pct float64) error {
	ch := s.trainerChannel()
	st, _ := s.hub.Latest()
	switch {
	case ch == nil || st.Trainer.Sensor.Status != StatusConnected:
		return ErrTrainerUnavailable
	case st.Trainer.Calibration.Phase.Active():
		return ErrCalibrationActive
	}
	return sendAck(ctx, ch, s.log, "basic resistance", fec.BasicResistance(pct))
}

// SetTargetPower puts the trainer in ERG mode at watts (FE page 0x31).
func (s *Service) SetTargetPower(ctx context.Context, watts float64) error {
	ch := s.trainerChannel()
	st, _ := s.hub.Latest()
	switch {
	case ch == nil || st.Trainer.Sensor.Status != StatusConnected:
		return ErrTrainerUnavailable
	case st.Trainer.Calibration.Phase.Active():
		return ErrCalibrationActive
	}
	return sendAck(ctx, ch, s.log, "target power", fec.TargetPower(watts))
}

func setStatus(hub *Hub, sensor func(*State) *Sensor, st SensorStatus) {
	hub.Update(func(s *State) bool {
		sensor(s).Status = st
		return true
	})
}

// sensorLoop is the part of channel handling shared by all sensors.
type sensorLoop struct {
	ch     *ant.Channel
	hub    *Hub
	log    *slog.Logger
	sensor func(*State) *Sensor
	// apply decodes a payload into the state. Only the loop goroutine calls
	// it, so it may keep decoder state.
	apply func(p [8]byte, s *State)
	// onPair runs in its own goroutine after the first data arrives, and
	// again whenever the sensor comes back after being lost: a trainer
	// that reset meanwhile needs the user config again (owner, 2026-10-10:
	// the Flux dropped out for 5 s, came back asking for it, and had no
	// rider weight until something else sent it 30 s later).
	onPair func(ctx context.Context)
}

func (l *sensorLoop) run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()
	paired := false
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-l.ch.Messages():
			if !ok {
				return
			}
			if p, ok := m.Payload(); ok {
				first := !paired
				if first {
					paired = true
					wg.Go(func() { l.pair(ctx) })
				}
				if l.data(p) && !first && l.onPair != nil {
					wg.Go(func() { l.onPair(ctx) }) // back after being lost
				}
				continue
			}
			if e, ok := ant.ParseChannelEvent(m); ok && e.IsRF() {
				switch e.Code {
				case ant.EventRxFailGoToSearch:
					l.lost("signal lost, searching again")
				case ant.EventChannelClosed:
					// Only happens if a search times out; ours don't.
					l.lost("channel closed by stick")
				}
			}
		}
	}
}

// data takes a payload; whether the sensor was not connected before.
func (l *sensorLoop) data(p [8]byte) bool {
	var reconnected bool
	l.hub.Update(func(s *State) bool {
		sn := l.sensor(s)
		reconnected = sn.Status != StatusConnected
		sn.Status = StatusConnected
		sn.LastSeen = s.Time
		l.apply(p, s)
		return true
	})
	if reconnected {
		l.log.Info("connected")
	}
	return reconnected
}

func (l *sensorLoop) lost(why string) {
	l.hub.Update(func(s *State) bool { return markLost(s, l.sensor(s)) })
	l.log.Warn(why)
}

func (l *sensorLoop) pair(ctx context.Context) {
	id, err := l.ch.ID(ctx)
	if err != nil {
		l.log.Warn("reading channel ID", "err", err)
	} else {
		l.hub.Update(func(s *State) bool {
			l.sensor(s).DeviceNumber = id.DeviceNumber
			return true
		})
		l.log.Info("paired", "device", id.DeviceNumber, "trans_type", id.TransType)
	}
	if l.onPair != nil {
		l.onPair(ctx)
	}
}

// sendAck sends an acknowledged page, retrying when the radio misses it.
func sendAck(ctx context.Context, ch *ant.Channel, log *slog.Logger, what string, p [8]byte) error {
	var err error
	for range ackAttempts {
		if err = ch.SendAcknowledged(ctx, p); err == nil || ctx.Err() != nil {
			break
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Warn("send failed", "page", what, "err", err)
	}
	return err
}

// markLost flips a connected sensor to lost and clears its readings so no
// renderer shows stale numbers. It reports whether anything changed.
func markLost(s *State, sn *Sensor) bool {
	if sn.Status != StatusConnected {
		return false
	}
	sn.Status = StatusLost
	switch sn {
	case &s.Trainer.Sensor:
		s.Trainer.PowerW, s.Trainer.CadenceRPM = Opt[uint16]{}, Opt[uint8]{}
		s.Trainer.SpeedMPS, s.Trainer.HeartRateBPM = Opt[float64]{}, Opt[uint8]{}
		if s.Trainer.Calibration.Phase.Active() {
			s.Trainer.Calibration.setPhase(CalFailed, s.Time, "lost contact with the trainer")
		}
	case &s.HeartRate.Sensor:
		s.HeartRate.BPM = Opt[uint8]{}
	}
	return true
}

func watchStale(ctx context.Context, hub *Hub, after time.Duration) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		hub.Update(func(s *State) bool {
			changed := false
			for _, sn := range []*Sensor{&s.Trainer.Sensor, &s.HeartRate.Sensor} {
				if sn.Status == StatusConnected && s.Time-sn.LastSeen > after {
					changed = markLost(s, sn) || changed
				}
			}
			switch c := &s.Trainer.Calibration; {
			case c.Phase == CalRequested && s.Time-c.Changed > requestTimeout:
				c.setPhase(CalFailed, s.Time, "trainer did not start the calibration")
				changed = true
			case c.Phase == CalInProgress && s.Time-c.LastProgress > progressTimeout:
				if c.CancelRequested {
					c.setPhase(CalCancelled, s.Time, "")
				} else {
					c.setPhase(CalFailed, s.Time, "the trainer stopped calibrating without a result")
				}
				changed = true
			}
			return changed
		})
	}
}

func newTrainerLoop(ch *ant.Channel, hub *Hub, user func() fec.UserConfig, log *slog.Logger) *sensorLoop {
	var dec trainerDecoder
	l := &sensorLoop{
		ch: ch, hub: hub, log: log,
		sensor: func(s *State) *Sensor { return &s.Trainer.Sensor },
		apply:  dec.apply,
	}
	l.onPair = func(ctx context.Context) {
		// The product pages also arrive unrequested, but much later.
		sendAck(ctx, ch, log, "request manufacturer", fec.RequestPage(fec.PageManufacturer, 1))
		sendAck(ctx, ch, log, "request product", fec.RequestPage(fec.PageProduct, 1))
		if u := user(); u.UserWeightKg > 0 {
			if sendAck(ctx, ch, log, "user config", u.Page()) == nil {
				log.Info("user config sent", "rider_kg", u.UserWeightKg, "bike_kg", u.BikeWeightKg)
			}
		}
	}
	return l
}

// trainerDecoder turns FE-C pages into state, removing counter rollovers.
type trainerDecoder struct {
	distance, elapsed rollover
}

func (d *trainerDecoder) apply(p [8]byte, s *State) {
	t := &s.Trainer
	switch pg := fec.Decode(p).(type) {
	case fec.General:
		t.State = pg.State
		if v, ok := pg.SpeedMPS(); ok {
			t.SpeedMPS = Some(v)
		} else {
			t.SpeedMPS = Opt[float64]{}
		}
		t.HeartRateBPM = Opt[uint8]{V: pg.HeartRate, OK: pg.HeartRate != 0xFF && pg.HeartRate != 0}
		if pg.DistanceEnabled {
			t.DistanceM = float64(d.distance.add(pg.Distance))
		}
		t.Elapsed = time.Duration(d.elapsed.add(pg.ElapsedTime)) * 250 * time.Millisecond
	case fec.Trainer:
		t.State = pg.State
		t.PowerW = Opt[uint16]{V: pg.Power, OK: pg.Power != 0xFFF}
		t.CadenceRPM = Opt[uint8]{V: pg.Cadence, OK: pg.Cadence != 0xFF}
		t.Flags = pg.Status
		t.TargetPowerLimit = pg.TargetPowerLimit
	case fec.Manufacturer:
		t.Sensor.ManufacturerID, t.Sensor.ModelNumber = pg.ManufacturerID, pg.ModelNumber
	case fec.Product:
		t.Sensor.SWVersion = pg.SWVersion()
	case fec.CalibrationProgress:
		c := &t.Calibration
		c.LastProgress = s.Time
		switch {
		case c.Phase == CalCancelled:
			// Still calibrating after a cancel: say so rather than hide it.
			c.setPhase(CalInProgress, s.Time, cancelIgnored)
		case c.Phase != CalInProgress:
			c.setPhase(CalInProgress, s.Time, "")
		case c.CancelRequested && c.Message == "":
			c.Message = cancelIgnored
		}
		c.SpeedCondition, c.TemperatureCondition = pg.SpeedCondition, pg.TemperatureCondition
		c.TargetSpeedMPS = OptOf(pg.TargetSpeedMPS())
		c.TemperatureC = OptOf(pg.TemperatureC())
		c.TargetSpinDownMS = OptOf(pg.TargetSpinDownMS())
	case fec.CalibrationResponse:
		c := &t.Calibration
		// The trainer repeats the response; a cancelled request ends as is.
		if c.Phase == CalSucceeded || c.Phase == CalFailed || c.Phase == CalCancelled {
			break
		}
		if c.CancelRequested && !pg.SpinDownSuccess {
			c.setPhase(CalCancelled, s.Time, "")
			break
		}
		if v, ok := pg.TemperatureC(); ok {
			c.TemperatureC = Some(v)
		}
		if pg.SpinDownSuccess {
			c.SpinDownMS = OptOf(pg.SpinDownMS())
			c.setPhase(CalSucceeded, s.Time, "")
		} else {
			c.setPhase(CalFailed, s.Time, "trainer reported the spin-down failed")
		}
	}
}

// rollover accumulates an 8-bit counter that wraps. The first value only
// sets the baseline, so totals count from the moment the core saw the device.
type rollover struct {
	last  uint8
	have  bool
	total uint64
}

func (r *rollover) add(v uint8) uint64 {
	if r.have {
		r.total += uint64(v - r.last) // uint8 arithmetic wraps for us
	}
	r.last, r.have = v, true
	return r.total
}

func newHRMLoop(ch *ant.Channel, hub *Hub, log *slog.Logger) *sensorLoop {
	var dec hrm.Decoder
	return &sensorLoop{
		ch: ch, hub: hub, log: log,
		sensor: func(s *State) *Sensor { return &s.HeartRate.Sensor },
		apply: func(p [8]byte, s *State) {
			pg := dec.Decode(p)
			h := &s.HeartRate
			h.BPM = Opt[uint8]{V: pg.HeartRate, OK: pg.HeartRate != 0}
			h.Legacy = dec.Legacy()
			switch pg.Number {
			case hrm.PageManufacturer:
				h.Sensor.ManufacturerID = uint16(pg.ManufacturerID)
			case hrm.PageProduct:
				h.Sensor.ModelNumber = uint16(pg.ModelNumber)
				h.Sensor.SWVersion = strconv.Itoa(int(pg.SWVersion))
			}
		},
	}
}
