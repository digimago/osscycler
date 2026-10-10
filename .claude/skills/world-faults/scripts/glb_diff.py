#!/usr/bin/env python3
"""Where two builds of a world differ.

    python3 -I glb_diff.py WORLD_A WORLD_B [--max 12]

Compares world.json, instances.bin and ground.bin whole, and world.glb per
node, primitive and attribute (POSITION, NORMAL, COLOR_0, TEXCOORD_0,
indices). For positions that differ it says how far the vertices moved
(each differing vertex to the nearest of the other build's, over the first
300 of them): sub-millimetre to centimetres is arithmetic in another order
(map iteration, float sums), metres is a different decision.

Two builds of one course by one builder must be byte-identical
(TestBuildIsTheSameEveryTime); compare after any change to the road
network, junctions or the cut, and before believing a before/after diff.
"""

import argparse
import hashlib
import math
import os
import sys
from collections import Counter

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from glb import World  # noqa: E402


SIZE = {5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}
COMPONENTS = {"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4}


def arrays(w):
    out = {}
    g = w.gltf
    for n in g["nodes"]:
        if "mesh" not in n:
            continue
        for pi, pr in enumerate(g["meshes"][n["mesh"]]["primitives"]):
            items = list(pr["attributes"].items())
            if "indices" in pr:
                items.append(("indices", pr["indices"]))
            for k, ai in items:
                a, bv, off = w._accessor(ai)
                size = SIZE[a["componentType"]] * COMPONENTS[a["type"]]
                stride = bv.get("byteStride", size)
                data = w.bin[off : off + stride * (a["count"] - 1) + size]
                out[(n.get("name", ""), pi, k)] = (a["count"], hashlib.md5(data).hexdigest(), ai)
    return out


def sha(path):
    try:
        return hashlib.sha256(open(path, "rb").read()).hexdigest()[:16]
    except FileNotFoundError:
        return "(missing)"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("a")
    ap.add_argument("b")
    ap.add_argument("--max", type=int, default=12)
    o = ap.parse_args()
    for f in ("world.json", "instances.bin", "ground.bin", "world.glb"):
        x, y = sha(f"{o.a}/{f}"), sha(f"{o.b}/{f}")
        print(f"{f:14s} {'same' if x == y else 'DIFFERS'}  {x} {y}")
    wa, wb = World(o.a), World(o.b)
    A, B = arrays(wa), arrays(wb)
    diff = sorted(k for k in set(A) | set(B) if A.get(k, (0, ""))[:2] != B.get(k, (0, ""))[:2])
    print(f"{len(A)} and {len(B)} arrays, {len(diff)} differ")
    print("by kind:", Counter((k[0].split(" ")[0], k[2]) for k in diff).most_common())
    shown = 0
    for k in diff:
        if k[2] != "POSITION" or k not in A or k not in B or shown >= o.max:
            continue
        shown += 1
        pa = set(wa.positions(A[k][2]))
        pb = set(wb.positions(B[k][2]))
        only_a, only_b = list(pa - pb)[:300], list(pb - pa)
        if not only_a or not only_b:
            print(f"  {k[0]} #{k[1]}: {A[k][0]} vs {B[k][0]} vertices, same positions in another order")
            continue
        moved = max(min(math.dist(p, q) for q in only_b) for p in only_a)
        print(f"  {k[0]} #{k[1]}: {A[k][0]} vs {B[k][0]} vertices, {len(pa - pb)} moved, up to {moved:.4f} m")


if __name__ == "__main__":
    main()
