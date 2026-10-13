using System;
using Osscycler.V1;

namespace Osscycler.Hud;

// Laps is the arithmetic of a course ridden lap after lap (owner,
// 2026-10-13), as the TUI's (tui/loop.go): a test track goes round as a
// whole; a course whose end comes back to its start has a run-in, then a
// lap from lap_start_m to lap_end_m, the course running on past its end
// into the lap's start again, where a ride goes on a lap back. Plain C#
// apart from Godot, tested.
public static class Laps
{
    // Lapped reports whether course c is ridden lap after lap.
    public static bool Lapped(Course c) => c.Loop || c.LapEndM > 0;

    // Len is a loop ride's lap: the course on a test track, its lap after
    // the run-in on a course with one.
    public static double Len(Ride r) => r.LapLengthM > 0 ? r.LapLengthM : r.CourseDistanceM;

    // CoursePos is s on course c: on a test track round the loop, past the
    // end of a course with a lap a lap back, as often as it takes.
    public static double CoursePos(Course c, double s)
    {
        double f = c.DistanceM;
        if (c.Loop && f > 0)
            return ((s % f) + f) % f;
        if (c.LapEndM > c.LapStartM)
            for (double l = c.LapEndM - c.LapStartM; s > f;)
                s -= l;
        return s;
    }

    // Line reports whether a lap's line lies in (from, to] on course c:
    // every lap on a test track; on a course with a run-in the lap's start
    // once, then its end each lap.
    public static bool Line(Course c, double from, double to)
    {
        if (c.Loop)
            return c.DistanceM > 0 && Math.Floor(from / c.DistanceM) != Math.Floor(to / c.DistanceM);
        double s = c.LapStartM, e = c.LapEndM;
        if (e <= s)
            return false;
        if (from < s && s <= to)
            return true;
        if (to < e)
            return false;
        double line = e + Math.Floor((to - e) / (e - s)) * (e - s);
        return line > from;
    }

    // ToGo is what a lap tile says: the lap left, or on the run-in how far
    // to the lap (label "TO THE LAP").
    public static (string Label, double Metres) ToGo(Ride r)
    {
        if (!r.Loop)
            return ("TO GO", r.CourseDistanceM - r.DistanceM);
        if (r.LapLengthM <= 0)
            return ("LAP TO GO", r.CourseDistanceM - r.DistanceM);
        return r.LapDistanceM < 0 ? ("TO THE LAP", -r.LapDistanceM) : ("LAP TO GO", r.LapLengthM - r.LapDistanceM);
    }
}
