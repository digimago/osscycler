using System;
using System.Collections.Generic;
using Osscycler.V1;

namespace Osscycler.Hud;

// Tile is one number on the HUD: its label, value and unit, in colours.
public sealed record Tile(string Label, string Value, string Unit, Rgb ValueColor, Rgb? UnitColor = null);

// Tiles picks and fills the HUD's tiles from the core's state, as the
// TUI's screens do (tui/layout.go): the workout's while one is on, the
// course ride's during a ride, else the dashboard's; in the rider's
// arrangement (Layout), else each screen's defaults.
public static class Tiles
{
    public const string Dashboard = "dashboard", Ride = "ride", Workout = "workout";

    public static bool RideActive(State? st) => st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding;

    public static bool WorkoutActive(State? st) =>
        st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused;

    public static string Screen(State? st) => WorkoutActive(st) ? Workout : RideActive(st) ? Ride : Dashboard;

    // For is the screen's tiles; cadence is the 5 s average (null: none).
    public static (string Screen, List<Tile> Tiles) For(State? st, double? cadence, Layout? layout = null)
    {
        var screen = Screen(st);
        var tiles = new List<Tile>();
        foreach (var id in (layout ?? new Layout()).Shown(screen))
            tiles.Add(Make(id, st, cadence));
        return (screen, tiles);
    }

    public static Tile Make(string id, State? st, double? cadence)
    {
        var tr = st?.Trainer;
        var r = st?.Ride ?? new Ride();
        var w = st?.Workout ?? new WorkoutProgress();
        bool riding = RideActive(st), working = WorkoutActive(st);
        switch (id)
        {
            case "power":
                return new("POWER", tr != null && tr.HasPowerW ? tr.PowerW.ToString() : "--", "W", Palette.Power);
            case "heart_rate":
                string hr = "--";
                if (st?.HeartRate != null && st.HeartRate.HasBpm)
                    hr = st.HeartRate.Bpm.ToString();
                else if (tr != null && tr.HasHeartRateBpm)
                    hr = tr.HeartRateBpm.ToString(); // the strap first, else what the trainer forwards
                return new("HEART RATE", hr, "bpm", Palette.Heart);
            case "cadence":
                return new("CADENCE", tr != null && tr.HasCadenceRpm ? tr.CadenceRpm.ToString() : "--", "rpm", Palette.Cadence);
            case "cadence_5s":
                string unit = working && w.TargetCadence > 0 ? $"rpm · aim {w.TargetCadence}" : "rpm";
                return new("CADENCE 5s", cadence is double c ? Format.Fixed(c, 0) : "--", unit, Palette.Cadence);
            case "speed":
                if (riding)
                    return new("SPEED", Format.Fixed(r.SpeedMps * 3.6, 1), "km/h", Palette.Speed);
                return new("SPEED", tr != null && tr.HasSpeedMps ? Format.Fixed(tr.SpeedMps * 3.6, 1) : "--", "km/h", Palette.Speed);
            case "grade":
                return new("GRADE", Format.Fixed(r.GradePct, 1), "%", Palette.Grade(r.GradePct));
            case "time":
                return Time(r);
            case "to_go":
                return new(r.Loop ? "LAP TO GO" : "TO GO", Format.Fixed(Math.Max(0, r.CourseDistanceM - r.DistanceM) / 1000, 2), "km", Palette.Speed);
            case "climbed":
                return new("CLIMBED", Format.Fixed(r.ClimbedM, 0), r.Loop ? "m" : $"of {Format.Fixed(r.CourseGainM, 0)} m", Palette.Plain);
            case "avg_power":
                return new("AVG POWER", Format.Fixed(working ? w.AvgPowerW : r.AvgPowerW, 0), "W", Palette.Power);
            case "target":
                if (w.Free)
                    return new("TARGET", "--", w.SegmentLabel.StartsWith("Max") ? "MAX" : "FREE", Palette.Dim);
                return new("TARGET", Format.Fixed(w.TargetW, 0), "W", Palette.Zone(w.FtpW > 0 ? w.TargetW / w.FtpW : 0));
            case "interval":
                return new(w.SegmentLabel.ToUpperInvariant(), Format.Clock(w.SegmentRemainingS), "left", Palette.Plain);
            case "workout_left":
                return new("WORKOUT LEFT", Format.Clock(Math.Max(0, w.DurationS - w.ElapsedS)), "", Palette.Plain);
        }
        return new(id.ToUpperInvariant(), "--", "", Palette.Dim);
    }

    // Time is the ride clock (on a loop the lap's), with the ghost under
    // it: its time before the start, then the gap, green ahead, red
    // behind (tui/history.go timeTile).
    static Tile Time(Ride r)
    {
        string label = "TIME", value = Format.Clock(r.ElapsedS);
        if (r.Loop)
            (label, value) = ($"LAP {r.Lap}", Format.Clock(r.LapElapsedS));
        var g = r.Ghost;
        if (g == null)
            return new(label, value, "", Palette.Plain);
        if (!r.Loop)
            label += " vs " + g.Label; // on a loop the ghost is always the best lap
        double gap = g.GapS;
        return r.Phase switch
        {
            RidePhase.Armed when r.Loop => new(label, value, "lap PB " + Format.LapTime(g.TimeS), Palette.Plain, Palette.Ghost),
            RidePhase.Armed => new(label, value, "PB " + Format.Clock(g.TimeS), Palette.Plain, Palette.Ghost),
            _ when gap < 0 => new(label, value, "-" + Format.Secs(-gap) + " ahead", Palette.Plain, Palette.Ahead),
            _ => new(label, value, "+" + Format.Secs(gap) + " behind", Palette.Plain, Palette.Behind),
        };
    }
}
