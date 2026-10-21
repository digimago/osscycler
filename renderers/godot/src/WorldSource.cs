using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using Godot;

namespace Osscycler;

// WorldSource gets the 3D world of the course being ridden (plan step 10;
// owner, 2026-10-09: worlds are the client's, built where they are drawn,
// never on the core, which may be a Pi by the trainer). It takes the
// course's GPX from the core (the tracks and included routes need none)
// and runs osscycler-world -if-stale beside it at low priority: that uses
// a current world (built here before, or ready-built with the program)
// or builds one, saying how far it is. While it builds one, a preview
// comes first (osscycler-world -preview, owner 2026-10-09: ride a road
// along the GPX with generic terrain meanwhile): For hands that out until
// the world itself is there.
public sealed class WorldSource
{
    readonly Core _core;
    readonly string _dir;
    readonly object _mu = new();
    string _id = "";
    string? _ready;      // the world's directory, once it is there
    string _stamp = "";  // its manifest's stamp ("" unknown)
    string _status = ""; // what is going on meanwhile
    CancellationTokenSource? _cancel;

    public WorldSource(Core core, string dir) => (_core, _dir) = (core, dir);

    // For is course id's world: its directory once ready (null until
    // then; the preview's while the world is built) and its stamp (to tell
    // a rebuild in the same directory from the world shown), and what is
    // going on meanwhile ("" once the world itself is there). Asking for
    // another course drops the last one.
    public (string? Dir, string Stamp, string Status) For(string id)
    {
        lock (_mu)
        {
            if (id != _id)
            {
                _cancel?.Cancel();
                (_id, _ready, _stamp, _status) = (id, null, "", "looking for the 3D world");
                _cancel = new CancellationTokenSource();
                var token = _cancel.Token;
                _ = Task.Run(() => Get(id, token));
            }
            return (_ready, _stamp, _status);
        }
    }

    // Stop drops what is going on (the build is killed): the renderer quits.
    public void Stop()
    {
        lock (_mu)
            _cancel?.Cancel();
    }

    void Set(string id, string? ready, string status)
    {
        var stamp = ready != null ? StampOf(ready) : "";
        lock (_mu)
            if (id == _id)
                (_ready, _stamp, _status) = (ready, stamp, status);
    }

    // StampOf is the stamp in dir's manifest ("" if it can't be read).
    static string StampOf(string dir)
    {
        try
        {
            using var doc = System.Text.Json.JsonDocument.Parse(File.ReadAllBytes(Path.Combine(dir, "world.json")));
            return doc.RootElement.TryGetProperty("stamp", out var s) ? s.GetString() ?? "" : "";
        }
        catch (Exception)
        {
            return "";
        }
    }

    static bool SafeName(string s) =>
        s != "" && s != "." && s != ".." && s.IndexOfAny(new[] { '/', '\\', '\0' }) < 0;

    async Task Get(string id, CancellationToken token)
    {
        try
        {
            var courses = await FetchCourse(_core, _dir, id, token);
            await Build(id, courses, token);
        }
        catch (OperationCanceledException)
        {
        }
        catch (Exception e)
        {
            Set(id, null, "getting the 3D world failed: " + e.Message);
        }
    }

    // FetchCourse puts course id's GPX from the core beside the worlds (the
    // core's courses directory may be on another machine; the tracks and
    // included routes need none) and says where: the courses directory to
    // build from.
    internal static async Task<string> FetchCourse(Core core, string dir, string id, CancellationToken token)
    {
        var courses = Path.Combine(dir, ".courses");
        Directory.CreateDirectory(courses);
        if (await core.CourseFile(id) is { } f)
        {
            if (!SafeName(f.Name) || !f.Name.EndsWith(".gpx", StringComparison.Ordinal))
                throw new InvalidDataException($"the core sent a course file named \"{f.Name}\"");
            var path = Path.Combine(courses, f.Name);
            if (!File.Exists(path) || !File.ReadAllBytes(path).AsSpan().SequenceEqual(f.Data))
                await File.WriteAllBytesAsync(path, f.Data, token);
        }
        return courses;
    }

    async Task Build(string id, string courses, CancellationToken token)
    {
        string? world = null, preview = null;
        var (code, last) = await RunBuilder(Args(courses, _dir, id, "-preview"), ProcessPriorityClass.BelowNormal, line =>
        {
            if (line.StartsWith("phase: ", StringComparison.Ordinal))
                Set(id, preview, preview != null ? $"preview (the 3D world: {line[7..]})" : $"building the 3D world: {line[7..]}");
            else if (Head.BuildStatus.Parse(line) is { } how)
                Set(id, preview, preview != null ? $"preview (the 3D world: {how})" : $"building the 3D world: {how}");
            else if (line.StartsWith("preview: ", StringComparison.Ordinal))
            {
                preview = line[9..];
                Set(id, preview, "preview (the 3D world is being built)");
            }
            else if (line.StartsWith("world: ", StringComparison.Ordinal))
                world = line[7..];
        }, token);
        if (code == null)
        {
            Set(id, null, "no osscycler-world to build the 3D world with");
            return;
        }
        if (code != 0 || world == null)
        {
            Set(id, preview, (preview != null ? "preview: " : "") + "the 3D world couldn't be built: " + (last != "" ? last : $"osscycler-world exited with {code}"));
            return;
        }
        Set(id, world, "");
    }

    // Args are osscycler-world's arguments for course id's world in dir,
    // from the GPX in courses, with this machine's map data and ground
    // model caches, and extra flags (-preview, -if-cached).
    internal static string[] Args(string courses, string dir, string id, params string[] extra)
    {
        var cache = Path.Combine(Core.Home(), "courses");
        var a = new List<string> { "-courses", courses, "-osm-cache", Path.Combine(cache, ".osm"), "-dem-cache", Path.Combine(cache, ".dem"),
            "-out", dir, "-progress", "-if-stale", "-check=warn" };
        a.AddRange(extra);
        a.Add("--");
        a.Add(id);
        return a.ToArray();
    }

    // RunBuilder runs osscycler-world with args at priority, handing each
    // line it prints to onLine, until it ends (killed when token is
    // cancelled: OperationCanceledException). Its exit code (null: there is
    // no osscycler-world) and the last line it wrote to stderr.
    internal static async Task<(int? Code, string Last)> RunBuilder(string[] args, ProcessPriorityClass priority, Action<string> onLine, CancellationToken token)
    {
        var builder = Builder();
        if (builder == null)
            return (null, "");
        var psi = new ProcessStartInfo(builder)
        {
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false,
        };
        foreach (var a in args)
            psi.ArgumentList.Add(a);
        using var p = Process.Start(psi) ?? throw new IOException("osscycler-world didn't start");
        try
        {
            // Below the ride: the view and the core beside it come first.
            p.PriorityClass = priority;
        }
        catch (Exception)
        {
        }
        using var kill = token.Register(() =>
        {
            try
            {
                p.Kill();
            }
            catch (InvalidOperationException)
            {
            }
        });
        string last = "";
        var errors = Task.Run(async () =>
        {
            while (await p.StandardError.ReadLineAsync() is { } line)
                if (line.Trim() != "")
                    last = line.Trim();
        });
        while (await p.StandardOutput.ReadLineAsync(token) is { } line)
            onLine(line);
        await p.WaitForExitAsync(token);
        await errors;
        return (p.ExitCode, last);
    }

    // Builder is osscycler-world: $OSSCYCLER_WORLD, beside this program
    // (an installed build), the source tree's _bin/, or the PATH.
    static string? Builder()
    {
        if (System.Environment.GetEnvironmentVariable("OSSCYCLER_WORLD") is { Length: > 0 } env)
            return env;
        var candidates = new[]
        {
            Path.Combine(Path.GetDirectoryName(OS.GetExecutablePath()) ?? "", "osscycler-world"),
            Path.GetFullPath(Path.Combine(ProjectSettings.GlobalizePath("res://"), "..", "..", "_bin", "osscycler-world")),
        };
        foreach (var c in candidates)
            if (File.Exists(c))
                return c;
        foreach (var d in (System.Environment.GetEnvironmentVariable("PATH") ?? "").Split(Path.PathSeparator))
            if (d != "" && File.Exists(Path.Combine(d, "osscycler-world")))
                return Path.Combine(d, "osscycler-world");
        return null;
    }
}
