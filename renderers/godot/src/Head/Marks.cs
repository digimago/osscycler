using System;
using System.Globalization;
using System.IO;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Osscycler.Head;

// Marks are spots the rider flagged as flawed while riding (owner,
// 2026-10-10: in debug mode, space marks where you are, for analysis): one
// JSON line each in marks.jsonl, with a screenshot beside it, for the
// world-faults skill (.claude/skills/world-faults) to pick up. Positions
// are in the world's frame (world.json: x east, y up, z south of its
// origin), heights as built (not scaled by --relief).
public sealed class Mark
{
    [JsonPropertyName("time")] public string Time { get; set; } = "";
    [JsonPropertyName("course_id")] public string CourseId { get; set; } = "";
    [JsonPropertyName("course_name")] public string CourseName { get; set; } = "";
    // The world shown: its directory and stamp (which build it was).
    [JsonPropertyName("world")] public string World { get; set; } = "";
    [JsonPropertyName("stamp")] public string Stamp { get; set; } = "";
    [JsonPropertyName("preview")] public bool Preview { get; set; }
    // The rider's distance as ridden (on a loop it runs on past a lap), and
    // the marked spot's along the course (the world's path: the km mark to
    // look at; the rider's own unless a debug view was moved, view_offset_m).
    [JsonPropertyName("distance_m")] public double DistanceM { get; set; }
    [JsonPropertyName("along_m")] public double AlongM { get; set; }
    // A debug view moved along the course from the rider (ctrl+arrows): the
    // mark is of the spot looked at, this far from the rider (0: the rider's).
    [JsonPropertyName("view_offset_m")] public double ViewOffsetM { get; set; }
    [JsonPropertyName("speed_mps")] public double SpeedMps { get; set; }
    [JsonPropertyName("source")] public string Source { get; set; } = ""; // "core", or "fixed" (--world, --speed)
    [JsonPropertyName("view")] public string View { get; set; } = "";     // "chase" or "eyes"
    [JsonPropertyName("look_deg")] public double LookDeg { get; set; }
    // The rider on the riding line, and the camera with the way it faces.
    [JsonPropertyName("rider")] public double[] Rider { get; set; } = [];
    [JsonPropertyName("lat")] public double Lat { get; set; }
    [JsonPropertyName("lon")] public double Lon { get; set; }
    [JsonPropertyName("camera")] public double[] Camera { get; set; } = [];
    [JsonPropertyName("camera_forward")] public double[] CameraForward { get; set; } = [];
    [JsonPropertyName("seed")] public int Seed { get; set; }
    [JsonPropertyName("shot")] public string Shot { get; set; } = ""; // beside marks.jsonl ("" if it failed)
}

public static class Marks
{
    public const string File = "marks.jsonl";

    // Earth's radius as course.Project has it (internal/course).
    const double MetresPerDegree = 6371000.0 * Math.PI / 180;

    // LatLon is the world-frame point x, z as latitude and longitude:
    // course.Unproject from the world's origin.
    public static (double Lat, double Lon) LatLon(double lat0, double lon0, double x, double z)
    {
        double kx = MetresPerDegree * Math.Cos(lat0 * Math.PI / 180);
        return (lat0 - z / MetresPerDegree, lon0 + x / kx);
    }

    // Name is a mark's file name stem from when it was made, to the
    // millisecond (two marks in one second keep apart).
    public static string Name(DateTimeOffset t) => t.ToString("yyyy-MM-dd-HHmmss-fff", CultureInfo.InvariantCulture);

    // Relaxed escaping: "·" and "+02:00" as they are (the file is read by
    // people and scripts, never put in HTML).
    static readonly JsonSerializerOptions Options = new() { WriteIndented = false, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping };

    // Line is a mark as its line in marks.jsonl.
    public static string Line(Mark m) => JsonSerializer.Serialize(m, Options);

    // Append adds a mark to dir's marks.jsonl (made if need be); its path.
    public static string Append(string dir, Mark m)
    {
        Directory.CreateDirectory(dir);
        var path = Path.Combine(dir, File);
        System.IO.File.AppendAllText(path, Line(m) + "\n", new UTF8Encoding(false));
        return path;
    }
}
