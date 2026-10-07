// Package record writes every ride to a FIT activity file on local disk,
// whatever renderers are connected: one file per riding session, laps
// for each course ride and workout in it, auto-pause when the rider
// stops. Files are written as they go (name.fit.part) and finished when
// the session ends; a file left unfinished by a crash is finished on the
// next start.
package record

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/digimago/osscycler/fit"
)

// FIT profile numbers used here, checked against the FIT SDK profile
// 21.218 on 2026-10-07.
const (
	fileActivity    = 4
	mfrDevelopment  = 255
	sportCycling    = 2
	subIndoor       = 6
	subVirtual      = 58
	eventTimer      = 0
	eventSession    = 8
	eventLap        = 9
	eventActivity   = 26
	typeStart       = 0
	typeStop        = 1
	typeStopAll     = 4
	lapManual       = 0
	lapSessionEnd   = 7
	sessionActEnd   = 0
	activityManual  = 0
	sourceANTPlus   = 1
	sourceLocal     = 5
	product         = 1 // ours, under the development manufacturer ID
	softwareVersion = 1
)

// Messages, each under its own local type so recovery never depends on
// which definition came last.
var (
	fileIDMsg = &fit.Message{Num: 0, Local: 0, Fields: []fit.Field{
		{Num: 0, Type: fit.Enum},    // type
		{Num: 1, Type: fit.Uint16},  // manufacturer
		{Num: 2, Type: fit.Uint16},  // product
		{Num: 3, Type: fit.Uint32z}, // serial_number
		{Num: 4, Type: fit.Uint32},  // time_created
	}}
	fileCreatorMsg = &fit.Message{Num: 49, Local: 1, Fields: []fit.Field{
		{Num: 0, Type: fit.Uint16}, // software_version
	}}
	deviceInfoMsg = &fit.Message{Num: 23, Local: 2, Fields: []fit.Field{
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 0, Type: fit.Uint8},    // device_index
		{Num: 1, Type: fit.Uint8},    // device_type (ANT+ device type)
		{Num: 2, Type: fit.Uint16},   // manufacturer
		{Num: 4, Type: fit.Uint16},   // product
		{Num: 5, Type: fit.Uint16},   // software_version, × 100
		{Num: 21, Type: fit.Uint16z}, // ant_device_number
		{Num: 25, Type: fit.Enum},    // source_type
	}}
	zonesTargetMsg = &fit.Message{Num: 7, Local: 8, Fields: []fit.Field{
		{Num: 3, Type: fit.Uint16}, // functional_threshold_power
	}}
	eventMsg = &fit.Message{Num: 21, Local: 3, Fields: []fit.Field{
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 0, Type: fit.Enum},     // event
		{Num: 1, Type: fit.Enum},     // event_type
		{Num: 4, Type: fit.Uint8},    // event_group
	}}
	recordMsg = &fit.Message{Num: 20, Local: 4, Fields: []fit.Field{
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 7, Type: fit.Uint16},   // power, W
		{Num: 3, Type: fit.Uint8},    // heart_rate, bpm
		{Num: 4, Type: fit.Uint8},    // cadence, rpm
		{Num: 6, Type: fit.Uint16},   // speed, mm/s
		{Num: 5, Type: fit.Uint32},   // distance, cm
		{Num: 2, Type: fit.Uint16},   // altitude, (m + 500) × 5
		{Num: 9, Type: fit.Sint16},   // grade, % × 100
		{Num: 0, Type: fit.Sint32},   // position_lat, semicircles
		{Num: 1, Type: fit.Sint32},   // position_long, semicircles
	}}
	lapMsg = &fit.Message{Num: 19, Local: 5, Fields: []fit.Field{
		{Num: 254, Type: fit.Uint16}, // message_index
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 0, Type: fit.Enum},     // event
		{Num: 1, Type: fit.Enum},     // event_type
		{Num: 2, Type: fit.Uint32},   // start_time
		{Num: 7, Type: fit.Uint32},   // total_elapsed_time, ms
		{Num: 8, Type: fit.Uint32},   // total_timer_time, ms
		{Num: 9, Type: fit.Uint32},   // total_distance, cm
		{Num: 11, Type: fit.Uint16},  // total_calories
		{Num: 13, Type: fit.Uint16},  // avg_speed, mm/s
		{Num: 14, Type: fit.Uint16},  // max_speed
		{Num: 15, Type: fit.Uint8},   // avg_heart_rate
		{Num: 16, Type: fit.Uint8},   // max_heart_rate
		{Num: 17, Type: fit.Uint8},   // avg_cadence
		{Num: 18, Type: fit.Uint8},   // max_cadence
		{Num: 19, Type: fit.Uint16},  // avg_power
		{Num: 20, Type: fit.Uint16},  // max_power
		{Num: 21, Type: fit.Uint16},  // total_ascent
		{Num: 22, Type: fit.Uint16},  // total_descent
		{Num: 24, Type: fit.Enum},    // lap_trigger
		{Num: 25, Type: fit.Enum},    // sport
		{Num: 39, Type: fit.Enum},    // sub_sport
		{Num: 41, Type: fit.Uint32},  // total_work, J
	}}
	sessionMsg = &fit.Message{Num: 18, Local: 6, Fields: []fit.Field{
		{Num: 254, Type: fit.Uint16}, // message_index
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 0, Type: fit.Enum},     // event
		{Num: 1, Type: fit.Enum},     // event_type
		{Num: 2, Type: fit.Uint32},   // start_time
		{Num: 7, Type: fit.Uint32},   // total_elapsed_time, ms
		{Num: 8, Type: fit.Uint32},   // total_timer_time, ms
		{Num: 9, Type: fit.Uint32},   // total_distance, cm
		{Num: 11, Type: fit.Uint16},  // total_calories
		{Num: 14, Type: fit.Uint16},  // avg_speed, mm/s
		{Num: 15, Type: fit.Uint16},  // max_speed
		{Num: 16, Type: fit.Uint8},   // avg_heart_rate
		{Num: 17, Type: fit.Uint8},   // max_heart_rate
		{Num: 18, Type: fit.Uint8},   // avg_cadence
		{Num: 19, Type: fit.Uint8},   // max_cadence
		{Num: 20, Type: fit.Uint16},  // avg_power
		{Num: 21, Type: fit.Uint16},  // max_power
		{Num: 22, Type: fit.Uint16},  // total_ascent
		{Num: 23, Type: fit.Uint16},  // total_descent
		{Num: 5, Type: fit.Enum},     // sport
		{Num: 6, Type: fit.Enum},     // sub_sport
		{Num: 25, Type: fit.Uint16},  // first_lap_index
		{Num: 26, Type: fit.Uint16},  // num_laps
		{Num: 28, Type: fit.Enum},    // trigger
		{Num: 34, Type: fit.Uint16},  // normalized_power
		{Num: 45, Type: fit.Uint16},  // threshold_power
		{Num: 48, Type: fit.Uint32},  // total_work, J
	}}
	activityMsg = &fit.Message{Num: 34, Local: 7, Fields: []fit.Field{
		{Num: 253, Type: fit.Uint32}, // timestamp
		{Num: 0, Type: fit.Uint32},   // total_timer_time, ms
		{Num: 1, Type: fit.Uint16},   // num_sessions
		{Num: 2, Type: fit.Enum},     // type
		{Num: 3, Type: fit.Enum},     // event
		{Num: 4, Type: fit.Enum},     // event_type
		{Num: 5, Type: fit.Uint32},   // local_timestamp
	}}
)

// sample is one record: what the rider did at one moment.
type sample struct {
	t          time.Time
	power      int     // W; -1 = no reading
	hr, cad    int     // -1 = no reading
	speed      float64 // m/s
	dist       float64 // m since the start of the activity
	alt, grade float64 // on a course
	onCourse   bool    // alt and grade are set
	lat, lon   float64 // degrees, when hasPos
	hasPos     bool
}

// semicircles is FIT's angle unit: 2^31 per 180°.
func semicircles(deg float64) int64 { return int64(math.Round(deg * (1 << 31) / 180)) }

func degrees(semi int64) float64 { return float64(semi) * 180 / (1 << 31) }

// summary accumulates a lap or a session.
type summary struct {
	start, last time.Time
	timer       time.Duration
	dist0, dist float64
	n           int

	powerSum, powerN, powerMax int
	hrSum, hrN, hrMax          int
	cadSum, cadN, cadMax       int // cadence averages leave out zeros, as Garmin does
	speedMax                   float64
	ascent, descent            float64
	work                       float64 // J
	onCourse                   bool    // any sample on a course: a virtual ride

	prevAlt   float64
	prevAltOK bool

	// Normalized power: 30 s rolling mean of 1 Hz power, to the 4th.
	roll    [30]int
	rollSum int
	np4     float64
	npN     int
}

func newSummary(t time.Time, dist float64) summary {
	return summary{start: t, last: t, dist0: dist, dist: dist}
}

func (s *summary) add(x sample, dt time.Duration) {
	s.n++
	s.last, s.dist, s.timer = x.t, x.dist, s.timer+dt
	if x.power >= 0 {
		s.powerSum += x.power
		s.powerN++
		s.powerMax = max(s.powerMax, x.power)
		s.work += float64(x.power) * dt.Seconds()
		i := s.powerN % len(s.roll)
		s.rollSum += x.power - s.roll[i]
		s.roll[i] = x.power
		if s.powerN >= len(s.roll) {
			s.np4 += math.Pow(float64(s.rollSum)/float64(len(s.roll)), 4)
			s.npN++
		}
	}
	if x.hr > 0 {
		s.hrSum += x.hr
		s.hrN++
		s.hrMax = max(s.hrMax, x.hr)
	}
	if x.cad > 0 {
		s.cadSum += x.cad
		s.cadN++
		s.cadMax = max(s.cadMax, x.cad)
	}
	s.speedMax = math.Max(s.speedMax, x.speed)
	if x.onCourse {
		s.onCourse = true
		if s.prevAltOK {
			if d := x.alt - s.prevAlt; d > 0 {
				s.ascent += d
			} else {
				s.descent -= d
			}
		}
	}
	s.prevAlt, s.prevAltOK = x.alt, x.onCourse
}

func avg(sum, n int) int64 {
	if n == 0 {
		return fit.Invalid
	}
	return int64(math.Round(float64(sum) / float64(n)))
}

func orInvalid(v, n int) int64 {
	if n == 0 {
		return fit.Invalid
	}
	return int64(v)
}

func ms(d time.Duration) int64 { return d.Milliseconds() }

func (s *summary) avgSpeed() float64 {
	if s.timer <= 0 {
		return 0
	}
	return (s.dist - s.dist0) / s.timer.Seconds()
}

func (s *summary) subSport() int64 {
	if s.onCourse {
		return subVirtual
	}
	return subIndoor
}

// normalizedPower is 0 for rides under 30 s of power.
func (s *summary) normalizedPower() int64 {
	if s.npN == 0 {
		return fit.Invalid
	}
	return int64(math.Round(math.Pow(s.np4/float64(s.npN), 0.25)))
}

// calories: mechanical work in kJ is close to the food energy burned in
// kcal (about 24 % efficiency × 4.184 kJ/kcal ≈ 1), the usual estimate.
func (s *summary) calories() int64 { return int64(math.Round(s.work / 1000)) }

// activity is a FIT file being written.
type activity struct {
	f       *os.File
	path    string // name.fit.part while written
	enc     *fit.Encoder
	session summary
	lap     summary
	laps    int
	running bool
	prev    time.Time // last record while running; zero right after a (re)start
	devices map[uint16]bool
	ftp     float64
	synced  time.Time
	err     error // first write error; the activity is lost after one
}

func (a *activity) write(m *fit.Message, values ...int64) {
	if err := a.enc.Write(m, values...); err != nil && a.err == nil {
		a.err = err
	}
}

// create starts a new activity file at t.
func create(path string, t time.Time) (*activity, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(fit.Header(0)); err != nil {
		f.Close()
		return nil, err
	}
	a := &activity{f: f, path: path, enc: fit.NewEncoder(f), devices: map[uint16]bool{},
		session: newSummary(t, 0), lap: newSummary(t, 0), synced: t}
	ts := fit.Time(t)
	a.write(fileIDMsg, fileActivity, mfrDevelopment, product, fit.Invalid, ts)
	a.write(fileCreatorMsg, softwareVersion)
	a.write(deviceInfoMsg, ts, 0, fit.Invalid, mfrDevelopment, product, softwareVersion*100, fit.Invalid, sourceLocal)
	a.resume(t)
	return a, a.flush(t)
}

// flush hands buffered messages to the OS, and syncs to disk now and then.
func (a *activity) flush(t time.Time) error {
	if a.err != nil {
		return a.err
	}
	if err := a.enc.Flush(); err != nil {
		return err
	}
	if t.Sub(a.synced) >= 30*time.Second {
		a.synced = t
		return a.f.Sync()
	}
	return nil
}

func (a *activity) resume(t time.Time) {
	a.write(eventMsg, fit.Time(t), eventTimer, typeStart, 0)
	a.running, a.prev = true, time.Time{}
}

func (a *activity) pause(t time.Time, kind int64) {
	a.write(eventMsg, fit.Time(t), eventTimer, kind, 0)
	a.running = false
}

// device writes a device_info for an ANT+ sensor the first time it shows up.
func (a *activity) device(t time.Time, index, antType int, number, mfr, model uint16, sw string) {
	if number == 0 || a.devices[number] {
		return
	}
	a.devices[number] = true
	swv := int64(fit.Invalid)
	var v float64
	if _, err := fmt.Sscan(sw, &v); err == nil {
		swv = int64(math.Round(v * 100))
	}
	a.write(deviceInfoMsg, fit.Time(t), int64(index), int64(antType), int64(mfr), int64(model), swv, int64(number), sourceANTPlus)
}

// setFTP records the rider's FTP when it is first known or changes.
func (a *activity) setFTP(ftp float64) {
	if ftp > 0 && ftp != a.ftp {
		a.write(zonesTargetMsg, int64(math.Round(ftp)))
	}
	a.ftp = ftp
}

// record writes a sample while the timer runs.
func (a *activity) record(x sample) {
	var dt time.Duration
	if !a.prev.IsZero() {
		dt = min(x.t.Sub(a.prev), 2*time.Second) // a stall must not count as riding
	}
	a.prev = x.t
	alt, grade := int64(fit.Invalid), int64(fit.Invalid)
	if x.onCourse {
		alt, grade = int64(math.Round((x.alt+500)*5)), int64(math.Round(x.grade*100))
	}
	lat, lon := int64(fit.Invalid), int64(fit.Invalid)
	if x.hasPos {
		lat, lon = semicircles(x.lat), semicircles(x.lon)
	}
	a.write(recordMsg, fit.Time(x.t), opt(x.power), opt(x.hr), opt(x.cad),
		int64(math.Round(x.speed*1000)), int64(math.Round(x.dist*100)), alt, grade, lat, lon)
	a.session.add(x, dt)
	a.lap.add(x, dt)
}

func opt(v int) int64 {
	if v < 0 {
		return fit.Invalid
	}
	return int64(v)
}

// endLap closes the current lap at t, unless nothing was recorded in it.
// It returns the lap's index, or -1.
func (a *activity) endLap(t time.Time, trigger int64) int {
	l := &a.lap
	if l.n == 0 {
		a.lap = newSummary(t, a.session.dist)
		return -1
	}
	a.write(lapMsg, int64(a.laps), fit.Time(t), eventLap, typeStop, fit.Time(l.start),
		ms(t.Sub(l.start)), ms(l.timer), int64(math.Round((l.dist-l.dist0)*100)), l.calories(),
		int64(math.Round(l.avgSpeed()*1000)), int64(math.Round(l.speedMax*1000)),
		avg(l.hrSum, l.hrN), orInvalid(l.hrMax, l.hrN), avg(l.cadSum, l.cadN), orInvalid(l.cadMax, l.cadN),
		avg(l.powerSum, l.powerN), orInvalid(l.powerMax, l.powerN),
		int64(math.Round(l.ascent)), int64(math.Round(l.descent)),
		trigger, sportCycling, l.subSport(), int64(math.Round(l.work)))
	a.laps++
	a.lap = newSummary(t, a.session.dist)
	// A finished lap is an effort worth keeping through a crash or a power
	// cut: the next flush puts it on the disk.
	a.synced = time.Time{}
	return a.laps - 1
}

// finish writes the summary messages at t (the last record) and turns
// the .part file into the finished .fit. It reports whether there was
// anything to keep; an empty activity is removed.
func (a *activity) finish(t time.Time) (string, bool, error) {
	if a.session.n == 0 {
		a.f.Close()
		return "", false, os.Remove(a.path)
	}
	if a.running {
		a.pause(t, typeStopAll)
	}
	a.endLap(t, lapSessionEnd)
	s := &a.session
	ftp := int64(fit.Invalid)
	if a.ftp > 0 {
		ftp = int64(math.Round(a.ftp))
	}
	ts := fit.Time(t)
	a.write(sessionMsg, 0, ts, eventSession, typeStop, fit.Time(s.start),
		ms(t.Sub(s.start)), ms(s.timer), int64(math.Round((s.dist-s.dist0)*100)), s.calories(),
		int64(math.Round(s.avgSpeed()*1000)), int64(math.Round(s.speedMax*1000)),
		avg(s.hrSum, s.hrN), orInvalid(s.hrMax, s.hrN), avg(s.cadSum, s.cadN), orInvalid(s.cadMax, s.cadN),
		avg(s.powerSum, s.powerN), orInvalid(s.powerMax, s.powerN),
		int64(math.Round(s.ascent)), int64(math.Round(s.descent)),
		sportCycling, s.subSport(), 0, int64(a.laps), sessionActEnd, s.normalizedPower(), ftp, int64(math.Round(s.work)))
	_, offset := t.Zone()
	a.write(activityMsg, ts, ms(s.timer), 1, activityManual, eventActivity, typeStop, ts+int64(offset))
	if err := a.flush(time.Time{}); err != nil {
		a.f.Close()
		return "", true, err
	}
	path, err := seal(a.f, a.path)
	return path, true, err
}

// seal writes the header's data size and the CRC, syncs, closes and
// renames name.fit.part to name.fit.
func seal(f *os.File, path string) (string, error) {
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(fit.HeaderSize, io.SeekStart); err != nil {
		return "", err
	}
	var crc uint16
	r := bufio.NewReader(io.LimitReader(f, end-fit.HeaderSize))
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		crc = fit.CRC(crc, buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if _, err := f.WriteAt(binary.LittleEndian.AppendUint16(nil, crc), end); err != nil {
		return "", err
	}
	if _, err := f.WriteAt(fit.Header(uint32(end-fit.HeaderSize)), 0); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	done := strings.TrimSuffix(path, ".part")
	return done, os.Rename(path, done)
}
