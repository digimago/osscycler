using Osscycler.Hud;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

// A course with a run-in and a lap (owner, 2026-10-13), as the TUI's
// TestLapOnACourseWithARunIn: its lap from 500 to 3000 m, the course
// running on 100 m into the lap's start again.
public class LapsTests
{
    static readonly Course C = new() { DistanceM = 3100, LapStartM = 500, LapEndM = 3000 };

    [Fact]
    public void PastTheEndALapBack()
    {
        Assert.True(Laps.Lapped(C));
        Assert.False(Laps.Lapped(new Course { DistanceM = 3100 }));
        Assert.Equal(400, Laps.CoursePos(C, 400));
        Assert.Equal(3050, Laps.CoursePos(C, 3050));
        Assert.Equal(650, Laps.CoursePos(C, 3150), 6);
        Assert.Equal(150, Laps.CoursePos(new Course { DistanceM = 400, Loop = true }, 550), 6);
    }

    [Fact]
    public void TheLineAtTheLapsStartOnceThenAtItsEndEachLap()
    {
        Assert.True(Laps.Line(C, 490, 510));
        Assert.False(Laps.Line(C, 1000, 1010));
        Assert.True(Laps.Line(C, 2990, 3010));
        Assert.False(Laps.Line(C, 3050, 3060));
        Assert.True(Laps.Line(C, 5490, 5510));
    }

    [Fact]
    public void ToGoSaysTheLapOrTheWayToIt()
    {
        Assert.Equal(("TO THE LAP", 300), Laps.ToGo(new Ride { Loop = true, LapLengthM = 2500, LapDistanceM = -300 }));
        Assert.Equal(("LAP TO GO", 1500), Laps.ToGo(new Ride { Loop = true, LapLengthM = 2500, LapDistanceM = 1000 }));
        Assert.Equal(("TO GO", 600), Laps.ToGo(new Ride { CourseDistanceM = 1000, DistanceM = 400 }));
    }
}
