using System;
using System.IO;
using System.Linq;
using System.Text.Json.Nodes;
using Godot;

namespace Osscycler;

// The control socket's commands (ControlSocket, --control PORT), served on
// the main thread between frames, one at a time: a command that needs
// frames (to settle, to measure) holds the others back until it is done.
//
//	state                          where the view is, the world, the view's settings
//	goto  m? look_deg? speed_kmh? settle?   move along the course (with a core:
//	                               the view, as ctrl+arrows do), turn, set the speed
//	shot  path m? look_deg? settle? width?  a screenshot after settle frames
//	                               (.jpg or .png by the path), with what goto takes
//	stats frames?                  the frame measured over frames (as --stats)
//	keys  keys: [..]               keys pressed, named as the head names them
//	load  world?                   the world in that directory, or the one shown
//	                               again (after a rebuild), the view kept where it is
//	hide  parts: [..]              --hide's parts left out ([]: all shown)
//	mark                           a mark, as space in debug mode
//	quit
public partial class Main
{
    ControlSocket? _control;
    ControlSocket.Request? _job; // a command waiting on frames
    int _jobFrames;
    Action<ControlSocket.Request>? _jobDone;
    int _quitIn;

    void ControlArgs(Args args)
    {
        if (!args.Has("control"))
            return;
        try
        {
            _control = new ControlSocket((int)args.Number("control", 0));
        }
        catch (Exception e)
        {
            GD.PushError($"osscycler: no control socket: {e.Message}");
            return;
        }
        GD.Print($"osscycler: control socket on 127.0.0.1:{_control.Port}");
        // Frames can be measured on request.
        _stats = true;
        RenderingServer.ViewportSetMeasureRenderTime(GetViewport().GetViewportRid(), true);
    }

    // ServeControl serves what the control socket has waiting, at the end
    // of a frame.
    void ServeControl()
    {
        if (_control == null)
            return;
        if (_quitIn > 0)
        {
            if (--_quitIn == 0) // the reply is on its way
                GetTree().Quit();
            return;
        }
        if (_job != null)
        {
            if (--_jobFrames > 0)
                return;
            var (j, done) = (_job, _jobDone!);
            (_job, _jobDone) = (null, null);
            Try(j, done);
        }
        while (_job == null && _quitIn == 0 && _control.TryNext(out var r))
            Try(r, Serve);
    }

    static void Try(ControlSocket.Request r, Action<ControlSocket.Request> f)
    {
        try
        {
            f(r);
        }
        catch (Exception e)
        {
            r.Fail($"{r.Cmd}: {e.Message}");
        }
    }

    // After answers r with done once frames frames have been drawn.
    void After(ControlSocket.Request r, int frames, Action<ControlSocket.Request> done) =>
        (_job, _jobFrames, _jobDone) = (r, Math.Max(1, frames), done);

    void Serve(ControlSocket.Request r)
    {
        switch (r.Cmd)
        {
            case "state":
                r.Ok(State());
                return;
            case "goto":
                if (Move(r))
                    After(r, (int)(r.Num("settle") ?? 2), x => x.Ok(State()));
                return;
            case "shot":
                string path = r.Str("path");
                if (!Path.IsPathRooted(path))
                {
                    r.Fail("shot: \"path\" must be an absolute file name (.png or .jpg)");
                    return;
                }
                if (Move(r))
                    After(r, (int)(r.Num("settle") ?? 10), x => Shoot(x, path, r.Num("width")));
                return;
            case "stats":
                int frames = (int)Math.Clamp(r.Num("frames") ?? StatFrames, 1, 600);
                ResetStats();
                _measureLeft = frames;
                After(r, frames, x => x.Ok(StatsReply()));
                return;
            case "keys":
                foreach (var k in r.Strs("keys"))
                    if (k != "")
                        Press(k);
                After(r, 2, x => x.Ok(State()));
                return;
            case "load":
                string dir = r.Str("world", _shownDir);
                if (dir == "" || !Show(dir))
                {
                    r.Fail($"load: no world in {(dir == "" ? "(none shown)" : dir)}: {_missing}");
                    return;
                }
                After(r, 5, x => x.Ok(State()));
                return;
            case "hide":
                _hide = r.Strs("parts").Where(p => p != "").ToArray();
                ShowParts();
                if (_world != null)
                    HideParts(_world);
                r.Ok(State());
                return;
            case "mark":
                Mark();
                r.Ok(new JsonObject { ["marks"] = _marksDir });
                return;
            case "quit":
                r.Ok();
                _quitIn = 10;
                return;
            default:
                r.Fail($"unknown command \"{r.Cmd}\" (state, goto, shot, stats, keys, load, hide, mark, quit)");
                return;
        }
    }

    // Move takes goto's and shot's m, look_deg and speed_kmh; false (with
    // r answered) if it can't.
    bool Move(ControlSocket.Request r)
    {
        if (r.Num("look_deg") is double look)
            _look = Mathf.DegToRad((float)look);
        if (r.Num("speed_kmh") is double kmh)
        {
            if (_core != null)
            {
                r.Fail($"{r.Cmd}: the core sets the speed while following a ride");
                return false;
            }
            _speed = Math.Max(0, kmh) / 3.6;
        }
        if (r.Num("m") is double m)
        {
            if (_world is not { } w)
            {
                r.Fail($"{r.Cmd}: no world shown");
                return false;
            }
            m = w.Manifest.Course.Loop && w.Length > 0 ? ((m % w.Length) + w.Length) % w.Length : Math.Clamp(m, 0, w.Length);
            if (_core == null)
                _distance = m;
            else
                _viewOffset = m - _distance; // the ride goes on where it is
            _gaze = Vector3.Zero; // snap there rather than swing round
        }
        return true;
    }

    void Shoot(ControlSocket.Request r, string path, double? width)
    {
        var img = GetViewport().GetTexture().GetImage();
        if (width is double wd && wd >= 16 && wd < img.GetWidth())
            img.Resize((int)wd, (int)Math.Round(img.GetHeight() * wd / img.GetWidth()), Image.Interpolation.Lanczos);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        var ext = Path.GetExtension(path).ToLowerInvariant();
        var err = ext is ".jpg" or ".jpeg" ? img.SaveJpg(path, 0.85f) : img.SavePng(path);
        if (err != Error.Ok)
        {
            r.Fail($"shot: {path}: {err}");
            return;
        }
        var o = State();
        o["path"] = path;
        o["width"] = img.GetWidth();
        o["height"] = img.GetHeight();
        r.Ok(o);
    }

    // State is where the view is and what it shows.
    JsonObject State()
    {
        static double R(double v, int digits) => Math.Round(v, digits);
        static JsonArray V(Vector3 v, int digits) => new(R(v.X, digits), R(v.Y, digits), R(v.Z, digits));
        var o = new JsonObject
        {
            ["source"] = _core != null ? "core" : "fixed",
            ["view"] = _head.Chase(_core?.Latest().State) ? "chase" : "eyes",
            ["look_deg"] = R(Mathf.RadToDeg(_look), 1),
            ["speed_kmh"] = R(_speed * 3.6, 1),
            ["fps"] = Engine.GetFramesPerSecond(),
            ["seed"] = _seed,
            ["hidden"] = new JsonArray(_hide.Select(h => (JsonNode?)JsonValue.Create(h)).ToArray()),
        };
        if (_ride != null)
            o["ride_course_id"] = _ride.CourseId;
        if (_world is not { } w)
        {
            o["world"] = null;
            o["missing"] = _missing;
            return o;
        }
        var m = w.Manifest;
        var s = Spot(w);
        o["world"] = _shownDir;
        o["stamp"] = m.Stamp;
        o["course_id"] = m.Course.Id;
        o["course_name"] = m.Course.Name;
        o["length_m"] = R(w.Length, 1);
        o["loop"] = m.Course.Loop;
        o["distance_m"] = R(_distance, 2);
        o["along_m"] = R(s.Along, 2);
        o["view_offset_m"] = R(s.At - _distance, 1);
        o["rider"] = V(s.Rider, 3);
        o["lat"] = R(s.Lat, 7);
        o["lon"] = R(s.Lon, 7);
        o["camera"] = V(s.Cam, 3);
        o["camera_forward"] = V(s.Fwd, 4);
        return o;
    }

    // Spot is the spot looked at (the rider's, or a debug view ahead or
    // behind) in the world's frame (heights back from --relief): its
    // distance, along the course, the rider on the riding line there, its
    // lat/lon, and the camera.
    (double At, double Along, Vector3 Rider, double Lat, double Lon, Vector3 Cam, Vector3 Fwd) Spot(World w)
    {
        var m = w.Manifest;
        Vector3 Frame(Vector3 v) => v with { Y = v.Y / _relief };
        double at = Viewed;
        var rider = Frame(w.InLane(at));
        var cam = Frame(_camera.GlobalPosition);
        var fwd = -_camera.GlobalTransform.Basis.Z;
        fwd = new Vector3(fwd.X, fwd.Y / _relief, fwd.Z).Normalized();
        var (lat, lon) = Head.Marks.LatLon(m.Origin.Lat, m.Origin.Lon, rider.X, rider.Z);
        double along = m.Course.Loop && w.Length > 0 ? ((at % w.Length) + w.Length) % w.Length : Math.Clamp(at, 0, w.Length);
        return (at, along, rider, lat, lon, cam, fwd);
    }
}
