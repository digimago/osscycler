// Package telemetry turns sensor traffic into a single ride state and
// publishes it to any number of readers without ever blocking the producer.
package telemetry

import (
	"sync"
	"time"

	"github.com/digimago/osscycler/fec"
	"github.com/digimago/osscycler/sim"
)

// Opt is a reading that may be invalid or not received yet.
type Opt[T any] struct {
	V  T
	OK bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{V: v, OK: true} }

// OptOf adapts the (value, ok) results the page decoders return.
func OptOf[T any](v T, ok bool) Opt[T] { return Opt[T]{V: v, OK: ok} }

type SensorStatus uint8

const (
	StatusDisabled SensorStatus = iota + 1
	StatusSearching
	StatusConnected
	StatusLost // was connected, searching for the same device again
)

func (s SensorStatus) String() string {
	switch s {
	case StatusDisabled:
		return "disabled"
	case StatusSearching:
		return "searching"
	case StatusConnected:
		return "connected"
	case StatusLost:
		return "lost"
	}
	return "unknown"
}

// Sensor is what we know about one paired (or searching) device.
type Sensor struct {
	Status         SensorStatus
	DeviceNumber   uint16 // 0 until paired
	ManufacturerID uint16
	ModelNumber    uint16
	SWVersion      string
	LastSeen       time.Duration // State.Time of the last message
}

type Trainer struct {
	Sensor           Sensor
	State            fec.State
	PowerW           Opt[uint16]
	CadenceRPM       Opt[uint8]
	SpeedMPS         Opt[float64]
	HeartRateBPM     Opt[uint8] // forwarded by the trainer, if it has a source
	DistanceM        float64    // since core start, rollovers removed
	Elapsed          time.Duration
	Flags            fec.TrainerStatus // calibration / user config required
	TargetPowerLimit fec.TargetPowerLimit
	Calibration      Calibration
}

type CalibrationPhase uint8

const (
	CalIdle       CalibrationPhase = iota
	CalRequested                   // request sent, trainer hasn't started yet
	CalInProgress                  // trainer sends calibration-in-progress pages
	CalSucceeded
	CalFailed
	CalCancelled
)

func (p CalibrationPhase) String() string {
	return [...]string{"idle", "requested", "in-progress", "succeeded", "failed", "cancelled"}[p]
}

// Active reports whether a calibration is requested or running.
func (p CalibrationPhase) Active() bool { return p == CalRequested || p == CalInProgress }

// Calibration tracks a spin-down calibration (FE profile section 6.5).
type Calibration struct {
	Phase                CalibrationPhase
	SpeedCondition       fec.Condition
	TemperatureCondition fec.Condition
	TargetSpeedMPS       Opt[float64] // pedal up to this, then coast
	TemperatureC         Opt[float64]
	TargetSpinDownMS     Opt[uint16]
	SpinDownMS           Opt[uint16]   // the result
	Message              string        // why it failed, or advice while it runs
	Changed              time.Duration // State.Time of the last phase change
	LastProgress         time.Duration // State.Time of the last in-progress page
	CancelRequested      bool          // we asked the trainer to stop
}

func (c *Calibration) setPhase(p CalibrationPhase, now time.Duration, msg string) {
	c.Phase, c.Changed, c.Message = p, now, msg
}

type HeartRate struct {
	Sensor Sensor
	BPM    Opt[uint8]
	Legacy bool // strap uses the legacy (unpaged) format
}

// State is a value type: copies share nothing, so readers can keep them.
type State struct {
	Seq       uint64        // increments on every change
	Time      time.Duration // monotonic time since the hub was created
	Trainer   Trainer
	HeartRate HeartRate
	Ride      Ride
	Workout   Workout
	Recording Recording
	Profile   Profile
	Control   Control
	Radio     Radio
}

// Radio is the ANT+ stick. Known is false without one to look for (-fake).
type Radio struct {
	Known   bool
	Present bool   // the stick is open and running
	Error   string // why it isn't, while it's looked for
}

// ControlMode is what manual control makes the trainer hold.
type ControlMode uint8

const (
	ControlOff   ControlMode = iota // free riding, or a ride or workout drives it
	ControlPower                    // ERG at Target watts
	ControlGrade                    // simulation at Target percent
	ControlLevel                    // basic resistance at Target percent of maximum
)

func (m ControlMode) String() string {
	return [...]string{"off", "power", "grade", "level"}[m]
}

// Control is manual trainer control outside rides and workouts.
type Control struct {
	Mode    ControlMode
	Target  float64
	Changed time.Duration // State.Time of the last change
}

// Profile is the rider's settings as the core uses them (see package
// profile). Known is false for a core without profiles.
type Profile struct {
	Known            bool
	Complete         bool // weight and FTP are set: no onboarding needed
	NeedWeight       bool // course rides wait for it
	NeedFTP          bool // workouts wait for it
	WeightKg         float64
	FTPW             float64
	HeightCm         float64 // zero: unknown
	CdA              float64 // drag area rides start with, m²
	DifficultyPct    float64
	SuggestedFTPW    float64 // a starting point for onboarding
	WeightForced     bool    // from a flag this run: shown, never saved
	FTPForced        bool
	DifficultyForced bool
	HeightForced     bool
	CdAForced        bool   // -cda: the size of the rider doesn't apply
	Path             string // where it is saved
}

// Recording is the FIT recorder's status.
type Recording struct {
	Active    bool   // an activity file is open
	Paused    bool   // timer stopped: the rider isn't moving
	File      string // name of the file being written
	Timer     time.Duration
	DistanceM float64
	LastSaved string // name of the last file saved
	Error     string // why recording failed; cleared by the next activity
}

type WorkoutPhase uint8

const (
	WorkoutNone    WorkoutPhase = iota
	WorkoutArmed                // chosen; the clock starts at the first pedal stroke
	WorkoutRunning              // clock running
	WorkoutPaused               // rider stopped pedalling for a while; clock stopped
	WorkoutFinished
	WorkoutAborted
)

func (p WorkoutPhase) String() string {
	return [...]string{"none", "armed", "running", "paused", "finished", "aborted"}[p]
}

// Active reports whether a workout is armed, running or paused.
func (p WorkoutPhase) Active() bool {
	return p == WorkoutArmed || p == WorkoutRunning || p == WorkoutPaused
}

// Workout is an ERG workout in progress. FTPW and IntensityPct are
// settings, published with or without a workout.
type Workout struct {
	Phase    WorkoutPhase
	ID, Name string
	Duration time.Duration
	Elapsed  time.Duration

	Segment          int // index into the workout's timeline
	SegmentLabel     string
	SegmentRemaining time.Duration
	Free             bool    // no ERG target in this segment
	TargetW          float64 // ERG target; 0 when Free
	TargetCadence    int
	NextLabel        string
	NextTargetW      float64
	NextFree         bool
	Message          string // text event currently shown

	FTPW         float64
	IntensityPct float64
	AvgPowerW    float64
	Changed      time.Duration // State.Time of the last phase change
}

type RidePhase uint8

const (
	RideNone  RidePhase = iota
	RideArmed           // course chosen; the clock starts at the first pedal stroke
	RideRiding
	RideFinished
	RideAborted
)

func (p RidePhase) String() string {
	return [...]string{"none", "armed", "riding", "finished", "aborted"}[p]
}

// Active reports whether a ride is armed or under way.
func (p RidePhase) Active() bool { return p == RideArmed || p == RideRiding }

// Ride is a course ride: the simulated position, and the clock for a timed
// effort.
type Ride struct {
	Phase           RidePhase
	CourseID        string
	CourseName      string
	CourseDistanceM float64
	CourseGainM     float64

	DistanceM       float64 // along the course
	ElevationM      float64
	GradePct        float64 // course grade here
	TrainerGradePct float64 // what the trainer is asked to apply
	SpeedMPS        float64 // simulated speed
	ClimbedM        float64
	AvgPowerW       float64
	Elapsed         time.Duration
	Changed         time.Duration // State.Time of the last phase change
	// DifficultyPct is the share of climbs the trainer applies. It is a
	// setting, so it is published with or without a ride.
	DifficultyPct float64
	// StartDistanceM is where on the course the ride began (0 = the start).
	StartDistanceM float64
	// Lat and Lon place the rider on the course's GPX track, in degrees.
	Lat, Lon float64
	// Sim is what the ride is simulated with, so a recording can be
	// replayed the same way.
	Sim sim.Params
	// Ghost is the earlier ride raced against; Label is "" without one.
	// On a loop it races the current lap.
	Ghost RideGhost

	// Loop: the course goes round, and the ride with it until the rider
	// stops; DistanceM is then along the current lap.
	Loop bool
	// Lap is the lap under way, from 1; LapElapsed its time so far.
	Lap        int
	LapElapsed time.Duration
	// LastLap and BestLap are this ride's completed laps (N 0: none yet).
	LastLap, BestLap Lap
	// Yielded: a workout or manual control drives the trainer, and the
	// ride only moves the rider along the loop.
	Yielded bool
}

// OnLoop reports whether a loop ride is under way: it moves the rider
// round, and follows the grade whenever nothing else drives the trainer.
func (r Ride) OnLoop() bool { return r.Loop && r.Phase.Active() }

// Lap is a completed lap of a loop.
type Lap struct {
	N         int
	Elapsed   time.Duration
	AvgPowerW float64
	ClimbedM  float64
	// StartSpeedMPS is how fast the rider crossed the line into it (0 for
	// the first lap, from a standstill), so the lap can be replayed.
	StartSpeedMPS float64
	Finished      time.Time
}

// RideGhost is an earlier ride on the same stretch, raced alongside.
type RideGhost struct {
	Label     string        // e.g. "PB 7 Oct"
	DistanceM float64       // where the ghost is now on the course
	Gap       time.Duration // the rider behind the ghost (negative: ahead), at the rider's position
	Elapsed   time.Duration // the ghost's time to the line
}

// Hub holds the latest State. Writers apply changes with Update; readers
// take the latest value and a channel that closes on the next change, so a
// slow reader skips intermediate states instead of building a backlog.
type Hub struct {
	start time.Time

	mu      sync.Mutex
	st      State
	changed chan struct{}
}

func NewHub() *Hub {
	return &Hub{start: time.Now(), changed: make(chan struct{})}
}

// Update applies f to a copy of the state stamped with the current time.
// If f returns true the copy is published; otherwise it is discarded.
func (h *Hub) Update(f func(*State) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	next := h.st
	next.Time = time.Since(h.start)
	if !f(&next) {
		return
	}
	next.Seq++
	h.st = next
	close(h.changed)
	h.changed = make(chan struct{})
}

// Now is the hub's clock, on the scale of State.Time.
func (h *Hub) Now() time.Duration { return time.Since(h.start) }

// Latest returns the current state and a channel closed by the next Update.
func (h *Hub) Latest() (State, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st, h.changed
}
