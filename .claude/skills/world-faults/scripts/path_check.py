#!/usr/bin/env python3
"""Where the rider's path does something a rider wouldn't: the worst places.

    python3 -I path_check.py WORLD_DIR [FROM TO] [--top 8]

Reads world.json's path (a point every step_m of course; the riding line
lane_m to its right) and ranks, per kind, the --top worst places (with
distances along the course):

  turn     heading change within 20 m (smoothTurns in internal/world/
           riding.go sweeps turns over 45° within 20 m over 15 m either
           side; street corners stay near 90°, over that is a hairpin or a
           hop between roads)
  zigzag   a point off the line through its two neighbours (a jump sideways
           and back: a snap to another road, a road change)
  spacing  distance to the next point for one step of course, far from
           step_m (evenPace spreads them; the Posbank Loop's are 1.6-7 m)
  lane     change of lane_m per step (limitDrift allows 0.03 m per m before
           evenPace re-spreads the points; flat roundabouts set it to 0)
  grade    the path's own height change between points (a step in the road
           under it, or the path leaving it)

A ranking, not a verdict: compare it with the same course built before
your change (the Posbank Loop as built 2026-10-10: worst turn 66° within
20 m and worst zigzag 2.84 m, both at 18.17 km; worst grade step 10.8 % at
3.355 km, the Diepesteeg), and confirm a place with under_path.py,
near_point.py and a screenshot.
"""

import argparse
import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glb import World  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("world")
    ap.add_argument("frm", type=float, nargs="?", default=0)
    ap.add_argument("to", type=float, nargs="?", default=math.inf)
    ap.add_argument("--top", type=int, default=8)
    a = ap.parse_args()
    w = World(a.world)
    P, L, st = w.path, w.lane, w.step
    n = len(P)
    half = max(1, round(10 / st))  # 20 m: smoothTurns' turnSpanM
    kinds = {"turn": [], "zigzag": [], "spacing": [], "lane": [], "grade": []}

    def head(i, j):
        return math.atan2(P[j][2] - P[i][2], P[j][0] - P[i][0])

    for i in range(1, n - 1):
        d = i * st
        if d < a.frm or d > a.to:
            continue
        if i - half >= 0 and i + half < n:
            t = abs((head(i, i + half) - head(i - half, i) + math.pi) % (2 * math.pi) - math.pi)
            kinds["turn"].append((math.degrees(t), d, f"{math.degrees(t):.0f}° within {2 * half * st:.0f} m"))
        (ax, _, az), (px, _, pz), (bx, _, bz) = P[i - 1], P[i], P[i + 1]
        cl = math.hypot(bx - ax, bz - az)
        if cl > 1e-6:
            off = abs((bx - ax) * (pz - az) - (bz - az) * (px - ax)) / cl
            kinds["zigzag"].append((off, d, f"{off:.2f} m off the line through its neighbours"))
        gap = math.hypot(bx - px, bz - pz)
        kinds["spacing"].append((abs(math.log(max(gap, 1e-3) / st)), d, f"{gap:.1f} m to the next point"))
        kinds["lane"].append((abs(L[i + 1] - L[i]), d, f"lane {L[i]:.2f} -> {L[i + 1]:.2f} m"))
        if gap > 0.5:
            g = (P[i + 1][1] - P[i][1]) / gap
            kinds["grade"].append((abs(g), d, f"{100 * g:+.1f} % over {gap:.1f} m"))
    for k, rows in kinds.items():
        rows.sort(reverse=True)
        picked = []
        for score, d, what in rows:  # the worst, one per 50 m
            if all(abs(d - p[1]) > 50 for p in picked):
                picked.append((score, d, what))
            if len(picked) == a.top:
                break
        print(f"{k}:")
        for _, d, what in picked:
            print(f"  {d / 1000:8.3f} km  {what}")
    print(f"path every {st:g} m, {n} points")


if __name__ == "__main__":
    main()
