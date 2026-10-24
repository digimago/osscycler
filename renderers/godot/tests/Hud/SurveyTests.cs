using System;
using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

// The camera looking at a spot from outside (view_camera, view_survey).
public class SurveyTests
{
    const double Eps = 1e-6;

    [Fact]
    public void StraightDownHasTheWayAheadAtTheTop()
    {
        // Riding east (90°): straight above the spot, yaw east, pitch -90,
        // from whichever side.
        foreach (var from in new[] { 180.0, 90, 0, -90 })
        {
            var o = Survey.Orbit(10, 5, -20, 90, from, 90, 50);
            Assert.Equal(10, o.X, 6);
            Assert.Equal(55, o.Y, 6);
            Assert.Equal(-20, o.Z, 6);
            Assert.Equal(90, o.Yaw, 6);
            Assert.Equal(-90, o.Pitch, 6);
        }
    }

    [Fact]
    public void FromASideLooksAtTheSpot()
    {
        // Riding north; from behind at 45° the camera stands south of the
        // spot (z grows southwards) and above it, facing north, down 45°.
        var o = Survey.Orbit(0, 0, 0, 0, 180, 45, 10);
        double h = 10 * Math.Cos(Math.PI / 4);
        Assert.Equal(0, o.X, 6);
        Assert.Equal(h, o.Z, 6);
        Assert.Equal(h, o.Y, 6);
        Assert.Equal(0, o.Yaw, 6);
        Assert.Equal(-45, o.Pitch, 6);
        // From the right of a rider going north: east of the spot, facing west.
        o = Survey.Orbit(0, 0, 0, 0, 90, 30, 10);
        Assert.True(o.X > 0 && Math.Abs(o.Z) < Eps);
        Assert.Equal(270, o.Yaw, 6);
    }

    [Fact]
    public void HeadingIsClockwiseFromNorth()
    {
        Assert.Equal(0, Survey.Heading(0, -1), 6);
        Assert.Equal(90, Survey.Heading(1, 0), 6);
        Assert.Equal(180, Survey.Heading(0, 1), 6);
        Assert.Equal(270, Survey.Heading(-1, 0), 6);
    }

    [Fact]
    public void DistanceFramesTheRadius()
    {
        double d = Survey.DistanceFor(40);
        Assert.Equal(40, d * Math.Tan(Survey.FovDeg / 2 * Math.PI / 180), 6);
    }
}
