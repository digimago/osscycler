using System;
using Xunit;

namespace Osscycler.Hud.Tests;

public class SolarTests
{
    // At noon the sun stands due south; its height there is 90° less the
    // latitude plus the declination (owner, 2026-10-15: the Vosges in
    // October had a sun high in the north-east).
    [Theory]
    [InlineData(48.0, 288, 33.0)] // the Vosges, 15 October: about -9° declination
    [InlineData(48.0, 172, 65.4)] // midsummer
    [InlineData(52.0, 355, 14.6)] // the Netherlands at midwinter
    public void NoonIsDueSouth(double lat, int doy, double height)
    {
        var (h, az) = Solar.Position(lat, doy, 12);
        Assert.InRange(az, 179, 181);
        Assert.InRange(h, height - 1, height + 1);
    }

    // Morning in the east of south, afternoon in the west; never north of
    // east or west by day in October at 48° N.
    [Fact]
    public void MorningEastAfternoonWest()
    {
        var (_, am) = Solar.Position(48, 288, 10);
        var (_, pm) = Solar.Position(48, 288, 16);
        Assert.InRange(am, 120, 170);
        Assert.InRange(pm, 190, 245);
        int low = 0;
        for (double r = 0; r < 1; r += 0.05)
        {
            for (double l = 0; l < 1; l += 0.05)
            {
                double hours = Solar.RideHours(48, 288, r, l);
                var (h, az) = Solar.Position(48, 288, hours);
                Assert.InRange(az, 100, 260);
                Assert.True(h > 5.5, $"the sun {h:F1}° up at {hours:F1} h");
                if (h < 15)
                    low++;
            }
        }
        // About one ride in four in the low light.
        Assert.InRange(low / 400.0, 0.2, 0.35);
    }

    // The light's yaw: the sun in the south shines north (yaw 0), in the
    // east it shines west (Godot's yaw turns -Z towards -X).
    [Fact]
    public void LightYawTurnsTheLightAwayFromTheSun()
    {
        Assert.Equal(0, Solar.LightYaw(180), 6);
        Assert.Equal(90, Solar.LightYaw(90), 6);
        Assert.Equal(270, Solar.LightYaw(270), 6);
        // The light's direction (Godot: -Z yawed) points away from the sun.
        foreach (double az in new[] { 100.0, 180, 250 })
        {
            double yaw = Solar.LightYaw(az) * Math.PI / 180;
            double east = -Math.Sin(yaw), south = -Math.Cos(yaw); // -Z yawed, in glTF axes
            double sunEast = Math.Sin(az * Math.PI / 180), sunSouth = -Math.Cos(az * Math.PI / 180);
            Assert.InRange(east * sunEast + south * sunSouth, -1.0001, -0.9999);
        }
    }

    [Fact]
    public void DayOfYearFromTheSeed()
    {
        Assert.Equal(288, Solar.DayOfYear(2026288));
        Assert.Equal(366, Solar.DayOfYear(999));
        Assert.Equal(1, Solar.DayOfYear(2026000));
    }
}
