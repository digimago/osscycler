package record

import (
	"path/filepath"
	"testing"

	"github.com/digimago/osscycler/internal/telemetry"
)

// The activity under way keeps a sample a second of riding time, for the
// live graph; once saved, the same samples come back from its file.
func TestHistory(t *testing.T) {
	r := newRecorder(t)
	if name, ss := r.History(0); name != "" || ss != nil {
		t.Fatalf("history before riding: %q %v", name, ss)
	}
	play(r, []step{{90, func(i int) telemetry.State {
		if i < 60 {
			return state(200, 90, 8)
		}
		return state(300, 95, 9)
	}}})
	name, ss := r.History(0)
	if name != t0.Local().Format("2006-01-02-150405")+".fit" || len(ss) != 90 {
		t.Fatalf("history %q with %d samples, want the open file and 90", name, len(ss))
	}
	if ss[0].PowerW != 200 || ss[89].PowerW != 300 || ss[89].HR != 140 || ss[89].T != 89 {
		t.Errorf("samples %+v ... %+v", ss[0], ss[89])
	}
	if _, tail := r.History(80); len(tail) != 10 || tail[0].T != 80 {
		t.Errorf("history since 80 s: %d samples from %v", len(tail), tail)
	}
	saved, err := r.endByRider(state(0, 0, 0), false)
	if err != nil {
		t.Fatal(err)
	}
	if name, ss := r.History(0); name != "" || ss != nil {
		t.Errorf("history after the end: %q, %d samples", name, len(ss))
	}
	cat := &Catalog{Dir: r.cfg.Dir}
	back, ftp, err := cat.Samples(saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 90 || back[89].PowerW != 300 || back[0].Cadence != 90 || ftp != 250 {
		t.Errorf("from the file: %d samples, last %+v, FTP %.0f; want 90, 300 W, 250", len(back), back[len(back)-1], ftp)
	}
	if _, _, err := cat.Samples(filepath.Join("..", saved)); err == nil {
		t.Error("a path was read")
	}
}
