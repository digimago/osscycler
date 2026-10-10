using System;
using System.Numerics;
using Osscycler.Avatar;
using Xunit;

namespace Osscycler.Hud.Tests;

public class AvatarTests
{
    [Theory]
    [InlineData(1.60f)]
    [InlineData(1.80f)]
    [InlineData(1.95f)]
    public void LegsKeepTheirLengthRoundTheCranks(float height)
    {
        var p = new Pose(height);
        float bottomBend = 0;
        for (int i = 0; i < 36; i++)
        {
            float a = i * MathF.PI / 18;
            p.Crank(a);
            foreach (var (hip, knee, ankle) in new[] { (p.HipL, p.KneeL, p.AnkleL), (p.HipR, p.KneeR, p.AnkleR) })
            {
                Assert.Equal(p.Thigh, Vector3.Distance(hip, knee), 3);
                Assert.Equal(p.Shank, Vector3.Distance(knee, ankle), 3);
                // The knee in front of the line from hip to ankle.
                var d = Vector3.Normalize(ankle - hip);
                var k = knee - hip - d * Vector3.Dot(knee - hip, d);
                Assert.True(k.Z < 0, $"knee behind at {a:F2}");
            }
            if (i == 18) // the right pedal at the bottom
                bottomBend = 180 - Angle(p.HipR - p.KneeR, p.AnkleR - p.KneeR);
        }
        // A saddle at the right height leaves the knee bent 25-40° at the
        // bottom of the stroke.
        Assert.InRange(bottomBend, 25, 40);
    }

    [Fact]
    public void HandsOnTheHoodsAndArmsTheirLength()
    {
        var p = new Pose(1.8f);
        Assert.Equal(p.UpperArm, Vector3.Distance(p.ShoulderR, p.ElbowR), 3);
        Assert.Equal(p.Forearm, Vector3.Distance(p.ElbowR, p.HandR), 3);
        Assert.True(p.ElbowR.Y < p.ShoulderR.Y && p.ElbowR.X > p.ShoulderR.X - 0.05f);
        Assert.InRange(p.Saddle.Y, 0.9f, 1.05f);
        Assert.InRange(p.Head.Y, 1.35f, 1.6f);
    }

    [Fact]
    public void CadenceFromSpeed()
    {
        Assert.Equal(0, Pose.CadenceFromSpeed(0.5f));
        Assert.InRange(Pose.CadenceFromSpeed(10), 88, 100);
        Assert.Equal(60, Pose.CadenceFromSpeed(3));
    }

    static float Angle(Vector3 a, Vector3 b) =>
        MathF.Acos(Math.Clamp(Vector3.Dot(Vector3.Normalize(a), Vector3.Normalize(b)), -1, 1)) * 180 / MathF.PI;
}
