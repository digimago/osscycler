// Command clip builds the world of a stretch of a course from the cached
// map data (and ground model), for reproducing a world fault in seconds
// instead of minutes:
//
//	go run ./.claude/skills/world-faults/scripts/clip -courses _gpx -course ID -to 2200 -out _scratch/clip
//
// The stretch runs from -from to -to metres along the course, resampled
// every 5 m. With -from 0 the clip starts where the course does, so its
// frame (x east, z south of the start) and its 250 m chunk grid are the
// course's own: faults that depend on where chunk edges fall reproduce,
// and positions compare one to one with the full world. Any other -from
// moves the frame to that point.
//
// Map data comes from <courses>/.osm/<ID>-*.json, every cached stretch of
// the course (no fetching); -dem DIR adds the AHN ground model from its
// cache (no fetching either), as a real build with -dem auto has it.
// Without -dem the terrain is shaped from the profile and roads are not
// cut out of it: a fault that shows only with -dem lies in the cut
// (cutRoads, rasterize, the cutter, verges).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/digimago/osscycler/internal/course"
	"github.com/digimago/osscycler/internal/dem"
	"github.com/digimago/osscycler/internal/scenery"
	"github.com/digimago/osscycler/internal/world"
	"github.com/digimago/osscycler/internal/worlds"
)

func main() {
	courses := flag.String("courses", "_gpx", "the courses directory: its .gpx files and its .osm cache")
	id := flag.String("course", "", "the course ID (an included one, or a .gpx file's name without .gpx)")
	from := flag.Float64("from", 0, "start of the stretch, m along the course")
	to := flag.Float64("to", 0, "end of the stretch, m along the course (0: the end)")
	out := flag.String("out", "", "directory to write the world to")
	demDir := flag.String("dem", "", "AHN tile cache (e.g. _gpx/.dem); empty: no ground model")
	flag.Parse()
	if *id == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	c, err := find(*courses, *id)
	if err != nil {
		log.Fatal(err)
	}
	end := *to
	if end <= 0 || end > c.Distance {
		end = c.Distance
	}
	var pts []course.Point
	for d := *from; d <= end; d += 5 {
		lat, lon := c.Position(d)
		ele, _ := c.At(d)
		pts = append(pts, course.Point{Lat: lat, Lon: lon, Ele: ele})
	}
	cc, err := course.New("clip", "Clip of "+c.Name, pts)
	if err != nil {
		log.Fatal(err)
	}
	// The course's regional frame, as osscycler-world builds it: a clip
	// from anywhere lays the same grids.
	if !c.Builtin {
		cc = cc.InFrame(worlds.Anchor(c.Position(0)))
	}

	files, _ := filepath.Glob(filepath.Join(*courses, ".osm", *id+"-*.json"))
	if len(files) == 0 {
		log.Printf("no cached map data for %s in %s/.osm: building without", *id, *courses)
	}
	d := &scenery.Data{}
	seen := map[string]bool{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		var x scenery.Data
		if err := json.Unmarshal(b, &x); err != nil {
			log.Fatalf("%s: %v", f, err)
		}
		for _, e := range x.Elements {
			if k := e.Type + strconv.FormatInt(e.ID, 10); !seen[k] {
				seen[k] = true
				d.Elements = append(d.Elements, e)
			}
		}
	}

	o := world.Options{Generator: "clip"}
	if len(d.Elements) > 0 {
		o.Land, o.Scenery, o.Buildings = scenery.NewLandMap(cc, d), scenery.Build(cc, d), scenery.Footprints(cc, d)
	}
	if *demDir != "" {
		a := &dem.AHN{CacheDir: *demDir, CellM: 5}
		var ll []dem.LatLon
		for at := 0.0; at <= cc.Distance; at += 20 {
			lat, lon := cc.Position(at)
			ll = append(ll, dem.LatLon{Lat: lat, Lon: lon})
		}
		m, err := a.Load(context.Background(), ll, 300+1.5*250) // the corridor and a chunk, as worlds.Builder
		if err != nil {
			log.Fatal(err)
		}
		o.Elevation, o.TerrainSource = m.Elevation, "AHN DTM, 5 m"
	}
	w, err := world.Build(cc, o)
	if err != nil {
		log.Fatal(err)
	}
	if err := w.Write(*out); err != nil {
		log.Fatal(err)
	}
	r := w.Check()
	fmt.Printf("%s %.0f-%.0f m: %d map elements, %d triangles; check: holes %.2f m² %v, land over road %.2f m² %v, off road %.0f m %v → %s\n",
		*id, *from, end, len(d.Elements), w.Triangles, r.HolesM2, r.Holes, r.LandOverRoadM2, r.LandOverRoad, r.OffRoadM, r.OffRoad, *out)
}

// find is course id: an included one, or the .gpx of that name in dir.
func find(dir, id string) (*course.Course, error) {
	for _, c := range course.Included() {
		if c.ID == id {
			return c, nil
		}
	}
	cs, err := course.LoadDir(dir)
	for _, c := range cs {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, errors.Join(fmt.Errorf("no course %q (included or in %s)", id, dir), err)
}
