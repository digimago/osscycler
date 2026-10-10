using System;
using System.Collections.Generic;
using Osscycler.V1;

namespace Osscycler.Hud;

// Strips are the HUD's two course strips, as the TUI draws them
// (tui/ride.go gradeStrip, elevationProfile): the look-ahead (the next
// 250 m in 10 m cells coloured by grade, Zwift's climb bar) and the whole
// course's profile with the rider and the ghost marked.
public static class Strips
{
    public const double LookAheadM = 250, CellM = 10, LabelEveryM = 50;

    public enum CellKind { Grade, Finish, Lap }

    // Cell is one look-ahead cell: what it shows, its colour and its label
    // ("" for none). Finish and lap cells are chequered (Even alternates).
    public readonly record struct Cell(CellKind Kind, Rgb Color, string Label, bool Even);

    // LookAhead is the cells from pos onward; ghostAt is the ghost's place
    // as a fraction of the strip (null: not within it).
    public static (List<Cell> Cells, double? GhostAt) LookAhead(Course c, double pos, double ghost)
    {
        int n = (int)(LookAheadM / CellM);
        var cells = new List<Cell>(n);
        double finish = c.DistanceM;
        bool loop = c.Loop && finish > 0, finishLabelled = false;
        for (int i = 0; i < n; i++)
        {
            double from = pos + i * CellM, to = from + CellM, mid = from + CellM / 2;
            bool onLabel = Math.Abs(i * CellM % LabelEveryM) < 1e-9;
            if (loop && Math.Floor(from / finish) != Math.Floor(to / finish))
            {
                cells.Add(new(CellKind.Lap, Rgb.Hex(0xe0e0e0), "LAP", i % 2 == 0));
                continue;
            }
            if (!loop && mid > finish)
            {
                cells.Add(new(CellKind.Finish, Rgb.Hex(0xe0e0e0), finishLabelled ? "" : "FINISH", i % 2 == 0));
                finishLabelled = true;
                continue;
            }
            double g = GradeAt(c, loop ? mid % finish : mid);
            cells.Add(new(CellKind.Grade, Palette.Grade(g), onLabel ? Format.Fixed(g, 0) + "%" : "", false));
        }
        double off = ghost - pos;
        return (cells, ghost >= 0 && off >= 0 && off < LookAheadM ? off / LookAheadM : null);
    }

    // Ticks are the ruler's labels under the strip: "now", then every 50 m.
    public static IEnumerable<(double At, string Text)> Ticks()
    {
        for (double m = 0; m < LookAheadM; m += LabelEveryM)
            yield return (m / LookAheadM, m == 0 ? "now" : Format.Fixed(m, 0));
    }

    // GradeAt is the profile's grade at or before d.
    public static double GradeAt(Course c, double d)
    {
        var g = c.ProfileGradePct;
        if (g.Count == 0 || c.ProfileStepM <= 0)
            return 0;
        return g[Math.Clamp((int)(d / c.ProfileStepM), 0, g.Count - 1)];
    }

    public static double ElevationAt(Course c, double d)
    {
        var e = c.ProfileElevationM;
        if (e.Count == 0)
            return 0;
        double x = Math.Max(0, d / c.ProfileStepM);
        int i = (int)x;
        if (i >= e.Count - 1)
            return e[^1];
        double f = x - i;
        return e[i] * (1 - f) + e[i + 1] * f;
    }

    // Column is one column of the profile: its height (0-1 of the
    // course's range, at least a little so the lowest point shows) and
    // the colour of the grade over the column's stretch (on a long course
    // a column covers hundreds of metres: one sample's grade flickered).
    public readonly record struct Column(double Height, Rgb Color);

    // Profile is the course in columns, west to east along the ride.
    public static List<Column> Profile(Course c, int columns)
    {
        var cols = new List<Column>(columns);
        if (c.ProfileElevationM.Count < 2 || columns < 2)
            return cols;
        double lo = double.MaxValue, hi = double.MinValue;
        foreach (var e in c.ProfileElevationM)
            (lo, hi) = (Math.Min(lo, e), Math.Max(hi, e));
        double span = Math.Max(hi - lo, 1);
        for (int x = 0; x < columns; x++)
        {
            double d = (double)x / (columns - 1) * c.DistanceM;
            double half = c.DistanceM / (columns - 1) / 2;
            double d0 = Math.Max(0, d - half), d1 = Math.Min(c.DistanceM, d + half);
            double grade = d1 > d0 ? (ElevationAt(c, d1) - ElevationAt(c, d0)) / (d1 - d0) * 100 : GradeAt(c, d);
            cols.Add(new(Math.Max(0.04, (ElevationAt(c, d) - lo) / span), Palette.Grade(grade)));
        }
        return cols;
    }

    // Workout is the workout's profile in columns over its time, as the
    // TUI draws it (tui/workout.go workoutProfile): each column's height
    // the target as a fraction of FTP (of the highest, at least 1.2), in
    // its zone's colour; free parts a low grey bar.
    public static List<Column> Workout(WorkoutDef w, int columns)
    {
        var cols = new List<Column>(columns);
        var segs = w.Timeline;
        if (w.DurationS <= 0 || segs.Count == 0 || columns < 2)
            return cols;
        double top = 1.2;
        foreach (var s in segs)
            top = Math.Max(top, Math.Max(s.FromFtp, s.ToFtp));
        int si = 0;
        for (int x = 0; x < columns; x++)
        {
            double t = (x + 0.5) / columns * w.DurationS;
            while (si < segs.Count - 1 && t >= segs[si].StartS + segs[si].DurationS)
                si++;
            var s = segs[si];
            if (s.Free)
            {
                cols.Add(new(0.35 / top, Rgb.Hex(0x505050)));
                continue;
            }
            double f = s.DurationS > 0 ? (t - s.StartS) / s.DurationS : 0;
            double frac = s.FromFtp + (s.ToFtp - s.FromFtp) * f;
            cols.Add(new(Math.Max(0.04, frac / top), Palette.Zone(frac)));
        }
        return cols;
    }
}
