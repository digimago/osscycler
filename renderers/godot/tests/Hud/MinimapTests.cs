using System;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class MinimapTests
{
    // A 400 m loop: 100 m north, east, south and back west, a sample every
    // 10 m.
    static Course Square()
    {
        var c = new Course { Id = "sq", DistanceM = 400, ProfileStepM = 10, Loop = true };
        for (int i = 0; i <= 40; i++)
        {
            double d = i * 10;
            var (e, n) = d switch
            {
                <= 100 => (0.0, d),
                <= 200 => (d - 100, 100.0),
                <= 300 => (100.0, 300 - d),
                _ => (400 - d, 0.0),
            };
            c.ProfileEastM.Add((float)e);
            c.ProfileNorthM.Add((float)n);
        }
        return c;
    }

    [Fact]
    public void FitsTheCourseNorthUp()
    {
        var m = new Minimap(Square());
        Assert.Equal(41, m.Points.Count);
        Assert.Equal(100, m.SpanM);
        Assert.All(m.Points, p => Assert.InRange(p.X, 0, 1));
        var (x0, y0, h0) = m.At(0);
        Assert.Equal((0.0, 1.0), (x0, y0)); // south-west corner: bottom left
        Assert.Equal(0, h0, 3); // heading north
        var (x, y, h) = m.At(150); // half way along the north side, heading east
        Assert.Equal(0.5, x, 6);
        Assert.Equal(0.0, y, 6);
        Assert.Equal(Math.PI / 2, h, 3);
        Assert.Equal(m.At(50), m.At(450)); // round the loop
    }

    [Fact]
    public void ThinsLongCourses()
    {
        var c = new Course { Id = "long", DistanceM = 100_000, ProfileStepM = 10 };
        for (int i = 0; i <= 10_000; i++)
        {
            c.ProfileEastM.Add(i);
            c.ProfileNorthM.Add(0);
        }
        var m = new Minimap(c);
        Assert.InRange(m.Points.Count, 2, Minimap.MaxPoints + 1);
        Assert.Equal(100_000, m.Points[^1].D); // the finish kept
    }
}
