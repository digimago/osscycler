package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDir(t *testing.T) {
	t.Setenv(Env, "/srv/osscycler")
	if Dir() != "/srv/osscycler" || Courses() != "/srv/osscycler/courses" || Token() != "/srv/osscycler/api-token" {
		t.Errorf("with %s: %s %s %s", Env, Dir(), Courses(), Token())
	}
	t.Setenv(Env, "")
	t.Setenv("HOME", "/home/rider")
	if Dir() != "/home/rider/osscycler" || Profile() != "/home/rider/osscycler/profile.json" {
		t.Errorf("default: %s %s", Dir(), Profile())
	}
}

func TestEnsureMovesOldData(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv(Env, "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, ".local", "share"))
	// Where 0.1.0 kept things (on Linux; macOS uses its own folders, which
	// the same code finds through os.UserConfigDir).
	oldP, oldR := oldProfile(), oldRides()
	os.MkdirAll(filepath.Dir(oldP), 0o700)
	os.WriteFile(oldP, []byte(`{"weight_kg": 87}`), 0o600)
	os.MkdirAll(oldR, 0o700)
	os.WriteFile(filepath.Join(oldR, "2026-10-07-182817.fit"), []byte("ride"), 0o600)

	moved, err := Ensure()
	if err != nil || len(moved) != 2 {
		t.Fatalf("moved %v, %v", moved, err)
	}
	for _, sub := range []string{"courses", "workouts", "rides"} {
		if fi, err := os.Stat(filepath.Join(h, "osscycler", sub)); err != nil || !fi.IsDir() {
			t.Errorf("%s: %v", sub, err)
		}
	}
	if b, _ := os.ReadFile(Profile()); string(b) != `{"weight_kg": 87}` {
		t.Errorf("profile %q", b)
	}
	if _, err := os.Stat(filepath.Join(Rides(), "2026-10-07-182817.fit")); err != nil {
		t.Errorf("ride not moved: %v", err)
	}
	// Once there, nothing moves again, even if old files reappear.
	os.MkdirAll(filepath.Dir(oldP), 0o700)
	os.WriteFile(oldP, []byte(`{"weight_kg": 60}`), 0o600)
	if moved, err := Ensure(); err != nil || len(moved) != 0 {
		t.Errorf("second Ensure moved %v, %v", moved, err)
	}
}

func TestEnsureToken(t *testing.T) {
	t.Setenv(Env, t.TempDir())
	if _, err := ReadToken(); err == nil {
		t.Error("ReadToken invented a token")
	}
	tok, created, err := EnsureToken()
	if err != nil || !created || len(tok) != 64 || strings.TrimSpace(tok) != tok {
		t.Fatalf("%q, %v, %v", tok, created, err)
	}
	if fi, _ := os.Stat(Token()); fi.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v", fi.Mode().Perm())
	}
	again, created, _ := EnsureToken()
	read, err := ReadToken()
	if again != tok || created || read != tok || err != nil {
		t.Errorf("second call: %q %v; read %q %v", again, created, read, err)
	}
}

func TestChosenFolderNeverTakesOldData(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, ".local", "share"))
	oldP := oldProfile()
	os.MkdirAll(filepath.Dir(oldP), 0o700)
	os.WriteFile(oldP, []byte(`{"weight_kg": 87}`), 0o600)

	t.Setenv(Env, filepath.Join(h, "elsewhere"))
	if moved, err := Ensure(); err != nil || len(moved) != 0 {
		t.Fatalf("moved %v, %v", moved, err)
	}
	if _, err := os.Stat(oldP); err != nil {
		t.Errorf("the rider's profile left its place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h, "elsewhere", "courses")); err != nil {
		t.Errorf("folders not made: %v", err)
	}
}
