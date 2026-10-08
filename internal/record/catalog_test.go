package record

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalog(t *testing.T) {
	r := newRecorder(t)
	play(r, aRide) // one activity: free riding, a course ride, more riding
	dir := r.cfg.Dir
	os.WriteFile(filepath.Join(dir, "2026-10-07-200000.fit.part"), []byte("being written"), 0o600)
	os.WriteFile(filepath.Join(dir, "2026-10-08-090000.fit"), []byte("not a fit file"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.fit"), []byte("x"), 0o600)

	c := &Catalog{Dir: dir}
	as, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 2 || as[0].Name != "2026-10-08-090000.fit" || as[0].Err == nil {
		t.Fatalf("activities %+v: want the broken one first (newest), then the ride", as)
	}
	a := as[1]
	if a.Err != nil || !a.Start.Equal(t0.Add(10*time.Second)) || a.Laps != 3 || !a.Virtual {
		t.Errorf("ride %+v", a)
	}
	if a.Timer != 234*time.Second || a.Elapsed <= a.Timer || a.DistanceM < 1000 || a.AvgPowerW < 150 {
		t.Errorf("summary %+v", a)
	}

	// The cached summary is reused, and dropped with the file.
	again, _ := c.List()
	if again[1] != a {
		t.Error("second listing differs")
	}
	os.Remove(filepath.Join(dir, "2026-10-08-090000.fit"))
	if as, _ := c.List(); len(as) != 1 || len(c.cache) != 1 {
		t.Errorf("after removing a file: %d listed, %d cached", len(as), len(c.cache))
	}

	// Open serves exactly the listed files.
	f, size, err := c.Open(a.Name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if int64(len(b)) != size || size != a.Size {
		t.Errorf("read %d bytes of %d (listed %d)", len(b), size, a.Size)
	}
	for _, bad := range []string{"../profile.json", "/etc/passwd", "notes.fit", "2026-10-07-200000.fit.part", "2026-10-07-200000.fit", ""} {
		if _, _, err := c.Open(bad); !errors.Is(err, ErrNoActivity) {
			t.Errorf("Open(%q): %v", bad, err)
		}
	}
}

func TestCatalogNoDir(t *testing.T) {
	as, err := (&Catalog{Dir: filepath.Join(t.TempDir(), "nothing")}).List()
	if err != nil || len(as) != 0 {
		t.Errorf("%v, %v", as, err)
	}
}
