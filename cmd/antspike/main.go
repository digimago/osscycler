// Command antspike is the headless telemetry spike: it opens the ANT stick,
// pairs with an FE-C trainer, prints what the trainer reports and optionally
// sends one control setpoint (grade, ERG target or basic resistance).
//
// The ANT+ network key is read from ANT_PLUS_NETWORK_KEY (16 hex digits).
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digimago/osscycler/ant"
	"github.com/digimago/osscycler/fec"
)

const keyEnv = "ANT_PLUS_NETWORK_KEY"

// optFloat is a float flag that remembers whether it was set.
type optFloat struct {
	v   float64
	set bool
}

func (o *optFloat) String() string {
	if !o.set {
		return ""
	}
	return strconv.FormatFloat(o.v, 'f', -1, 64)
}

func (o *optFloat) Set(s string) error {
	v, err := strconv.ParseFloat(s, 64)
	o.v, o.set = v, err == nil
	return err
}

type config struct {
	port       string
	device     uint
	debug      bool
	rawPath    string
	duration   time.Duration
	grade      optFloat
	erg        optFloat
	resistance optFloat
	crr        float64
	user       fec.UserConfig
	requests   []requestSpec
	rawPages   []string
}

// requestSpec is one -request flag: page[:response], where response is the
// raw "requested transmission response" byte of page 0x46.
type requestSpec struct{ page, response byte }

type requestList []requestSpec

func (r *requestList) String() string { return fmt.Sprint(*r) }

func (r *requestList) Set(s string) error {
	page, resp, found := strings.Cut(s, ":")
	p, err := strconv.ParseUint(page, 0, 8)
	if err != nil {
		return err
	}
	spec := requestSpec{page: byte(p), response: 1}
	if found {
		v, err := strconv.ParseUint(resp, 0, 8)
		if err != nil {
			return err
		}
		spec.response = byte(v)
	}
	*r = append(*r, spec)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "antspike:", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg config
	flag.StringVar(&cfg.port, "port", "/dev/ttyANT", "ANT stick tty (see deploy/udev)")
	flag.UintVar(&cfg.device, "device", 0, "FE-C device number to pair with (0 = first found)")
	flag.BoolVar(&cfg.debug, "debug", false, "log every raw ANT frame")
	flag.StringVar(&cfg.rawPath, "rawlog", "", "append raw ANT frames to this file")
	flag.DurationVar(&cfg.duration, "duration", 0, "stop after this long (0 = until interrupted)")
	flag.Var(&cfg.grade, "grade", "send simulation grade in percent (page 0x33)")
	flag.Var(&cfg.erg, "erg", "send ERG target power in watts (page 0x31)")
	flag.Var(&cfg.resistance, "resistance", "send basic resistance in percent (page 0x30)")
	flag.Float64Var(&cfg.crr, "crr", 0, "rolling resistance sent with -grade (0 = trainer default)")
	flag.Float64Var(&cfg.user.UserWeightKg, "rider-kg", 0, "rider weight; if set, user config (page 0x37) is sent")
	flag.Float64Var(&cfg.user.BikeWeightKg, "bike-kg", 9, "bike weight for user config")
	flag.Float64Var(&cfg.user.WheelDiameterM, "wheel-m", 0.672, "wheel diameter for user config")
	flag.Var((*requestList)(&cfg.requests), "request", "request page `PAGE[:RESPONSE]` after pairing, e.g. 0x47 or 0x47:0x84 (repeatable)")
	flag.Func("page", "send this 8-byte page (hex, e.g. \"01 00 00 FF FF FF FF FF\") after pairing (repeatable)", func(s string) error {
		if _, err := parsePage(s); err != nil {
			return err
		}
		cfg.rawPages = append(cfg.rawPages, s)
		return nil
	})
	flag.Parse()

	if n := btoi(cfg.grade.set) + btoi(cfg.erg.set) + btoi(cfg.resistance.set); n > 1 {
		return errors.New("use at most one of -grade, -erg, -resistance")
	}
	if cfg.device > 0xFFFF {
		return errors.New("-device must fit in 16 bits")
	}
	keyHex := os.Getenv(keyEnv)
	if keyHex == "" {
		return fmt.Errorf("%s is not set; get the ANT+ network key from the thisisant.com adopter area", keyEnv)
	}
	key, err := ant.ParseNetworkKey(keyHex)
	if err != nil {
		return fmt.Errorf("%s: %w", keyEnv, err)
	}

	level := slog.LevelInfo
	if cfg.debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.duration)
		defer cancel()
	}

	tr, err := newTracer(log, cfg.debug, cfg.rawPath)
	if err != nil {
		return err
	}
	defer tr.close()

	port, err := ant.OpenSerial(cfg.port)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w (install deploy/udev/99-ant-usb.rules or pass -port /dev/ttyUSB0)", err)
	}
	if err != nil {
		return err
	}
	node := ant.NewNode(port, ant.Options{Logger: log, Trace: tr.trace})
	defer node.Close()

	if err := node.Reset(ctx); err != nil {
		// Not fatal: the stick should send a startup message, but carry on if not.
		log.Warn("no startup message after reset", "err", err)
	}
	if err := node.SetNetworkKey(ctx, 0, key); err != nil {
		return err
	}
	ch, err := node.OpenChannel(ctx, ant.ChannelConfig{
		DeviceNumber:  uint16(cfg.device),
		DeviceType:    fec.DeviceType,
		Period:        fec.Period,
		RFFrequency:   fec.RFFrequency,
		SearchTimeout: 0xFF,
	})
	if err != nil {
		return err
	}
	log.Info("channel open, searching for FE-C trainer", "device", cfg.device)

	err = loop(ctx, log, node, ch, &cfg)

	closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if cerr := ch.Close(closeCtx); cerr != nil {
		log.Warn("closing channel", "err", cerr)
	}
	st := node.Stats()
	log.Info("done", "rx", st.Rx, "tx", st.Tx, "dropped_bytes", st.DroppedBytes,
		"bad_checksums", st.BadChecksums, "overflows", st.Overflows)
	return err
}

func loop(ctx context.Context, log *slog.Logger, node *ant.Node, ch *ant.Channel, cfg *config) error {
	var (
		s      = summary{pages: make(map[byte]int), events: make(map[ant.EventCode]int)}
		paired bool
		wg     sync.WaitGroup
	)
	setupCtx, cancelSetup := context.WithCancel(ctx)
	defer func() { cancelSetup(); wg.Wait() }()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if paired {
				log.Info("trainer", s.attrs()...)
			}
		case m, ok := <-ch.Messages():
			if !ok {
				return fmt.Errorf("ANT stick went away: %v", node.Err())
			}
			if p, ok := m.Payload(); ok {
				if !paired {
					paired = true
					wg.Add(1)
					go func() { defer wg.Done(); setup(setupCtx, log, ch, cfg) }()
				}
				s.update(log, fec.Decode(p))
				continue
			}
			e, ok := ant.ParseChannelEvent(m)
			if !ok || !e.IsRF() {
				continue
			}
			s.events[e.Code]++
			switch e.Code {
			case ant.EventRxFailGoToSearch:
				log.Warn("lost trainer, searching again")
			case ant.EventRxSearchTimeout:
				log.Warn("search timed out")
			case ant.EventChannelClosed:
				return errors.New("channel closed by stick")
			}
		}
	}
}

// setup runs once the trainer is found: it logs the device ID, asks for the
// product pages and sends the requested configuration and control pages.
func setup(ctx context.Context, log *slog.Logger, ch *ant.Channel, cfg *config) {
	id, err := ch.ID(ctx)
	if err != nil {
		log.Warn("reading channel ID", "err", err)
	} else {
		log.Info("paired", "device", id.DeviceNumber, "device_type", id.DeviceType&0x7F, "trans_type", id.TransType)
	}

	send := func(what string, p [8]byte) {
		// The radio may miss an acknowledged message; retry a few times.
		for attempt := 1; ; attempt++ {
			err := ch.SendAcknowledged(ctx, p)
			if err == nil {
				log.Info("sent", "page", what, "bytes", fmt.Sprintf("% X", p))
				return
			}
			if attempt == 3 || ctx.Err() != nil {
				log.Warn("send failed", "page", what, "err", err)
				return
			}
		}
	}

	send("request manufacturer", fec.RequestPage(fec.PageManufacturer, 2))
	send("request product", fec.RequestPage(fec.PageProduct, 2))
	if cfg.user.UserWeightKg > 0 {
		send("user config", cfg.user.Page())
	}
	for _, raw := range cfg.rawPages {
		p, _ := parsePage(raw)
		send("raw page", p)
	}
	for _, r := range cfg.requests {
		send(fmt.Sprintf("request 0x%02X/0x%02X", r.page, r.response), fec.RequestPage(r.page, r.response))
	}
	switch {
	case cfg.grade.set:
		if cfg.grade.v < 0 {
			log.Warn("negative grade: the Flux 2 cannot simulate descents, expect no effect", "grade", cfg.grade.v)
		}
		send("track resistance", fec.TrackResistance(cfg.grade.v, cfg.crr))
	case cfg.erg.set:
		send("target power", fec.TargetPower(cfg.erg.v))
	case cfg.resistance.set:
		send("basic resistance", fec.BasicResistance(cfg.resistance.v))
	default:
		return
	}
	// Per the profile this confirms the setpoint; the Flux 2 ignores it.
	send("request command status", fec.RequestPage(fec.PageCommandStatus, 1))
}

// summary keeps the latest decoded state for the once-a-second log line.
type summary struct {
	general fec.General
	trainer fec.Trainer
	pages   map[byte]int
	events  map[ant.EventCode]int
}

func (s *summary) update(log *slog.Logger, p fec.Page) {
	s.pages[p.PageNumber()]++
	switch p := p.(type) {
	case fec.General:
		s.general = p
	case fec.Trainer:
		s.trainer = p
	case fec.CommandStatus:
		log.Info("command status", "last_page", fmt.Sprintf("0x%02X", p.LastCommand), "seq", p.Sequence,
			"status", p.Status, "data", fmt.Sprintf("% X", p.Data))
	case fec.Manufacturer:
		log.Info("manufacturer", "id", p.ManufacturerID, "model", p.ModelNumber, "hw_rev", p.HWRevision)
	case fec.Product:
		log.Info("product", "sw_version", p.SWVersion(), "serial", p.SerialNumber)
	case fec.Unknown:
		if s.pages[p.PageNumber()] == 1 {
			log.Info("first sighting of undecoded page", "page", fmt.Sprintf("0x%02X", p[0]), "bytes", fmt.Sprintf("% X", p[:]))
		}
	}
}

func (s *summary) attrs() []any {
	g, t := s.general, s.trainer
	speed := "-"
	if v, ok := g.SpeedMPS(); ok {
		speed = fmt.Sprintf("%.1f", v*3.6)
	}
	power, cadence, hr := "-", "-", "-"
	if t.Power != 0xFFF {
		power = strconv.Itoa(int(t.Power))
	}
	if t.Cadence != 0xFF {
		cadence = strconv.Itoa(int(t.Cadence))
	}
	if g.HeartRate != 0xFF && g.HeartRate != 0 {
		hr = strconv.Itoa(int(g.HeartRate))
	}
	return []any{
		"state", g.State, "power_w", power, "cadence_rpm", cadence, "speed_kmh", speed, "hr", hr,
		"status", t.Status, "erg", t.TargetPowerLimit,
		"rx_fail", s.events[ant.EventRxFail], "pages", len(s.pages),
	}
}

// tracer sends raw frames to the debug log and/or a raw log file.
type tracer struct {
	log   *slog.Logger
	debug bool
	f     *os.File
	fl    *ant.FrameLog
}

func newTracer(log *slog.Logger, debug bool, path string) (*tracer, error) {
	t := &tracer{log: log, debug: debug}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		t.f, t.fl = f, ant.NewFrameLog(f)
	}
	return t, nil
}

func (t *tracer) trace(dir ant.Direction, frame []byte) {
	if t.debug {
		t.log.Debug("frame", "dir", dir, "bytes", fmt.Sprintf("% X", frame))
	}
	if t.fl != nil {
		t.fl.Trace(dir, frame)
	}
}

func (t *tracer) close() {
	if t.fl != nil {
		t.fl.Flush()
		t.f.Close()
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parsePage parses 8 hex bytes, with or without separators.
func parsePage(s string) ([8]byte, error) {
	var p [8]byte
	b, err := hex.DecodeString(strings.NewReplacer(" ", "", ":", "", ",", "").Replace(s))
	if err != nil || len(b) != len(p) {
		return p, fmt.Errorf("page must be 8 hex bytes, got %q", s)
	}
	copy(p[:], b)
	return p, nil
}
