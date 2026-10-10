using System;

namespace Osscycler.Head;

// GhostTrack carries the ghost forward between the core's updates (owner,
// 2026-10-10: on the trainer the ghost was jumpy). The core moves the
// ghost on its 4 Hz ride clock; between its reports the ghost goes on at
// its speed, taken from the change in the core's own ride clock (not when
// the reports arrive, which wobbles with the network), and a correction at
// a report blends out over BlendS rather than snapping. Plain C# apart
// from Godot, tested.
public sealed class GhostTrack
{
    public const double BlendS = 0.4, MaxCarryS = 0.5;

    double _dist = double.NaN, _rideS, _at, _err;
    bool _measured; // a speed taken from two reports

    // Speed is the ghost's, m/s.
    public double Speed { get; private set; }

    // Report takes the core's ghost distance at its ride clock rideS,
    // arriving at clock (this program's seconds).
    public void Report(double dist, double rideS, double clock)
    {
        if (dist == _dist && rideS == _rideS)
            return;
        if (!double.IsNaN(_dist))
        {
            double shown = At(clock);
            if (rideS > _rideS)
            {
                double v = (dist - _dist) / (rideS - _rideS);
                if (v >= 0 && v < 30)
                    Speed = _measured ? Speed + (v - Speed) * 0.5 : v;
                _measured = true;
            }
            _err = Math.Abs(shown - dist) < 20 ? shown - dist : 0;
        }
        (_dist, _rideS, _at) = (dist, rideS, clock);
    }

    // At is where the ghost is at clock.
    public double At(double clock)
    {
        if (double.IsNaN(_dist))
            return double.NaN;
        double dt = Math.Max(0, clock - _at);
        return _dist + Speed * Math.Min(dt, MaxCarryS) + _err * Math.Max(0, 1 - dt / BlendS);
    }

    // Reset forgets the ghost (none, or a new ride).
    public void Reset()
    {
        (_dist, _rideS, _at, _err) = (double.NaN, 0, 0, 0);
        (Speed, _measured) = (0, false);
    }

    // Stand holds it where it was reported (not riding: armed, paused).
    public void Stand() => (Speed, _err) = (0, 0);
}
