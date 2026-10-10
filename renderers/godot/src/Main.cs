using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using Godot;
using Osscycler.V1;

namespace Osscycler;

// Main shows a course's world from the rider's eyes, moving along the road
// by distance: following a core's ride (--core), or at a fixed speed.
//
// Arguments, after "--" on Godot's command line:
//
//	--core ADDR       follow the ride on the core at ADDR (e.g. 127.0.0.1:7420);
//	                  the world is <worlds>/<course id>
//	--worlds DIR      where the worlds are (default ~/osscycler/worlds)
//	--token-file F    the API token (default $OSSCYCLER_API_TOKEN, then ~/osscycler/api-token)
//	--world DIR       without --core: the world to ride (default <worlds>/posbank)
//	--from M          without --core: start this far along the course
//	--speed KMH       without --core: riding speed (default 25)
//	--shot FILE       save a PNG of the view after --frames frames, then quit
//	--frames N        (default 90: shadows and fog settle first)
//	--trace FILE      write the time and distance shown every frame (for checking smoothness)
//	--fov DEG         horizontal field of view (default 70, as the TUI's road view)
//	--relief K        scale the world's heights (default 1)
//	--seed N          the ride's light and haze, and the heather's flowering
//	                  by its day of the year, N % 1000 (default: today's)
//	--look DEG        turn the view this far to the right (for checking what lies beside the road)
//	--ghost M         without --core: a ghost this far ahead at the same speed (for checks)
//	--layout FILE     the HUD's arrangement and the view (default ~/osscycler/hud.json; o, z and v change it)
//	--keys K@N,...    press these keys (named as the head names them) at these frames (for checks)
//	--debug           space marks the spot as flawed (a line in marks.jsonl and a screenshot), for analysis;
//	                  ctrl+→ / ctrl+← look 100 m further or back along the course (shift: 1 km), ctrl+↓ back to the rider
//	--marks DIR       where the marks go (default ~/osscycler/marks)
public partial class Main : Node3D
{
    const float EyeM = 1.6f;      // a rider's eyes above the road
    const double HeadingM = 6;    // the view faces the riding line this far ahead
    const float SteerPerS = 10f;  // how fast the view follows it (1/s)
    const double PitchSpanM = 30; // the grade the view tilts with: over this far ahead
    const float PitchFollow = 0.15f; // how much of it (the TUI's road view: 0.15)
    // On the level the eyes look slightly down, at the road about 25 m ahead.
    static readonly float LookDown = Mathf.Atan(0.2f * EyeM / 25);
    const double BendSpanM = 5;   // the bend's curvature over this far either side
    const float HeadLean = 0.3f;  // the part of the bike's lean the head takes
    const double PosBlendS = 0.4; // a correction from the core blends out this fast

    World? _world;
    float _look; // radians to the right of the way ahead
    string _worldsDir = "";
    Camera3D _camera = null!;
    Hud.HudView _hud = null!;
    Head.Controller _head = null!;
    Vector3 _gaze;
    float _pitch, _roll;
    Grass? _grass;
    float _bloom; // the heather in flower, 0 to 1
    string? _shot;
    int _shotFrames = 90, _frames;
    StreamWriter? _trace;
    readonly List<(int Frame, string Key)> _keys = new();
    float _relief = 1;
    int _seed;
    bool _debug;          // --debug: space marks the spot (Mark)
    string _marksDir = "";

    // Fixed-speed riding.
    double _distance, _speed;
    // The view's offset along the course from the rider (debug: ctrl+arrows
    // with a core, whose ride stays where it is).
    double _viewOffset;
    const double JumpM = 100, LongJumpM = 1000;

    // Viewed is where the view is: the rider, or the debug offset from it,
    // kept on the course.
    double Viewed
    {
        get
        {
            double d = _distance + _viewOffset;
            if (_world is { } w && !w.Manifest.Course.Loop)
                d = Math.Clamp(d, 0, w.Length);
            return d;
        }
    }

    // The cyclists (plan step 9): the rider, seen in the chase view, and
    // the ghost; its position as last reported, when, and its speed
    // estimated from the reports (RideGhost has none), to carry it forward
    // between them as the rider is.
    Avatar.Cyclist? _me, _ghostRider;
    float _riderHeight;
    readonly Head.GhostTrack _ghost = new();
    double _ghostSpeed;
    double _ghostAhead = double.NaN; // --ghost
    const float ChaseBackM = 4.2f, ChaseUpM = 1.9f, ChaseAimM = 7;
    const float GhostAlpha = 0.5f;

    // Following a core: the ride as last reported, when that was, and the
    // difference to what was shown then, blended out.
    Core? _core;
    Ride? _ride;
    double _reportedAt, _posErr, _clock;
    string _missing = "";
    WorldSource? _source; // the ride's world: here, or built here (a preview first)
    string _worldStatus = "";
    string _shownDir = ""; // the directory of the world shown

    // A world loaded in the background (a 98 km world takes about 9 s:
    // in the frame loop it would stop the view that long), swapped in
    // behind a fade through the haze; the preview gives way to the world
    // this way mid-ride, the rider kept by distance.
    System.Threading.Tasks.Task<World>? _loading;
    string _loadingDir = "";
    World? _swapIn;
    double _fadeAt = -1;
    ColorRect _fade = null!;
    // The cover while the ride's course has no world of its own on screen
    // (Head/Cover.cs): its opacity and what it says; and the swap's fade.
    double _coverA, _swapA;
    Label _coverText = null!;
    const double FadeInS = 0.35, FadeOutS = 0.6;

    public override void _Ready()
    {
        var args = Args.Parse(OS.GetCmdlineUserArgs());
        _worldsDir = args.File("worlds", Path.Combine(Core.Home(), "worlds"));
        _shot = args.File("shot", "") is { Length: > 0 } s ? s : null;
        _shotFrames = (int)args.Number("frames", _shotFrames);
        StatsArgs(args);
        // --keys "?@60,x@120": keys pressed at those frames (for checks).
        foreach (var part in args.Get("keys", "").Split(',', StringSplitOptions.RemoveEmptyEntries))
        {
            int at = part.LastIndexOf('@');
            if (at > 0 && int.TryParse(part[(at + 1)..], out int frame))
                _keys.Add((frame, part[..at]));
        }
        if (args.File("trace", "") is { Length: > 0 } t)
            _trace = new StreamWriter(t);

        int seed = (int)args.Number("seed", DateTime.Today.DayOfYear + 1000 * DateTime.Today.Year);
        _seed = seed;
        _debug = args.Has("debug");
        _marksDir = args.File("marks", Path.Combine(Core.Home(), "marks"));
        _bloom = Grass.Bloom(seed % 1000); // the seed's day of the year
        var weather = Weather.From(seed);
        AddChild(Sky(weather));
        AddChild(Sun(weather));
        // Godot's field of view is vertical by default: 70° made about 102°
        // across on a wide screen, which looked flat and far away.
        _camera = new Camera3D
        {
            KeepAspect = Camera3D.KeepAspectEnum.Width,
            Fov = (float)args.Number("fov", 70),
            Near = 0.1f,
            Far = 4000,
            Current = true,
        };
        _relief = (float)Math.Max(0.1, args.Number("relief", 1));
        if (args.Get("ghost", "") != "")
            _ghostAhead = args.Number("ghost", 10);
        _look = Mathf.DegToRad((float)args.Number("look", 0));
        AddChild(_camera);
        // The fade under the HUD: the haze's own colour, so a swap reads as
        // riding through a bank of mist.
        var fadeLayer = new CanvasLayer { Layer = 5 };
        _fade = new ColorRect { Color = new Color(0.80f, 0.84f, 0.88f, 0), MouseFilter = Control.MouseFilterEnum.Ignore };
        _fade.SetAnchorsPreset(Control.LayoutPreset.FullRect);
        fadeLayer.AddChild(_fade);
        _coverText = new Label
        {
            HorizontalAlignment = HorizontalAlignment.Center,
            VerticalAlignment = VerticalAlignment.Center,
            MouseFilter = Control.MouseFilterEnum.Ignore,
            Modulate = new Color(1, 1, 1, 0),
        };
        _coverText.SetAnchorsPreset(Control.LayoutPreset.FullRect);
        _coverText.AddThemeFontOverride("font", Hud.Fonts.Hud(600));
        _coverText.AddThemeColorOverride("font_color", new Color(0.2f, 0.26f, 0.34f));
        fadeLayer.AddChild(_coverText);
        AddChild(fadeLayer);
        _hud = new Hud.HudView(GetViewport());
        AddChild(_hud.Node);

        var core = args.Get("core", "");
        // The HUD's arrangement, kept on this machine (as the TUI's tui.json).
        var layout = args.File("layout", Path.Combine(Core.Home(), "hud.json"));
        // A head unless scripted (--shot, --keys: checks): a screen the
        // rider watches, in debug mode too (owner, 2026-10-10: a ride in the
        // debug view, the only screen, was paused as unwatched after 30 s).
        bool watcher = _shot != null || _keys.Count > 0;
        _head = new Head.Controller(new Head.NoCore(), layout) { Watcher = watcher };
        if (core != "")
        {
            try
            {
                _core = new Core(core, Core.Token(args.File("token-file", "")), head: !watcher);
                _source = new WorldSource(_core, _worldsDir);
                _head = new Head.Controller(_core, layout) { Watcher = watcher };
                if (_debug)
                    _head.Note($"debug: space marks a flawed spot ({_marksDir})");
            }
            catch (Exception e)
            {
                Fail($"osscycler: can't follow the core: {e.Message}");
            }
            return;
        }
        if (_debug)
            _head.Note($"debug: space marks a flawed spot ({_marksDir})");
        _distance = args.Number("from", 0);
        _speed = args.Number("speed", 25) / 3.6;
        var dir = args.File("world", Path.Combine(_worldsDir, "posbank"));
        if (!Show(dir))
            Fail($"osscycler: no world in {dir}: {_missing}");
    }

    public override void _ExitTree()
    {
        _source?.Stop(); // a world being built for this screen: no one to load it now
        _core?.Dispose();
        _trace?.Dispose();
    }

    void Fail(string msg)
    {
        GD.PushError(msg);
        GetTree().Quit(1);
    }

    // Show loads the world in dir in place of the one shown; false (with
    // _missing saying why) if there is none.
    bool Show(string dir)
    {
        World w;
        try
        {
            w = World.Load(dir, _relief, _bloom);
        }
        catch (Exception e)
        {
            _missing = e.Message;
            return false;
        }
        Use(w, dir);
        return true;
    }

    // LoadInBackground starts loading the world in dir, to swap in when
    // it is there (Swap).
    void LoadInBackground(string dir)
    {
        _loadingDir = dir;
        float relief = _relief, bloom = _bloom;
        _loading = System.Threading.Tasks.Task.Run(() => World.Load(dir, relief, bloom));
    }

    // Swap takes a world loaded in the background once it is there, and
    // fades it in in place of the one shown; a world loaded for a course
    // no longer ridden is dropped.
    void Swap()
    {
        if (_loading is { IsCompleted: true } t)
        {
            string dir = _loadingDir;
            (_loading, _loadingDir) = (null, "");
            if (!t.IsCompletedSuccessfully)
            {
                GD.PushWarning($"osscycler: the world in {dir} didn't load: {t.Exception?.GetBaseException().Message}");
                _missing = dir;
            }
            else if (t.Result.Manifest.Course.Id != _ride?.CourseId)
                t.Result.Scene.Free();
            else if (_world == null || _coverA >= 1)
                Use(t.Result, dir); // nothing to fade from, or under the cover
            else
            {
                _swapIn = t.Result;
                _swapDir = dir;
                _fadeAt = _clock;
            }
        }
        if (_fadeAt < 0)
        {
            _swapA = 0;
            return;
        }
        double f = _clock - _fadeAt;
        if (f >= FadeInS && _swapIn != null)
        {
            Use(_swapIn, _swapDir);
            _swapIn = null;
        }
        float a = f < FadeInS ? (float)(f / FadeInS) : (float)Math.Max(0, 1 - (f - FadeInS) / FadeOutS);
        _swapA = a;
        if (f >= FadeInS + FadeOutS)
            _fadeAt = -1;
    }

    string _swapDir = "";

    // Covered: a ride is on whose course has no world of its own on screen.
    bool Covered => _core != null && Head.Cover.Needed(_ride?.CourseId ?? "", _world?.Manifest.Course.Id ?? "");

    // CoverUp raises the cover while Covered (saying what is loading) and
    // lifts it after; the swap's fade shows through it.
    void CoverUp(double delta)
    {
        bool up = Covered;
        _coverA = Head.Cover.Step(_coverA, up, delta);
        _fade.Color = _fade.Color with { A = (float)Math.Max(_coverA, _swapA) };
        _coverText.Modulate = new Color(1, 1, 1, (float)_coverA);
        if (up && _ride != null)
        {
            _coverText.Text = Head.Cover.Text(_ride.CourseName, _worldStatus);
            _coverText.AddThemeFontSizeOverride("font_size", (int)(44 * GetViewport().GetVisibleRect().Size.Y / 1080));
        }
    }

    // Use shows world w (from dir) in place of the one shown.
    void Use(World w, string dir)
    {
        GD.Print($"osscycler: showing the world in {dir}");
        _shownDir = dir;
        if (_world != null)
        {
            RemoveChild(_world.Scene);
            _world.Scene.QueueFree();
            if (_grass != null)
            {
                RemoveChild(_grass.Node);
                _grass.Node.QueueFree();
                _grass = null;
            }
        }
        _world = w;
        AddChild(w.Scene);
        // Grass beside the scene, not in it: the scene is scaled by the
        // relief, the grass shouldn't be.
        if (w.Ground != null)
        {
            _grass = new Grass(w.Ground, _relief, w.TerrainMaterial, _bloom);
            AddChild(_grass.Node);
        }
        HideParts(w);
        _missing = "";
        _gaze = Vector3.Zero;
    }

    public override void _Process(double delta)
    {
        _clock += delta;
        Lap("");
        if (_head.Quit) // set after calls to the core (ending the ride) finish
        {
            GetTree().Quit();
            return;
        }
        Swap();
        Lap("swap");
        if (_core != null)
        {
            Follow();
            if (_ride == null && _world != null)
            {
                // Free riding (no course ride): the rider rides the world
                // shown at the trainer's speed (owner, 2026-10-10: the rider
                // stood still while pedalling), round it if it's a loop.
                var tr = _core.Latest().State?.Trainer;
                _speed = tr != null && tr.HasSpeedMps ? tr.SpeedMps : 0;
                _distance += _speed * delta;
                _distance = _world.Manifest.Course.Loop ? _distance % _world.Length : Math.Min(_distance, _world.Length);
            }
        }
        else if (_world != null)
        {
            _distance += _speed * delta;
            if (!_world.Manifest.Course.Loop)
                _distance = Math.Min(_distance, _world.Length);
        }
        Lap("follow");
        if (_world != null)
            Place(delta);
        Lap("place");
        Cyclists(delta);
        Lap("cyclists");
        CoverUp(delta);
        Lap("cover");
        var now = _core?.Latest().State;
        _head.Started(now);
        _head.Tick(now, _clock);
        Lap("head");
        _hud.Arrange(_head.Layout);
        _hud.Head(_head.Asking(_clock), _head.Notice(_clock), _head.Help, _head.List(), _head.Prompt, _head.Dialog(now, _clock), _head.Form(now));
        if (_core != null)
        {
            var st = _core.Latest().State;
            var c = _ride != null ? _core.Course(_ride.CourseId) : null;
            var wk = st?.Workout is { } p && p.Id != "" ? _core.Workout(p.Id) : null;
            // Riding a preview: what the world's build is doing.
            string note = _ride != null && _world?.Manifest.Course.Id == _ride.CourseId ? _worldStatus : "";
            if (Math.Abs(Viewed - _distance) > 0.5) // a debug view ahead or behind the rider
                note = string.Format(CultureInfo.InvariantCulture, "view {0:+0;-0} m, at {1:F2} km · ctrl+↓ back", Viewed - _distance, Viewed / 1000)
                    + (note != "" ? "   " + note : "");
            _hud.Update(st, _clock, Status(), c, _distance, _ride?.Ghost?.DistanceM ?? -1, wk, note);
        }
        else
            _hud.Update(Riding(), _clock, Status(), _world?.Profile(), _distance);
        Lap("hud");
        _trace?.WriteLine(string.Format(CultureInfo.InvariantCulture, "{0:F4} {1:F3}", _clock, _distance));

        foreach (var (frame, key) in _keys)
            if (frame == _frames)
                Press(key);
        MeasureFrame(delta);
        if (_shot != null && ++_frames == _shotFrames)
        {
            PrintStats();
            var err = GetViewport().GetTexture().GetImage().SavePng(_shot);
            GD.Print($"osscycler: view at {_distance:F0} m saved to {_shot}: {err}");
            GetTree().Quit();
        }
    }

    // Keys go to the head (Head.Controller), named as the TUI names them.
    public override void _UnhandledKeyInput(InputEvent e)
    {
        if (e is not InputEventKey { Pressed: true, Echo: false } k)
            return;
        string key = KeyName(k);
        if (key == "")
            return;
        Press(key);
        if (_head.Quit)
            GetTree().Quit();
    }

    // Press hands a key to the head, but for the renderer's own: in debug
    // mode, space while nothing that takes keys is open marks the spot,
    // unless paused: then it carries on, as the pause banner says.
    void Press(string key)
    {
        if (_debug && key is " " or "space" && _head.Free && _core?.Latest().State?.Paused != true) // "space": as --keys can carry it
        {
            Mark();
            return;
        }
        if (_debug && Jump(key))
            return;
        _head.Key(key, _core?.Latest().State ?? Riding(), _clock);
    }

    // Jump moves the view along the course in debug mode: with a core the
    // camera only (the ride stays where it is), without one the fixed-speed
    // ride itself. Whether the key was one of its own.
    bool Jump(string key)
    {
        double by = key switch
        {
            "ctrl+right" => JumpM,
            "ctrl+left" => -JumpM,
            "ctrl+shift+right" => LongJumpM,
            "ctrl+shift+left" => -LongJumpM,
            "ctrl+down" => double.NaN, // back to the rider
            _ => 0,
        };
        if (by == 0)
            return false;
        if (_world == null)
            return true;
        if (_core == null)
        {
            if (double.IsNaN(by))
                return true;
            _distance += by;
            if (!_world.Manifest.Course.Loop)
                _distance = Math.Clamp(_distance, 0, _world.Length);
        }
        else
        {
            _viewOffset = double.IsNaN(by) ? 0 : Viewed + by - _distance;
            if (!_world.Manifest.Course.Loop)
                _viewOffset = Math.Clamp(_distance + _viewOffset, 0, _world.Length) - _distance;
        }
        _gaze = Vector3.Zero; // the view snaps to the new place rather than swinging round
        _head.Note(_viewOffset == 0
            ? string.Format(CultureInfo.InvariantCulture, "at {0:F2} km", Viewed / 1000)
            : string.Format(CultureInfo.InvariantCulture, "view {0:+0;-0} m: {1:F2} km (ctrl+↓ back)", _viewOffset, Viewed / 1000));
        return true;
    }

    // Mark records where the rider is as a flawed spot (Head.Marks): the
    // world, the distance, the rider's and the camera's place, and a
    // screenshot of what is on the screen.
    void Mark()
    {
        if (Covered)
        {
            _head.Note("debug: the world shown isn't this ride's: nothing to mark");
            return;
        }
        if (_world == null)
        {
            _head.Note("debug: no world shown to mark");
            return;
        }
        var w = _world;
        var m = w.Manifest;
        var now = DateTimeOffset.Now;
        var name = Head.Marks.Name(now);
        string shot = name + ".png";
        try
        {
            Directory.CreateDirectory(_marksDir);
            var err = GetViewport().GetTexture().GetImage().SavePng(Path.Combine(_marksDir, shot));
            if (err != Error.Ok)
                shot = "";
        }
        catch (Exception)
        {
            shot = "";
        }
        // Heights back to the world's frame: the scene is scaled by --relief.
        Vector3 Frame(Vector3 v) => v with { Y = v.Y / _relief };
        double at = Viewed; // the spot looked at (the rider's, or a debug view ahead or behind)
        var rider = Frame(w.InLane(at));
        var cam = Frame(_camera.GlobalPosition);
        var fwd = -_camera.GlobalTransform.Basis.Z;
        fwd = new Vector3(fwd.X, fwd.Y / _relief, fwd.Z).Normalized();
        var (lat, lon) = Head.Marks.LatLon(m.Origin.Lat, m.Origin.Lon, rider.X, rider.Z);
        double along = m.Course.Loop && w.Length > 0 ? ((at % w.Length) + w.Length) % w.Length : Math.Clamp(at, 0, w.Length);
        double R(double v, int digits) => Math.Round(v, digits);
        var mark = new Head.Mark
        {
            Time = now.ToString("o", CultureInfo.InvariantCulture),
            CourseId = m.Course.Id,
            CourseName = m.Course.Name,
            World = _shownDir,
            Stamp = m.Stamp,
            Preview = m.Stamp.StartsWith("preview-", StringComparison.Ordinal),
            DistanceM = R(_distance, 2),
            ViewOffsetM = R(at - _distance, 1),
            AlongM = R(along, 2),
            SpeedMps = R(_speed, 2),
            Source = _core != null ? "core" : "fixed",
            View = _head.Chase(_core?.Latest().State) ? "chase" : "eyes",
            LookDeg = R(Mathf.RadToDeg(_look), 1),
            Rider = [R(rider.X, 3), R(rider.Y, 3), R(rider.Z, 3)],
            Lat = R(lat, 7),
            Lon = R(lon, 7),
            Camera = [R(cam.X, 3), R(cam.Y, 3), R(cam.Z, 3)],
            CameraForward = [R(fwd.X, 4), R(fwd.Y, 4), R(fwd.Z, 4)],
            Seed = _seed,
            Shot = shot,
        };
        try
        {
            var path = Head.Marks.Append(_marksDir, mark);
            GD.Print($"osscycler: marked {along / 1000:F3} km of {m.Course.Id} in {path}");
            _head.Note(string.Format(CultureInfo.InvariantCulture, "marked {0:F3} km{1}", along / 1000, shot == "" ? " (no screenshot)" : ""));
        }
        catch (Exception e)
        {
            _head.Note("debug: the mark couldn't be saved: " + e.Message);
        }
    }

    static string KeyName(InputEventKey k) => k.Keycode switch
    {
        Key.Enter or Key.KpEnter => "enter",
        Key.Escape => "esc",
        Key.F1 => "f1",
        Key.Up => k.ShiftPressed ? "shift+up" : "up",
        Key.Down when k.CtrlPressed => "ctrl+down",
        Key.Down => k.ShiftPressed ? "shift+down" : "down",
        Key.Left when k.CtrlPressed => k.ShiftPressed ? "ctrl+shift+left" : "ctrl+left",
        Key.Right when k.CtrlPressed => k.ShiftPressed ? "ctrl+shift+right" : "ctrl+right",
        Key.Left => "left",
        Key.Right => "right",
        Key.Tab => k.ShiftPressed ? "shift+tab" : "tab",
        Key.Backspace => "backspace",
        Key.KpAdd => "+",
        Key.KpSubtract => "-",
        _ => k.Unicode != 0 ? char.ConvertFromUtf32((int)k.Unicode) : "",
    };

    // Follow takes the core's newest state: the world of the ride's course,
    // and the rider's position, carried forward at the ride's speed between
    // the core's updates (4 a second) as the TUI does, a correction blended
    // out rather than jumped.
    void Follow()
    {
        var (st, _) = _core!.Latest();
        var r = st?.Ride;
        if (r == null || r.CourseId == "")
        {
            _ride = null;
            if (_world == null && !_idleTried)
                Idle();
            return;
        }
        // Here, or built here: a preview first while the world is built,
        // then the world itself in its place.
        var (dir, status) = _source!.For(r.CourseId);
        _worldStatus = status;
        if (dir != null && dir != _shownDir && dir != _loadingDir && dir != _missing && _swapIn == null)
            LoadInBackground(dir); // a failure sets _missing: not tried every frame
        if (_ride == null || r.DistanceM != _ride.DistanceM || r.Phase != _ride.Phase)
        {
            // Where the last report puts the rider now, not where the last
            // frame showed them: else every update would stall a frame.
            bool same = _ride != null && r.CourseId == _ride.CourseId && r.Phase == RidePhase.Riding;
            double shown = same ? Carried(_ride!) : 0;
            _posErr = same && Math.Abs(shown - r.DistanceM) < 20 ? shown - r.DistanceM : 0;
            _reportedAt = _clock;
            _ride = r;
        }
        _distance = Carried(r);
        _speed = r.SpeedMps;
    }

    bool _idleTried;

    // Idle shows a world at rest while no ride is on (free riding, the
    // dashboard's tiles over it): the figure 8 if it has been built, else
    // the first world there is.
    void Idle()
    {
        _idleTried = true;
        var dirs = new List<string> { Path.Combine(_worldsDir, "figure-8") };
        if (Directory.Exists(_worldsDir))
            dirs.AddRange(Directory.GetDirectories(_worldsDir));
        foreach (var d in dirs)
            if (File.Exists(Path.Combine(d, "world.json")) && Show(d))
            {
                _distance = 0;
                return;
            }
    }

    // Carried is r's position now: carried forward at its speed since it
    // was reported (at most half a second), the correction blending out.
    double Carried(Ride r)
    {
        double d = r.DistanceM;
        if (r.Phase != RidePhase.Riding)
            return d;
        double dt = Math.Max(0, _clock - _reportedAt);
        d += r.SpeedMps * Math.Min(dt, 0.5) + _posErr * Math.Max(0, 1 - dt / PosBlendS);
        return r.Loop ? d : Math.Min(d, r.CourseDistanceM);
    }

    // Place puts the camera at the rider's eyes, facing where the rider is
    // going. Aiming at a point far ahead (25 m) turned the view well into
    // every bend, away from the way it moved, which looked like a car
    // drifting; now the heading is the riding line's own, a few metres
    // ahead (riders look a little into a bend). The head stays nearer
    // level than the bike, on climbs as the TUI's road view does, and
    // leans a little with it in bends.
    void Place(double delta)
    {
        var w = _world!;
        double at = Viewed;
        var here = w.InLane(at);
        var dir = w.InLane(at + HeadingM) - here;
        dir.Y = 0;
        if (dir.LengthSquared() < 1e-6)
            dir = w.At(at + 1) - w.At(at - 1) with { Y = 0 };
        dir = dir.Normalized();
        // The road's grade ahead, in the world's (possibly scaled) heights.
        float slope = (w.At(at + PitchSpanM).Y - w.At(at).Y) / (float)PitchSpanM;
        float pitch = PitchFollow * Mathf.Atan(slope) - LookDown;
        // Lean: the bike's (tan = v²/(g·r)) over the bend's curvature on
        // the riding line; the head takes a part of it.
        var a = w.InLane(at - BendSpanM) - here;
        var b = w.InLane(at + BendSpanM) - here;
        a.Y = 0;
        b.Y = 0;
        float turn = a.LengthSquared() > 0 && b.LengthSquared() > 0 ? (-a).SignedAngleTo(b, Vector3.Up) : 0;
        float curvature = turn / (2 * (float)BendSpanM); // left positive
        float roll = HeadLean * Mathf.Atan((float)(_speed * _speed) * curvature / 9.81f);

        // Smoothed lightly against the path's 5 m steps; a new world snaps.
        float k = _gaze == Vector3.Zero ? 1 : 1 - Mathf.Exp(-SteerPerS * (float)delta);
        _gaze = _gaze.Lerp(dir, k);
        _roll = Mathf.Lerp(_roll, roll, k);
        _pitch = Mathf.Lerp(_pitch, pitch, k);
        if (_head.Chase(_core?.Latest().State))
        {
            // Behind and above the rider, aimed a few metres ahead of them,
            // following the riding line with the gaze's lag.
            var flat = _gaze.Normalized().Rotated(Vector3.Up, -_look);
            var eye = here - flat * ChaseBackM + Vector3.Up * ChaseUpM;
            var aim = here + flat * ChaseAimM + Vector3.Up * 0.9f;
            _camera.Transform = new Transform3D(Basis.LookingAt(aim - eye, Vector3.Up), eye);
            _grass?.Update(_camera.Position);
            return;
        }
        var fwd = _gaze.Normalized().Rotated(Vector3.Up, -_look);
        var right = fwd.Cross(Vector3.Up).Normalized();
        fwd = fwd.Rotated(right, _pitch);
        // Rolled about the view axis: in a left bend (roll > 0) the head's
        // up tilts left.
        var basis = Basis.LookingAt(fwd, Vector3.Up).Rotated(fwd, -_roll);
        _camera.Transform = new Transform3D(basis, here + Vector3.Up * EyeM);
        _grass?.Update(_camera.Position);
    }

    // Cyclists places the rider (in the chase view) and the ghost on the
    // riding line, pedalling and leaning as they ride.
    void Cyclists(double delta)
    {
        var w = _world;
        var st = _core?.Latest().State;
        float height = st?.Profile?.HeightCm is > 0 and var cm ? (float)cm / 100 : Avatar.Pose.RefHeight;
        if (_me == null || Math.Abs(height - _riderHeight) > 0.005f)
        {
            foreach (var c in new[] { _me, _ghostRider })
                c?.Node.QueueFree();
            _riderHeight = height;
            _me = new Avatar.Cyclist(height, Avatar.Cyclist.Plain);
            _ghostRider = new Avatar.Cyclist(height, Avatar.Cyclist.Plain);
            _ghostRider.Ghostly(Colors.Magenta, GhostAlpha);
            AddChild(_me.Node);
            AddChild(_ghostRider.Node);
        }
        _me.Node.Visible = w != null && _head.Chase(st);
        if (w != null && _me.Node.Visible)
        {
            var tr = st?.Trainer;
            float cadence = tr != null && tr.HasCadenceRpm ? tr.CadenceRpm : Avatar.Pose.CadenceFromSpeed((float)_speed);
            if (_ride?.Phase is RidePhase.Armed)
                cadence = 0;
            _me.Advance(delta, cadence, (float)_speed);
            Ride(_me, Viewed, (float)_speed, 0); // with a debug view offset, the figure the camera follows
        }
        double g = GhostNow();
        if (w == null || double.IsNaN(g))
        {
            _ghostRider.Ghostly(Colors.Magenta, 0);
            return;
        }
        // Beside the rider rather than through them: within a few metres it
        // moves a rider's width towards the middle of the road.
        double gap = g - _distance;
        if (w.Manifest.Course.Loop && w.Length > 0)
            gap = ((gap % w.Length) + w.Length * 1.5) % w.Length - w.Length / 2;
        float aside = 0.8f * Mathf.SmoothStep(3.5f, 1.5f, (float)Math.Abs(gap));
        if (w.Manifest.Road.Keep != "left")
            aside = -aside;
        _ghostRider.Advance(delta, Avatar.Pose.CadenceFromSpeed((float)_ghostSpeed), (float)_ghostSpeed);
        Ride(_ghostRider, g, (float)_ghostSpeed, aside);
        // Faded as it comes near the eyes (in the chase view the rider is
        // between them and it).
        float near = _camera.Position.DistanceTo(_ghostRider.Node.Position + Vector3.Up);
        var c8 = Hud.Palette.Ghost;
        _ghostRider.Ghostly(Color.Color8(c8.R, c8.G, c8.B), GhostAlpha * Mathf.SmoothStep(1.2f, 4f, near));
    }

    // GhostNow is where the ghost is now (NaN: none): as last reported,
    // carried forward at its estimated speed for at most half a second.
    double GhostNow()
    {
        if (_core == null)
        {
            if (double.IsNaN(_ghostAhead))
                return double.NaN;
            _ghostSpeed = _speed;
            return _distance + _ghostAhead;
        }
        var gh = _ride?.Ghost;
        if (gh == null)
        {
            _ghost.Reset();
            _ghostSpeed = 0;
            return double.NaN;
        }
        _ghost.Report(gh.DistanceM, _ride!.ElapsedS, _clock);
        if (_ride.Phase != RidePhase.Riding || _core.Latest().State?.Paused == true)
        {
            _ghost.Stand();
            _ghostSpeed = 0;
            return gh.DistanceM;
        }
        _ghostSpeed = _ghost.Speed;
        return _ghost.At(_clock);
    }

    // Ride puts a cyclist at distance d on the riding line, aside metres to
    // the right of it: along the road, nose up its grade, leaning into
    // bends as a bike does (tan = v²/(g·r)).
    void Ride(Avatar.Cyclist c, double d, float speed, float aside)
    {
        var w = _world!;
        var here = w.InLane(d);
        var f = (w.InLane(d + 2) - w.InLane(d - 2)) with { Y = 0 };
        if (f.LengthSquared() < 1e-6f)
            f = (w.At(d + 2) - w.At(d - 2)) with { Y = 0 };
        f = f.Normalized();
        var right = f.Cross(Vector3.Up).Normalized();
        float slope = (w.At(d + 2).Y - w.At(d - 2).Y) / 4;
        var a = (w.InLane(d - BendSpanM) - here) with { Y = 0 };
        var b = (w.InLane(d + BendSpanM) - here) with { Y = 0 };
        float turn = a.LengthSquared() > 0 && b.LengthSquared() > 0 ? (-a).SignedAngleTo(b, Vector3.Up) : 0;
        float lean = Mathf.Atan(speed * speed * turn / (2 * (float)BendSpanM) / 9.81f);
        var basis = Basis.LookingAt(f, Vector3.Up).Rotated(right, Mathf.Atan(slope)).Rotated(f, -lean);
        c.Node.Transform = new Transform3D(basis, here + right * aside);
    }

    string Status()
    {
        var c = CultureInfo.InvariantCulture;
        string conn = "";
        if (_core != null)
        {
            var (st, status) = _core.Latest();
            if (st == null)
                return status;
            if (_ride == null)
                return Head.Controller.ControlText(st) is { Length: > 0 } ctl ? $"{ctl}   + / - step   x free riding   ? keys" : "free riding   m menu   ? keys";
            if (_world == null || _world.Manifest.Course.Id != _ride.CourseId)
                return $"{_ride.CourseName}: {(_worldStatus != "" ? _worldStatus : _loadingDir != "" || _swapIn != null ? "loading the 3D world" : "no 3D world")}";
            conn = _ride.Phase switch
            {
                RidePhase.Armed => "   ready: pedal to start",
                RidePhase.Finished => "   finished",
                _ => "",
            };
        }
        var w = _world!;
        var name = w.Manifest.Course.Name;
        if (name.Length > 32)
            name = name[..31].TrimEnd() + "…"; // the strip beside it
        return string.Format(c, "{0}   {1:F2} of {2:F1} km   {3} fps{4}",
            name, _distance / 1000, w.Manifest.Course.DistanceM / 1000, Engine.GetFramesPerSecond(), conn);
    }

    // Riding is the ride at a fixed speed as a core would report it, for
    // the HUD (no core: no power, heart rate or cadence).
    State? Riding()
    {
        if (_world == null)
            return null;
        var w = _world;
        var course = w.Manifest.Course;
        double grade = (w.At(_distance + 10).Y - w.At(_distance - 10).Y) / 20 * 100 / _relief;
        double d = course.Loop && course.DistanceM > 0 ? _distance % course.DistanceM : _distance;
        return new State
        {
            Sequence = (ulong)(_clock * 4), // the core's 4 Hz
            Ride = new Ride
            {
                Phase = RidePhase.Riding,
                CourseId = course.Id,
                CourseName = course.Name,
                CourseDistanceM = course.DistanceM,
                DistanceM = d,
                SpeedMps = _speed,
                GradePct = grade,
                ElapsedS = _clock,
                Loop = course.Loop,
                Lap = course.Loop && course.DistanceM > 0 ? (uint)(_distance / course.DistanceM) + 1 : 0,
                LapElapsedS = _clock,
            },
        };
    }

    // Weather is a ride's light and air: the sun's height and direction,
    // how far the haze reaches, the sky's tint. Picked from a seed (the
    // day, or --seed), so rides vary and a seed repeats a look.
    readonly record struct Weather(float SunHeight, float SunYaw, float HazeFrom, float HazeTo, float Tint)
    {
        public static Weather From(int seed)
        {
            var r = new Random(seed);
            float f(float lo, float hi) => lo + (float)r.NextDouble() * (hi - lo);
            // The haze is clear near the rider and full by 360-460 m: the
            // world ends 300 m from the road, so it must hide that edge.
            return new Weather(f(22, 58), f(0, 360), f(60, 140), f(360, 460), f(-1, 1));
        }
    }

    // Sky is a daylight sky, its haze by distance: clear up close (the
    // colours show), thickening towards the world's edge.
    static WorldEnvironment Sky(Weather w)
    {
        var horizon = new Color(0.72f, 0.8f, 0.88f).Lerp(w.Tint > 0 ? new Color(0.85f, 0.82f, 0.74f) : new Color(0.66f, 0.76f, 0.9f), Math.Abs(w.Tint) * 0.5f);
        var sky = new ProceduralSkyMaterial
        {
            SkyTopColor = new Color(0.3f, 0.48f, 0.78f).Lerp(new Color(0.42f, 0.58f, 0.82f), (w.Tint + 1) / 2),
            SkyHorizonColor = horizon,
            GroundHorizonColor = horizon,
            GroundBottomColor = new Color(0.35f, 0.38f, 0.32f),
        };
        var env = new Godot.Environment
        {
            BackgroundMode = Godot.Environment.BGMode.Sky,
            Sky = new Godot.Sky { SkyMaterial = sky },
            AmbientLightSource = Godot.Environment.AmbientSource.Sky,
            TonemapMode = Godot.Environment.ToneMapper.Agx,
            FogEnabled = true,
            FogMode = Godot.Environment.FogModeEnum.Depth,
            FogDepthBegin = w.HazeFrom,
            FogDepthEnd = w.HazeTo,
            FogDepthCurve = 1.6f,
            FogLightColor = horizon,
            FogSkyAffect = 0.0f,
            FogSunScatter = 0.15f,
        };
        return new WorldEnvironment { Environment = env };
    }

    static DirectionalLight3D Sun(Weather w) => new()
    {
        RotationDegrees = new Vector3(-w.SunHeight, w.SunYaw, 0),
        LightEnergy = 0.9f + w.SunHeight / 100f,
        ShadowEnabled = true,
        DirectionalShadowMaxDistance = 150,
    };
}
