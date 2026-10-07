package workout

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLibrary(t *testing.T) {
	dir := t.TempDir()
	lib := &Library{Dir: filepath.Join(dir, "workouts")} // created on first save

	if entries, err := lib.List(); err != nil || len(entries) != 0 {
		t.Fatalf("empty library: %v, %v", entries, err)
	}
	w, _ := ParseText(strings.NewReader(textSample))
	id, err := lib.Save("", w)
	if err != nil || id != "sweet-spot-3x10" {
		t.Fatalf("Save new: %q, %v", id, err)
	}
	// Same name again gets a fresh ID instead of overwriting.
	if id2, _ := lib.Save("", w); id2 != "sweet-spot-3x10-2" {
		t.Errorf("second save: %q", id2)
	}
	if fi, err := os.Stat(filepath.Join(lib.Dir, id+".zwo")); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("saved file mode %v, %v; want 0644", fi.Mode().Perm(), err)
	}
	got, err := lib.Get(id)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatalf("Get after Save: %v\n%+v", err, got)
	}

	// Overwrite by ID.
	w.Name = "Renamed"
	if id3, err := lib.Save(id, w); err != nil || id3 != id {
		t.Fatalf("overwrite: %q, %v", id3, err)
	}
	if got, _ := lib.Get(id); got.Name != "Renamed" {
		t.Error("overwrite didn't stick")
	}

	// A hand-copied Zwift file with spaces in its name is listed and usable.
	os.WriteFile(filepath.Join(lib.Dir, "Zwift Export.zwo"), []byte(sample), 0o644)
	os.WriteFile(filepath.Join(lib.Dir, "broken.zwo"), []byte("<workout_file>"), 0o644)
	entries, _ := lib.List()
	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.ID] = e
	}
	if e := byID["Zwift Export"]; e.Err != nil || e.Workout == nil {
		t.Errorf("hand-copied file: %+v", e)
	}
	if e := byID["broken"]; e.Err == nil {
		t.Error("broken file loaded without error")
	}
	if _, err := lib.Save("Zwift Export", w); err != nil {
		t.Errorf("overwriting an existing odd ID: %v", err)
	}
}

func TestLibraryRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	lib := &Library{Dir: filepath.Join(dir, "lib")}
	w, _ := ParseText(strings.NewReader(textSample))
	for _, id := range []string{"../evil", "a/b", ".hidden", "/etc/passwd", "..", "a\\b"} {
		if _, err := lib.Save(id, w); !errors.Is(err, ErrBadID) {
			t.Errorf("Save(%q): err %v, want ErrBadID", id, err)
		}
		if _, err := lib.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q): err %v, want ErrNotFound", id, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.zwo")); err == nil {
		t.Fatal("a file escaped the library directory")
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Sweet spot 3x10": "sweet-spot-3x10", "  VO2 max!! ": "vo2-max", "Überkurz": "berkurz", "": "",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
