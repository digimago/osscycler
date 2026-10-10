using System;
using System.IO;
using System.Text.Json;
using System.Text.Json.Serialization;
using Godot;

namespace Osscycler;

// Manifest is world.json, as osscycler-world writes it (internal/world).
public sealed class Manifest
{
    [JsonPropertyName("format")] public string Format { get; set; } = "";
    [JsonPropertyName("version")] public int Version { get; set; }
    [JsonPropertyName("course")] public CourseInfo Course { get; set; } = new();
    [JsonPropertyName("model")] public string Model { get; set; } = "world.glb";
    [JsonPropertyName("path")] public PathInfo Path { get; set; } = new();
    [JsonPropertyName("attribution")] public string[] Attribution { get; set; } = [];
    // What the world was built from (worlds.Stamp; "preview-…" for a
    // preview), and the course's start: the frame's 0, 0.
    [JsonPropertyName("stamp")] public string Stamp { get; set; } = "";
    [JsonPropertyName("origin")] public OriginInfo Origin { get; set; } = new();

    public sealed class OriginInfo
    {
        [JsonPropertyName("lat")] public double Lat { get; set; }
        [JsonPropertyName("lon")] public double Lon { get; set; }
    }

    public sealed class CourseInfo
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("name")] public string Name { get; set; } = "";
        [JsonPropertyName("distance_m")] public double DistanceM { get; set; }
        [JsonPropertyName("loop")] public bool Loop { get; set; }
    }

    public sealed class PathInfo
    {
        [JsonPropertyName("step_m")] public double StepM { get; set; }
        [JsonPropertyName("xyz")] public double[] Xyz { get; set; } = [];
        // The carriageway's width at each point (worlds from before roads
        // had widths leave it out).
        [JsonPropertyName("width_m")] public double[] WidthM { get; set; } = [];
        // The riding line: metres right of the centre line at each point.
        [JsonPropertyName("lane_m")] public double[] LaneM { get; set; } = [];
    }

    [JsonPropertyName("road")] public RoadInfo Road { get; set; } = new();
    [JsonPropertyName("terrain")] public TerrainInfo Terrain { get; set; } = new();
    [JsonPropertyName("ground")] public GroundInfo Ground { get; set; } = new();

    public sealed class TerrainInfo
    {
        [JsonPropertyName("cell_m")] public double CellM { get; set; }
        [JsonPropertyName("chunk_m")] public double ChunkM { get; set; }
    }

    // The ground map near the route (osscycler-world's ground.bin): what
    // the ground is like in each CellM cell of the listed terrain chunks.
    public sealed class GroundInfo
    {
        [JsonPropertyName("file")] public string File { get; set; } = "";
        [JsonPropertyName("cell_m")] public double CellM { get; set; }
        [JsonPropertyName("layout")] public string Layout { get; set; } = "";
        [JsonPropertyName("classes")] public string[] Classes { get; set; } = [];
        [JsonPropertyName("chunks")] public GroundChunk[] Chunks { get; set; } = [];
    }

    public sealed class GroundChunk
    {
        [JsonPropertyName("chunk")] public int[] Chunk { get; set; } = [];
        [JsonPropertyName("offset")] public int Offset { get; set; }
        [JsonPropertyName("size")] public int Size { get; set; }
    }
    [JsonPropertyName("instances")] public InstancesInfo Instances { get; set; } = new();

    public sealed class InstancesInfo
    {
        [JsonPropertyName("file")] public string File { get; set; } = "";
        [JsonPropertyName("layout")] public string Layout { get; set; } = "";
        [JsonPropertyName("groups")] public Group[] Groups { get; set; } = [];
    }

    public sealed class Group
    {
        [JsonPropertyName("kind")] public string Kind { get; set; } = "";
        [JsonPropertyName("template")] public string Template { get; set; } = "";
        [JsonPropertyName("offset")] public int Offset { get; set; }
        [JsonPropertyName("count")] public int Count { get; set; }
        [JsonPropertyName("draw_m")] public float DrawM { get; set; } = 500;
    }

    public sealed class RoadInfo
    {
        [JsonPropertyName("width_m")] public double WidthM { get; set; } = 5;
        [JsonPropertyName("keep")] public string Keep { get; set; } = "right";
    }
}

// World is a course's world: its model in the scene, and the road's centre
// line for placing the rider by distance along the course.
public sealed class World
{
    public const string Format = "osscycler-world";
    public const int Version = 1;

    public Manifest Manifest { get; }
    public Node3D Scene { get; }
    // Ground is the world's ground map and terrain heights, for growing
    // grass near the rider (null: a world built without a ground map).
    public GroundMap? Ground { get; }
    // TerrainMaterial is the terrain's shader material (the grass hands it
    // the ground map's window, so the ground turns grassy where grass grows).
    public ShaderMaterial TerrainMaterial { get; }
    readonly Vector3[] _path;
    readonly double _step;

    World(Manifest m, Node3D scene, float relief, GroundMap? ground, ShaderMaterial terrain)
    {
        TerrainMaterial = terrain;
        Manifest = m;
        Scene = scene;
        Ground = ground;
        _step = m.Path.StepM;
        _path = new Vector3[m.Path.Xyz.Length / 3];
        for (int i = 0; i < _path.Length; i++)
            _path[i] = new Vector3((float)m.Path.Xyz[3 * i], (float)m.Path.Xyz[3 * i + 1] * relief, (float)m.Path.Xyz[3 * i + 2]);
        scene.Scale = new Vector3(1, relief, 1);
    }

    // Load reads a world directory: world.json, then its model. Relief
    // scales heights (1: as built; more exaggerates hills, as maps and
    // flight views often do; the road and the rider scale with them).
    static bool _quantization; // the glTF extension registered

    public static World Load(string dir, float relief = 1, float bloom = 0)
    {
        var m = JsonSerializer.Deserialize<Manifest>(File.ReadAllText(System.IO.Path.Combine(dir, "world.json")))
            ?? throw new InvalidDataException("world.json is empty");
        if (m.Format != Format || m.Version != Version)
            throw new InvalidDataException($"world.json is {m.Format} {m.Version}, want {Format} {Version}");
        if (m.Path.StepM <= 0 || m.Path.Xyz.Length < 6)
            throw new InvalidDataException("world.json has no path");

        if (!_quantization)
        {
            GltfDocument.RegisterGltfDocumentExtension(new Quantization());
            _quantization = true;
        }
        var doc = new GltfDocument();
        var state = new GltfState();
        var err = doc.AppendFromFile(System.IO.Path.Combine(dir, m.Model), state);
        if (err != Error.Ok)
            throw new IOException($"loading {m.Model}: {err}");
        var scene = (Node3D)doc.GenerateScene(state);
        var terrain = Dress(scene);
        Plant(scene, m, dir, bloom);
        return new World(m, scene, relief, GroundMap.Load(m, dir, scene), terrain);
    }

    // Dress sets up the materials as glTF means them. Godot's runtime loader
    // leaves vertex colours unused (4.7), while in glTF they multiply the
    // base colour: the terrain's land cover is in them. The terrain gets
    // its own shader (shaders/terrain.gdshader: the land cover with detail
    // on top).
    static ShaderMaterial Dress(Node scene)
    {
        var terrain = new ShaderMaterial { Shader = GD.Load<Shader>("res://shaders/terrain.gdshader") };
        foreach (var n in scene.FindChildren("*", "MeshInstance3D", true, false))
        {
            var mi = (MeshInstance3D)n;
            for (int s = 0; s < mi.Mesh.GetSurfaceCount(); s++)
            {
                if (mi.Mesh.SurfaceGetMaterial(s) is not BaseMaterial3D mat)
                    continue;
                if (mat.ResourceName == "terrain")
                {
                    mi.SetSurfaceOverrideMaterial(s, terrain);
                    continue;
                }
                var arrays = mi.Mesh.SurfaceGetArrays(s);
                if (arrays[(int)Mesh.ArrayType.Color].VariantType != Variant.Type.Nil)
                {
                    mat.VertexColorUseAsAlbedo = true;
                    mat.VertexColorIsSrgb = false; // glTF colours are linear
                }
                if (mat.ResourceName == "water")
                {
                    mat.Roughness = 0.08f;
                    mat.MetallicSpecular = 0.8f;
                }
            }
        }
        return terrain;
    }

    // Plant adds the vegetation: a MultiMesh per group of instances, of the
    // group's template (a hidden node of the model), drawn up to the
    // group's distance. Heather goes by its own shader: near the rider the
    // grass shaders grow it (Grass.cs), these plants only beyond, in or out
    // of flower by bloom.
    static void Plant(Node3D scene, Manifest m, string dir, float bloom)
    {
        var heather = new ShaderMaterial { Shader = GD.Load<Shader>("res://shaders/heather_far.gdshader") };
        heather.SetShaderParameter("bloom", bloom);
        var inst = m.Instances;
        if (inst.File == "" || inst.Layout != "x y z scale yaw")
            return;
        var templates = new System.Collections.Generic.Dictionary<string, Mesh>();
        foreach (var n in scene.FindChildren("template *", "MeshInstance3D", true, false))
        {
            var mi = (MeshInstance3D)n;
            templates[mi.Name] = mi.Mesh;
            mi.Visible = false;
        }
        var data = File.ReadAllBytes(System.IO.Path.Combine(dir, inst.File));
        int stride = 5 * 4;
        foreach (var g in inst.Groups)
        {
            if (!templates.TryGetValue(g.Template, out var mesh) || (g.Offset + g.Count) * stride > data.Length)
                continue;
            // The node at its plants' middle: draw distances are measured
            // to a node's position.
            var mid = Vector3.Zero;
            for (int i = 0; i < g.Count; i++)
            {
                int o = (g.Offset + i) * stride;
                mid += new Vector3(BitConverter.ToSingle(data, o), BitConverter.ToSingle(data, o + 4), BitConverter.ToSingle(data, o + 8));
            }
            mid /= g.Count;
            var mm = new MultiMesh { TransformFormat = MultiMesh.TransformFormatEnum.Transform3D, Mesh = mesh, InstanceCount = g.Count };
            for (int i = 0; i < g.Count; i++)
            {
                int o = (g.Offset + i) * stride;
                float x = BitConverter.ToSingle(data, o), y = BitConverter.ToSingle(data, o + 4), z = BitConverter.ToSingle(data, o + 8);
                float sc = BitConverter.ToSingle(data, o + 12), yaw = BitConverter.ToSingle(data, o + 16);
                var basis = new Basis(Vector3.Up, yaw).Scaled(new Vector3(sc, sc, sc));
                mm.SetInstanceTransform(i, new Transform3D(basis, new Vector3(x, y, z) - mid));
            }
            scene.AddChild(new MultiMeshInstance3D
            {
                Multimesh = mm,
                Position = mid,
                Name = $"plants {g.Kind}",
                VisibilityRangeEnd = g.DrawM,
                VisibilityRangeEndMargin = g.DrawM * 0.1f,
                VisibilityRangeFadeMode = GeometryInstance3D.VisibilityRangeFadeModeEnum.Self,
                CastShadow = g.Kind == "heather" || g.Kind.StartsWith("bush ") ? GeometryInstance3D.ShadowCastingSetting.Off : GeometryInstance3D.ShadowCastingSetting.On,
                MaterialOverride = g.Kind == "heather" ? heather : null,
            });
        }
    }

    public double Length => (_path.Length - 1) * _step;

    Osscycler.V1.Course? _profile;

    // Profile is the course as the world's path has it (its own heights,
    // grade over ±30 m), for the HUD's strips when no core lists the
    // course.
    public Osscycler.V1.Course Profile()
    {
        if (_profile != null)
            return _profile;
        var m = Manifest;
        var c = new Osscycler.V1.Course { Id = m.Course.Id, Name = m.Course.Name, DistanceM = m.Course.DistanceM, Loop = m.Course.Loop, ProfileStepM = m.Path.StepM };
        int n = m.Path.Xyz.Length / 3, k = Math.Max(1, (int)Math.Round(30 / m.Path.StepM)); // ±30 m: the core smooths over 60 m
        float Y(int i) => (float)m.Path.Xyz[3 * Math.Clamp(i, 0, n - 1) + 1];
        for (int i = 0; i < n; i++)
        {
            c.ProfileElevationM.Add(Y(i));
            c.ProfileGradePct.Add((Y(i + k) - Y(i - k)) / (float)(2 * k * m.Path.StepM) * 100);
        }
        return _profile = c;
    }

    // InLane is where a rider at distance d rides: on the world's riding
    // line (in the middle of their half, out-in-out through bends), or in
    // worlds without one about 1 m in from their edge. The ride itself
    // follows the centre line: this only moves the view sideways.
    public Vector3 InLane(double d)
    {
        double off;
        var lane = Manifest.Path.LaneM;
        if (lane.Length == _path.Length)
        {
            double f = Wrap(d) / _step;
            int i = Math.Min((int)f, _path.Length - 2);
            off = lane[i] + (lane[i + 1] - lane[i]) * (f - i);
        }
        else
        {
            double width = Manifest.Road.WidthM;
            if (Manifest.Path.WidthM.Length == _path.Length)
                width = Manifest.Path.WidthM[(int)Math.Round(Wrap(d) / _step)];
            off = Math.Max(0, width / 2 - RiderFromEdgeM);
            if (Manifest.Road.Keep == "left")
                off = -off;
        }
        var along = At(d + 1) - At(d - 1);
        var right = new Vector3(-along.Z, 0, along.X).Normalized(); // along × up
        return At(d) + right * (float)off;
    }

    const double RiderFromEdgeM = 1.0;

    // At is the road's centre at distance d along the course: a Catmull-Rom
    // spline through the path points, so the view turns smoothly rather
    // than every 5 m; a loop wraps, a course stops at its ends.
    public Vector3 At(double d)
    {
        double f = Wrap(d) / _step;
        int i = Math.Min((int)f, _path.Length - 2);
        float t = (float)(f - i);
        Vector3 p0 = Point(i - 1), p1 = _path[i], p2 = _path[i + 1], p3 = Point(i + 2);
        return 0.5f * (2 * p1 + (p2 - p0) * t + (2 * p0 - 5 * p1 + 4 * p2 - p3) * t * t + (3 * p1 - p0 - 3 * p2 + p3) * t * t * t);
    }

    double Wrap(double d)
    {
        if (Manifest.Course.Loop && Length > 0)
            d = ((d % Length) + Length) % Length;
        return Math.Clamp(d, 0, Length);
    }

    // Point is path point i, on a loop round the line, else held at the ends.
    Vector3 Point(int i)
    {
        int n = _path.Length;
        if (Manifest.Course.Loop)
            return _path[((i % (n - 1)) + (n - 1)) % (n - 1)]; // the last point is the first
        return _path[Math.Clamp(i, 0, n - 1)];
    }
}
