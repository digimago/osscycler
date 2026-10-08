package record

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/digimago/osscycler/internal/sim"
	"github.com/digimago/osscycler/internal/telemetry"
)

// Config sets when an activity starts, pauses and ends.
type Config struct {
	Dir string
	// PauseAfter without moving stops the timer (auto-pause).
	PauseAfter time.Duration
	// EndAfter without moving ends and saves the activity.
	EndAfter time.Duration
	// GPS writes the course's map position into the records of course
	// rides, so uploads show the route on a map (as Zwift's do).
	GPS bool
}

func DefaultConfig(dir string) Config {
	return Config{Dir: dir, PauseAfter: 3 * time.Second, EndAfter: 5 * time.Minute, GPS: true}
}

// ResultsFile in the recording directory gets one JSON line per
// finished course ride.
const ResultsFile = "results.jsonl"

// Result is a finished course ride. File and Lap point at the lap in the
// activity that holds the ride, e.g. for a ghost.
type Result struct {
	Finished      time.Time `json:"finished"`
	CourseID      string    `json:"course_id"`
	CourseName    string    `json:"course_name"`
	StartM        float64   `json:"start_m"`
	DistanceM     float64   `json:"distance_m"`
	ElapsedS      float64   `json:"elapsed_s"`
	AvgPowerW     float64   `json:"avg_power_w"`
	ClimbedM      float64   `json:"climbed_m"`
	DifficultyPct float64   `json:"difficulty_pct"`
	File          string    `json:"file"`
	Lap           int       `json:"lap"`
	// StartSpeedMPS is how fast a lap of a loop began (0 from a standstill).
	StartSpeedMPS float64 `json:"start_speed_mps,omitempty"`
	// Sim is what the ride was simulated with, for replays.
	Sim SimParams `json:"sim"`
}

// SimParams mirror sim.Params in the results file.
type SimParams struct {
	MassKg        float64 `json:"mass_kg"`
	CdA           float64 `json:"cda"`
	Crr           float64 `json:"crr"`
	AirDensity    float64 `json:"air_density"`
	DrivetrainEff float64 `json:"drivetrain_eff"`
}

// ReadResults reads the results file in dir, oldest first. A missing
// file is no results; a line that doesn't parse (such as one being
// appended right now) is skipped.
func ReadResults(dir string) ([]Result, error) {
	f, err := os.Open(filepath.Join(dir, ResultsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rs []Result
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r Result
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			rs = append(rs, r)
		}
	}
	return rs, sc.Err()
}

// Params converts back; ok is false for results written before the
// parameters were recorded.
func (p SimParams) Params() (sim.Params, bool) {
	return sim.Params(p), p.MassKg > 0
}

// Recorder samples the hub once a second into FIT files.
type Recorder struct {
	hub *telemetry.Hub
	cfg Config
	log *slog.Logger

	a          *activity
	lastStep   time.Time
	lastMoving time.Time
	stillSince time.Time // zero while moving
	dist       float64
	riding     bool // course ride under way at the last step
	working    bool // workout under way at the last step
	ride       telemetry.Ride
	lastSaved  string
	errMsg     string
	// held: the rider ended the activity; the next one waits until they
	// have stopped, so pedalling on doesn't open a new file at once.
	held bool
	ends chan endReq
	done chan struct{} // closed when Run returns
}

type endReq struct {
	discard bool
	reply   chan endReply
}

type endReply struct {
	file string
	err  error
}

var (
	// ErrNotRecording: there is no activity to end.
	ErrNotRecording = errors.New("no ride is being recorded")
	// ErrBusy: a course ride or workout is under way; stop it first.
	ErrBusy = errors.New("stop the course ride or workout first")
	// ErrStopped: the recorder isn't running.
	ErrStopped = errors.New("the recorder has stopped")
)

func New(hub *telemetry.Hub, cfg Config, log *slog.Logger) *Recorder {
	return &Recorder{hub: hub, cfg: cfg, log: log, ends: make(chan endReq), done: make(chan struct{})}
}

// End ends the activity under way, as the rider's "end ride": saved, as
// by default, or discarded (the file is deleted). It returns the saved
// file's name.
func (r *Recorder) End(ctx context.Context, discard bool) (string, error) {
	req := endReq{discard: discard, reply: make(chan endReply, 1)}
	select {
	case r.ends <- req:
	case <-r.done:
		return "", ErrStopped
	case <-ctx.Done():
		return "", ctx.Err()
	}
	rep := <-req.reply
	return rep.file, rep.err
}

// Run finishes files a crash left behind, then records until ctx ends,
// when the activity under way is saved.
func (r *Recorder) Run(ctx context.Context) error {
	if err := os.MkdirAll(r.cfg.Dir, 0o700); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	defer close(r.done)
	r.recoverAll()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			r.end()
			r.publish()
			return nil
		case now := <-tick.C:
			st, _ := r.hub.Latest()
			r.step(st, now)
			r.publish()
		case req := <-r.ends:
			st, _ := r.hub.Latest()
			file, err := r.endByRider(st, req.discard)
			req.reply <- endReply{file, err}
			r.publish()
		}
	}
}

func (r *Recorder) recoverAll() {
	parts, _ := filepath.Glob(filepath.Join(r.cfg.Dir, "*.fit.part"))
	for _, p := range parts {
		path, err := Recover(p)
		switch {
		case err != nil:
			r.log.Error("recording left by a crash can't be finished", "file", p, "err", err)
		case path == "":
			r.log.Info("removed an empty unfinished recording", "file", p)
		default:
			r.log.Info("finished a recording left by a crash", "file", path)
		}
	}
}

// moving reports whether the rider is riding: pedalling, or rolling on a
// course (coasting downhill counts).
func moving(st telemetry.State, speed float64) bool {
	t := st.Trainer
	return (t.PowerW.OK && t.PowerW.V > 0) || (t.CadenceRPM.OK && t.CadenceRPM.V > 0) || speed > 0.3
}

// speedOf is the speed that counts: simulated on a course, the trainer's
// own otherwise.
func speedOf(st telemetry.State) float64 {
	if st.Ride.Phase == telemetry.RideRiding {
		return st.Ride.SpeedMPS
	}
	if s := st.Trainer.SpeedMPS; s.OK {
		return s.V
	}
	return 0
}

func (r *Recorder) step(st telemetry.State, now time.Time) {
	speed := speedOf(st)
	mov := moving(st, speed)
	dt := time.Duration(0)
	if !r.lastStep.IsZero() {
		dt = min(now.Sub(r.lastStep), 2*time.Second)
	}
	r.lastStep = now
	if mov {
		r.lastMoving, r.stillSince = now, time.Time{}
	} else if r.stillSince.IsZero() {
		r.stillSince = now
	}

	if r.a == nil {
		if !mov {
			r.riding, r.working, r.held = false, false, false
			return
		}
		if r.held {
			return
		}
		if !r.open(now) {
			return
		}
		dt = 0
	}
	a := r.a
	a.setFTP(st.Workout.FTPW)

	// Laps: one for each course ride and each workout.
	riding := st.Ride.Phase == telemetry.RideRiding
	working := st.Workout.Phase == telemetry.WorkoutRunning || st.Workout.Phase == telemetry.WorkoutPaused
	if riding != r.riding || working != r.working {
		lap := a.endLap(now, lapManual)
		if r.riding && !riding && st.Ride.Phase == telemetry.RideFinished {
			r.result(st.Ride, now, lap)
		}
	} else if l := st.Ride.LastLap; riding && st.Ride.Loop && l.N > 0 && l.N != r.ride.LastLap.N {
		// Round a loop: a lap of the activity, and a result, for each.
		r.lapResult(st.Ride, a.endLap(now, lapPosition))
	}
	r.riding, r.working, r.ride = riding, working, st.Ride

	if tr := st.Trainer.Sensor; tr.Status == telemetry.StatusConnected {
		a.device(now, 1, 17, tr.DeviceNumber, tr.ManufacturerID, tr.ModelNumber, tr.SWVersion)
	}
	if hr := st.HeartRate.Sensor; hr.Status == telemetry.StatusConnected {
		a.device(now, 2, 120, hr.DeviceNumber, hr.ManufacturerID, hr.ModelNumber, hr.SWVersion)
	}

	switch {
	case mov && !a.running:
		a.resume(now)
	case !mov && a.running && now.Sub(r.stillSince) >= r.cfg.PauseAfter:
		a.pause(now, typeStop)
	}
	if a.running {
		r.dist += speed * dt.Seconds()
		x := sampleOf(st, now, speed, r.dist)
		x.hasPos = x.hasPos && r.cfg.GPS
		a.record(x)
	}
	if err := a.flush(now); err != nil {
		r.fail(err)
		return
	}
	if !mov && now.Sub(r.lastMoving) >= r.cfg.EndAfter {
		r.end()
	}
}

func sampleOf(st telemetry.State, now time.Time, speed, dist float64) sample {
	x := sample{t: now, power: -1, hr: -1, cad: -1, speed: speed, dist: dist}
	t := st.Trainer
	if t.PowerW.OK {
		x.power = int(t.PowerW.V)
	}
	if t.CadenceRPM.OK {
		x.cad = int(t.CadenceRPM.V)
	}
	// Prefer the strap; fall back to heart rate the trainer forwards.
	if h := st.HeartRate.BPM; h.OK {
		x.hr = int(h.V)
	} else if t.HeartRateBPM.OK {
		x.hr = int(t.HeartRateBPM.V)
	}
	if st.Ride.Phase == telemetry.RideRiding {
		x.onCourse, x.alt, x.grade = true, st.Ride.ElevationM, st.Ride.GradePct
		x.hasPos, x.lat, x.lon = true, st.Ride.Lat, st.Ride.Lon
	}
	return x
}

func (r *Recorder) open(now time.Time) bool {
	name := now.Local().Format("2006-01-02-150405") + ".fit.part"
	a, err := create(filepath.Join(r.cfg.Dir, name), now)
	if err != nil {
		r.fail(err)
		return false
	}
	r.a, r.dist, r.errMsg = a, 0, ""
	r.log.Info("recording", "file", a.path)
	return true
}

// end saves the activity under way, if any.
func (r *Recorder) end() {
	a := r.a
	if a == nil {
		return
	}
	r.a = nil
	at := a.session.last
	if a.lap.n == 0 && a.session.n == 0 {
		at = r.lastStep
	}
	path, kept, err := a.finish(at)
	switch {
	case err != nil:
		r.fail(err)
	case kept:
		r.lastSaved = filepath.Base(path)
		r.log.Info("ride saved", "file", path, "timer", a.session.timer.Round(time.Second),
			"distance_km", fmt.Sprintf("%.2f", (a.session.dist-a.session.dist0)/1000), "laps", a.laps)
	}
}

// endByRider ends the activity on the rider's request.
func (r *Recorder) endByRider(st telemetry.State, discard bool) (string, error) {
	switch {
	case r.a == nil:
		return "", ErrNotRecording
	case st.Ride.Phase.Active() || st.Workout.Phase.Active():
		return "", ErrBusy
	}
	r.held = true
	if !discard {
		r.end()
		if r.errMsg != "" {
			return "", errors.New(r.errMsg)
		}
		return r.lastSaved, nil
	}
	a := r.a
	r.a = nil
	a.f.Close()
	if err := os.Remove(a.path); err != nil {
		r.fail(err)
		return "", err
	}
	r.log.Info("ride discarded", "file", a.path, "timer", a.session.timer.Round(time.Second))
	return "", nil
}

func (r *Recorder) fail(err error) {
	r.errMsg = err.Error()
	r.log.Error("recording failed", "err", err)
	if r.a != nil {
		r.a.f.Close()
		r.a = nil
	}
}

// result appends a finished course ride to the results file.
func (r *Recorder) result(rd telemetry.Ride, now time.Time, lap int) {
	res := Result{
		Finished: now.UTC().Truncate(time.Second), CourseID: rd.CourseID, CourseName: rd.CourseName,
		StartM: rd.StartDistanceM, DistanceM: rd.CourseDistanceM - rd.StartDistanceM,
		ElapsedS: rd.Elapsed.Seconds(), AvgPowerW: rd.AvgPowerW, ClimbedM: rd.ClimbedM,
		DifficultyPct: rd.DifficultyPct, Lap: lap, Sim: SimParams(rd.Sim),
	}
	r.appendResult(res)
}

// appendResult adds a result to the results file, naming the recording
// it is in.
func (r *Recorder) appendResult(res Result) {
	if r.a != nil {
		res.File = filepath.Base(r.a.path[:len(r.a.path)-len(".part")])
	}
	b, _ := json.Marshal(res)
	f, err := os.OpenFile(filepath.Join(r.cfg.Dir, ResultsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(b, '\n'))
		err = errors.Join(err, f.Close())
	}
	if err != nil {
		r.log.Error("saving the course result failed", "err", err)
	}
}

// lapResult appends a completed lap of a loop to the results file, as a
// ride of the whole loop: the history then knows the best lap on it.
func (r *Recorder) lapResult(rd telemetry.Ride, lap int) {
	l := rd.LastLap
	r.appendResult(Result{
		Finished: l.Finished.UTC().Truncate(time.Second), CourseID: rd.CourseID, CourseName: rd.CourseName,
		DistanceM: rd.CourseDistanceM, ElapsedS: l.Elapsed.Seconds(), AvgPowerW: l.AvgPowerW, ClimbedM: l.ClimbedM,
		DifficultyPct: rd.DifficultyPct, Lap: lap, Sim: SimParams(rd.Sim), StartSpeedMPS: l.StartSpeedMPS,
	})
}

// publish puts the recorder's status in the state for renderers.
func (r *Recorder) publish() {
	rec := telemetry.Recording{LastSaved: r.lastSaved, Error: r.errMsg}
	if a := r.a; a != nil {
		rec.Active, rec.Paused, rec.File = true, !a.running, filepath.Base(a.path[:len(a.path)-len(".part")])
		rec.Timer, rec.DistanceM = a.session.timer, a.session.dist
	}
	r.hub.Update(func(st *telemetry.State) bool {
		if st.Recording == rec {
			return false
		}
		st.Recording = rec
		return true
	})
}
