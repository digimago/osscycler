// Package profile keeps the rider's settings (weight, FTP, difficulty)
// with the installed program, in osscycler's folder (see home), so a
// release binary needs no flags. The core owns the file: onboarding fills
// it, and changes made while riding are saved to it.
//
// Settings forced with command-line flags (for testing and validation)
// win for that run and are never written back.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Profile is the rider's settings. Zero weight or FTP means unknown.
type Profile struct {
	WeightKg float64 `json:"weight_kg,omitempty"`
	FTPW     float64 `json:"ftp_w,omitempty"`
	// HeightCm sizes the rider's drag; optional (zero: unknown).
	HeightCm      float64 `json:"height_cm,omitempty"`
	DifficultyPct float64 `json:"difficulty_pct"`
	BikeKg        float64 `json:"bike_kg"`
}

// Defaults are a new rider's settings before onboarding: Zwift's trainer
// difficulty and a road bike.
var Defaults = Profile{DifficultyPct: 50, BikeKg: 9}

// Valid ranges, as onboarding and the API check them.
const (
	MinWeightKg = 30
	MaxWeightKg = 200
	MinFTPW     = 50
	MaxFTPW     = 600
	MinHeightCm = 120
	MaxHeightCm = 220
)

// Complete reports whether onboarding has what it needs.
func (p Profile) Complete() bool { return p.WeightKg > 0 && p.FTPW > 0 }

// Field names a setting that can be forced.
type Field int

const (
	Weight Field = iota
	FTP
	Difficulty
	Height
	numFields
)

// Manager holds the profile in effect: the saved one, with any forced
// settings on top.
type Manager struct {
	path string

	mu     sync.Mutex
	saved  Profile
	forced [numFields]*float64 // non-nil: this run's value, from a flag
}

// Open loads the profile at path; a missing file is a new rider. path ""
// keeps the profile in memory only.
func Open(path string) (*Manager, error) {
	m := &Manager{path: path, saved: Defaults}
	if path == "" {
		return m, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m.saved); err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	return m, nil
}

// Path is where the profile is saved ("" for none).
func (m *Manager) Path() string { return m.path }

// Force sets a field for this run only, as a command-line flag does.
func (m *Manager) Force(f Field, v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forced[f] = &v
}

// Forced reports whether a field comes from a flag this run.
func (m *Manager) Forced(f Field) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.forced[f] != nil
}

// Get returns the profile in effect.
func (m *Manager) Get() Profile {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.effective()
}

func (m *Manager) effective() Profile {
	p := m.saved
	for f, v := range m.forced {
		if v != nil {
			*field(&p, Field(f)) = *v
		}
	}
	return p
}

func field(p *Profile, f Field) *float64 {
	switch f {
	case Weight:
		return &p.WeightKg
	case FTP:
		return &p.FTPW
	case Height:
		return &p.HeightCm
	}
	return &p.DifficultyPct
}

// ErrIncomplete matches (errors.Is) refusals because the profile lacks
// what something needs: a course ride needs the weight, a workout the FTP.
var ErrIncomplete = errors.New("rider profile incomplete")

// ErrOutOfRange matches (errors.Is) a value outside its field's range.
var ErrOutOfRange = errors.New("out of range")

type rangeError string

func (e rangeError) Error() string      { return string(e) }
func (rangeError) Is(target error) bool { return target == ErrOutOfRange }

// Check validates a value for a field.
func Check(f Field, v float64) error {
	switch f {
	case Weight:
		if v < MinWeightKg || v > MaxWeightKg {
			return rangeError(fmt.Sprintf("weight %.0f kg: want %d to %d kg", v, MinWeightKg, MaxWeightKg))
		}
	case FTP:
		if v < MinFTPW || v > MaxFTPW {
			return rangeError(fmt.Sprintf("FTP %.0f W: want %d to %d W", v, MinFTPW, MaxFTPW))
		}
	case Height:
		if v < MinHeightCm || v > MaxHeightCm {
			return rangeError(fmt.Sprintf("height %.0f cm: want %d to %d cm", v, MinHeightCm, MaxHeightCm))
		}
	case Difficulty:
		if v < 0 || v > 100 {
			return rangeError(fmt.Sprintf("difficulty %.0f %%: want 0 to 100 %%", v))
		}
	}
	return nil
}

// Set changes a field and saves the profile, unless the field is forced
// this run: then only this run's value changes. It returns the profile in
// effect.
func (m *Manager) Set(f Field, v float64) (Profile, error) {
	if err := Check(f, v); err != nil {
		return Profile{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.forced[f] != nil {
		m.forced[f] = &v
		return m.effective(), nil
	}
	next := m.saved
	*field(&next, f) = v
	if err := m.save(next); err != nil {
		return m.effective(), err
	}
	m.saved = next
	return m.effective(), nil
}

// save writes atomically: a crash leaves the old profile or the new one.
func (m *Manager) save(p Profile) error {
	if m.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".profile-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after a successful rename, there is nothing left to remove
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.path)
}
