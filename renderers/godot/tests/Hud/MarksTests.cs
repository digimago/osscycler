using System;
using System.IO;
using System.Text.Json;
using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

public class MarksTests
{
    [Fact]
    public void LatLonIsTheWorldsFrame()
    {
        // course.Project: x east, z south of the origin, 6371 km earth.
        var (lat, lon) = Marks.LatLon(52, 6, 0, -1000);
        Assert.Equal(52 + 1000 / (6371000.0 * Math.PI / 180), lat, 9);
        Assert.Equal(6, lon, 9);
        (lat, lon) = Marks.LatLon(52, 6, 1000, 0);
        Assert.Equal(52, lat, 9);
        Assert.Equal(6 + 1000 / (6371000.0 * Math.PI / 180 * Math.Cos(52 * Math.PI / 180)), lon, 9);
    }

    [Fact]
    public void AMarkIsALineOfItsOwn()
    {
        var dir = Path.Combine(Path.GetTempPath(), "marks-" + Guid.NewGuid().ToString("N"));
        try
        {
            var m = new Mark { CourseId = "posbank", AlongM = 5790.5, Rider = [1.5, 2, -3], Shot = "x.png" };
            var path = Marks.Append(dir, m);
            Marks.Append(dir, new Mark { CourseId = "figure-8" });
            var lines = File.ReadAllLines(path);
            Assert.Equal(2, lines.Length);
            using var doc = JsonDocument.Parse(lines[0]);
            Assert.Equal("posbank", doc.RootElement.GetProperty("course_id").GetString());
            Assert.Equal(5790.5, doc.RootElement.GetProperty("along_m").GetDouble());
            Assert.Equal(-3, doc.RootElement.GetProperty("rider")[2].GetDouble());
            Assert.Equal("figure-8", JsonDocument.Parse(lines[1]).RootElement.GetProperty("course_id").GetString());
        }
        finally
        {
            Directory.Delete(dir, true);
        }
    }

    [Fact]
    public void NamesKeepMarksApart()
    {
        var t = new DateTimeOffset(2026, 10, 10, 15, 30, 12, 345, TimeSpan.FromHours(2));
        Assert.Equal("2026-10-10-153012-345", Marks.Name(t));
        Assert.NotEqual(Marks.Name(t), Marks.Name(t.AddMilliseconds(1)));
    }

    [Fact]
    public void SpaceIsFreeOnlyWhenNothingTakesKeys()
    {
        var h = new Controller(new Calls());
        Assert.True(h.Free);
        h.Key("?", null, 0); // the key help
        Assert.False(h.Free);
        h.Key("x", null, 0); // closes it
        Assert.True(h.Free);
        h.Key("m", null, 0); // the menu
        Assert.False(h.Free);
    }
}
