using System.Collections.Generic;

namespace Osscycler.Hud;

// Cadence5s is the cadence over the last five seconds, each reading
// weighted by how long it held, invalid readings left out: steadier than
// the trainer's 4 Hz value (tui/cadence.go).
public sealed class Cadence5s
{
    public const double WindowS = 5;
    readonly List<(double At, double Rpm, bool Ok)> _s = new();

    // Add records the cadence in a state received at time now (seconds);
    // ok is false when the trainer reports none.
    public void Add(double now, double rpm, bool ok)
    {
        double from = now - WindowS;
        // Keep the reading that holds into the window, drop older ones.
        while (_s.Count > 1 && _s[1].At <= from)
            _s.RemoveAt(0);
        _s.Add((now, rpm, ok));
    }

    // Average at time now; false without valid readings in the window.
    public bool Average(double now, out double rpm)
    {
        double from = now - WindowS, sum = 0, weight = 0;
        for (int i = 0; i < _s.Count; i++)
        {
            double to = i + 1 < _s.Count ? _s[i + 1].At : now;
            double start = _s[i].At < from ? from : _s[i].At;
            double d = to - start;
            if (d > 0 && _s[i].Ok)
            {
                sum += _s[i].Rpm * d;
                weight += d;
            }
        }
        rpm = weight > 0 ? sum / weight : 0;
        return weight > 0;
    }
}
