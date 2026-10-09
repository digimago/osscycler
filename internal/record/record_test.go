package record

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/digimago/osscycler/internal/fit"
	"github.com/digimago/osscycler/internal/telemetry"
)

var t0 = time.Date(2026, 10, 7, 18, 30, 0, 0, time.UTC)

func state(power, cad int, speed float64) telemetry.State {
	var st telemetry.State
	st.Trainer.Sensor = telemetry.Sensor{Status: telemetry.StatusConnected, DeviceNumber: 47508, ManufacturerID: 89, ModelNumber: 2980, SWVersion: "45.3"}
	st.Trainer.PowerW = telemetry.Some(uint16(power))
	st.Trainer.CadenceRPM = telemetry.Some(uint8(cad))
	st.Trainer.SpeedMPS = telemetry.Some(speed)
	st.HeartRate.BPM = telemetry.Some(uint8(140))
	st.Workout.FTPW = 250
	return st
}

// script is a ride: each step lasts n seconds with the state from f(i),
// where i counts seconds from the start of the ride.
type step struct {
	n int
	f func(i int) telemetry.State
}

func play(r *Recorder, steps []step) time.Time {
	i := 0
	for _, s := range steps {
		for range s.n {
			r.step(s.f(i), t0.Add(time.Duration(i)*time.Second))
			i++
		}
	}
	return t0.Add(time.Duration(i) * time.Second)
}

func newRecorder(t *testing.T) *Recorder {
	return New(telemetry.NewHub(), DefaultConfig(t.TempDir()), slog.New(slog.DiscardHandler))
}

func onCourse(phase telemetry.RidePhase) func(int) telemetry.State {
	return func(i int) telemetry.State {
		st := state(250, 85, 6)
		st.Ride = telemetry.Ride{Phase: phase, CourseID: "mash", CourseName: "Mountain Mash", SpeedMPS: 5,
			ElevationM: 100 + float64(i%1000)*0.5, GradePct: 8, CourseDistanceM: 300, Elapsed: 60 * time.Second, AvgPowerW: 250,
			Lat: 52 + float64(i)*1e-5, Lon: 5}
		return st
	}
}

// aRide: idle, free riding, a course ride that finishes, a stop, more
// riding, then idle until the activity ends.
var aRide = []step{
	{10, func(int) telemetry.State { return state(0, 0, 0) }},
	{120, func(int) telemetry.State { return state(200, 90, 8) }},
	{60, onCourse(telemetry.RideRiding)},
	{20, func(i int) telemetry.State {
		st := onCourse(telemetry.RideFinished)(i)
		st.Ride.SpeedMPS = 0
		return st
	}},
	{10, func(int) telemetry.State { return state(0, 0, 0) }},
	{30, func(int) telemetry.State { return state(150, 80, 7) }},
	{301, func(int) telemetry.State { return state(0, 0, 0) }},
}

func decodeOnly(t *testing.T, dir string) (string, []fit.Msg) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.fit"))
	if len(files) != 1 {
		parts, _ := filepath.Glob(filepath.Join(dir, "*"))
		t.Fatalf("want one .fit file, have %v", parts)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := fit.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	return files[0], msgs
}

func byNum(msgs []fit.Msg, num uint16) []fit.Msg {
	var out []fit.Msg
	for _, m := range msgs {
		if m.Num == num {
			out = append(out, m)
		}
	}
	return out
}

func field(t *testing.T, m fit.Msg, num uint8) int64 {
	t.Helper()
	v, ok := m.Get(num)
	if !ok {
		t.Fatalf("message %d lacks field %d", m.Num, num)
	}
	return v
}

func TestRecordsARide(t *testing.T) {
	r := newRecorder(t)
	play(r, aRide)
	if r.a != nil {
		t.Fatal("activity still open after 5 min idle")
	}
	path, msgs := decodeOnly(t, r.cfg.Dir)
	if filepath.Base(path) != t0.Add(10*time.Second).Local().Format("2006-01-02-150405")+".fit" {
		t.Errorf("file name %s", filepath.Base(path))
	}

	records := byNum(msgs, recordMsg.Num)
	// Riding 120+60+20 s, 3 s until the auto-pause, then 30 s, and 3 s again.
	if n := len(records); n != 120+60+20+3+30+3 {
		t.Errorf("%d records", n)
	}
	laps := byNum(msgs, lapMsg.Num)
	if len(laps) != 3 {
		t.Fatalf("%d laps, want free / course / free", len(laps))
	}
	course := laps[1]
	if field(t, course, 39) != subVirtual || field(t, laps[0], 39) != subIndoor {
		t.Error("sub sports: course lap should be virtual, free riding indoor")
	}
	if got := field(t, course, 8); got != 60_000 {
		t.Errorf("course lap timer %d ms, want 60 s", got)
	}
	if got := field(t, course, 9); got != 60*5*100 {
		t.Errorf("course lap distance %d cm, want 300 m at the simulated 5 m/s", got)
	}
	if got := field(t, course, 21); got != 30 { // 60 s at +0.5 m/s, the first sample only sets the base
		t.Errorf("course lap ascent %d m", got)
	}
	if field(t, course, 19) != 250 || field(t, laps[0], 19) != 200 {
		t.Error("lap average power")
	}

	sessions := byNum(msgs, sessionMsg.Num)
	if len(sessions) != 1 {
		t.Fatalf("%d sessions", len(sessions))
	}
	s := sessions[0]
	wantTimer := int64(120+60+20+3+30+3-2) * 1000 // the first record after each start adds no time
	if got := field(t, s, 8); got != wantTimer {
		t.Errorf("session timer %d, want %d", got, wantTimer)
	}
	if field(t, s, 7) <= field(t, s, 8) {
		t.Error("elapsed time should include the pause")
	}
	if field(t, s, 26) != 3 || field(t, s, 6) != subVirtual || field(t, s, 45) != 250 {
		t.Errorf("session laps %d, sub sport %d, FTP %d", field(t, s, 26), field(t, s, 6), field(t, s, 45))
	}
	if np := field(t, s, 34); np < 190 || np > 230 {
		t.Errorf("normalized power %d", np)
	}
	if field(t, s, 16) != 140 || field(t, s, 17) != 140 {
		t.Error("heart rate")
	}

	events := byNum(msgs, eventMsg.Num)
	var kinds []int64
	for _, e := range events {
		kinds = append(kinds, field(t, e, 1))
	}
	if want := []int64{typeStart, typeStop, typeStart, typeStop}; !slices.Equal(kinds, want) {
		t.Errorf("timer events %v, want start stop start stop", kinds)
	}
	if devs := byNum(msgs, deviceInfoMsg.Num); len(devs) != 2 || field(t, devs[1], 21) != 47508 || field(t, devs[1], 2) != 89 {
		t.Errorf("device infos %v", devs)
	}

	// The finished course ride is in the results, pointing at its lap.
	f, err := os.Open(filepath.Join(r.cfg.Dir, ResultsFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var res []Result
	for sc.Scan() {
		var x Result
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			t.Fatal(err)
		}
		res = append(res, x)
	}
	if len(res) != 1 || res[0].CourseID != "mash" || res[0].Lap != 1 || res[0].File != filepath.Base(path) || res[0].ElapsedS != 60 {
		t.Errorf("results %+v", res)
	}
}

// crash plays a ride and abandons the activity mid-way, as a crash would.
func crash(t *testing.T, cut int64) *Recorder {
	r := newRecorder(t)
	play(r, aRide[:4])
	if err := r.a.enc.Flush(); err != nil {
		t.Fatal(err)
	}
	if cut > 0 {
		end, _ := r.a.f.Seek(0, 2)
		r.a.f.Truncate(end - cut)
	}
	r.a.f.Close()
	r.a = nil
	return r
}

func TestRecoverMatchesCleanFinish(t *testing.T) {
	clean := newRecorder(t)
	play(clean, aRide[:4])
	clean.end()
	_, want := decodeOnly(t, clean.cfg.Dir)

	r := crash(t, 0)
	parts, _ := filepath.Glob(filepath.Join(r.cfg.Dir, "*.fit.part"))
	if len(parts) != 1 {
		t.Fatalf("parts %v", parts)
	}
	r.recoverAll()
	_, got := decodeOnly(t, r.cfg.Dir)
	ws, gs := byNum(want, sessionMsg.Num)[0], byNum(got, sessionMsg.Num)[0]
	for k, v := range ws.Fields {
		if gs.Fields[k] != v {
			t.Errorf("session field %d: recovered %d, clean %d", k, gs.Fields[k], v)
		}
	}
	if len(byNum(got, lapMsg.Num)) != len(byNum(want, lapMsg.Num)) {
		t.Error("lap count differs")
	}
}

func TestRecoverTornWrite(t *testing.T) {
	r := crash(t, 7) // the last record is cut short
	r.recoverAll()
	_, msgs := decodeOnly(t, r.cfg.Dir)
	if n := len(byNum(msgs, recordMsg.Num)); n != 120+60+20-1 {
		t.Errorf("%d records survive, want all but the torn one", n)
	}
	if len(byNum(msgs, sessionMsg.Num)) != 1 || len(byNum(msgs, activityMsg.Num)) != 1 {
		t.Error("summary messages missing")
	}
}

func TestRecoverCutShortFinish(t *testing.T) {
	r := newRecorder(t)
	play(r, aRide[:4])
	a := r.a
	// Finish, then pretend the activity message and sealing never happened.
	a.endLap(t0.Add(210*time.Second), lapSessionEnd)
	a.write(sessionMsg, make([]int64, len(sessionMsg.Fields))...)
	a.enc.Flush()
	a.f.Close()
	r.a = nil
	r.recoverAll()
	_, msgs := decodeOnly(t, r.cfg.Dir)
	if n := len(byNum(msgs, sessionMsg.Num)); n != 1 {
		t.Errorf("%d sessions after recovery", n)
	}
	if n := len(byNum(msgs, lapMsg.Num)); n != 3 {
		t.Errorf("%d laps after recovery", n)
	}
}

func TestRecoverEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.fit.part")
	a, err := create(path, t0)
	if err != nil {
		t.Fatal(err)
	}
	a.enc.Flush()
	a.f.Close()
	if got, err := Recover(path); err != nil || got != "" {
		t.Errorf("Recover = %q, %v", got, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("empty recording kept")
	}
}

func TestNoActivityWhileIdle(t *testing.T) {
	r := newRecorder(t)
	play(r, []step{{60, func(int) telemetry.State { return state(0, 0, 0) }}})
	if files, _ := filepath.Glob(filepath.Join(r.cfg.Dir, "*")); len(files) != 0 {
		t.Errorf("files while idle: %v", files)
	}
}

func TestPublish(t *testing.T) {
	r := newRecorder(t)
	play(r, aRide[:3])
	r.publish()
	st, _ := r.hub.Latest()
	if !st.Recording.Active || st.Recording.Paused || st.Recording.Timer != 179*time.Second {
		t.Errorf("recording %+v", st.Recording)
	}
	r.end()
	r.publish()
	st, _ = r.hub.Latest()
	if st.Recording.Active || st.Recording.LastSaved == "" {
		t.Errorf("after end %+v", st.Recording)
	}
}

func TestEndByRider(t *testing.T) {
	pedal := func(int) telemetry.State { return state(200, 90, 8) }
	still := func(int) telemetry.State { return state(0, 0, 0) }

	r := newRecorder(t)
	end := play(r, []step{{60, pedal}})
	file, err := r.endByRider(state(200, 90, 8), false)
	if err != nil || file == "" || r.a != nil {
		t.Fatalf("save: %q, %v", file, err)
	}
	if _, msgs := decodeOnly(t, r.cfg.Dir); len(byNum(msgs, recordMsg.Num)) != 60 {
		t.Error("saved file incomplete")
	}
	// Pedalling on doesn't start a new file; stopping and starting does.
	for i := range 10 {
		r.step(pedal(0), end.Add(time.Duration(i)*time.Second))
	}
	if r.a != nil {
		t.Fatal("a new activity opened right after ending one")
	}
	r.step(still(0), end.Add(10*time.Second))
	r.step(pedal(0), end.Add(11*time.Second))
	if r.a == nil {
		t.Fatal("no new activity after stopping and riding on")
	}

	// Discard deletes the file under way.
	part := r.a.path
	if file, err := r.endByRider(state(0, 0, 0), true); err != nil || file != "" {
		t.Errorf("discard: %q, %v", file, err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Error("discarded recording still on disk")
	}
	if _, err := r.endByRider(state(0, 0, 0), false); err != ErrNotRecording {
		t.Errorf("nothing to end: %v", err)
	}

	// Not during a course ride.
	r2 := newRecorder(t)
	play(r2, []step{{5, onCourse(telemetry.RideRiding)}})
	if _, err := r2.endByRider(onCourse(telemetry.RideRiding)(0), false); err != ErrBusy {
		t.Errorf("during a course ride: %v", err)
	}
}

func TestEndThroughRun(t *testing.T) {
	r := newRecorder(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- r.Run(ctx) }()
	if _, err := r.End(context.Background(), false); err != ErrNotRecording {
		t.Errorf("End while idle: %v", err)
	}
	cancel()
	<-done
	if _, err := r.End(context.Background(), false); err != ErrStopped {
		t.Errorf("End after Run: %v", err)
	}
}

func TestPositions(t *testing.T) {
	if got := degrees(semicircles(52.1234567)); math.Abs(got-52.1234567) > 1e-7 {
		t.Errorf("semicircles round trip: %v", got)
	}
	for _, gps := range []bool{true, false} {
		r := newRecorder(t)
		r.cfg.GPS = gps
		play(r, aRide)
		_, msgs := decodeOnly(t, r.cfg.Dir)
		records := byNum(msgs, recordMsg.Num)
		free, course := records[10], records[150] // second 20 of the ride, and on the course
		if _, ok := free.Get(0); ok {
			t.Error("free riding has a position")
		}
		lat, ok := course.Get(0)
		if ok != gps {
			t.Fatalf("GPS %v: course record position present %v", gps, ok)
		}
		if gps {
			lon, _ := course.Get(1)
			// The course sample is 150 s into the ride, 160 s after t0.
			if math.Abs(degrees(lat)-(52+160e-5)) > 1e-6 || math.Abs(degrees(lon)-5) > 1e-6 {
				t.Errorf("position %.6f, %.6f", degrees(lat), degrees(lon))
			}
		}
	}

	// Recovery keeps positions.
	r := crash(t, 0)
	r.recoverAll()
	_, msgs := decodeOnly(t, r.cfg.Dir)
	if _, ok := byNum(msgs, recordMsg.Num)[150].Get(0); !ok {
		t.Error("recovered course record lost its position")
	}
}

func TestLoopLaps(t *testing.T) {
	r := newRecorder(t)
	loop := func(phase telemetry.RidePhase) func(int) telemetry.State {
		return func(i int) telemetry.State {
			st := state(250, 85, 10)
			st.Ride = telemetry.Ride{Phase: phase, Loop: true, CourseID: "oval-400", CourseName: "Oval 400 m", CourseDistanceM: 400,
				SpeedMPS: 10, Lat: -25.5, Lon: -20, Lap: i/30 + 1}
			// A lap every 30 s; the first from a standstill.
			if n := i / 30; n > 0 {
				st.Ride.LastLap = telemetry.Lap{N: n, Elapsed: 30 * time.Second, AvgPowerW: 250,
					StartSpeedMPS: map[bool]float64{true: 0, false: 10}[n == 1], Finished: t0.Add(time.Duration(n*30) * time.Second)}
			}
			return st
		}
	}
	end := play(r, []step{{31, loop(telemetry.RideRiding)}})
	// The first lap is on the disk at once, not up to 30 s later.
	if r.a == nil || !r.a.synced.Equal(end.Add(-time.Second)) {
		t.Errorf("lap not synced: synced %v, lap at %v", r.a.synced, end.Add(-time.Second))
	}
	for i := 31; i < 100; i++ {
		r.step(loop(telemetry.RideRiding)(i), t0.Add(time.Duration(i)*time.Second))
	}
	for i := 100; i < 105; i++ { // the rider stops the loop ride
		r.step(loop(telemetry.RideAborted)(i), t0.Add(time.Duration(i)*time.Second))
	}
	r.end()

	_, msgs := decodeOnly(t, r.cfg.Dir)
	var loopLaps []int64
	for _, m := range byNum(msgs, 19) { // lap
		if field(t, m, 24) == lapPosition { // lap_trigger
			loopLaps = append(loopLaps, field(t, m, 254))
		}
	}
	if len(loopLaps) != 3 {
		t.Fatalf("laps of the loop %v, want 3", loopLaps)
	}
	res, err := ReadResults(r.cfg.Dir)
	if err != nil || len(res) != 3 {
		t.Fatalf("results %+v, %v", res, err)
	}
	for i, x := range res {
		if x.CourseID != "oval-400" || x.StartM != 0 || x.DistanceM != 400 || x.ElapsedS != 30 || int64(x.Lap) != loopLaps[i] || x.File == "" {
			t.Errorf("result %d: %+v", i, x)
		}
	}
	if res[0].StartSpeedMPS != 0 || res[1].StartSpeedMPS != 10 {
		t.Errorf("start speeds %v, %v", res[0].StartSpeedMPS, res[1].StartSpeedMPS)
	}
}

// A finished lap is on the disk at once, not up to 30 s later: here the
// lap of free riding that ends when a course ride starts.
func TestLapSynced(t *testing.T) {
	r := newRecorder(t)
	end := play(r, []step{
		{5, func(int) telemetry.State { return state(200, 90, 8) }},
		{1, onCourse(telemetry.RideRiding)},
	})
	if r.a == nil || !r.a.synced.Equal(end.Add(-time.Second)) {
		t.Errorf("synced at %v, the lap ended at %v", r.a.synced, end.Add(-time.Second))
	}
}
