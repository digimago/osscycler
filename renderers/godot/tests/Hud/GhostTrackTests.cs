using System;
using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

public class GhostTrackTests
{
    // A ghost at 8 m/s, reported every 0.25 s of the core's ride clock,
    // the reports arriving up to 60 ms early or late: drawn at 60 fps it
    // moves on far more evenly than the old way (snapping to each report,
    // its speed from when reports arrived), which made it jumpy on the oval
    // (owner, 2026-10-10).
    [Fact]
    public void MovesEvenlyThroughJitteryReports()
    {
        var g = new GhostTrack();
        var rng = new Random(7);
        const double v = 8, frame = 1 / 60.0;
        double nextReport = 0, rideS = 0, last = double.NaN, worst = 0;
        // The old way, alongside.
        double oldDist = double.NaN, oldAt = 0, oldSpeed = 0, oldLast = double.NaN, oldWorst = 0;
        for (double clock = 0; clock < 20; clock += frame)
        {
            if (clock >= nextReport)
            {
                double d = v * rideS;
                g.Report(d, rideS, clock);
                if (!double.IsNaN(oldDist) && clock - oldAt > 0.05)
                    oldSpeed += ((d - oldDist) / (clock - oldAt) - oldSpeed) * 0.5;
                (oldDist, oldAt) = (d, clock);
                rideS += 0.25;
                nextReport = rideS + (rng.NextDouble() - 0.5) * 0.12;
            }
            double at = g.At(clock), oldPos = oldDist + oldSpeed * Math.Min(clock - oldAt, 0.5);
            if (!double.IsNaN(last) && clock > 2) // after settling
            {
                worst = Math.Max(worst, Math.Abs(at - last - v * frame));
                oldWorst = Math.Max(oldWorst, Math.Abs(oldPos - oldLast - v * frame));
            }
            (last, oldLast) = (at, oldPos);
        }
        Assert.True(worst < 0.05, $"a frame's step off by up to {worst:F3} m (a step is {v * frame:F3} m)");
        Assert.True(worst < oldWorst / 4, $"off by {worst:F3} m, the old way {oldWorst:F3} m");
        Assert.InRange(g.Speed, 7.9, 8.1);
    }

    // A correction blends out rather than snapping: the ghost goes on from
    // where it was shown, and is on the new track BlendS later.
    [Fact]
    public void CorrectionsBlendOut()
    {
        var g = new GhostTrack();
        g.Report(0, 0, 0);
        g.Report(2, 0.25, 0.25); // 8 m/s
        Assert.Equal(8, g.Speed, 6);
        double before = g.At(0.5);
        g.Report(3, 0.5, 0.5); // 1 m behind where it was carried
        Assert.Equal(before, g.At(0.5), 6);
        Assert.Equal(3 + g.Speed * 0.45, g.At(0.95), 6);
    }
}
