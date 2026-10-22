#!/usr/bin/env python3
"""Where the holes and the land over road are, near the path.

    python3 -I area_check.py WORLD_DIR FROM TO [--reach 40] [--cell 0.25]

World.Check gives totals per 200 m; this finds the cells themselves
(within --reach of the path from FROM to TO metres along the course, every
--cell), clusters them, and prints each cluster's area, where it is (km,
ahead and right of the path there, x z in the frame) and, for land over
road, by how much the land stands over it. Then raster_at.py on a spot.
"""
import argparse
import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import glb  # noqa: E402

ap = argparse.ArgumentParser()
ap.add_argument("world")
ap.add_argument("frm", type=float)
ap.add_argument("to", type=float)
ap.add_argument("--reach", type=float, default=40)
ap.add_argument("--cell", type=float, default=0.25)
a = ap.parse_args()
w = glb.World(a.world)
pts = [w.at(d)[0] for d in [a.frm + k for k in range(0, int(a.to - a.frm) + 1, 5)]]
x0 = min(p[0] for p in pts) - a.reach
x1 = max(p[0] for p in pts) + a.reach
z0 = min(p[2] for p in pts) - a.reach
z1 = max(p[2] for p in pts) + a.reach
g = glb.Grid(list(w.triangles((x0, z0, x1, z1))), 4.0)
road_mats = glb.ROAD | {"running track"}
pidx = {}
for k, p in enumerate(pts):
    pidx.setdefault((int(p[0] // 40), int(p[2] // 40)), []).append((k, p))


def nearpath(x, z):
    best = None
    for gx in range(int(x // 40) - 1, int(x // 40) + 2):
        for gz in range(int(z // 40) - 1, int(z // 40) + 2):
            for k, p in pidx.get((gx, gz), []):
                d = math.hypot(p[0] - x, p[2] - z)
                if d <= a.reach and (best is None or d < best[0]):
                    best = (d, k)
    return best


holes, over = [], []
c = a.cell
for j in range(int((z1 - z0) / c)):
    z = z0 + (j + 0.5) * c
    for i in range(int((x1 - x0) / c)):
        x = x0 + (i + 0.5) * c
        np_ = nearpath(x, z)
        if not np_:
            continue
        hs = g.at(x, z)
        land = [h for m, n, h in hs if m in glb.LAND]
        road = [h for m, n, h in hs if m in road_mats]
        roof = [h for m, n, h in hs if m in glb.ROOF]
        if not hs:
            holes.append((x, z, np_[1], 0))
        elif land and road and max(land) > max(road) + 0.02 and not (roof and max(roof) > max(road)):
            over.append((x, z, np_[1], max(land) - max(road)))


def cluster(cells):
    out = []
    for cell in cells:
        for cl in out:
            if any(math.hypot(cell[0] - q[0], cell[1] - q[1]) < 3 for q in cl[-20:]):
                cl.append(cell)
                break
        else:
            out.append([cell])
    return out


for name, cells in (("holes", holes), ("land over road", over)):
    cl = cluster(cells)
    print(f"{name}: {len(cells) * c * c:.2f} m² in {len(cl)} clusters")
    for q in sorted(cl, key=len, reverse=True)[:15]:
        x = sum(v[0] for v in q) / len(q)
        z = sum(v[1] for v in q) / len(q)
        d = a.frm + 5 * q[0][2]
        p, _ = w.at(d)
        hx, hz = w.heading(d)
        ahead = (x - p[0]) * hx + (z - p[2]) * hz
        right = (x - p[0]) * (-hz) + (z - p[2]) * hx
        extra = f" over by {max(v[3] for v in q):.2f} m" if name != "holes" else ""
        print(f"  {len(q) * c * c:6.2f} m² at {d / 1000:.3f} km, ahead {ahead:.1f} right {right:.1f} (x {x:.1f} z {z:.1f}){extra}")
