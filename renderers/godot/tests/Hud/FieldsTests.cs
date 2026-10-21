using Osscycler.Plants;
using Xunit;

namespace Osscycler.Hud.Tests;

// The world's sown fields reach the shaders as one code each: the crop and
// the way its rows run (world/ground.go writes "maize 3" and so on).
public class FieldsTests
{
    [Theory]
    [InlineData("ploughed 0", 16)]
    [InlineData("maize 3", 27)]
    [InlineData("wheat 7", 39)]
    [InlineData("potatoes 5", 45)]
    public void ASownFieldHasItsCode(string cls, int code) => Assert.Equal((byte)code, Fields.Code(cls));

    [Theory]
    [InlineData("farmland")]
    [InlineData("dune grass")]
    [InlineData("maize 8")]
    [InlineData("rye 2")]
    [InlineData("maize")]
    public void AnythingElseHasNone(string cls) => Assert.Null(Fields.Code(cls));
}
