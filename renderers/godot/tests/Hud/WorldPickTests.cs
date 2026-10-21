using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

// A ride's world is loaded when it isn't the one on screen: another
// directory, or the same directory built again since (owner, 2026-10-12:
// the figure 8 rebuilt while the view was open, then ridden, showed the
// old build).
public class WorldPickTests
{
    const string Dir = "/w/figure-8";

    [Fact]
    public void ARebuildInTheSameDirectoryIsLoaded() =>
        Assert.True(WorldPick.NeedsLoad(Dir, "g10-new", Dir, "4-old", "", "", false));

    [Fact]
    public void TheWorldShownStays() =>
        Assert.False(WorldPick.NeedsLoad(Dir, "g10-new", Dir, "g10-new", "", "", false));

    [Fact]
    public void AnotherDirectoryIsLoaded() =>
        Assert.True(WorldPick.NeedsLoad("/w/posbank", "g10-p", Dir, "g10-new", "", "", false));

    [Fact]
    public void AnUnknownStampLeavesTheSameDirectoryAlone() =>
        Assert.False(WorldPick.NeedsLoad(Dir, "", Dir, "4-old", "", "", false));

    [Fact]
    public void NotWhileLoadingSwappingOrAfterAFailure()
    {
        Assert.False(WorldPick.NeedsLoad(null, "", Dir, "4-old", "", "", false));
        Assert.False(WorldPick.NeedsLoad(Dir, "g10-new", Dir, "4-old", Dir, "", false));
        Assert.False(WorldPick.NeedsLoad(Dir, "g10-new", Dir, "4-old", "", Dir, false));
        Assert.False(WorldPick.NeedsLoad(Dir, "g10-new", Dir, "4-old", "", "", true));
    }
}
