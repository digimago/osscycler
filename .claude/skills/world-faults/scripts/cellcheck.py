"""cellcheck.py WORLD FROM TO: metres where the 0.25 m check cell holding the riding point has no road at its centre (as World.Check counts off road), and the road at the point itself."""
import sys, math
sys.path.insert(0, __import__("os").path.dirname(__import__("os").path.abspath(__file__)))
import glb
w = glb.World(sys.argv[1]); a, b = int(float(sys.argv[2])), int(float(sys.argv[3]))
pts = [w.riding(d) for d in range(a, b + 1)]
box = (min(p[0] for p in pts) - 5, min(p[2] for p in pts) - 5, max(p[0] for p in pts) + 5, max(p[2] for p in pts) + 5)
g = glb.Grid(list(w.triangles(box)), 4.0)
for d, p in zip(range(a, b + 1), pts):
    cx, cz = (math.floor(p[0] / 0.25) + 0.5) * 0.25, (math.floor(p[2] / 0.25) + 0.5) * 0.25
    road_c = [h for h in g.at(cx, cz) if h[0] in glb.ROAD]
    road_p = [h for h in g.at(p[0], p[2]) if h[0] in glb.ROAD]
    if not road_c:
        print(f"{d} m: cell centre ({cx:.3f}, {cz:.3f}) has no road; the point ({p[0]:.3f}, {p[2]:.3f}) has {[h[0] for h in road_p] or 'nothing'}, lane {w.at(d)[1]:.2f}")
