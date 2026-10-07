// Package home is osscycler's folder in the user's home directory,
// visible on purpose: courses and workouts are files the rider drops in,
// and the recorded rides and settings are theirs to back up in one go.
//
//	~/osscycler/
//	  profile.json   weight, FTP, difficulty
//	  api-token      the API token (mode 600), created on the core's first start
//	  courses/       .gpx routes
//	  workouts/      workouts (.zwo)
//	  rides/         recorded FIT files and results.jsonl
//
// OSSCYCLER_HOME moves it, e.g. to /var/lib/osscycler for a service.
package home

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Env overrides the folder.
const Env = "OSSCYCLER_HOME"

// Dir is the folder: $OSSCYCLER_HOME, else ~/osscycler; "" if there is no
// home directory.
func Dir() string {
	if d := os.Getenv(Env); d != "" {
		return d
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, "osscycler")
}

func join(name string) string {
	d := Dir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, name)
}

func Courses() string  { return join("courses") }
func Workouts() string { return join("workouts") }
func Rides() string    { return join("rides") }
func Profile() string  { return join("profile.json") }
func Token() string    { return join("api-token") }

// Ensure creates the folder and its subfolders. For the default folder
// only, it also moves a profile and rides from where osscycler 0.1.0 kept
// them (the XDG config and data directories) if there are none here yet:
// a folder chosen with OSSCYCLER_HOME (a test, a service) never takes the
// rider's own data. It returns what it moved, to log.
func Ensure() (moved []string, err error) {
	d := Dir()
	if d == "" {
		return nil, errors.New("home: no home directory; set " + Env)
	}
	for _, sub := range []string{"", "courses", "workouts", "rides"} {
		if err := os.MkdirAll(filepath.Join(d, sub), 0o755); err != nil {
			return moved, err
		}
	}
	if os.Getenv(Env) != "" {
		return nil, nil
	}
	for _, m := range [][2]string{{oldProfile(), Profile()}, {oldRides(), Rides()}} {
		from, to := m[0], m[1]
		if from == "" || !exists(from) || !emptyOrMissing(to) {
			continue
		}
		if fi, err := os.Stat(to); err == nil && fi.IsDir() {
			os.Remove(to) // the empty folder just made, so the old one can take its place
		}
		if err := os.Rename(from, to); err != nil {
			return moved, fmt.Errorf("home: moving %s to %s: %w", from, to, err)
		}
		moved = append(moved, from+" → "+to)
	}
	return moved, nil
}

// EnsureToken reads the API token file, creating it with a fresh token
// (mode 600) if it doesn't exist. created says whether it did.
func EnsureToken() (token string, created bool, err error) {
	path := Token()
	if path == "" {
		return "", false, errors.New("home: no home directory; set " + Env)
	}
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token = hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return token, true, nil
}

// ReadToken reads the API token file without creating it.
func ReadToken() (string, error) {
	path := Token()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if t := strings.TrimSpace(string(b)); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("%s is empty", path)
}

// Where osscycler 0.1.0 kept the profile and the rides.
func oldProfile() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "osscycler", "profile.json")
}

func oldRides() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(h, ".local", "share")
		if runtime.GOOS == "darwin" {
			base = filepath.Join(h, "Library", "Application Support")
		}
	}
	return filepath.Join(base, "osscycler", "rides")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func emptyOrMissing(p string) bool {
	fi, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil || !fi.IsDir() {
		return false
	}
	entries, err := os.ReadDir(p)
	return err == nil && len(entries) == 0
}
