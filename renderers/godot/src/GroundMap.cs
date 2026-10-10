using System;
using System.Collections.Generic;
using System.IO;
using System.IO.Compression;
using Godot;

namespace Osscycler;

// GroundMap answers what the ground is like and how high it is anywhere
// near the route: the world's ground map (ground.bin: per cell the land
// use, 1 + its class, 0 beyond the map, and the clearance, the distance
// to the nearest road edge, wall or car park in 0.1 m steps) and the
// heights of the terrain the world loaded. Positions are east and north
// (glTF x and -z).
public sealed class GroundMap
{
    readonly Dictionary<(int, int), byte[]> _ground = new();
    readonly Dictionary<(int, int), float[]> _heights = new();
    readonly Dictionary<(int, int), float[]> _fine = new(); // the ground map's heights
    readonly float _chunk, _cell, _groundCell;
    readonly int _groundN, _heightN;

    // Classes names the ground map's values, by value (from the manifest).
    public string[] Classes { get; }
    // CellM is the ground map's cell size.
    public float CellM => _groundCell;

    GroundMap(Manifest m)
    {
        _chunk = (float)m.Terrain.ChunkM;
        _cell = (float)m.Terrain.CellM;
        _groundCell = (float)m.Ground.CellM;
        _groundN = (int)Math.Round(_chunk / _groundCell);
        _heightN = (int)Math.Round(_chunk / _cell) + 1;
        Classes = m.Ground.Classes;
    }

    // Load reads the ground map and the terrain's heights from the loaded
    // scene; null for a world without a ground map (built before it).
    public static GroundMap? Load(Manifest m, string dir, Node3D scene)
    {
        var g = m.Ground;
        if (g.File == "" || g.CellM <= 0 || m.Terrain.ChunkM <= 0 || m.Terrain.CellM <= 0 || g.Layout != "land, clearance_0.1m, height_cm_delta")
            return null;
        var gm = new GroundMap(m);
        var data = File.ReadAllBytes(System.IO.Path.Combine(dir, g.File));
        int nn = gm._groundN * gm._groundN;
        int cells = 2 * nn + 4 + 2 * nn; // land, clearance, then heights
        foreach (var ch in g.Chunks)
        {
            using var z = new ZLibStream(new MemoryStream(data, ch.Offset, ch.Size), CompressionMode.Decompress);
            var b = new byte[cells];
            int read = 0;
            while (read < cells)
            {
                int k = z.Read(b, read, cells - read);
                if (k == 0)
                    break;
                read += k;
            }
            if (read != cells)
                continue;
            var key = (ch.Chunk[0], ch.Chunk[1]);
            gm._ground[key] = b;
            // Heights: centimetres above the chunk's lowest, as differences
            // along each row.
            int hb = 2 * nn;
            float low = BitConverter.ToInt32(b, hb) / 100f;
            var fine = new float[nn];
            for (int j = 0; j < gm._groundN; j++)
            {
                int cm = 0;
                for (int i = 0; i < gm._groundN; i++)
                {
                    int k = j * gm._groundN + i;
                    cm += BitConverter.ToInt16(b, hb + 4 + 2 * k);
                    fine[k] = b[k] == 0 ? float.NaN : low + cm / 100f;
                }
            }
            gm._fine[key] = fine;
        }
        gm.ReadHeights(scene);
        return gm;
    }

    // ReadHeights takes the terrain's heights from its chunks' meshes: on a
    // regular grid per chunk, each point over the mesh triangle above it
    // (the mesh is coarser away from the route: its level of detail).
    void ReadHeights(Node3D scene)
    {
        foreach (var n in scene.FindChildren("terrain *", "MeshInstance3D", true, false))
        {
            var mi = (MeshInstance3D)n;
            var parts = mi.Name.ToString().Split(' ');
            if (parts.Length != 3 || !int.TryParse(parts[1], out int ci) || !int.TryParse(parts[2], out int cj))
                continue;
            float e0 = ci * _chunk, n0 = cj * _chunk;
            var h = new float[_heightN * _heightN];
            Array.Fill(h, float.NaN);
            for (int s = 0; s < mi.Mesh.GetSurfaceCount(); s++)
            {
                var arrays = mi.Mesh.SurfaceGetArrays(s);
                var v = arrays[(int)Mesh.ArrayType.Vertex].AsVector3Array();
                var idx = arrays[(int)Mesh.ArrayType.Index].AsInt32Array();
                for (int t = 0; t + 2 < idx.Length; t += 3)
                {
                    // In grid units, x east and y north.
                    Vector3 a = v[idx[t]], b = v[idx[t + 1]], c = v[idx[t + 2]];
                    float ax = (a.X - e0) / _cell, ay = (-a.Z - n0) / _cell;
                    float bx = (b.X - e0) / _cell, by = (-b.Z - n0) / _cell;
                    float cx = (c.X - e0) / _cell, cy = (-c.Z - n0) / _cell;
                    float den = (by - cy) * (ax - cx) + (cx - bx) * (ay - cy);
                    if (Math.Abs(den) < 1e-9f)
                        continue;
                    int i0 = Math.Max(0, (int)Math.Ceiling(Math.Min(ax, Math.Min(bx, cx)) - 1e-4f));
                    int i1 = Math.Min(_heightN - 1, (int)Math.Floor(Math.Max(ax, Math.Max(bx, cx)) + 1e-4f));
                    int j0 = Math.Max(0, (int)Math.Ceiling(Math.Min(ay, Math.Min(by, cy)) - 1e-4f));
                    int j1 = Math.Min(_heightN - 1, (int)Math.Floor(Math.Max(ay, Math.Max(by, cy)) + 1e-4f));
                    for (int j = j0; j <= j1; j++)
                        for (int i = i0; i <= i1; i++)
                        {
                            float wa = ((by - cy) * (i - cx) + (cx - bx) * (j - cy)) / den;
                            float wb = ((cy - ay) * (i - cx) + (ax - cx) * (j - cy)) / den;
                            float wc = 1 - wa - wb;
                            if (wa < -1e-4f || wb < -1e-4f || wc < -1e-4f)
                                continue;
                            h[j * _heightN + i] = wa * a.Y + wb * b.Y + wc * c.Y;
                        }
                }
            }
            _heights[(ci, cj)] = h;
        }
    }

    // Height is the ground at east e, north n: from the ground map's 1 m
    // heights where it has them (as the verges beside the roads have the
    // ground: the terrain mesh leaves the cells under roads and verges
    // out), else the terrain mesh's; NaN off both.
    public float Height(float e, float n)
    {
        float f = FineHeight(e, n);
        return float.IsNaN(f) ? MeshHeight(e, n) : f;
    }

    // FineHeight is the ground map's heights, bilinear between its cells'
    // middles; NaN where a cell around has none.
    float FineHeight(float e, float n)
    {
        float x = e / _groundCell - 0.5f, y = n / _groundCell - 0.5f;
        int i0 = (int)Math.Floor(x), j0 = (int)Math.Floor(y);
        float u = x - i0, w = y - j0;
        float At(int i, int j)
        {
            int cpc = _groundN;
            int ci = (int)Math.Floor((float)i / cpc), cj = (int)Math.Floor((float)j / cpc);
            if (!_fine.TryGetValue((ci, cj), out var h))
                return float.NaN;
            return h[(j - cj * cpc) * cpc + (i - ci * cpc)];
        }
        float a = At(i0, j0), b = At(i0 + 1, j0), c = At(i0, j0 + 1), d = At(i0 + 1, j0 + 1);
        return (a * (1 - u) + b * u) * (1 - w) + (c * (1 - u) + d * u) * w;
    }

    // MeshHeight is the terrain at east e, north n over its mesh's own
    // triangles ((i, j), (i+1, j), (i, j+1) and (i+1, j), (i+1, j+1),
    // (i, j+1), as the world builder splits each cell); NaN off it.
    float MeshHeight(float e, float n)
    {
        int ci = (int)Math.Floor(e / _chunk), cj = (int)Math.Floor(n / _chunk);
        if (!_heights.TryGetValue((ci, cj), out var h))
            return float.NaN;
        float x = (e - ci * _chunk) / _cell, y = (n - cj * _chunk) / _cell;
        int i = Math.Clamp((int)x, 0, _heightN - 2), j = Math.Clamp((int)y, 0, _heightN - 2);
        float u = x - i, w = y - j;
        float At(int di, int dj) => h[(j + dj) * _heightN + i + di];
        if (u + w <= 1)
            return At(0, 0) + u * (At(1, 0) - At(0, 0)) + w * (At(0, 1) - At(0, 0));
        return At(1, 1) + (1 - u) * (At(0, 1) - At(1, 1)) + (1 - w) * (At(1, 0) - At(1, 1));
    }

    // Ground is the ground map's cell at east e, north n: its land (0:
    // beyond the map) and clearance (0.1 m steps). (Chunk size must be a
    // whole number of cells.)
    public (byte land, byte clearance) Ground(float e, float n)
    {
        int ci = (int)Math.Floor(e / _chunk), cj = (int)Math.Floor(n / _chunk);
        if (!_ground.TryGetValue((ci, cj), out var g))
            return (0, 0);
        int i = (int)((e - ci * _chunk) / _groundCell), j = (int)((n - cj * _chunk) / _groundCell);
        if (i < 0 || i >= _groundN || j < 0 || j >= _groundN)
            return (0, 0);
        int k = j * _groundN + i;
        return (g[k], g[_groundN * _groundN + k]);
    }
}
