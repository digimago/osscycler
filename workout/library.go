package workout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	ErrNotFound = errors.New("workout: not found")
	ErrBadID    = errors.New("workout: invalid id")
)

// Library is a directory of .zwo files; the file name without extension is
// the workout ID. It rescans on every call, so files dropped in by hand
// (exports from Zwift, downloads) show up without a restart.
type Library struct {
	Dir string
}

// Entry is a workout in the library, or a file that failed to load.
type Entry struct {
	ID      string
	Workout *Workout // nil if Err is set
	Err     error
}

// List loads every .zwo file, sorted by ID.
func (l *Library) List() ([]Entry, error) {
	paths, err := filepath.Glob(filepath.Join(l.Dir, "*.zwo"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []Entry
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		w, err := load(p)
		out = append(out, Entry{ID: id, Workout: w, Err: err})
	}
	return out, nil
}

// Get loads one workout. The ID must name an existing file; it is never
// used to build a path directly.
func (l *Library) Get(id string) (*Workout, error) {
	p, err := l.existing(id)
	if err != nil {
		return nil, err
	}
	return load(p)
}

// existing maps an ID to the path of a file that is in the directory.
func (l *Library) existing(id string) (string, error) {
	entries, err := l.List()
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.ID == id {
			return filepath.Join(l.Dir, id+".zwo"), nil
		}
	}
	return "", ErrNotFound
}

// safeID is what a new workout's ID may look like: no separators, no
// leading dot, so it can't escape the directory.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// Save writes the workout as .zwo. With an existing ID it overwrites that
// file; with an empty ID it derives a new, unused one from the name.
// It returns the ID written.
func (l *Library) Save(id string, w *Workout) (string, error) {
	if err := w.Validate(); err != nil {
		return "", err
	}
	var path string
	if id != "" {
		p, err := l.existing(id)
		switch {
		case err == nil:
			path = p
		case errors.Is(err, ErrNotFound) && safeID.MatchString(id):
			path = filepath.Join(l.Dir, id+".zwo")
		case errors.Is(err, ErrNotFound):
			return "", ErrBadID
		default:
			return "", err
		}
	} else {
		var err error
		if id, err = l.newID(w.Name); err != nil {
			return "", err
		}
		path = filepath.Join(l.Dir, id+".zwo")
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return "", err
	}
	// Write to a temporary file and rename, so a crash never leaves half a
	// workout behind.
	tmp, err := os.CreateTemp(l.Dir, ".save-*.zwo")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	// CreateTemp makes the file private; a workout is an ordinary document.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", err
	}
	if err := w.WriteZWO(tmp); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return id, nil
}

func (l *Library) newID(name string) (string, error) {
	base := slug(name)
	if base == "" {
		base = "workout"
	}
	entries, err := l.List()
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, e := range entries {
		taken[e.ID] = true
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id, nil
}

// slug makes a file-name-safe ID from a workout name.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimSuffix(b.String(), "-")
	if len(s) > 60 {
		s = strings.TrimSuffix(s[:60], "-")
	}
	return s
}

func load(path string) (*Workout, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseZWO(f)
}
