using Xunit;

namespace Osscycler.Hud.Tests;

// Full resolution up to 1440p, then 1440 lines (owner, 2026-10-14).
public class RenderScaleTests
{
    [Fact]
    public void FullUpTo1440Lines()
    {
        Assert.Equal(1f, RenderScale.Of(900));
        Assert.Equal(1f, RenderScale.Of(1080));
        Assert.Equal(1f, RenderScale.Of(1440)); // 2560×1440, 3440×1440 ultrawide
    }

    [Fact]
    public void Above1440LinesRendersThat()
    {
        Assert.Equal(1440, RenderScale.Of(2160) * 2160, 2); // 4K
        Assert.Equal(1440, RenderScale.Of(2260) * 2260, 2); // a Retina Mac, full screen
        Assert.True(RenderScale.Of(2260) < 1);
    }
}
