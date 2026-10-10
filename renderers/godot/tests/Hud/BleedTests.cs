using Osscycler.Plants;
using Xunit;

namespace Osscycler.Hud.Tests;

public class BleedTests
{
    // A green blade on transparent: every texel takes a drawn colour, the
    // transparent ones stay transparent, so a mipmap's average of a blade's
    // edge stays green instead of darkening towards black.
    [Fact]
    public void TransparentTexelsTakeTheNearestDrawnColour()
    {
        const int n = 8;
        var px = new byte[4 * n * n];
        void Set(int x, int y, byte r, byte g, byte b) { int i = 4 * (y * n + x); px[i] = r; px[i + 1] = g; px[i + 2] = b; px[i + 3] = 255; }
        Set(1, 1, 80, 200, 40);
        Set(6, 6, 160, 220, 90);
        Bleed.Pad(px, n);
        for (int i = 0; i < n * n; i++)
        {
            int x = i % n, y = i / n;
            bool drawn = (x, y) == (1, 1) || (x, y) == (6, 6);
            Assert.Equal(drawn ? 255 : 0, px[4 * i + 3]);
            Assert.True(px[4 * i + 1] >= 200, $"texel {x},{y} green {px[4 * i + 1]}");
        }
        Assert.Equal(80, px[4 * (0 * n + 0)]); // nearest the first blade
        Assert.Equal(160, px[4 * (7 * n + 7)]); // nearest the second
    }

    [Fact]
    public void AnEmptyPictureStaysAsItIs()
    {
        var px = new byte[4 * 4 * 4];
        Bleed.Pad(px, 4);
        Assert.All(px, b => Assert.Equal(0, b));
    }
}
