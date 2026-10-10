using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

public class CoverTests
{
    [Theory]
    [InlineData("grebbeberg", "figure-8", true)] // another course's world still shown
    [InlineData("grebbeberg", "", true)]         // nothing shown yet
    [InlineData("grebbeberg", "grebbeberg", false)]
    [InlineData("", "figure-8", false)]          // free riding: any world will do
    public void CoversAWorldThatIsntTheRides(string ride, string world, bool want) =>
        Assert.Equal(want, Cover.Needed(ride, world));

    [Fact]
    public void ComesInQuicklyAndLiftsGently()
    {
        double a = 0;
        for (int i = 0; i < 21; i++)
            a = Cover.Step(a, true, 1 / 60.0);
        Assert.Equal(1, a, 3); // 0.35 s
        a = Cover.Step(a, false, 0.3);
        Assert.Equal(0.5, a, 3);
        Assert.Equal(0, Cover.Step(a, false, 1), 3);
    }

    [Fact]
    public void SaysWhatIsLoading()
    {
        Assert.Equal("Loading Posbank Loop…", Cover.Text("Posbank Loop", ""));
        Assert.Equal("Loading Posbank Loop\nbuilding the world: map data", Cover.Text("Posbank Loop", "building the world: map data"));
    }
}
