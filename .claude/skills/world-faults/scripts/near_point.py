#!/usr/bin/env python3
"""What is drawn around a spot, seen from the rider.

    python3 -I near_point.py WORLD_DIR D [--r 9] [--right M] [--max 40]

The spot is D metres along the course, on the path's centre line moved
--right metres to the right of the direction of travel (default: the
riding line, lane_m). Prints:

  - the surfaces over the spot (material, node, height);
  - per material and node, the distinct vertices within --r metres, as
    (ahead, right, up) relative to the spot: metres ahead along the
    direction of travel, to its right, and above the spot's uppermost
    surface.

Two surfaces that should meet but don't show as two rows of vertices with
a strip between them (a road end at ahead 0.0 and the next at ahead 4.3:
the 4 m between them is drawn by neither); a step shows in "up".
"""

import argparse
import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glb import Grid, World  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("world")
    ap.add_argument("d", type=float)
    ap.add_argument("--r", type=float, default=9.0)
    ap.add_argument("--right", type=float, default=None, help="metres right of the centre line (default: lane_m)")
    ap.add_argument("--max", type=int, default=40, help="vertices shown per material and node")
    a = ap.parse_args()
    w = World(a.world)
    c, lane = w.at(a.d)
    off = lane if a.right is None else a.right
    hx, hz = w.heading(a.d)
    rx, rz = -hz, hx  # right of the direction of travel, in x, z
    px, pz = c[0] + rx * off, c[2] + rz * off
    box = (px - a.r - 1, pz - a.r - 1, px + a.r + 1, pz + a.r + 1)
    tris = list(w.triangles(box))
    hits = Grid(tris).at(px, pz)
    base = max((h for _, _, h in hits), default=c[1])
    lat, lon = w.to_latlon(px, pz)
    print(f"{a.d:.1f} m, {off:+.2f} m right of the centre line: x {px:.2f} z {pz:.2f} "
          f"(lat {lat:.6f} lon {lon:.6f}), path y {c[1]:.3f}")
    for m, n, h in sorted(hits, key=lambda x: -x[2]):
        print(f"  over the spot: {m} ({n}) at y {h:.3f}")
    if not hits:
        print("  over the spot: nothing (a hole)")
    groups = {}
    for m, n, t in tris:
        for q in t:
            dx, dz = q[0] - px, q[2] - pz
            if math.hypot(dx, dz) <= a.r:
                groups.setdefault((m, n), set()).add(
                    (round(dx * hx + dz * hz, 1), round(dx * rx + dz * rz, 1), round(q[1] - base, 2)))
    for (m, n), vs in sorted(groups.items()):
        vs = sorted(vs)
        more = f" (first {a.max} of {len(vs)})" if len(vs) > a.max else ""
        print(f"{m} ({n}){more}: (ahead, right, up)")
        print("  " + " ".join(f"({x},{y},{u:+.2f})" for x, y, u in vs[: a.max]))


if __name__ == "__main__":
    main()
