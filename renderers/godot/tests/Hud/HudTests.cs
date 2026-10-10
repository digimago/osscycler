using System;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class FormatTests
{
    [Theory]
    [InlineData(0, "0:00")]
    [InlineData(65.9, "1:05")]
    [InlineData(3725, "1:02:05")]
    public void Clock(double s, string want) => Assert.Equal(want, Format.Clock(s));

    [Fact]
    public void LapTimeHasTenths() => Assert.Equal("0:39.4", Format.LapTime(39.42));

    [Theory]
    [InlineData(4.24, "4.2 s")]
    [InlineData(65, "1:05")]
    public void Secs(double s, string want) => Assert.Equal(want, Format.Secs(s));
}

public class PaletteTests
{
    [Fact]
    public void GradeFollowsTheTuisHeatScale()
    {
        Assert.Equal(Rgb.Hex(0x2c7bb6), Palette.Grade(0));
        Assert.Equal(Rgb.Hex(0x90eb9d), Palette.Grade(7));
        Assert.Equal(Rgb.Hex(0x7f0000), Palette.Grade(25));
        Assert.Equal(Palette.Descent, Palette.Grade(-3));
        // Halfway between the 0 and 3 % stops.
        Assert.Equal(new Rgb(0x16, 0x91, 0xc0), Palette.Grade(1.5));
    }

    [Fact]
    public void Zones()
    {
        Assert.Equal(Rgb.Hex(0x7f7f7f), Palette.Zone(0.5));
        Assert.Equal(Rgb.Hex(0xffcc3f), Palette.Zone(1.0));
        Assert.Equal(Rgb.Hex(0xff330c), Palette.Zone(1.3));
    }
}

public class CadenceTests
{
    [Fact]
    public void WeightedByHowLongEachReadingHeld()
    {
        var c = new Cadence5s();
        c.Add(0, 60, true);
        c.Add(4, 90, true);
        // 60 held from 0 (cut to 1) to 4, 90 from 4 to 6: (3*60 + 2*90) / 5.
        Assert.True(c.Average(6, out var rpm));
        Assert.Equal(72, rpm, 6);
    }

    [Fact]
    public void InvalidReadingsLeftOut()
    {
        var c = new Cadence5s();
        c.Add(0, 0, false);
        Assert.False(c.Average(1, out _));
        c.Add(1, 80, true);
        Assert.True(c.Average(3, out var rpm));
        Assert.Equal(80, rpm, 6);
    }
}

public class TilesTests
{
    static State Riding(Ride r) => new() { Ride = r, Trainer = new Trainer { PowerW = 231, CadenceRpm = 88 } };

    [Fact]
    public void ScreensAndDefaults()
    {
        var (screen, tiles) = Tiles.For(new State(), null);
        Assert.Equal(Tiles.Dashboard, screen);
        Assert.Equal(new[] { "POWER", "HEART RATE", "CADENCE", "SPEED" }, tiles.ConvertAll(t => t.Label));
        Assert.Equal("--", tiles[0].Value);

        (screen, tiles) = Tiles.For(Riding(new Ride { Phase = RidePhase.Riding, CourseDistanceM = 5000, DistanceM = 1200, GradePct = 3, ElapsedS = 125 }), 87.6);
        Assert.Equal(Tiles.Ride, screen);
        Assert.Equal(new[] { "POWER", "HEART RATE", "CADENCE 5s", "GRADE", "TIME", "TO GO" }, tiles.ConvertAll(t => t.Label));
        Assert.Equal(new[] { "231", "--", "88", "3.0", "2:05", "3.80" }, tiles.ConvertAll(t => t.Value));
        Assert.Equal(Rgb.Hex(0x00a6ca), tiles[3].ValueColor);

        var w = new State { Workout = new WorkoutProgress { Phase = WorkoutPhase.Running, TargetW = 200, FtpW = 200, SegmentLabel = "Interval 2", SegmentRemainingS = 61, TargetCadence = 95 } };
        (screen, tiles) = Tiles.For(w, null);
        Assert.Equal(Tiles.Workout, screen);
        Assert.Equal("200", tiles[0].Value);
        Assert.Equal(Rgb.Hex(0xffcc3f), tiles[0].ValueColor);
        Assert.Equal("rpm · aim 95", tiles[2].Unit);
        Assert.Equal(("INTERVAL 2", "1:01"), (tiles[3].Label, tiles[3].Value));
    }

    [Fact]
    public void TimeAgainstTheGhost()
    {
        var r = new Ride { Phase = RidePhase.Riding, ElapsedS = 300, Ghost = new RideGhost { Label = "PB 7 Oct", GapS = -4.24 } };
        var t = Tiles.Make("time", Riding(r), null);
        Assert.Equal(("TIME vs PB 7 Oct", "5:00", "-4.2 s ahead"), (t.Label, t.Value, t.Unit));
        Assert.Equal(Palette.Ahead, t.UnitColor);

        r.Ghost.GapS = 12;
        t = Tiles.Make("time", Riding(r), null);
        Assert.Equal(("+12.0 s behind", Palette.Behind), (t.Unit, t.UnitColor));

        r.Loop = true;
        r.Lap = 3;
        r.LapElapsedS = 41;
        r.Phase = RidePhase.Armed;
        r.Ghost.TimeS = 39.4;
        t = Tiles.Make("time", Riding(r), null);
        Assert.Equal(("LAP 3", "0:41", "lap PB 0:39.4"), (t.Label, t.Value, t.Unit));
    }

    [Fact]
    public void LapToGoOnALoop()
    {
        var t = Tiles.Make("to_go", Riding(new Ride { Phase = RidePhase.Riding, Loop = true, CourseDistanceM = 400, DistanceM = 150 }), null);
        Assert.Equal(("LAP TO GO", "0.25"), (t.Label, t.Value));
    }
}

public class StripsTests
{
    // A course of 1 km: flat for 500 m, then 8 % (40 m up).
    static Course Course(bool loop = false)
    {
        var c = new Course { DistanceM = 1000, ProfileStepM = 10, Loop = loop };
        for (int i = 0; i <= 100; i++)
        {
            c.ProfileElevationM.Add(i <= 50 ? 10 : 10 + (i - 50) * 0.8f);
            c.ProfileGradePct.Add(i < 50 ? 0 : 8);
        }
        return c;
    }

    [Fact]
    public void LookAheadCellsGradesAndGhost()
    {
        var (cells, ghost) = Strips.LookAhead(Course(), 400, 520);
        Assert.Equal(25, cells.Count);
        Assert.Equal(("0%", Palette.Grade(0)), (cells[0].Label, cells[0].Color));
        Assert.Equal(("", Palette.Grade(8)), (cells[11].Label, cells[11].Color));
        Assert.Equal("8%", cells[15].Label); // every 50 m
        Assert.Equal(0.48, ghost!.Value, 6);
        Assert.Null(Strips.LookAhead(Course(), 400, 700).GhostAt);
        Assert.Null(Strips.LookAhead(Course(), 400, -1).GhostAt);
    }

    [Fact]
    public void FinishAndLap()
    {
        var (cells, _) = Strips.LookAhead(Course(), 900, -1);
        Assert.Equal(Strips.CellKind.Grade, cells[9].Kind);
        Assert.Equal((Strips.CellKind.Finish, "FINISH"), (cells[10].Kind, cells[10].Label));
        Assert.Equal((Strips.CellKind.Finish, ""), (cells[11].Kind, cells[11].Label));

        (cells, _) = Strips.LookAhead(Course(loop: true), 905, -1);
        Assert.Equal((Strips.CellKind.Lap, "LAP"), (cells[9].Kind, cells[9].Label));
        Assert.Equal(Strips.CellKind.Grade, cells[10].Kind); // the next lap, flat again
        Assert.Equal(Palette.Grade(0), cells[10].Color);
    }

    [Fact]
    public void ProfileColumns()
    {
        var cols = Strips.Profile(Course(), 11);
        Assert.Equal(11, cols.Count);
        Assert.Equal(0.04, cols[0].Height, 6); // the lowest still shows
        Assert.Equal(1, cols[10].Height, 6);
        Assert.Equal(Palette.Grade(8), cols[7].Color);
    }
}

public class WorkoutStripTests
{
    [Fact]
    public void ZonesFreeAndRamps()
    {
        var w = new WorkoutDef { DurationS = 300 };
        w.Timeline.Add(new WorkoutSegment { StartS = 0, DurationS = 100, Free = true });
        w.Timeline.Add(new WorkoutSegment { StartS = 100, DurationS = 100, FromFtp = 0.5, ToFtp = 1.1 });
        w.Timeline.Add(new WorkoutSegment { StartS = 200, DurationS = 100, FromFtp = 1.5, ToFtp = 1.5 });
        var cols = Strips.Workout(w, 30);
        Assert.Equal(30, cols.Count);
        Assert.Equal(Rgb.Hex(0x505050), cols[0].Color);       // free: low grey
        Assert.Equal(Palette.Zone(0.53), cols[10].Color);      // the ramp's start
        Assert.Equal(Palette.Zone(1.07), cols[19].Color);      // its end
        Assert.Equal(1, cols[25].Height, 6);                   // the highest fills the strip
        Assert.Equal(0.35 / 1.5, cols[0].Height, 6);
    }
}

public class MomentsTests
{
    [Fact]
    public void ReadyThenResult()
    {
        var m = new Moments();
        var st = new State { Ride = new Ride { Phase = RidePhase.Armed, CourseId = "c", CourseName = "Hills", Ghost = new RideGhost { Label = "PB 7 Oct", TimeS = 301 } } };
        m.Update(st, null, 0, 0);
        Assert.Equal(("Pedal to start", "racing your PB 7 Oct (5:01)"), (m.Big!.Title, m.Big.Sub));

        st.Ride.Phase = RidePhase.Riding;
        m.Update(st, null, 10, 1);
        Assert.Null(m.Big);

        st.Ride.Phase = RidePhase.Finished;
        st.Ride.ElapsedS = 296;
        st.Ride.Ghost.GapS = -5;
        m.Update(st, null, 0, 100);
        Assert.Equal(("Finished  4:56", "NEW PB  5.0 s faster than 5:01"), (m.Big!.Title, m.Big.Sub));
        Assert.Equal(Palette.Power, m.Big.SubColor);
        m.Update(st, null, 0, 100 + Moments.ResultShownS + 1);
        Assert.Null(m.Big);

        st.Ride.Ghost = null;
        st.Ride.Phase = RidePhase.Armed;
        m.Update(st, null, 0, 200);
        st.Ride.Phase = RidePhase.Finished;
        m.Update(st, null, 0, 300);
        Assert.StartsWith("First ride on this stretch", m.Big!.Sub);
    }

    [Fact]
    public void Announcements()
    {
        // Announce: shown until it expires, over a workout's message.
        var m = new Moments();
        long now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        var st = new State { Announcement = new Announcement { Text = "GRADE -5 %", UntilUnixMs = now + 60000 } };
        m.Update(st, null, 0, 0);
        Assert.Equal("GRADE -5 %", m.Message);
        st.Workout = new WorkoutProgress { Message = "Ramp to 85 %" };
        m.Update(st, null, 0, 1);
        Assert.Equal("GRADE -5 %\nRamp to 85 %", m.Message);
        st.Announcement.UntilUnixMs = now - 1000;
        m.Update(st, null, 0, 2);
        Assert.Equal("Ramp to 85 %", m.Message);
    }

    [Fact]
    public void Place()
    {
        var c = new Course { Attribution = "© OpenStreetMap contributors" };
        c.PlaceSigns.Add(new PlaceSign { DistanceM = 1000, Name = "Rheden" });
        c.PlaceSigns.Add(new PlaceSign { DistanceM = 1200, Name = "De Steeg" });
        var m = new Moments();
        var st = new State { Ride = new Ride { Phase = RidePhase.Riding, CourseId = "c", ElapsedS = 5 } };
        m.Update(st, c, 999, 0);
        Assert.Equal("", m.Place);
        m.Update(st, c, 1100, 1);
        Assert.Equal("Rheden", m.Place);
        st.Ride.ElapsedS = 25;
        m.Update(st, c, 1300, 2);
        Assert.Equal("De Steeg", m.Place);
        m.Update(st, c, 1700, 3);
        Assert.Equal("", m.Place);
    }

    [Fact]
    public void Warnings()
    {
        var m = new Moments();
        m.Update(new State
        {
            Radio = new Radio { Present = false },
            Trainer = new Trainer { UserConfigRequired = true, TargetPowerLimit = TargetPowerLimit.SpeedTooLow },
            HeartRate = new HeartRate { Sensor = new Sensor { Status = SensorStatus.Lost } },
            Recording = new Recording { Error = "disk full" },
        }, null, 0, 0);
        Assert.Equal(new[]
        {
            "ANT+ stick not found: plug it in", "heart rate strap lost", "the trainer wants your weight (TUI: p)",
            "ERG: speed too low for the target, shift up", "recording failed: disk full",
        }, m.Warnings);
        m.Update(new State { Radio = new Radio { Present = true } }, null, 0, 1);
        Assert.Empty(m.Warnings);
    }

    [Fact]
    public void LapPopIn()
    {
        var m = new Moments();
        var r = new Ride { Phase = RidePhase.Armed, CourseId = "oval", Loop = true, Ghost = new RideGhost { Label = "lap", TimeS = 44.0 } };
        var st = new State { Ride = r };
        m.Update(st, null, 0, 0);
        r.Phase = RidePhase.Riding;
        r.Lap = 2;
        r.LastLap = new RideLap { Number = 1, TimeS = 46.2 };
        m.Update(st, null, 0, 50);
        Assert.Equal(new[] { "PB  lap", "lap 1", "lap 2" }, m.Laps.ConvertAll(l => l.Label));
        Assert.True(m.Laps[0].Fastest);
        Assert.True(m.Laps[2].Running);

        r.Lap = 3;
        r.LastLap = new RideLap { Number = 2, TimeS = 43.5 }; // faster than the PB
        m.Update(st, null, 0, 95);
        Assert.Equal(new[] { "lap 1", "lap 2", "lap 3" }, m.Laps.ConvertAll(l => l.Label));
        Assert.True(m.Laps[1].Fastest);
        m.Update(st, null, 0, 95 + Moments.LapsShownS + 1);
        Assert.Empty(m.Laps);
    }
}
