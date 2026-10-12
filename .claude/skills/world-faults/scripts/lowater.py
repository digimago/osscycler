import sys
sys.path.insert(0, __import__("os").path.dirname(__import__("os").path.abspath(__file__)))
import glb
w = glb.World(sys.argv[1]); lo, hi = int(sys.argv[2]), int(sys.argv[3])
over, wet = {}, {}
for d in range(lo, hi, 5):
    p, _ = w.at(d); hx, hz = w.heading(d)
    g = glb.Grid(list(w.triangles((p[0]-42, p[2]-42, p[0]+42, p[2]+42))), 4.0)
    b = d // 200 * 200
    for sg in (-1, 1):
        for k in range(3, 40):
            x, z = p[0] - hz * k * sg, p[2] + hx * k * sg
            hs = g.at(x, z)
            ws = [h for m, n, h in hs if m == 'water']
            ls = [h for m, n, h in hs if m == 'terrain']
            if ws:
                wet[b] = wet.get(b, 0) + 1
                if ls and max(ls) > max(ws) + 0.02:
                    over[b] = over.get(b, 0) + 1
print(w.manifest['stamp'], ' '.join(f"{b/1000:.1f}:{over.get(b,0)}/{wet[b]}" for b in sorted(wet)))
