using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using Godot;

namespace Osscycler;

// Measuring the frame (for finding what costs it): --stats prints, over the
// last StatFrames frames before a --shot, the frame's CPU and GPU time,
// draw calls, triangles and objects, and the world's nodes by kind (the
// first word of their name) with their triangles; --hide a,b leaves out
// the world's nodes whose names start with a or b ("plants bush",
// "terrain"), and "grass" (the grass grown near the rider) or "shadows"
// (the sun's).
public partial class Main
{
    const int StatFrames = 60;
    bool _stats;
    string[] _hide = [];
    double _cpuMs, _gpuMs, _draws, _prims, _objects;
    int _measured;
    readonly System.Diagnostics.Stopwatch _lap = new();
    readonly Dictionary<string, double> _laps = new();

    // Lap adds the time since the last lap to section name ("" starts the
    // frame), while measuring.
    void Lap(string name)
    {
        if (!_stats)
            return;
        if (name != "" && _shot != null && _frames >= _shotFrames - StatFrames)
            _laps[name] = _laps.GetValueOrDefault(name) + _lap.Elapsed.TotalMilliseconds;
        _lap.Restart();
    }

    void StatsArgs(Args args)
    {
        _stats = args.Has("stats");
        _hide = args.Get("hide", "").Split(',', System.StringSplitOptions.RemoveEmptyEntries | System.StringSplitOptions.TrimEntries);
        if (_stats)
            RenderingServer.ViewportSetMeasureRenderTime(GetViewport().GetViewportRid(), true);
    }

    // HideParts hides what --hide names in a world just added.
    void HideParts(World w)
    {
        foreach (var h in _hide)
        {
            if (h == "grass" && _grass != null)
                _grass.Node.Visible = false;
            else if (h == "shadows")
                foreach (var l in FindChildren("*", "DirectionalLight3D", true, false))
                    ((DirectionalLight3D)l).ShadowEnabled = false;
            else
                foreach (var n in w.Scene.GetChildren())
                    if (n is Node3D n3 && n.Name.ToString().StartsWith(h))
                        n3.Visible = false;
        }
    }

    // MeasureFrame counts this frame when it is among the last StatFrames
    // before the shot.
    void MeasureFrame(double delta)
    {
        if (!_stats || _shot == null || _frames < _shotFrames - StatFrames)
            return;
        var vp = GetViewport().GetViewportRid();
        _cpuMs += RenderingServer.ViewportGetMeasuredRenderTimeCpu(vp);
        _gpuMs += RenderingServer.ViewportGetMeasuredRenderTimeGpu(vp);
        _draws += RenderingServer.GetRenderingInfo(RenderingServer.RenderingInfo.TotalDrawCallsInFrame);
        _prims += RenderingServer.GetRenderingInfo(RenderingServer.RenderingInfo.TotalPrimitivesInFrame);
        _objects += RenderingServer.GetRenderingInfo(RenderingServer.RenderingInfo.TotalObjectsInFrame);
        _measured++;
    }

    void PrintStats()
    {
        if (!_stats || _measured == 0)
            return;
        var c = CultureInfo.InvariantCulture;
        double n = _measured;
        GD.Print(string.Format(c, "stats: {0:F1} fps, process {1:F2} ms, render cpu {2:F2} ms, gpu {3:F2} ms, {4:F0} draw calls, {5:F2} M triangles, {6:F0} objects",
            Engine.GetFramesPerSecond(), Performance.GetMonitor(Performance.Monitor.TimeProcess) * 1000,
            _cpuMs / n, _gpuMs / n, _draws / n, _prims / n / 1e6, _objects / n));
        GD.Print("stats: _Process by section, ms: " + string.Join(", ", _laps.Select(kv => string.Format(c, "{0} {1:F2}", kv.Key, kv.Value / n))));
        if (_world == null)
            return;
        var kinds = new Dictionary<string, (int nodes, long tris, long inst)>();
        foreach (var node in _world.Scene.FindChildren("*", "GeometryInstance3D", true, false))
        {
            if (node is not GeometryInstance3D g || !g.IsVisibleInTree())
                continue;
            var name = node.Name.ToString();
            var kind = g is MultiMeshInstance3D mmi ? "plants " + (mmi.Multimesh?.Mesh?.ResourceName ?? "?") : name.Split(' ')[0];
            long tris = 0, inst = 1;
            Mesh? mesh = g switch { MeshInstance3D mi => mi.Mesh, MultiMeshInstance3D mm => mm.Multimesh?.Mesh, _ => null };
            if (g is MultiMeshInstance3D m3 && m3.Multimesh != null)
                inst = m3.Multimesh.InstanceCount;
            if (mesh != null)
                for (int s = 0; s < mesh.GetSurfaceCount(); s++)
                {
                    var a = mesh.SurfaceGetArrays(s);
                    var idx = a[(int)Mesh.ArrayType.Index];
                    tris += idx.VariantType != Variant.Type.Nil ? idx.AsInt32Array().Length / 3 : a[(int)Mesh.ArrayType.Vertex].AsVector3Array().Length / 3;
                }
            var k = kinds.GetValueOrDefault(kind);
            kinds[kind] = (k.nodes + 1, k.tris + tris * inst, k.inst + inst);
        }
        foreach (var (kind, v) in kinds.OrderByDescending(kv => kv.Value.tris).Take(25))
            GD.Print(string.Format(c, "stats: {0,-28} {1,5} nodes {2,9} instances {3,8:F2} M triangles (all, not just drawn)", kind, v.nodes, v.inst, v.tris / 1e6));
    }
}
