// Command osscycler-replay re-rides recorded course rides through the
// simulation: with the recorded parameters to check that a replay gives
// the recorded time, or with -mass-kg, -cda or -crr to see what those
// would have done to it.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/home"
	"github.com/digimago/osscycler/internal/record"
	"github.com/digimago/osscycler/internal/replay"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "osscycler-replay:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		rides   = flag.String("rides", home.Rides(), "recording directory (with "+record.ResultsFile+")")
		courses = flag.String("courses", home.Courses(), "directory of .gpx courses")
		mass    = flag.Float64("mass-kg", 0, "rider + bike mass to replay with (0 = as recorded)")
		cda     = flag.Float64("cda", 0, "drag area, m² (0 = as recorded)")
		crr     = flag.Float64("crr", 0, "rolling resistance (0 = as recorded)")
	)
	flag.Parse()

	rs, err := replay.Load(*rides)
	if err != nil {
		return err
	}
	if len(rs) == 0 {
		fmt.Println("no finished course rides in", filepath.Join(*rides, record.ResultsFile))
		return nil
	}
	cs, err := course.LoadDir(*courses)
	if err != nil {
		fmt.Fprintln(os.Stderr, "some courses didn't load:", err)
	}
	byID := map[string]*course.Course{}
	for _, c := range cs {
		byID[c.ID] = c
	}
	whatIf := *mass > 0 || *cda > 0 || *crr > 0

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "finished\tcourse\tfrom\trecorded\treplayed\tdifference\tavg W\t")
	for _, r := range rs {
		head := fmt.Sprintf("%s\t%s\t%.2f km\t%s\t", r.Finished.Local().Format("2006-01-02 15:04"), r.CourseName,
			r.StartM/1000, clock(time.Duration(r.ElapsedS*float64(time.Second))))
		c := byID[r.CourseID]
		switch {
		case r.Err != nil:
			fmt.Fprintf(w, "%s%s\t\t\t\n", head, r.Err)
			continue
		case c == nil:
			fmt.Fprintf(w, "%scourse %q not in %s\t\t\t\n", head, r.CourseID, *courses)
			continue
		}
		p := r.Params()
		if *mass > 0 {
			p.MassKg = *mass
		}
		if *cda > 0 {
			p.CdA = *cda
		}
		if *crr > 0 {
			p.Crr = *crr
		}
		got := r.Replay(c, p)
		if !got.Finished {
			fmt.Fprintf(w, "%sdidn't finish (%.2f km)\t\t%.0f\t\n", head, got.DistanceM/1000, got.AvgPowerW)
			continue
		}
		diff := got.Elapsed - time.Duration(r.ElapsedS*float64(time.Second))
		fmt.Fprintf(w, "%s%s\t%+.1f s\t%.0f\t\n", head, clock(got.Elapsed), diff.Seconds(), got.AvgPowerW)
	}
	w.Flush()
	if whatIf {
		fmt.Println("\nreplayed with changed parameters; without them a replay should be within a second or two")
	}
	return nil
}

func clock(d time.Duration) string {
	d = d.Round(100 * time.Millisecond)
	if d >= time.Hour {
		return fmt.Sprintf("%d:%02d:%04.1f", int(d.Hours()), int(d.Minutes())%60, d.Seconds()-float64(int(d.Minutes())*60))
	}
	return fmt.Sprintf("%d:%04.1f", int(d.Minutes()), d.Seconds()-float64(int(d.Minutes())*60))
}
