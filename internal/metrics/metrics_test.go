package metrics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/digimago/osscycler/internal/ant"
	"github.com/digimago/osscycler/internal/telemetry"
)

type stubANT struct{ st ant.Stats }

func (s stubANT) Stats() ant.Stats { return s.st }

func sources() Sources {
	hub := telemetry.NewHub()
	hub.Update(func(s *telemetry.State) bool {
		s.Trainer.Sensor = telemetry.Sensor{Status: telemetry.StatusConnected, DeviceNumber: 47508, LastSeen: s.Time}
		s.HeartRate.Sensor = telemetry.Sensor{Status: telemetry.StatusSearching}
		s.Trainer.PowerW = telemetry.Some(uint16(250))
		s.Recording = telemetry.Recording{Active: true}
		return true
	})
	return Sources{
		Hub: hub,
		ANT: stubANT{ant.Stats{Rx: 1000, Tx: 20, BadChecksums: 2, Channels: map[byte]ant.ChannelStats{
			0: {Data: 900, RFEvents: map[ant.EventCode]uint64{ant.EventRxFail: 90}},
		}}},
		Channels: telemetry.ChannelNames,
	}
}

// point finds a data point of the named metric with the given attributes.
func point(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) float64 {
	t.Helper()
	want := attribute.NewSet(attrs...)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					if p.Attributes.Equals(&want) {
						return float64(p.Value)
					}
				}
			case metricdata.Gauge[int64]:
				for _, p := range d.DataPoints {
					if p.Attributes.Equals(&want) {
						return float64(p.Value)
					}
				}
			case metricdata.Gauge[float64]:
				for _, p := range d.DataPoints {
					if p.Attributes.Equals(&want) {
						return p.Value
					}
				}
			}
		}
	}
	t.Errorf("no %s point with %v", name, attrs)
	return -1
}

func TestRegister(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	if err := register(mp.Meter(meterName), sources()); err != nil {
		t.Fatal(err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	trainer := attribute.String("sensor", "trainer")
	for _, c := range []struct {
		name  string
		attrs []attribute.KeyValue
		want  float64
	}{
		{"osscycler.ant.messages", []attribute.KeyValue{attribute.String("direction", "rx")}, 1000},
		{"osscycler.ant.bad_checksums", nil, 2},
		{"osscycler.ant.channel.messages", []attribute.KeyValue{attribute.Int("channel", 0), trainer}, 900},
		{"osscycler.ant.channel.rf_events", []attribute.KeyValue{attribute.Int("channel", 0), trainer, attribute.String("event", "EVENT_RX_FAIL")}, 90},
		{"osscycler.sensor.connected", []attribute.KeyValue{trainer}, 1},
		{"osscycler.sensor.connected", []attribute.KeyValue{attribute.String("sensor", "hrm")}, 0},
		{"osscycler.state.updates", nil, 1},
		{"osscycler.recording.active", nil, 1},
		{"osscycler.recording.failed", nil, 0},
	} {
		if got := point(t, rm, c.name, c.attrs...); got != c.want {
			t.Errorf("%s%v = %v, want %v", c.name, c.attrs, got, c.want)
		}
	}
	if a := point(t, rm, "osscycler.sensor.message_age", trainer); a < 0 || a > 5 {
		t.Errorf("trainer message age %v s", a)
	}
}

// receiver is an OTLP/HTTP metrics endpoint that keeps what it gets.
type receiver struct {
	mu   sync.Mutex
	reqs []*colmetrics.ExportMetricsServiceRequest
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	var m colmetrics.ExportMetricsServiceRequest
	if req.URL.Path != "/v1/metrics" || proto.Unmarshal(b, &m) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.reqs = append(r.reqs, &m)
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-protobuf")
	out, _ := proto.Marshal(&colmetrics.ExportMetricsServiceResponse{})
	w.Write(out)
}

func TestPushOverOTLP(t *testing.T) {
	rcv := &receiver{}
	srv := httptest.NewServer(rcv)
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=test")

	stop, err := Start(context.Background(), sources(), "v-test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stop() // the final flush pushes everything once

	rcv.mu.Lock()
	defer rcv.mu.Unlock()
	if len(rcv.reqs) == 0 {
		t.Fatal("nothing pushed")
	}
	rm := rcv.reqs[len(rcv.reqs)-1].GetResourceMetrics()[0]
	res := map[string]string{}
	for _, kv := range rm.GetResource().GetAttributes() {
		res[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	if res["service.name"] != "osscycler-core" || res["service.version"] != "v-test" ||
		len(res["service.instance.id"]) != 16 || res["deployment.environment"] != "test" {
		t.Errorf("resource %v", res)
	}
	names := map[string]bool{}
	for _, sm := range rm.GetScopeMetrics() {
		for _, m := range sm.GetMetrics() {
			names[m.GetName()] = true
		}
	}
	for _, want := range []string{"osscycler.ant.channel.rf_events", "osscycler.sensor.connected", "osscycler.recording.active"} {
		if !names[want] {
			t.Errorf("%s not pushed (got %v)", want, names)
		}
	}
	// Rider data never goes out as metrics.
	for n := range names {
		for _, banned := range []string{"power", "heart", "cadence", "speed", "position"} {
			if strings.Contains(n, banned) {
				t.Errorf("metric %s looks like rider data", n)
			}
		}
	}
}

func TestUnreachableReceiverDoesNotHold(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there now
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", url)
	var logs bytes.Buffer
	stop, err := Start(context.Background(), sources(), "v", slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	stop()
	if d := time.Since(start); d > shutdownTimeout+time.Second {
		t.Errorf("stop took %v with the receiver away", d)
	}
	if !strings.Contains(logs.String(), "last push failed") {
		t.Errorf("failed final push not logged:\n%s", logs.String())
	}
}

func TestEnabled(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{}, false},
		{map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://x:4318"}, true},
		{map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://x:4318/v1/metrics"}, true},
		{map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://x:4318", "OTEL_SDK_DISABLED": "true"}, false},
		{map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://x:4318", "OTEL_METRICS_EXPORTER": "none"}, false},
	} {
		for _, k := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_SDK_DISABLED", "OTEL_METRICS_EXPORTER"} {
			t.Setenv(k, c.env[k])
		}
		if got := Enabled(); got != c.want {
			t.Errorf("%v: Enabled = %v", c.env, got)
		}
	}
}

func TestErrorLogRateLimited(t *testing.T) {
	var logs bytes.Buffer
	e := &errorLog{log: slog.New(slog.NewTextHandler(&logs, nil))}
	for range 50 {
		e.Handle(errors.New("connection refused"))
	}
	if n := strings.Count(logs.String(), "metrics push failing"); n != 1 {
		t.Errorf("%d log lines for 50 failures", n)
	}
}
