using System;

namespace Osscycler.Head;

// Cover is the loading screen over world swaps (owner, 2026-10-10: a ride
// started on another course went on in the world shown before, the idle
// figure 8, for the seconds its own world took to load, and a mark made
// then named the figure 8): while the ride's course has no world of its
// own on screen, the view is covered in the haze's colour, saying what is
// loading; the course's world (or its preview) is put in under it, and the
// cover lifts. Plain C# apart from Godot, tested.
public static class Cover
{
    public const double InS = 0.35, OutS = 0.6;

    // Needed tells whether the world shown (worldCourse: its course, ""
    // for none) isn't the ride's (rideCourse, "" without a ride).
    public static bool Needed(string rideCourse, string worldCourse) =>
        rideCourse != "" && rideCourse != worldCourse;

    // Step moves the cover's opacity a towards up (1) or down (0) over dt
    // seconds: in quickly, out more gently.
    public static double Step(double a, bool up, double dt) =>
        up ? Math.Min(1, a + dt / InS) : Math.Max(0, a - dt / OutS);

    // Text is what the cover says: the course, and what its world is
    // waiting for (the build's phase), if anything is known.
    public static string Text(string course, string status)
    {
        var s = $"Loading {course}";
        return status != "" ? s + "\n" + status : s + "…";
    }
}
