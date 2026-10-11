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

// The catalog tells the highest heart rate held for 10 s: a strap's
// one-second spike doesn't count.
func TestHeldMaxHeartRate(t *testing.T) {
	r := newRecorder(t)
	play(r, []step{{60, func(i int) telemetry.State {
		st := state(200, 90, 8)
		hr := 150
		if i >= 20 && i < 32 {
			hr = 172 // held 12 s
		}
		if i == 45 {
			hr = 205 // a spike
		}
		st.HeartRate.BPM = telemetry.Some(uint8(hr))
		return st
	}}})
	if _, err := r.endByRider(state(0, 0, 0), false); err != nil {
		t.Fatal(err)
	}
	as, err := (&Catalog{Dir: r.cfg.Dir}).List()
	if err != nil || len(as) != 1 || as[0].HeldMaxHR != 172 {
		t.Errorf("held max HR %+v, %v; want 172 (the spike to 205 left out)", as, err)
	}
	if got := heldMax([]int{100, 0, 180, 180}, 2); got != 180 {
		t.Errorf("heldMax %d", got)
	}
}
