package record

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/digimago/osscycler/internal/fit"
)

// Recover finishes a name.fit.part file left by a crash: it keeps the
// complete messages, rebuilds the lap and session summaries from them
// and seals the file as name.fit, which it returns. A file without a
// single record is removed and "" returned.
func Recover(path string) (string, error) {
	if !strings.HasSuffix(path, ".fit.part") {
		return "", fmt.Errorf("record: %s is not a .fit.part file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	msgs, end, err := fit.DecodePartial(b)
	if err != nil {
		return "", fmt.Errorf("record: %s: %w", path, err)
	}

	// Replay into an activity that writes nowhere, to rebuild the
	// summaries. Anything from a finish that was cut short goes: the
	// summaries are written again.
	a := &activity{enc: fit.NewEncoder(io.Discard), devices: map[uint16]bool{}}
	started := false
replay:
	for _, m := range msgs {
		ts, _ := m.Get(253)
		t := fit.FromTime(ts)
		switch m.Num {
		case fileIDMsg.Num:
			created, _ := m.Get(4)
			a.session, a.lap = newSummary(fit.FromTime(created), 0), newSummary(fit.FromTime(created), 0)
			started = true
		case deviceInfoMsg.Num:
			if n, ok := m.Get(21); ok {
				a.devices[uint16(n)] = true
			}
		case eventMsg.Num:
			if ev, _ := m.Get(0); ev != eventTimer {
				break
			}
			if typ, _ := m.Get(1); typ == typeStart {
				a.running, a.prev = true, time.Time{}
			} else {
				a.running = false
			}
		case zonesTargetMsg.Num:
			if v, ok := m.Get(3); ok {
				a.ftp = float64(v)
			}
		case recordMsg.Num:
			a.record(sampleFromRecord(m))
		case lapMsg.Num:
			if trig, _ := m.Get(24); trig == lapSessionEnd {
				end = m.Offset
				break replay
			}
			a.endLap(t, lapManual)
		case sessionMsg.Num, activityMsg.Num:
			end = m.Offset
			break replay
		}
	}
	if !started {
		return "", fmt.Errorf("record: %s has no file_id", path)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := f.Truncate(int64(end)); err != nil {
		f.Close()
		return "", err
	}
	if _, err := f.Seek(int64(end), io.SeekStart); err != nil {
		f.Close()
		return "", err
	}
	a.f, a.path, a.enc = f, path, fit.NewEncoder(f)
	done, kept, err := a.finish(a.session.last)
	if !kept {
		return "", err
	}
	return done, err
}

func sampleFromRecord(m fit.Msg) sample {
	ts, _ := m.Get(253)
	x := sample{t: fit.FromTime(ts), power: -1, hr: -1, cad: -1}
	if v, ok := m.Get(7); ok {
		x.power = int(v)
	}
	if v, ok := m.Get(3); ok {
		x.hr = int(v)
	}
	if v, ok := m.Get(4); ok {
		x.cad = int(v)
	}
	if v, ok := m.Get(6); ok {
		x.speed = float64(v) / 1000
	}
	if v, ok := m.Get(5); ok {
		x.dist = float64(v) / 100
	}
	lat, okLat := m.Get(0)
	lon, okLon := m.Get(1)
	if okLat && okLon {
		x.hasPos, x.lat, x.lon = true, degrees(lat), degrees(lon)
	}
	if v, ok := m.Get(2); ok {
		x.onCourse, x.alt = true, float64(v)/5-500
		if g, ok := m.Get(9); ok {
			x.grade = float64(g) / 100
		}
	}
	return x
}
