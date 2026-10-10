#!/usr/bin/env python3
"""What the map has around a spot: the roads (and anything else tagged).

    python3 -I near_ways.py WORLD_DIR OSM_GLOB D [--r 40] [--all]

OSM_GLOB is the course's cached map data, every stretch:
"<courses>/.osm/<course id>-*.json" (quote it). D is metres along the
course; the spot is on the path's centre line there. Prints every way
with a highway tag (--all: any tag) that comes within --r metres, nearest
first: its distance, OSM ID, the tags that decide how it's drawn, its node
IDs at the ends (two ways sharing an end node are joined there), and its
points as (ahead, right) metres from the spot along the direction of
travel.

The cache holds what the Overpass query asked for (internal/scenery):
roads within 30 m of the route (no cycle paths, footways, tracks or
railways), car parks, buildings, land use, place signs. A road the rider
should be on but that isn't here was never fetched; one that is here but
isn't drawn was dropped by the world builder.
"""

import argparse
import glob
import json
import math
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glb import World  # noqa: E402

KEYS = ("highway", "name", "oneway", "lanes", "width", "junction", "bridge", "tunnel", "layer",
        "bicycle", "foot", "sidewalk", "cycleway", "cycleway:left", "cycleway:right", "cycleway:both",
        "surface", "service", "access", "dual_carriageway", "area")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("world")
    ap.add_argument("osm")
    ap.add_argument("d", type=float)
    ap.add_argument("--r", type=float, default=40.0)
    ap.add_argument("--all", action="store_true", help="every tagged way, not only roads")
    a = ap.parse_args()
    w = World(a.world)
    c, _ = w.at(a.d)
    hx, hz = w.heading(a.d)
    rx, rz = -hz, hx
    files = sorted(glob.glob(a.osm))
    if not files:
        sys.exit(f"no files match {a.osm}")
    seen, rows = set(), []
    for f in files:
        for e in json.load(open(f)).get("elements", []):
            if e.get("type") != "way" or e["id"] in seen or "geometry" not in e:
                continue
            t = e.get("tags", {})
            if not t or ("highway" not in t and not a.all):
                continue
            pts = []
            for g in e["geometry"]:
                if g is None:
                    continue
                x, z = w.to_xz(g["lat"], g["lon"])
                dx, dz = x - c[0], z - c[2]
                pts.append((dx * hx + dz * hz, dx * rx + dz * rz))
            if not pts:
                continue
            # Distance to the way's segments, not only its points.
            best = min(math.hypot(*p) for p in pts)
            for (ax, ay), (bx, by) in zip(pts, pts[1:]):
                l2 = (bx - ax) ** 2 + (by - ay) ** 2
                u = 0 if l2 == 0 else max(0, min(1, -(ax * (bx - ax) + ay * (by - ay)) / l2))
                best = min(best, math.hypot(ax + u * (bx - ax), ay + u * (by - ay)))
            if best <= a.r:
                seen.add(e["id"])
                rows.append((best, e, t, pts))
    lat, lon = w.to_latlon(c[0], c[2])
    print(f"{a.d:.1f} m on the centre line (lat {lat:.6f} lon {lon:.6f}): {len(rows)} way(s) within {a.r:g} m")
    for best, e, t, pts in sorted(rows, key=lambda r: r[0]):
        tags = {k: t[k] for k in KEYS if k in t}
        nodes = e.get("nodes", [])
        ends = f"nodes {nodes[0]}..{nodes[-1]}" if nodes else ""
        line = " ".join(f"({x:.0f},{y:.0f})" for x, y in pts)
        print(f"{best:5.1f} m  way {e['id']} {tags} {ends}")
        print(f"         {line[:400]}")


if __name__ == "__main__":
    main()
