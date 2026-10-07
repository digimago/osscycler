package profile

import (
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
