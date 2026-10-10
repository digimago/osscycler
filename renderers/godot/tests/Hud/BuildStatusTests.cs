using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

public class BuildStatusTests
{
    [Fact]
    public void ReadsTheBuildersProgressLines()
    {
        Assert.Equal("map data 3/8 · about 4 min left",
            BuildStatus.Parse("progress: {\"phase\":\"map data\",\"done\":3,\"total\":8,\"frac\":0.375,\"left_s\":245}"));
        Assert.Equal("building 45% · about 20 s left",
            BuildStatus.Parse("progress: {\"phase\":\"building\",\"done\":0,\"total\":0,\"frac\":0.45,\"left_s\":17}"));
        Assert.Equal("ground model", BuildStatus.Parse("progress: {\"phase\":\"ground model\",\"done\":0,\"total\":0,\"frac\":0,\"left_s\":-1}"));
        Assert.Null(BuildStatus.Parse("phase: building"));
        Assert.Null(BuildStatus.Parse("progress: not json"));
    }

    [Theory]
    [InlineData(4, "a few seconds left")]
    [InlineData(42, "about 45 s left")]
    [InlineData(70, "about a minute left")]
    [InlineData(440, "about 7 min left")]
    public void SaysTheTimeLeftAsPeopleDo(double s, string want) => Assert.Equal(want, BuildStatus.Left(s));
}
