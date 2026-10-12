"""shared.py WORLD_A WORLD_B: where A's path runs on B's path (within 8 m), and how the two worlds agree there."""
import sys, math, struct, json
sys.path.insert(0, __import__("os").path.dirname(__import__("os").path.abspath(__file__)))
import glb
A, B = glb.World(sys.argv[1]), glb.World(sys.argv[2])
def pts(w):
    L = int((len(w.path)-1)*w.step); return [(d, w.at(d)[0]) for d in range(0, L+1, 5)]
pa, pb = pts(A), pts(B)
cell = 20.0
grid = {}
for d, p in pb: grid.setdefault((int(p[0]//cell), int(p[2]//cell)), []).append((d, p))
def nearest(p):
    k = (int(p[0]//cell), int(p[2]//cell)); best = (1e9, None)
    for i in (-1,0,1):
        for j in (-1,0,1):
            for d, q in grid.get((k[0]+i, k[1]+j), []):
                dd = math.hypot(p[0]-q[0], p[2]-q[2])
                if dd < best[0]: best = (dd, d)
    return best
shared = [(d, p, nearest(p)) for d, p in pa]
runs, cur = [], None
for d, p, (dd, db) in shared:
    if dd < 8:
        if cur is None: cur = [d, d, db, db]
        cur[1], cur[3] = d, db
    elif cur: runs.append(cur); cur = None
if cur: runs.append(cur)
for r in runs:
    if r[1]-r[0] >= 50: print(f"A {r[0]/1000:.2f}-{r[1]/1000:.2f} km runs on B {r[2]/1000:.2f}-{r[3]/1000:.2f} km ({(r[1]-r[0])/1000:.2f} km)")
# road and land heights at A's riding line through the shared runs, in both worlds
def box_of(run):
    xs=[p[0] for d,p in pa if run[0]<=d<=run[1]]; zs=[p[2] for d,p in pa if run[0]<=d<=run[1]]
    return (min(xs)-40, min(zs)-40, max(xs)+40, max(zs)+40)
road = {'road','cycle lane','sidewalk','roundabout centre'}
dr, dl, nr, nl, miss = [], [], 0, 0, 0
for run in runs:
    if run[1]-run[0] < 50: continue
    box = box_of(run)
    ga, gb = glb.Grid(list(A.triangles(box)), 4.0), glb.Grid(list(B.triangles(box)), 4.0)
    for d in range(run[0], run[1]+1, 5):
        x, y, z = A.riding(d); hx, hz = A.heading(d)
        for (px, pz), which in (((x, z), 'road'), ((x + 12*hz, z - 12*hx), 'land')):
            ha = [h for h in ga.at(px, pz)]; hb = [h for h in gb.at(px, pz)]
            if not ha or not hb: miss += which == 'road'; continue
            ta, tb = max(ha, key=lambda q: q[2]), max(hb, key=lambda q: q[2])
            if which == 'road':
                if ta[0] in road and tb[0] in road: dr.append(abs(ta[2]-tb[2])); nr += 1
                else: miss += 1
            elif ta[0] == 'terrain' and tb[0] == 'terrain': dl.append(abs(ta[2]-tb[2])); nl += 1
def q(v, f): v=sorted(v); return v[min(len(v)-1, int(f*len(v)))] if v else float('nan')
print(f"road height: {nr} points, median {q(dr,.5):.3f} m, 95% {q(dr,.95):.3f}, worst {max(dr) if dr else 0:.3f}; {miss} points not road in both")
print(f"land 12 m right: {nl} points, median {q(dl,.5):.3f} m, 95% {q(dl,.95):.3f}, worst {max(dl) if dl else 0:.3f}")
# plants: the same kind at the same spot
def plants(w):
    m = json.load(open(w.dir + '/world.json')); raw = open(w.dir + '/instances.bin','rb').read()
    out = {}
    for g in m['instances']['groups']:
        for i in range(g['count']):
            x, y, z, s, yaw = struct.unpack_from('<5f', raw, (g['offset']+i)*20)
            out.setdefault((g['kind'], round(x,1), round(z,1)), 0); out[(g['kind'], round(x,1), round(z,1))] += 1
    return out
try:
    PA, PB = plants(A), plants(B)
    boxes = [box_of(r) for r in runs if r[1]-r[0] >= 50]
    inb = lambda k: any(b[0]<=k[1]<=b[2] and b[1]<=k[2]<=b[3] for b in boxes)
    ka = [k for k in PA if inb(k)]; same = sum(1 for k in ka if k in PB)
    print(f"plants within 40 m of the shared stretch: {len(ka)} in A, {same} the same kind at the same spot in B ({100*same/max(1,len(ka)):.0f} %)")
except Exception as e: print("plants:", e)
