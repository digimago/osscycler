using System;
using System.Collections.Generic;
using Osscycler.V1;

namespace Osscycler.Hud;

// Minimap is a course seen from above, north up (owner's list, item 5):
// its line from the course's profile positions (Course.profile_east_m,
// profile_north_m, every profile_step_m), thinned to what a small map
// shows, fitted into a square; where a distance along the course lies on
// it and which way the course runs there. Plain C#; StripsView draws it.
public sealed class Minimap
{
    // At most this many points are drawn: a corner of the screen shows no
    // more, and a 100 km course has 10,000 samples.
    public const int MaxPoints = 400;

    readonly float[] _e, _n;
    readonly double _step, _length;
    readonly bool _loop;
    readonly double _minE, _minN, _span;

    public readonly string CourseId;

    // Points are the course's line in the unit square (0..1, y down), each
    // with its distance along the course.
    public readonly List<(double X, double Y, double D)> Points = new();

    public Minimap(Course c)
    {
        CourseId = c.Id;
        _e = new float[c.ProfileEastM.Count];
        _n = new float[c.ProfileNorthM.Count];
        c.ProfileEastM.CopyTo(_e, 0);
        c.ProfileNorthM.CopyTo(_n, 0);
        _step = c.ProfileStepM > 0 ? c.ProfileStepM : 10;
        _length = c.DistanceM;
        _loop = c.Loop;
        int n = Math.Min(_e.Length, _n.Length);
        if (n == 0)
        {
            _span = 1;
            return;
        }
        double minE = double.PositiveInfinity, maxE = double.NegativeInfinity, minN = minE, maxN = maxE;
        for (int i = 0; i < n; i++)
        {
            (minE, maxE) = (Math.Min(minE, _e[i]), Math.Max(maxE, _e[i]));
            (minN, maxN) = (Math.Min(minN, _n[i]), Math.Max(maxN, _n[i]));
        }
        // A square around the course, the course centred in it.
        _span = Math.Max(1, Math.Max(maxE - minE, maxN - minN));
        _minE = (minE + maxE - _span) / 2;
        _minN = (minN + maxN - _span) / 2;
        int every = Math.Max(1, (n + MaxPoints - 1) / MaxPoints);
        for (int i = 0; i < n; i += every)
        {
            var (x, y) = Unit(_e[i], _n[i]);
            Points.Add((x, y, i * _step));
        }
        if ((n - 1) % every != 0)
        {
            var (x, y) = Unit(_e[n - 1], _n[n - 1]);
            Points.Add((x, y, (n - 1) * _step));
        }
    }

    (double X, double Y) Unit(double e, double n) => ((e - _minE) / _span, 1 - (n - _minN) / _span);

    // At is where distance d (m along the course) lies, in the unit square,
    // and the course's heading there (radians clockwise from north).
    public (double X, double Y, double Heading) At(double d)
    {
        int n = Math.Min(_e.Length, _n.Length);
        if (n == 0)
            return (0.5, 0.5, 0);
        if (_loop && _length > 0)
            d = ((d % _length) + _length) % _length;
        double f = Math.Clamp(d / _step, 0, n - 1);
        int i = Math.Min((int)f, n - 2);
        if (i < 0)
            return (Unit(_e[0], _n[0]).X, Unit(_e[0], _n[0]).Y, 0);
        double u = f - i;
        double e = _e[i] + u * (_e[i + 1] - _e[i]), no = _n[i] + u * (_n[i + 1] - _n[i]);
        // The heading over ±20 m, so a jog between two samples doesn't spin it.
        int a = Math.Max(0, i - 2), b = Math.Min(n - 1, i + 3);
        double heading = Math.Atan2(_e[b] - _e[a], _n[b] - _n[a]);
        var (x, y) = Unit(e, no);
        return (x, y, heading);
    }

    // SpanM is the side of the square, in metres (for a scale).
    public double SpanM => _span;
}
