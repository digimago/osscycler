package profile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRiderAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osscycler", "profile.json")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := m.Get(); p != Defaults || p.Complete() {
		t.Fatalf("new rider: %+v", p)
	}
	if _, err := m.Set(Weight, 87); err != nil {
		t.Fatal(err)
	}
	p, err := m.Set(FTP, 265)
	if err != nil || !p.Complete() || p.WeightKg != 87 || p.FTPW != 265 || p.DifficultyPct != 50 {
		t.Fatalf("after onboarding: %+v, %v", p, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("profile file: %v, %v", info, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".profile-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}

	// A fresh start reads it back.
	m2, err := Open(path)
	if err != nil || m2.Get() != p {
		t.Errorf("reloaded %+v, %v; want %+v", m2.Get(), err, p)
	}
}

func TestForcedIsNeverSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	m, _ := Open(path)
	m.Set(Weight, 80)
	m.Set(FTP, 250)

	m.Force(Weight, 87) // as -rider-kg does for a run
	if p := m.Get(); p.WeightKg != 87 || !m.Forced(Weight) || m.Forced(FTP) {
		t.Fatalf("forced: %+v", p)
	}
	// Changing a forced field changes this run only.
	if p, _ := m.Set(Weight, 90); p.WeightKg != 90 {
		t.Errorf("forced weight changed live: %+v", p)
	}
	m.Set(FTP, 260) // not forced: saved
	saved, _ := Open(path)
	if p := saved.Get(); p.WeightKg != 80 || p.FTPW != 260 {
		t.Errorf("on disk: %+v (forced weight must not be saved; FTP must be)", p)
	}
}

func TestValidation(t *testing.T) {
	m, _ := Open("")
	for _, c := range []struct {
		f Field
		v float64
	}{{Weight, 20}, {Weight, 250}, {FTP, 10}, {FTP, 900}, {Difficulty, -1}, {Difficulty, 101}} {
		if _, err := m.Set(c.f, c.v); err == nil {
			t.Errorf("field %d = %v accepted", c.f, c.v)
		}
	}
	if p, err := m.Set(Difficulty, 0); err != nil || p.DifficultyPct != 0 {
		t.Errorf("difficulty 0 %%: %+v, %v", p, err)
	}
}

func TestBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("broken profile: %v", err)
	}
}

// Building worlds ahead is set in the profile (owner, 2026-10-13): only
// while nothing is ridden and from the caches only unless the rider says
// otherwise; saved, and nothing else accepted.
func TestWorldSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := m.Get(); p.Choice("world_build") != WorldBuildIdle || p.Choice("world_fetch") != WorldFetchCached {
		t.Errorf("defaults: %q, %q", p.Choice("world_build"), p.Choice("world_fetch"))
	}
	if _, err := m.SetChoice("world_build", WorldBuildAlways); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetChoice("world_fetch", "sometimes"); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("world_fetch sometimes: %v", err)
	}
	if _, err := m.SetChoice("colour", "red"); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("an unknown setting: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := again.Get(); p.Choice("world_build") != WorldBuildAlways || p.Choice("world_fetch") != WorldFetchCached || p.ViewOrDefault() != ViewChase {
		t.Errorf("read back: %+v", p)
	}
}

// Heart rate zones come from the one the rider chose, else whichever is
// set, the maximum when both are (owner, 2026-10-21).
func TestHRZoneBasis(t *testing.T) {
	for _, c := range []struct {
		p    Profile
		th   bool
		bpm  float64
		name string
	}{
		{Profile{}, false, 0, "neither"},
		{Profile{MaxHRBpm: 190}, false, 190, "only the maximum"},
		{Profile{ThresholdHRBpm: 165}, true, 165, "only the threshold"},
		{Profile{MaxHRBpm: 190, ThresholdHRBpm: 165}, false, 190, "both, no choice"},
		{Profile{MaxHRBpm: 190, ThresholdHRBpm: 165, HRZones: HRZonesThreshold}, true, 165, "both, threshold chosen"},
		{Profile{MaxHRBpm: 190, HRZones: HRZonesThreshold}, false, 190, "threshold chosen, none set"},
		{Profile{ThresholdHRBpm: 165, HRZones: HRZonesMax}, true, 165, "maximum chosen, none set"},
	} {
		if th, bpm := c.p.HRZoneBasis(); th != c.th || bpm != c.bpm {
			t.Errorf("%s: threshold %v %v bpm, want %v %v", c.name, th, bpm, c.th, c.bpm)
		}
	}
	if Check(ThresholdHR, 79) == nil || Check(ThresholdHR, 221) == nil || Check(ThresholdHR, 165) != nil {
		t.Error("threshold heart rate range")
	}
}
