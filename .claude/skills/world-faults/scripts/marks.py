#!/usr/bin/env python3
"""The spots the rider marked as flawed, with a first look at each.

    python3 -I marks.py [MARKS_JSONL] [--last N] [--around 30] [--world DIR]

The renderer started with --debug (osscycler 3d --debug, or godot ... --
--debug) marks the spot when space is pressed: a line in marks.jsonl
(default ~/osscycler/marks/marks.jsonl, --marks DIR moves it) and a
screenshot beside it. For each mark (the last --last N, default all) this
prints when and where (course, km along it, lat/lon), the screenshot,
whether the world it was made in is still the one in its directory (a
rebuild since changes what the analysis sees), and on that world:

  - what is drawn under the rider there (under_path's verdict);
  - the runs not on a road on the riding line within --around metres;
  - the commands for a closer look (near_point.py, near_ways.py, a
    screenshot from the same place).

--world DIR analyses every mark against that world instead (a rebuild into
another directory, to see whether a fix took).
"""

import argparse
import json
import math
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
from glb import Grid, World  # noqa: E402
from under_path import verdict  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("marks", nargs="?", default=os.path.join(os.environ.get("OSSCYCLER_HOME") or os.path.expanduser("~/osscycler"), "marks", "marks.jsonl"))
    ap.add_argument("--last", type=int, default=0)
    ap.add_argument("--around", type=float, default=30.0)
    ap.add_argument("--world", default="")
    a = ap.parse_args()
    try:
        lines = open(a.marks, encoding="utf-8").read().splitlines()
    except FileNotFoundError:
        sys.exit(f"no marks in {a.marks}")
    marks = []
    for n, line in enumerate(lines, 1):
        if line.strip():
            try:
                marks.append((n, json.loads(line)))
            except json.JSONDecodeError as e:
                print(f"line {n}: not a mark ({e})")
    if a.last:
        marks = marks[-a.last:]
    shots = os.path.dirname(os.path.abspath(a.marks))
    worlds = {}
    for n, m in marks:
        d = m.get("along_m", 0.0)  # the spot marked (a debug view may have been moved off the rider: view_offset_m)
        print(f"#{n} {m.get('time', '')}  {m.get('course_id', '')}  {d / 1000:.3f} km"
              f"  lat {m.get('lat', 0):.6f} lon {m.get('lon', 0):.6f}  ({m.get('source', '')}, {m.get('view', '')} view"
              f"{', preview world' if m.get('preview') else ''})")
        if m.get("shot"):
            print(f"   screenshot: {os.path.join(shots, m['shot'])}")
        wdir = a.world or m.get("world", "")
        if not wdir or not os.path.exists(os.path.join(wdir, "world.json")):
            print(f"   world {wdir or '(none)'} isn't there: nothing to analyse")
            continue
        if wdir not in worlds:
            worlds[wdir] = World(wdir)
        w = worlds[wdir]
        stamp = w.manifest.get("stamp", "")
        if w.manifest["course"]["id"] != m.get("course_id"):
            print(f"   {wdir} is course {w.manifest['course']['id']}, not {m.get('course_id')}: skipped")
            continue
        if not a.world and stamp != m.get("stamp"):
            print(f"   the world was rebuilt since (stamp {stamp}, the mark's {m.get('stamp')}): what follows is the world as it is now")
        elif a.world:
            print(f"   analysed against {wdir}")
        # Where the mark says the rider was, against the riding line now.
        rx, _, rz = m.get("rider", [0, 0, 0])
        p = w.riding(d)
        moved = math.hypot(p[0] - rx, p[2] - rz)
        if moved > 0.5:
            print(f"   the riding line at {d:.0f} m is {moved:.1f} m from where the rider was: the path changed")
        lo, hi = max(0.0, d - a.around), d + a.around
        pts = []
        x = lo
        while x <= hi + 1e-9:
            q = w.riding(x)
            pts.append((x, q[0], q[2]))
            x += 1.0
        box = (min(q[1] for q in pts) - 5, min(q[2] for q in pts) - 5, max(q[1] for q in pts) + 5, max(q[2] for q in pts) + 5)
        grid = Grid(list(w.triangles(box)))
        v, names = verdict(grid.at(p[0], p[2]))
        print(f"   under the rider: {v}  {names}")
        runs = []
        for x, qx, qz in pts:
            vv, _ = verdict(grid.at(qx, qz))
            if vv == "road":
                continue
            if runs and runs[-1][2] == vv and x - runs[-1][1] <= 1.0 + 1e-9:
                runs[-1][1] = x
            else:
                runs.append([x, x, vv])
        if runs:
            print(f"   not on a road within {a.around:g} m: " + ", ".join(f"{r0:.0f}-{r1:.0f} m {vv}" for r0, r1, vv in runs))
        else:
            print(f"   the riding line is on a road throughout {lo:.0f}-{hi:.0f} m")
        cid = m.get("course_id", "")
        print(f"   closer: python3 -I {HERE}/near_point.py \"{wdir}\" {d:.1f}")
        print(f"           python3 -I {HERE}/near_ways.py \"{wdir}\" \"<courses>/.osm/{cid}-*.json\" {d:.1f}")
        look = m.get("look_deg", 0)
        print(f"           ~/.local/bin/godot --path renderers/godot -- --world \"{wdir}\" --from {max(0, d - 15):.0f}"
              f"{f' --look {look:g}' if look else ''} --shot <scratchpad>/m{n}.png --frames 150 --seed {m.get('seed', 1)}")


if __name__ == "__main__":
    main()
