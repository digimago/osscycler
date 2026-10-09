// Package metrics reports how the core is running (radio quality, sensor
// links, the recorder) as OpenTelemetry metrics pushed over OTLP/HTTP.
//
// It is off unless the standard OTEL_EXPORTER_OTLP_* environment
// variables name an endpoint, so the core needs no collector. Push suits a
// core that often isn't running: no data means it was off, where a
// scraper would record a failure.
//
// It never carries the rider's data (power, heart rate, position). That
// stays in the state stream and the FIT files.
package metrics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/telemetry"
)

const (
	// DefaultInterval between pushes, unless OTEL_METRIC_EXPORT_INTERVAL
	// says otherwise. The SDK's own default (60 s) is coarse for a ride.
	DefaultInterval = 15 * time.Second
	// shutdownTimeout bounds the final flush, so stopping the core never
	// waits on an unreachable receiver.
	shutdownTimeout = 3 * time.Second
	// errorLogEvery limits export failure logs while the receiver is away.
	errorLogEvery = 10 * time.Minute
	meterName     = "github.com/digimago/osscycler/internal/metrics"
)

// StatsSource gives the ANT transport counters; *ant.Node is one.
type StatsSource interface {
	Stats() ant.Stats
}

// Sources are what the metrics are read from, when an export is due.
type Sources struct {
	Hub      *telemetry.Hub
	ANT      StatsSource     // nil without a stick (-fake)
	Channels map[byte]string // ANT channel number → sensor name, for labels
}

// Enabled reports whether the environment configures an OTLP endpoint
// (and doesn't switch the SDK or metrics off).
func Enabled() bool {
	if strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") || os.Getenv("OTEL_METRICS_EXPORTER") == "none" {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
}

// Start pushes metrics if Enabled. stop sends the last interval and shuts
// the exporter down, waiting at most a few seconds; it is never nil.
func Start(ctx context.Context, src Sources, version string, log *slog.Logger) (stop func(), err error) {
	if !Enabled() {
		log.Info("metrics off (set OTEL_EXPORTER_OTLP_ENDPOINT to push them)")
		return func() {}, nil
	}
	otel.SetErrorHandler(&errorLog{log: log})

	exp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return func() {}, fmt.Errorf("metrics: %w", err)
	}
	res, err := newResource(ctx, version)
	if err != nil {
		return func() {}, fmt.Errorf("metrics: %w", err)
	}
	var opts []sdkmetric.PeriodicReaderOption
	if os.Getenv("OTEL_METRIC_EXPORT_INTERVAL") == "" {
		opts = append(opts, sdkmetric.WithInterval(DefaultInterval))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, opts...)),
		sdkmetric.WithResource(res),
	)
	if err := register(mp.Meter(meterName), src); err != nil {
		mp.Shutdown(context.Background())
		return func() {}, fmt.Errorf("metrics: %w", err)
	}
	log.Info("pushing metrics over OTLP/HTTP")
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := mp.Shutdown(ctx); err != nil {
			log.Warn("metrics: last push failed", "err", err)
		}
	}, nil
}

// newResource names this process. service.instance.id is new on every
// start, so each run of the core is its own series; OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES override the defaults.
func newResource(ctx context.Context, version string) (*resource.Resource, error) {
	id := make([]byte, 8)
	rand.Read(id)
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "osscycler-core"),
			attribute.String("service.version", version),
			attribute.String("service.instance.id", hex.EncodeToString(id)),
		),
		resource.WithHost(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
	)
}

// register creates the instruments. They are all observed: read when an
// export is due, so nothing runs on the radio's hot path.
func register(m metric.Meter, src Sources) error {
	var (
		insts []metric.Observable
		errs  []error
	)
	add := func(o metric.Observable, err error) {
		insts, errs = append(insts, o), append(errs, err)
	}

	antMessages, err := m.Int64ObservableCounter("osscycler.ant.messages", metric.WithUnit("{message}"),
		metric.WithDescription("Valid ANT messages exchanged with the stick"))
	add(antMessages, err)
	antDropped, err := m.Int64ObservableCounter("osscycler.ant.dropped_bytes", metric.WithUnit("By"),
		metric.WithDescription("Bytes discarded while resynchronising on the serial stream"))
	add(antDropped, err)
	antChecksums, err := m.Int64ObservableCounter("osscycler.ant.bad_checksums", metric.WithUnit("{message}"),
		metric.WithDescription("ANT messages with a bad checksum"))
	add(antChecksums, err)
	antOverflows, err := m.Int64ObservableCounter("osscycler.ant.overflows", metric.WithUnit("{message}"),
		metric.WithDescription("Channel messages dropped because the core fell behind"))
	add(antOverflows, err)
	chData, err := m.Int64ObservableCounter("osscycler.ant.channel.messages", metric.WithUnit("{message}"),
		metric.WithDescription("Data messages received per channel"))
	add(chData, err)
	chEvents, err := m.Int64ObservableCounter("osscycler.ant.channel.rf_events", metric.WithUnit("{event}"),
		metric.WithDescription("RF events per channel; EVENT_RX_FAIL is a missed message period"))
	add(chEvents, err)
	connected, err := m.Int64ObservableGauge("osscycler.sensor.connected",
		metric.WithDescription("1 while the sensor sends data"))
	add(connected, err)
	age, err := m.Float64ObservableGauge("osscycler.sensor.message_age", metric.WithUnit("s"),
		metric.WithDescription("Time since the sensor's last message"))
	add(age, err)
	updates, err := m.Int64ObservableCounter("osscycler.state.updates", metric.WithUnit("{update}"),
		metric.WithDescription("Changes to the core's state"))
	add(updates, err)
	recording, err := m.Int64ObservableGauge("osscycler.recording.active",
		metric.WithDescription("1 while a ride is being recorded"))
	add(recording, err)
	recFailed, err := m.Int64ObservableGauge("osscycler.recording.failed",
		metric.WithDescription("1 when recording has failed"))
	add(recFailed, err)
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		if src.ANT != nil {
			st := src.ANT.Stats()
			o.ObserveInt64(antMessages, int64(st.Rx), metric.WithAttributes(attribute.String("direction", "rx")))
			o.ObserveInt64(antMessages, int64(st.Tx), metric.WithAttributes(attribute.String("direction", "tx")))
			o.ObserveInt64(antDropped, int64(st.DroppedBytes))
			o.ObserveInt64(antChecksums, int64(st.BadChecksums))
			o.ObserveInt64(antOverflows, int64(st.Overflows))
			for ch, cs := range st.Channels {
				attrs := []attribute.KeyValue{attribute.Int("channel", int(ch)), attribute.String("sensor", src.Channels[ch])}
				o.ObserveInt64(chData, int64(cs.Data), metric.WithAttributes(attrs...))
				for code, n := range cs.RFEvents {
					o.ObserveInt64(chEvents, int64(n), metric.WithAttributes(append(attrs, attribute.String("event", code.String()))...))
				}
			}
		}
		if src.Hub == nil {
			return nil
		}
		st, _ := src.Hub.Latest()
		now := src.Hub.Now()
		for _, s := range []struct {
			name string
			s    telemetry.Sensor
		}{{"trainer", st.Trainer.Sensor}, {"hrm", st.HeartRate.Sensor}} {
			if s.s.Status == telemetry.StatusDisabled {
				continue
			}
			attrs := metric.WithAttributes(attribute.String("sensor", s.name))
			o.ObserveInt64(connected, boolInt(s.s.Status == telemetry.StatusConnected), attrs)
			if s.s.Status == telemetry.StatusConnected || s.s.Status == telemetry.StatusLost {
				o.ObserveFloat64(age, (now - s.s.LastSeen).Seconds(), attrs)
			}
		}
		o.ObserveInt64(updates, int64(st.Seq))
		o.ObserveInt64(recording, boolInt(st.Recording.Active))
		o.ObserveInt64(recFailed, boolInt(st.Recording.Error != ""))
		return nil
	}, insts...)
	return err
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// errorLog reports export failures, at most once per errorLogEvery: with
// the receiver away (the core off the home network) every push fails.
type errorLog struct {
	log    *slog.Logger
	mu     sync.Mutex
	last   time.Time
	missed int
}

func (e *errorLog) Handle(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.missed++
	if time.Since(e.last) < errorLogEvery {
		return
	}
	e.log.Warn("metrics push failing; data is dropped until the receiver is back", "err", err, "failures", e.missed)
	e.last, e.missed = time.Now(), 0
}
