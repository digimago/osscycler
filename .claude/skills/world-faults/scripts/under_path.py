#!/usr/bin/env python3
"""What lies under the rider, from FROM to TO metres along the course.

    python3 -I under_path.py WORLD_DIR FROM TO [--step M] [--centre]

For each point every --step metres (default 1) on the riding line (the
path moved lane_m to the right, where the renderer puts the rider; --centre
for the path's own line), the surfaces drawn over it, and a verdict:

    road      a road surface (World.Check's road materials) is uppermost
    MISSING   only terrain: no road drawn where the rider rides
    HOLE      nothing at all: the sky shows through
    LAND      terrain 2 cm or more above the uppermost road (World.Check's
              land over road)
    OTHER     something else uppermost (bridge deck, island, running track,
              water...): often right, sometimes not

Runs of the same verdict are merged; only runs that aren't "road" are
printed unless -v. The world's own check (World.Check) looks within 40 m
of the path for holes and land over road; it cannot see a MISSING road,
since the terrain there is whole.
"""

import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glb import LAND, ROAD, Grid, World  # noqa: E402


def verdict(hits):
    if not hits:
        return "HOLE", ""
    top = max(hits, key=lambda h: h[2])
    roads = [h for h in hits if h[0] in ROAD]
    lands = [h for h in hits if h[0] in LAND]
    names = ", ".join(sorted({f"{m} ({n})" for m, n, _ in hits}))
    if not roads:
        return ("MISSING" if top[0] in LAND else "OTHER"), names
    if lands and max(h[2] for h in lands) >= max(h[2] for h in roads) + 0.02:
        return "LAND", names
    return ("road" if top[0] in ROAD else "OTHER"), names


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("world")
    ap.add_argument("frm", type=float)
    ap.add_argument("to", type=float)
    ap.add_argument("--step", type=float, default=1.0)
    ap.add_argument("--centre", action="store_true", help="the path's own line instead of the riding line")
    ap.add_argument("-v", action="store_true", help="print every run, roads too")
    a = ap.parse_args()
    w = World(a.world)
    pts = []
    d = a.frm
    while d <= a.to + 1e-9:
        if a.centre:
            p, _ = w.at(d)
        else:
            p = w.riding(d)
        pts.append((d, p[0], p[2]))
        d += a.step
    xs, zs = [p[1] for p in pts], [p[2] for p in pts]
    box = (min(xs) - 5, min(zs) - 5, max(xs) + 5, max(zs) + 5)
    grid = Grid(list(w.triangles(box)))
    runs = []
    for d, x, z in pts:
        v, names = verdict(grid.at(x, z))
        if runs and runs[-1][2] == v and d - runs[-1][1] <= a.step + 1e-9:
            runs[-1][1] = d
            runs[-1][3] |= {names} if names else set()
        else:
            runs.append([d, d, v, {names} if names else set()])
    line = "centre line" if a.centre else "riding line"
    bad = 0
    for d0, d1, v, names in runs:
        if v != "road":
            bad += 1
        if v != "road" or a.v:
            print(f"{d0:9.1f}-{d1:9.1f} m  {v:8s} {'; '.join(sorted(names))}")
    print(f"{line}, {a.frm:.0f}-{a.to:.0f} m every {a.step:g} m: {bad} run(s) not on a road")


if __name__ == "__main__":
    main()
