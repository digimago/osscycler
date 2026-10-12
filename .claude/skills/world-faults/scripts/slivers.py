"""slivers.py WORLD X Z [R]: the check's cells within R of x z where land stands over a road surface, with the land triangle's vertices (x z y relative to the cell) and the road under it."""
import sys, math
sys.path.insert(0, __import__("os").path.dirname(__import__("os").path.abspath(__file__)))
import glb
w = glb.World(sys.argv[1]); X, Z = float(sys.argv[2]), float(sys.argv[3]); R = float(sys.argv[4]) if len(sys.argv) > 4 else 1.5
tris = list(w.triangles((X - R - 2, Z - R - 2, X + R + 2, Z + R + 2)))
g = glb.Grid(tris, 2.0)
n = int(R / 0.25)
seen = 0
for j in range(-n, n + 1):
    for i in range(-n, n + 1):
        cx, cz = (math.floor(X / 0.25) + i + 0.5) * 0.25, (math.floor(Z / 0.25) + j + 0.5) * 0.25
        hs = g.at(cx, cz)
        land = [h for h in hs if h[0] in glb.LAND]; road = [h for h in hs if h[0] in glb.ROAD]
        if not land or not road: continue
        hl, hr = max(land, key=lambda q: q[2]), max(road, key=lambda q: q[2])
        if hl[2] > hr[2] + 0.02:
            seen += 1
            if seen > 6: continue
            # the land triangle
            for mat, name, t in g.cells.get((math.floor(cx / 2.0), math.floor(cz / 2.0)), []):
                if mat in glb.LAND and glb.inside(cx, cz, t) and abs(glb.height_in(cx, cz, t) - hl[2]) < 1e-6:
                    vs = " ".join(f"({v[0]-cx:+.2f},{v[2]-cz:+.2f},{v[1]-hr[2]:+.3f})" for v in t)
                    print(f"cell ({cx:.2f},{cz:.2f}): land {hl[0]} {hl[2]:.3f} over {hr[0]} {hr[2]:.3f} by {hl[2]-hr[2]:.3f}; land tri (dx,dz,dy over road) {vs}")
                    break
print(f"{seen} cells over")
