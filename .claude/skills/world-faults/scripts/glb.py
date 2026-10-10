"""Reading an osscycler world (world.glb + world.json) for fault finding.

Only what the world builder writes: one buffer, triangle lists, float32
positions (KHR_mesh_quantization quantizes normals only), node translations
at most (no rotation or scale on drawn nodes), indices as u16 or u32.
Positions are in the world's frame: x east, y up, z south of the origin
(world.json "origin", the course's first point).
"""

import json
import math
import struct

METRES_PER_DEGREE = 6371000.0 * math.pi / 180  # internal/course: metresPerDegree


class World:
    def __init__(self, d):
        self.dir = d
        self.manifest = json.load(open(f"{d}/world.json"))
        b = open(f"{d}/world.glb", "rb").read()
        if b[:4] != b"glTF":
            raise ValueError(f"{d}/world.glb is not a .glb")
        jl = struct.unpack_from("<I", b, 12)[0]
        self.gltf = json.loads(b[20 : 20 + jl])
        self.bin = b[20 + jl + 8 :]
        self.materials = [m.get("name", "") for m in self.gltf.get("materials", [])]
        p = self.manifest["path"]
        self.step = p["step_m"]
        xyz = p["xyz"]
        self.path = [tuple(xyz[i : i + 3]) for i in range(0, len(xyz), 3)]
        self.lane = p.get("lane_m") or [0.0] * len(self.path)
        o = self.manifest["origin"]
        self.lat0, self.lon0 = o["lat"], o["lon"]
        self.kx = METRES_PER_DEGREE * math.cos(math.radians(self.lat0))

    # The frame: course.Project, with glTF's z = -north.
    def to_xz(self, lat, lon):
        return (lon - self.lon0) * self.kx, -(lat - self.lat0) * METRES_PER_DEGREE

    def to_latlon(self, x, z):
        return self.lat0 - z / METRES_PER_DEGREE, self.lon0 + x / self.kx

    def _accessor(self, i):
        a = self.gltf["accessors"][i]
        bv = self.gltf["bufferViews"][a["bufferView"]]
        return a, bv, bv.get("byteOffset", 0) + a.get("byteOffset", 0)

    def positions(self, i):
        a, bv, off = self._accessor(i)
        stride = bv.get("byteStride", 12)
        return [struct.unpack_from("<3f", self.bin, off + k * stride) for k in range(a["count"])]

    def indices(self, i):
        a, _, off = self._accessor(i)
        fmt = {5125: "I", 5123: "H", 5121: "B"}[a["componentType"]]
        return struct.unpack_from(f"<{a['count']}{fmt}", self.bin, off)

    def triangles(self, box=None, skip=("template",)):
        """Yields (material, node name, ((x,y,z),)*3) for every drawn triangle
        (hidden template nodes left out), only those whose bounding box meets
        box = (x0, z0, x1, z1) if given."""
        for n in self.gltf["nodes"]:
            name = n.get("name", "")
            if "mesh" not in n or any(name.startswith(s) for s in skip):
                continue
            tx, ty, tz = n.get("translation", [0, 0, 0])
            for pr in self.gltf["meshes"][n["mesh"]]["primitives"]:
                if pr.get("mode", 4) != 4:
                    continue
                mat = self.materials[pr["material"]] if "material" in pr else ""
                P = self.positions(pr["attributes"]["POSITION"])
                if box:
                    xs = [q[0] + tx for q in P]
                    zs = [q[2] + tz for q in P]
                    if not P or max(xs) < box[0] or min(xs) > box[2] or max(zs) < box[1] or min(zs) > box[3]:
                        continue
                I = self.indices(pr["indices"])
                for k in range(0, len(I) - 2, 3):
                    t = tuple((P[I[k + j]][0] + tx, P[I[k + j]][1] + ty, P[I[k + j]][2] + tz) for j in range(3))
                    if box and (max(q[0] for q in t) < box[0] or min(q[0] for q in t) > box[2]
                                or max(q[2] for q in t) < box[1] or min(q[2] for q in t) > box[3]):
                        continue
                    yield mat, name, t

    def at(self, d):
        """The path point at distance d (m along the course), interpolated."""
        f = max(0.0, min(d / self.step, len(self.path) - 1.0))
        i = min(int(f), len(self.path) - 2)
        u = f - i
        a, b = self.path[i], self.path[i + 1]
        return tuple(a[k] + u * (b[k] - a[k]) for k in range(3)), self.lane[i] + u * (self.lane[i + 1] - self.lane[i])

    def heading(self, d):
        """The unit direction of travel at d, in x, z."""
        (a, _), (b, _) = self.at(d - 1), self.at(d + 1)
        dx, dz = b[0] - a[0], b[2] - a[2]
        n = math.hypot(dx, dz) or 1
        return dx / n, dz / n

    def riding(self, d):
        """The riding line at d: the path point moved lane_m to the right of
        the direction of travel (in x, z, right of (dx, dz) is (-dz, dx))."""
        (p, lane) = self.at(d)
        hx, hz = self.heading(d)
        return (p[0] - hz * lane, p[1], p[2] + hx * lane)


def inside(px, pz, t):
    """Whether x, z lies in triangle t (seen from above), edges included.
    A triangle with no area seen from above (a wall, a skirt, or one
    welded to a point) covers nothing, as in World.Check."""
    (ax, _, az), (bx, _, bz), (cx, _, cz) = t
    if abs((bx - ax) * (cz - az) - (bz - az) * (cx - ax)) < 1e-9:
        return False
    d1 = (bx - ax) * (pz - az) - (bz - az) * (px - ax)
    d2 = (cx - bx) * (pz - bz) - (cz - bz) * (px - bx)
    d3 = (ax - cx) * (pz - cz) - (az - cz) * (px - cx)
    return (d1 >= 0 and d2 >= 0 and d3 >= 0) or (d1 <= 0 and d2 <= 0 and d3 <= 0)


def height_in(px, pz, t):
    """The triangle's height at x, z (barycentric)."""
    (ax, ay, az), (bx, by, bz), (cx, cy, cz) = t
    den = (bz - cz) * (ax - cx) + (cx - bx) * (az - cz)
    if abs(den) < 1e-12:
        return max(ay, by, cy)
    u = ((bz - cz) * (px - cx) + (cx - bx) * (pz - cz)) / den
    v = ((cz - az) * (px - cx) + (ax - cx) * (pz - cz)) / den
    return u * ay + v * by + (1 - u - v) * cy


class Grid:
    """Triangles bucketed by 4 m cells, for point lookups."""

    def __init__(self, tris, cell=4.0):
        self.cell, self.cells = cell, {}
        for k, (mat, name, t) in enumerate(tris):
            x0, x1 = min(q[0] for q in t), max(q[0] for q in t)
            z0, z1 = min(q[2] for q in t), max(q[2] for q in t)
            for gx in range(math.floor(x0 / cell), math.floor(x1 / cell) + 1):
                for gz in range(math.floor(z0 / cell), math.floor(z1 / cell) + 1):
                    self.cells.setdefault((gx, gz), []).append((mat, name, t))

    def at(self, x, z):
        """(material, node, height) of every triangle over x, z."""
        out = []
        for mat, name, t in self.cells.get((math.floor(x / self.cell), math.floor(z / self.cell)), []):
            if inside(x, z, t):
                out.append((mat, name, height_in(x, z, t)))
        return out


# Materials as World.Check (internal/world/check.go) classes them.
LAND = {"terrain"}
ROAD = {"road", "cycle lane", "sidewalk", "kerb", "roundabout centre", "start line"}
