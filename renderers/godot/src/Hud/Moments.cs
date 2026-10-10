using System;
using System.Collections.Generic;
using Osscycler.V1;

namespace Osscycler.Hud;

// Moments are what the HUD shows now and then, by the TUI's rules
// (tui/ride.go, loop.go, model.go): ready to start, the result, the lap
// pop-in, workout messages, warnings, the place just entered and the
// map's credit at a ride's start. It remembers what it needs from state
// to state (when the phase changed, the laps ridden). Times in seconds of
// the renderer's clock.
public sealed class Moments
{
    public const double ResultShownS = 30;  // no keys on the HUD yet: the result stays this long
    public const double AbortedShownS = 10; // as the TUI
    public const double LapsShownS = 15;    // the lap pop-in, after each lap
    public const double PlaceShownM = 500;  // a place's name, past its sign

    // Banner is the big message in the middle: a title, a line under it
    // (in its own colour, as a verdict), and a smaller line.
    public sealed record Banner(string Title, string Sub, Rgb SubColor, string Line);

    // LapRow is one line of the lap pop-in.
    public sealed record LapRow(string Label, string Time, bool Fastest, bool Running);

    public Banner? Big { get; private set; }
    public List<string> Warnings { get; } = new();
    public string Place { get; private set; } = "";
    public string Message { get; private set; } = "";
    public List<LapRow> Laps { get; } = new();

    RidePhase _phase;
    string _course = "";
    double _phaseAt;
    readonly List<RideLap> _laps = new();
    double _lapAt = double.NegativeInfinity;

    public void Update(State? st, Course? course, double pos, double now)
    {
        var r = st?.Ride ?? new Ride();
        if (r.Phase != _phase || r.CourseId != _course)
        {
            if (r.Phase == RidePhase.Armed || r.CourseId != _course)
            {
                _laps.Clear();
                _lapAt = double.NegativeInfinity;
            }
            (_phase, _course, _phaseAt) = (r.Phase, r.CourseId, now);
        }
        if (r.LastLap is { } last && last.Number > 0 && (_laps.Count == 0 || _laps[^1].Number != last.Number))
        {
            _laps.Add(last);
            _lapAt = now;
        }

        Big = st?.Paused == true ? Paused(st) : BannerFor(r, now - _phaseAt);
        Message = st?.Workout?.Message ?? "";
        // An announcement (Announce: a script running a session, a coach)
        // until it expires, over a workout's message.
        if (st?.Announcement is { Text: not "" } an && DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() < an.UntilUnixMs)
            Message = Message != "" ? an.Text + "\n" + Message : an.Text;

        Warnings.Clear();
        var tr = st?.Trainer;
        if (st?.Radio is { Present: false })
            Warnings.Add("ANT+ stick not found: plug it in");
        if (tr?.Sensor?.Status == SensorStatus.Lost)
            Warnings.Add("trainer lost");
        if (st?.HeartRate?.Sensor?.Status == SensorStatus.Lost)
            Warnings.Add("heart rate strap lost");
        if (tr?.UserConfigRequired == true)
            Warnings.Add("the trainer wants your weight (TUI: p)");
        if (tr?.TargetPowerLimit == TargetPowerLimit.SpeedTooLow)
            Warnings.Add("ERG: speed too low for the target, shift up");
        if (tr?.TargetPowerLimit == TargetPowerLimit.SpeedTooHigh)
            Warnings.Add("ERG: speed too high for the target, shift down");
        if (st?.Recording is { Error: not "" } rec)
            Warnings.Add("recording failed: " + rec.Error);

        bool riding = r.Phase == RidePhase.Riding;
        Place = "";
        if (riding && course != null)
        {
            foreach (var s in course.PlaceSigns)
                if (s.DistanceM <= pos && pos - s.DistanceM < PlaceShownM)
                    Place = s.Name; // the last one passed
        }

        Laps.Clear();
        if (r.Loop && riding && now - _lapAt < LapsShownS && _laps.Count > 0)
            FillLaps(r);
    }

    // Paused is the banner while the core is parked, with how long.
    static Banner Paused(State st)
    {
        string line = "the trainer is flat, the clocks stand";
        if (st.PausedSinceUnixMs > 0)
        {
            double s = (DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() - st.PausedSinceUnixMs) / 1000.0;
            if (s >= 1)
                line = $"{Format.Clock(s)} · " + line;
        }
        return new("Paused", "p or space carries on", Palette.Ghost, line);
    }

    static Banner? BannerFor(Ride r, double since)
    {
        switch (r.Phase)
        {
            case RidePhase.Armed:
            {
                string sub = r.Ghost is { } g ? $"racing your {g.Label} ({(r.Loop ? Format.LapTime(g.TimeS) : Format.Clock(g.TimeS))})" : r.CourseName;
                return new("Pedal to start", sub, Palette.Ghost, r.Ghost != null ? r.CourseName : "");
            }
            case RidePhase.Finished when since < ResultShownS && !r.Loop:
            {
                double avgKmh = r.ElapsedS > 0 ? (r.CourseDistanceM - r.StartDistanceM) / r.ElapsedS * 3.6 : 0;
                var (verdict, colour) = ("First ride on this stretch: the PB to beat next time", Palette.Dim);
                if (r.Ghost is { } g)
                    (verdict, colour) = g.GapS < 0
                        ? ($"NEW PB  {Format.Secs(-g.GapS)} faster than {Format.Clock(g.TimeS)}", Palette.Power)
                        : ($"{Format.Secs(g.GapS)} off your {g.Label} ({Format.Clock(g.TimeS)})", Palette.Behind);
                return new($"Finished  {Format.Clock(r.ElapsedS)}", verdict, colour,
                    $"{r.CourseName} · {Format.Fixed(r.AvgPowerW, 0)} W average · {Format.Fixed(avgKmh, 1)} km/h · {Format.Fixed(r.ClimbedM, 0)} m climbed");
            }
            case RidePhase.Aborted when since < AbortedShownS:
                return new("Ride ended", r.CourseName, Palette.Dim, r.Loop
                    ? $"{Math.Max(0, (int)r.Lap - 1)} laps · {Format.Clock(r.ElapsedS)} · {Format.Fixed(r.AvgPowerW, 0)} W average"
                    : $"{Format.Fixed(r.DistanceM / 1000, 2)} of {Format.Fixed(r.CourseDistanceM / 1000, 2)} km in {Format.Clock(r.ElapsedS)}");
        }
        return null;
    }

    // FillLaps is the pop-in: the best lap ever when it is from an earlier
    // ride (the ghost on a loop), this ride's last laps, the lap under way;
    // the fastest marked.
    void FillLaps(Ride r)
    {
        double best = double.MaxValue;
        foreach (var l in _laps)
            best = Math.Min(best, l.TimeS);
        bool earlier = r.Ghost is { } g && g.TimeS > 0 && g.TimeS < best - 0.05;
        if (earlier)
            Laps.Add(new($"PB  {r.Ghost!.Label}", Format.LapTime(r.Ghost.TimeS), true, false));
        for (int i = Math.Max(0, _laps.Count - 5); i < _laps.Count; i++)
            Laps.Add(new($"lap {_laps[i].Number}", Format.LapTime(_laps[i].TimeS), !earlier && _laps[i].TimeS == best, false));
        Laps.Add(new($"lap {r.Lap}", Format.LapTime(r.LapElapsedS), false, true));
    }
}
