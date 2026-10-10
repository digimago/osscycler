using System;
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
    string _status = ""; // what is going on meanwhile
    CancellationTokenSource? _cancel;

    public WorldSource(Core core, string dir) => (_core, _dir) = (core, dir);

    // For is course id's world: its directory once ready (null until
    // then; the preview's while the world is built), and what is going on
    // meanwhile ("" once the world itself is there). Asking for another
    // course drops the last one.
    public (string? Dir, string Status) For(string id)
    {
        lock (_mu)
        {
            if (id != _id)
            {
                _cancel?.Cancel();
                (_id, _ready, _status) = (id, null, "looking for the 3D world");
                _cancel = new CancellationTokenSource();
                var token = _cancel.Token;
                _ = Task.Run(() => Get(id, token));
            }
            return (_ready, _status);
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
        lock (_mu)
            if (id == _id)
                (_ready, _status) = (ready, status);
    }

    static bool SafeName(string s) =>
        s != "" && s != "." && s != ".." && s.IndexOfAny(new[] { '/', '\\', '\0' }) < 0;

    async Task Get(string id, CancellationToken token)
    {
        try
        {
            // The course's GPX, beside the worlds (the core's courses
            // directory may be on another machine).
            var courses = Path.Combine(_dir, ".courses");
            Directory.CreateDirectory(courses);
            if (await _core.CourseFile(id) is { } f)
            {
                if (!SafeName(f.Name) || !f.Name.EndsWith(".gpx", StringComparison.Ordinal))
                    throw new InvalidDataException($"the core sent a course file named \"{f.Name}\"");
                var path = Path.Combine(courses, f.Name);
                if (!File.Exists(path) || !File.ReadAllBytes(path).AsSpan().SequenceEqual(f.Data))
                    await File.WriteAllBytesAsync(path, f.Data, token);
            }
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

    async Task Build(string id, string courses, CancellationToken token)
    {
        var builder = Builder();
        if (builder == null)
        {
            Set(id, null, "no osscycler-world to build the 3D world with");
            return;
        }
        // The map data and ground model caches of this machine's osscycler.
        var cache = Path.Combine(Core.Home(), "courses");
        var psi = new ProcessStartInfo(builder)
        {
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false,
        };
        foreach (var a in new[] { "-courses", courses, "-osm-cache", Path.Combine(cache, ".osm"), "-dem-cache", Path.Combine(cache, ".dem"),
                     "-out", _dir, "-progress", "-if-stale", "-preview", "-check=warn", "--", id })
            psi.ArgumentList.Add(a);
        using var p = Process.Start(psi) ?? throw new IOException("osscycler-world didn't start");
        try
        {
            // Below the ride: the view and the core beside it come first.
            p.PriorityClass = ProcessPriorityClass.BelowNormal;
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
        string? world = null, preview = null;
        while (await p.StandardOutput.ReadLineAsync(token) is { } line)
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
        }
        await p.WaitForExitAsync(token);
        await errors;
        if (p.ExitCode != 0 || world == null)
        {
            Set(id, preview, (preview != null ? "preview: " : "") + "the 3D world couldn't be built: " + (last != "" ? last : $"osscycler-world exited with {p.ExitCode}"));
            return;
        }
        Set(id, world, "");
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
