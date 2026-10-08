// Package workout models structured workouts: blocks of target power as a
// fraction of FTP, read from and written to Zwift's .zwo format.
//
// The .zwo layout is written from memory and marked VERIFY until checked
// against real files exported by Zwift.
package workout

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// BlockType names follow the .zwo element names.
type BlockType string

const (
	Warmup      BlockType = "Warmup"      // ramp from Low to High
	SteadyState BlockType = "SteadyState" // constant Power
	Ramp        BlockType = "Ramp"        // ramp from Low to High
	Cooldown    BlockType = "Cooldown"    // ramp down; see Block.rampEnds
	IntervalsT  BlockType = "IntervalsT"  // Repeat x (On at OnPower, Off at OffPower)
	FreeRide    BlockType = "FreeRide"    // no ERG target
	MaxEffort   BlockType = "MaxEffort"   // no ERG target, all out
)

// Block is one element of a workout as authored. Powers are fractions of
// FTP; which fields matter depends on Type.
type Block struct {
	Type     BlockType
	Duration time.Duration // all but IntervalsT

	Power     float64 // SteadyState
	PowerLow  float64 // Warmup, Ramp, Cooldown
	PowerHigh float64

	Repeat      int // IntervalsT
	OnDuration  time.Duration
	OffDuration time.Duration
	OnPower     float64
	OffPower    float64

	Cadence int // suggested rpm, 0 = none

	Texts []Text // messages shown during the block
}

// Text is a message shown Offset into its block.
type Text struct {
	Offset  time.Duration
	Message string
}

// Workout is a named list of blocks.
type Workout struct {
	Name        string
	Author      string
	Description string
	Blocks      []Block
}

// Kind of a timeline segment.
type Kind int

const (
	KindERG  Kind = iota // trainer holds a target power
	KindFree             // no target: the rider controls the effort
)

// Segment is a stretch of the timeline with one linear target.
type Segment struct {
	Kind     Kind
	Start    time.Duration // from the start of the workout
	Duration time.Duration
	From, To float64 // fraction of FTP at the start and end (equal when steady)
	Cadence  int
	Label    string // e.g. "Warmup", "Interval 3/5 on"
	Block    int    // index into Workout.Blocks
}

// Validate checks that every block can be ridden.
func (w *Workout) Validate() error {
	if len(w.Blocks) == 0 {
		return errors.New("workout: no blocks")
	}
	for i, b := range w.Blocks {
		if err := b.validate(); err != nil {
			return fmt.Errorf("workout: block %d (%s): %w", i+1, b.Type, err)
		}
	}
	return nil
}

func (b Block) validate() error {
	power := func(ps ...float64) error {
		for _, p := range ps {
			if p < 0 || p > 5 || math.IsNaN(p) {
				return fmt.Errorf("power %.2f FTP out of range", p)
			}
		}
		return nil
	}
	switch b.Type {
	case IntervalsT:
		if b.Repeat < 1 || b.OnDuration <= 0 || b.OffDuration < 0 {
			return errors.New("needs Repeat ≥ 1 and a positive OnDuration")
		}
		return power(b.OnPower, b.OffPower)
	case SteadyState:
		if b.Duration <= 0 {
			return errors.New("needs a positive Duration")
		}
		return power(b.Power)
	case Warmup, Ramp, Cooldown:
		if b.Duration <= 0 {
			return errors.New("needs a positive Duration")
		}
		return power(b.PowerLow, b.PowerHigh)
	case FreeRide, MaxEffort:
		if b.Duration <= 0 {
			return errors.New("needs a positive Duration")
		}
		return nil
	}
	return fmt.Errorf("unknown block type %q", b.Type)
}

// rampEnds returns the start and end power of a ramp block. Zwift files
// write PowerLow as the start and PowerHigh as the end, except that
// cooldowns are often written low-to-high yet always ridden downwards.
// VERIFY against Zwift exports.
func (b Block) rampEnds() (from, to float64) {
	if b.Type == Cooldown {
		return math.Max(b.PowerLow, b.PowerHigh), math.Min(b.PowerLow, b.PowerHigh)
	}
	return b.PowerLow, b.PowerHigh
}

// Timeline flattens the blocks into segments; intervals become one segment
// per on and off part.
func (w *Workout) Timeline() []Segment {
	var segs []Segment
	at := time.Duration(0)
	add := func(s Segment) {
		s.Start = at
		segs = append(segs, s)
		at += s.Duration
	}
	for i, b := range w.Blocks {
		switch b.Type {
		case SteadyState:
			add(Segment{Duration: b.Duration, From: b.Power, To: b.Power, Cadence: b.Cadence, Label: "Steady", Block: i})
		case Warmup, Ramp, Cooldown:
			from, to := b.rampEnds()
			add(Segment{Duration: b.Duration, From: from, To: to, Cadence: b.Cadence, Label: string(b.Type), Block: i})
		case IntervalsT:
			for r := 1; r <= b.Repeat; r++ {
				add(Segment{Duration: b.OnDuration, From: b.OnPower, To: b.OnPower, Cadence: b.Cadence,
					Label: fmt.Sprintf("Interval %d/%d on", r, b.Repeat), Block: i})
				if b.OffDuration > 0 {
					add(Segment{Duration: b.OffDuration, From: b.OffPower, To: b.OffPower,
						Label: fmt.Sprintf("Interval %d/%d off", r, b.Repeat), Block: i})
				}
			}
		case FreeRide:
			add(Segment{Kind: KindFree, Duration: b.Duration, Label: "Free ride", Block: i})
		case MaxEffort:
			add(Segment{Kind: KindFree, Duration: b.Duration, Label: "Max effort", Block: i})
		}
	}
	return segs
}

// Duration is the total length of the workout.
func (w *Workout) Duration() time.Duration {
	segs := w.Timeline()
	if len(segs) == 0 {
		return 0
	}
	last := segs[len(segs)-1]
	return last.Start + last.Duration
}

// At returns the segment index and the target (fraction of FTP) t into the
// workout. ok is false past the end. Free segments return target 0.
func At(segs []Segment, t time.Duration) (index int, target float64, ok bool) {
	for i, s := range segs {
		if t < s.Start+s.Duration {
			if s.Kind == KindFree {
				return i, 0, true
			}
			f := float64(t-s.Start) / float64(s.Duration)
			return i, s.From + (s.To-s.From)*math.Max(0, f), true
		}
	}
	return len(segs), 0, false
}

// Watts turns a fraction of FTP into a trainer target: FTP times the
// fraction times the rider's intensity adjustment (1 = as written).
func Watts(fraction, ftp, intensity float64) float64 {
	return math.Round(fraction * ftp * intensity)
}
