#!/usr/bin/env python3
"""Which surface is on top round a spot, and the triangles over it.

    python3 -I raster_at.py WORLD_DIR X Z [--r 2] [--cell 0.1]

X Z in the world's frame (area_check.py prints them). A text raster,
north at the top: t terrain, R road, S sidewalk, K kerb, C cycle lane,
? anything else, . nothing (a hole); then every triangle over the spot
with its corners as (x, height, z) relative to it. Shows slivers between
surfaces, a land triangle reaching over a road's edge, an edge that
stops short.
"""
import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import glb  # noqa: E402

ap = argparse.ArgumentParser()
ap.add_argument("world")
ap.add_argument("x", type=float)
ap.add_argument("z", type=float)
ap.add_argument("--r", type=float, default=2.0)
ap.add_argument("--cell", type=float, default=0.1)
a = ap.parse_args()
w = glb.World(a.world)
r, c = a.r, a.cell
tris = list(w.triangles((a.x - r - 1, a.z - r - 1, a.x + r + 1, a.z + r + 1)))
g = glb.Grid(tris, 4.0)
ch = {"terrain": "t", "road": "R", "sidewalk": "S", "kerb": "K", "cycle lane": "C"}
n = int(2 * r / c)
for j in range(n):
    z = a.z - r + (j + 0.5) * c
    row = ""
    for i in range(n):
        hs = g.at(a.x - r + (i + 0.5) * c, z)
        row += ch.get(max(hs, key=lambda q: q[2])[0], "?") if hs else "."
    print(row)
print(f"over {a.x:.2f} {a.z:.2f}:")
for m, name, t in tris:
    if glb.inside(a.x, a.z, t):
        print(" ", m, name, [(round(p[0] - a.x, 2), round(p[1], 2), round(p[2] - a.z, 2)) for p in t])
