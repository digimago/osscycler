using System;
using System.Collections.Generic;
using Osscycler.V1;

namespace Osscycler.Hud;

// RideHistory is an activity's samples as fetched so far, a second of
// riding time each; -1 where there was no reading. Immutable: With gives
// the next one, so the HUD can draw one while the next is fetched.
public sealed class RideHistory
{
    public readonly string Name;
    readonly int[] _power, _hr;

    public RideHistory(string name, int[]? power = null, int[]? hr = null) =>
        (Name, _power, _hr) = (name, power ?? Array.Empty<int>(), hr ?? Array.Empty<int>());

    public int Count => _power.Length;

    // With adds a fetched piece: a piece of another activity from its
    // start replaces this one; one that doesn't carry on from Count (an
    // overlap or a gap) is left out, and fetched again.
    public RideHistory With(GetActivityHistoryResponse r)
    {
        if (r.Name != Name)
            return r.FromS == 0 ? new RideHistory(r.Name).With(r) : this;
        if (r.FromS != Count)
            return this;
        var p = new int[Count + r.PowerW.Count];
        var h = new int[Count + r.PowerW.Count];
        Array.Copy(_power, p, Count);
        Array.Copy(_hr, h, Math.Min(_hr.Length, Count));
        for (int i = 0; i < r.PowerW.Count; i++)
        {
            p[Count + i] = r.PowerW[i];
            h[Count + i] = i < r.HeartRateBpm.Count ? r.HeartRateBpm[i] : -1;
        }
        return new RideHistory(Name, p, h);
    }

    // Column is one column of a graph: average power and heart rate over
    // its seconds (0: no reading).
    public readonly record struct Column(double PowerW, double HeartRate);

    // Columns squeezes the whole activity into n columns; with the scale's
    // top (a round 50 W over the highest column, at least 100) and the
    // heart rate's range (0, 0 without any).
    public (List<Column> Cols, double TopW, double MinHr, double MaxHr) Columns(int n)
    {
        var cols = new List<Column>(n);
        double maxP = 0, minHr = double.PositiveInfinity, maxHr = double.NegativeInfinity;
        for (int c = 0; c < n; c++)
        {
            int a = c * Count / n, b = Math.Max(a + 1, (c + 1) * Count / n);
            double ps = 0, hs = 0;
            int pn = 0, hn = 0;
            for (int i = a; i < b && i < Count; i++)
            {
                if (_power[i] >= 0)
                    (ps, pn) = (ps + _power[i], pn + 1);
                if (_hr[i] > 0)
                    (hs, hn) = (hs + _hr[i], hn + 1);
            }
            var col = new Column(pn > 0 ? ps / pn : 0, hn > 0 ? hs / hn : 0);
            cols.Add(col);
            maxP = Math.Max(maxP, col.PowerW);
            if (col.HeartRate > 0)
                (minHr, maxHr) = (Math.Min(minHr, col.HeartRate), Math.Max(maxHr, col.HeartRate));
        }
        if (double.IsInfinity(minHr))
            (minHr, maxHr) = (0, 0);
        return (cols, Math.Max(100, Math.Ceiling(maxP / 50) * 50), minHr, maxHr);
    }
}
