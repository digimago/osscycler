using System;
using System.Globalization;
using System.Text.Json;

namespace Osscycler.Head;

// BuildStatus turns osscycler-world's progress lines into what the status
// line and the loading cover say (owner, 2026-10-10: a ride on a course
// whose world was being built never said how long it would take): the
// phase, its count, and the time left, e.g. "map data 3/8 · about 4 min
// left". Plain C# apart from Godot, tested.
public static class BuildStatus
{
    public const string Prefix = "progress: ";

    // Parse reads a progress line's JSON (after Prefix); null if it isn't one.
    public static string? Parse(string line)
    {
        if (!line.StartsWith(Prefix, StringComparison.Ordinal))
            return null;
        try
        {
            using var doc = JsonDocument.Parse(line[Prefix.Length..]);
            var r = doc.RootElement;
            return Text(r.GetProperty("phase").GetString() ?? "", r.GetProperty("done").GetInt32(), r.GetProperty("total").GetInt32(),
                r.GetProperty("frac").GetDouble(), r.GetProperty("left_s").GetDouble());
        }
        catch (Exception e) when (e is JsonException or InvalidOperationException or System.Collections.Generic.KeyNotFoundException)
        {
            return null;
        }
    }

    // Text is the phase with its count (or how far, in %), and the time left
    // when known.
    public static string Text(string phase, int done, int total, double frac, double leftS)
    {
        var c = CultureInfo.InvariantCulture;
        string s = total > 0 ? string.Format(c, "{0} {1}/{2}", phase, done, total)
            : frac > 0 ? string.Format(c, "{0} {1:0}%", phase, frac * 100) : phase;
        return leftS >= 0 ? s + " · " + Left(leftS) : s;
    }

    // Left is a time left, rounded as people say it.
    public static string Left(double s) => s switch
    {
        < 10 => "a few seconds left",
        < 60 => string.Format(CultureInfo.InvariantCulture, "about {0} s left", Math.Ceiling(s / 5) * 5),
        < 90 => "about a minute left",
        _ => string.Format(CultureInfo.InvariantCulture, "about {0} min left", Math.Round(s / 60)),
    };
}
