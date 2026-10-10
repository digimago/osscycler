using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Osscycler.Head;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

// Calls records the commands the head sends; Fail makes them fail.
sealed class Calls : ICommands
{
    public readonly List<string> Sent = new();
    public bool Fail;

    Task Add(string s)
    {
        lock (Sent)
            Sent.Add(s);
        return Fail ? Task.FromException(new InvalidOperationException("refused")) : Task.CompletedTask;
    }

    public Task StopRide() => Add("stop ride");
    public Task SetPaused(bool paused) => Add($"paused {paused}");
    public Task SetDifficulty(double pct) => Add($"difficulty {pct}");
    public Task StopWorkout() => Add("stop workout");
    public Task SkipSegment() => Add("skip");
    public Task SetIntensity(double pct) => Add($"intensity {pct}");

    public List<Course> Courses = new()
    {
        new Course { Id = "oval-400", Name = "Oval 400 m", DistanceM = 400, Loop = true },
        new Course { Id = "posbank", Name = "Posbank Loop", DistanceM = 32000, GainM = 162 },
    };
    public List<RideResult> Results = new()
    {
        new RideResult { CourseId = "posbank", CourseName = "Posbank Loop", FinishedUnixMs = 1_760_000_000_000, ElapsedS = 4000, PersonalBest = true, File = "2026-10-09-120000.fit" },
    };
    public List<Activity> Activities = new()
    {
        new Activity { Name = "2026-10-09-120000.fit", StartUnixMs = 1_760_000_000_000 },
        new Activity { Name = "2026-10-08-090000.fit", StartUnixMs = 1_759_900_000_000 },
    };
    public List<WorkoutDef> Workouts = new() { new WorkoutDef { Id = "intervals", Name = "Intervals", DurationS = 1800 } };

    public Task<IList<Course>> ListCourses() => Task.FromResult<IList<Course>>(Courses);
    public Task<IList<RideResult>> ListResults() => Task.FromResult<IList<RideResult>>(Results);
    public Task<IList<Activity>> ListActivities() => Task.FromResult<IList<Activity>>(Activities);
    public Task<IList<WorkoutDef>> ListWorkouts() => Task.FromResult<IList<WorkoutDef>>(Workouts);
    public string View = "chase";
    public Task<RiderProfile> SetView(string view)
    {
        View = view;
        return Add("view " + view).ContinueWith(t => { t.Wait(); return new RiderProfile { View = view }; });
    }
    public WorkoutDef? SavedWorkout;
    public Task<string> SaveWorkout(string id, WorkoutDef w)
    {
        SavedWorkout = w;
        return Add("save workout " + id).ContinueWith(t => { t.Wait(); return id == "" ? "my-workout" : id; });
    }
    public Task StartRide(string courseId) => Add("ride " + courseId);
    public Task StartRideAgainst(string courseId, long finishedUnixMs) => Add($"race {courseId} {finishedUnixMs}");
    public Task StartWorkout(string id) => Add("workout " + id);
    public Task SetPower(double watts) => Add($"power {watts}");
    public Task SetFtp(double watts) => Add($"ftp {watts}");
    public Task SetGrade(double pct) => Add($"grade {pct}");
    public Task SetLevel(double pct) => Add($"level {pct}");
    public Task ReleaseControl() => Add("release");
    public Task StartCalibration() => Add("calibrate");
    public Task CancelCalibration() => Add("cancel calibration");
    public RiderProfile Profile = new() { SuggestedFtpW = 190 };
    public Task<RiderProfile> SetProfile(double? weightKg, double? ftpW, double? heightCm)
    {
        Add($"profile {weightKg} {ftpW} {heightCm}");
        if (weightKg is double w) Profile.WeightKg = w;
        if (ftpW is double f) Profile.FtpW = f;
        if (heightCm is double h) Profile.HeightCm = h;
        return Task.FromResult(Profile.Clone());
    }
    public Task<string> EndActivity(bool discard)
    {
        Add("end " + (discard ? "discard" : "save"));
        return Task.FromResult(discard ? "" : "ride.fit");
    }
    public Task<string> SaveActivity(string name)
    {
        Add("save " + name);
        return Task.FromResult("/tmp/" + name);
    }
}

public class HeadTests
{
    static State Riding(double difficulty = 55, bool loop = false) =>
        new() { Ride = new Ride { Phase = RidePhase.Riding, DifficultyPct = difficulty, Loop = loop } };

    [Fact]
    public void DifficultyStepsToMultiplesOfTen()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("+", Riding(55), 0);
        h.Key("-", Riding(55), 0);
        h.Key("=", Riding(100), 0);
        h.Key("_", Riding(0), 0);
        Assert.Equal(new[] { "difficulty 60", "difficulty 50", "difficulty 100", "difficulty 0" }, c.Sent);
    }

    [Fact]
    public void AbortNeedsTwoPresses()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("x", Riding(), 10);
        Assert.Empty(c.Sent);
        Assert.Equal("press x again to abort the ride", h.Asking(11));
        h.Key("x", Riding(), 12);
        Assert.Equal(new[] { "stop ride" }, c.Sent);
        Assert.Equal("", h.Asking(12));

        h.Key("x", Riding(loop: true), 20);
        Assert.Equal("press x again to end the ride", h.Asking(20));
        h.Key("x", Riding(loop: true), 20 + Controller.AbortConfirmS + 1); // too late: asks again
        Assert.Single(c.Sent);
    }

    [Fact]
    public void AWorkoutHasTheKeysEvenOnALoop()
    {
        var c = new Calls();
        var h = new Controller(c);
        var st = Riding(loop: true);
        st.Workout = new WorkoutProgress { Phase = WorkoutPhase.Running, IntensityPct = 100 };
        h.Key("+", st, 0);
        h.Key("-", st, 0);
        h.Key("n", st, 0);
        h.Key("x", st, 1);
        h.Key("x", st, 2);
        Assert.Equal(new[] { "intensity 101", "intensity 99", "skip", "stop workout" }, c.Sent);
    }

    [Fact]
    public void TheResultClosesOnXOrEnter()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("enter", new State { Ride = new Ride { Phase = RidePhase.Finished } }, 0);
        h.Key("x", new State { Workout = new WorkoutProgress { Phase = WorkoutPhase.Finished } }, 0);
        Assert.Equal(new[] { "stop ride", "stop workout" }, c.Sent);
    }

    [Fact]
    public void HelpOpensAndAnyKeyClosesIt()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("?", Riding(), 0);
        Assert.Equal("COURSE RIDE", h.Help!.Value.Title);
        h.Key("+", Riding(), 0); // closes, doesn't act
        Assert.Null(h.Help);
        Assert.Empty(c.Sent);
        h.Key("q", Riding(), 0); // on a ride: the menu, pausing (quit from there)
        Assert.False(h.Quit);
        Assert.NotNull(h.List());
    }

    [Fact]
    public void ARefusedCommandShowsANotice()
    {
        var c = new Calls { Fail = true };
        var h = new Controller(c);
        h.Key("+", Riding(), 0);
        for (int i = 0; i < 50 && h.Notice(1) == ""; i++)
            Thread.Sleep(10);
        Assert.Equal("set difficulty failed: refused", h.Notice(1));
        Assert.Equal("", h.Notice(1 + Controller.NoticeS + 1));
    }
}

public class MenuTests
{
    // Loaded waits for the lists (they arrive on other threads).
    static void Loaded(Controller h)
    {
        for (int i = 0; i < 100 && (h.List() is not { } l || l.Rows.Count == 0 || l.Subtitle.StartsWith("loading")); i++)
            Thread.Sleep(5);
    }

    static void Settle() => Thread.Sleep(30); // commands are sent on other threads too

    [Fact]
    public void TheMenuGivesWayWhenSomethingElseTakesTheTrainer()
    {
        // Owner, 2026-10-10: a script took over the trainer and the menu stayed.
        var h = new Controller(new Calls());
        h.Started(new State());
        h.Tick(new State(), 0);
        Assert.NotNull(h.List());
        var grade = new State { Control = new TrainerControl { Mode = ControlMode.Grade, Target = 4 } };
        h.Tick(grade, 1);
        Assert.Null(h.List());
        // Opened again while it holds a grade (esc, pausing): it stays, until
        // the control changes.
        h.Key("esc", grade, 2);
        h.Tick(grade, 3);
        Assert.NotNull(h.List());
        h.Tick(new State { Control = new TrainerControl { Mode = ControlMode.Level, Target = 40 } }, 4);
        Assert.Null(h.List());
    }

    [Fact]
    public void TheMenuOpensAtStartUnlessARideIsOn()
    {
        var h = new Controller(new Calls());
        h.Started(new State());
        Assert.Equal("OSSCYCLER", h.List()!.Title);
        Assert.Contains("OpenStreetMap", h.List()!.Credit); // the map's credit lives in the menu
        h.Key("esc", new State(), 0);
        Assert.Null(h.List());

        var busy = new Controller(new Calls());
        busy.Started(new State { Ride = new Ride { Phase = RidePhase.Riding } });
        Assert.Null(busy.List());
    }

    [Fact]
    public void FreeRideRoundATrack()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("m", new State(), 0);
        h.Key("f", new State(), 0);
        for (int i = 0; i < 100 && h.List()!.Rows.Count < 2; i++)
            Thread.Sleep(5);
        var l = h.List()!;
        Assert.Equal("FREE RIDE", l.Title);
        Assert.Equal(new[] { "Just the numbers", "Oval 400 m" }, l.Rows.Select(r => r.Label));
        h.Key("2", new State(), 0);
        Settle();
        Assert.Equal(new[] { "ride oval-400" }, c.Sent);
        Assert.Null(h.List());
    }

    [Fact]
    public void CoursesHistoryActivities()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("r", new State(), 0);
        Loaded(h);
        var l = h.List()!;
        Assert.Equal(("RIDES", 0), (l.Title, l.Tab));
        Assert.True(l.Rows[1].Star); // the Posbank Loop has a PB
        h.Key("down", new State(), 0);
        h.Key("enter", new State(), 0);
        Settle();
        Assert.Equal("ride posbank", c.Sent[^1]);

        h.Key("r", new State(), 0);
        h.Key("tab", new State(), 0);
        Assert.Equal(1, h.List()!.Tab);
        h.Key("enter", new State(), 0);
        Settle();
        Assert.Equal("race posbank 1760000000000", c.Sent[^1]);

        h.Key("r", new State(), 0);
        h.Key("tab", new State(), 0); // HISTORY: the tab is remembered
        Assert.Equal(2, h.List()!.Tab);
        h.Key("enter", new State(), 0); // a course ride in it: raced
        Settle();
        Assert.Equal("race posbank 1760000000000", c.Sent[^1]);
        h.Key("r", new State(), 0);
        h.Key("down", new State(), 0);
        h.Key("enter", new State(), 0); // none: saved
        Settle();
        Assert.Equal("save 2026-10-08-090000.fit", c.Sent[^1]);
        Assert.StartsWith("saved /tmp/", h.Notice(1));
    }

    [Fact]
    public void WorkoutsFixedPowerAndFtp()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("w", new State(), 0);
        Loaded(h);
        Assert.Equal(new[] { "Fixed power (ERG)", "Intervals" }, h.List()!.Rows.Select(r => r.Label));
        h.Key("enter", new State(), 0);
        Assert.Equal("Fixed power in watts", h.Prompt!.Label);
        foreach (var k in new[] { "2", "x", "5", "backspace", "0" })
            h.Key(k, new State(), 0);
        Assert.Equal("20", h.Prompt!.Value); // x isn't typed; backspace took the 5
        h.Key("0", new State(), 0);
        h.Key("enter", new State(), 0);
        Settle();
        Assert.Equal("power 200", c.Sent[^1]);

        h.Key("w", new State(), 0);
        h.Key("f", new State(), 0);
        foreach (var k in new[] { "2", "1", "0", "enter" })
            h.Key(k, new State(), 0);
        Settle();
        Assert.Equal("ftp 210", c.Sent[^1]);

        h.Key("w", new State(), 0);
        h.Key("down", new State(), 0);
        h.Key("enter", new State(), 0);
        Settle();
        Assert.Equal("workout intervals", c.Sent[^1]);
    }
}

public class SetupTests
{
    static void Settle() => Thread.Sleep(30);

    [Fact]
    public void OnboardingAsksForWhatIsMissing()
    {
        var c = new Calls();
        var h = new Controller(c);
        var st = new State { Profile = new RiderProfile { SuggestedFtpW = 190 } };
        st.Profile.Missing.Add("weight_kg");
        h.Tick(st, 0);
        Assert.StartsWith("Your weight", h.Dialog(st, 0)!.Big);
        foreach (var k in new[] { "7", "5", "enter" })
            h.Key(k, st, 0);
        Settle();
        Assert.StartsWith("Your height", h.Dialog(st, 0)!.Big);
        h.Key("enter", st, 0); // optional: skipped
        Assert.StartsWith("Your FTP: 190_", h.Dialog(st, 0)!.Big); // the core's suggestion
        h.Key("backspace", st, 0);
        h.Key("5", st, 0);
        h.Key("enter", st, 0);
        Settle();
        Assert.Null(h.Dialog(st, 0));
        Assert.Equal(new[] { "profile 75  ", "profile  195 " }, c.Sent);
        Assert.Equal("profile saved", h.Notice(1));
    }

    [Fact]
    public void OnboardingPutOffStaysOff()
    {
        var h = new Controller(new Calls());
        var st = new State { Profile = new RiderProfile() };
        st.Profile.Missing.Add("weight_kg");
        h.Tick(st, 0);
        h.Key("esc", st, 0);
        h.Tick(st, 1);
        Assert.Null(h.Dialog(st, 1));
        h.Key("m", st, 2); // but the menu's Profile opens it
        h.Key("p", st, 2);
        Assert.Equal("RIDER PROFILE", h.Dialog(st, 2)!.Title);
    }

    [Fact]
    public void ManualControl()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("g", new State(), 0);
        Assert.Equal(("Grade in %", "3"), (h.Prompt!.Label, h.Prompt.Value));
        h.Key("enter", new State(), 0); // the suggestion as it is
        h.Key("l", new State(), 0);
        h.Key("4", new State(), 0); // typing replaces the suggestion (30)
        h.Key("5", new State(), 0);
        h.Key("enter", new State(), 0);
        h.Key("g", new State(), 0);
        h.Key("-", new State(), 0);
        h.Key("2", new State(), 0);
        h.Key("-", new State(), 0); // only at the start
        h.Key("enter", new State(), 0);
        var on = new State { Control = new TrainerControl { Mode = ControlMode.Power, Target = 203 } };
        h.Key("+", on, 0);
        h.Key("-", on, 0);
        h.Key("x", on, 0);
        Settle();
        Assert.Equal(new[] { "grade 3", "level 45", "grade -2", "power 205", "power 200", "release" }, c.Sent);
        Assert.Equal("ERG 203 W", Controller.ControlText(on));
    }

    [Fact]
    public void EndingARide()
    {
        var c = new Calls();
        var h = new Controller(c);
        var st = new State { Recording = new Recording { Active = true, TimerS = 600, DistanceM = 5000 } };
        h.Key("e", st, 0);
        Assert.Equal(("END RIDE?", "10:00 riding · 5.00 km"), (h.Dialog(st, 0)!.Title, h.Dialog(st, 0)!.Big));
        h.Key("d", st, 1);
        Assert.Equal("press d again to delete this ride", h.Dialog(st, 1)!.Big);
        h.Key("d", st, 2);
        Settle();
        Assert.Equal(new[] { "end discard" }, c.Sent);
        Assert.Equal("ride discarded", h.Notice(3));

        h.Key("e", st, 10);
        h.Key("enter", st, 10);
        Settle();
        Assert.Equal("end save", c.Sent[^1]);
    }

    [Fact]
    public void CalibrationCountsDownThenStarts()
    {
        var c = new Calls();
        var h = new Controller(c);
        var st = new State { Trainer = new Trainer { Sensor = new Sensor { Status = SensorStatus.Connected } } };
        h.Key("c", st, 0); // the trainer doesn't ask: c does nothing
        Assert.Null(h.Dialog(st, 0));
        h.Key("C", st, 0);
        Assert.Equal("10", h.Dialog(st, 0)!.Big);
        Assert.Equal("4", h.Dialog(st, 6.5)!.Big);
        h.Tick(st, 9);
        Assert.Empty(c.Sent);
        h.Tick(st, 10);
        Settle();
        Assert.Equal(new[] { "calibrate" }, c.Sent);

        st.Trainer.Calibration = new Calibration { Phase = CalibrationPhase.InProgress, SpeedCondition = CalibrationCondition.TooLow, TargetSpeedMps = 8.888 };
        st.Trainer.SpeedMps = 5;
        Assert.Equal("PEDAL UP to 32 km/h · 18.0 km/h", h.Dialog(st, 11)!.Big);
        h.Key("esc", st, 12);
        Settle();
        Assert.Equal("cancel calibration", c.Sent[^1]);
    }

}

public class LayoutTests
{
    static State Riding() => new() { Ride = new Ride { Phase = RidePhase.Riding, CourseId = "posbank", CourseDistanceM = 32000 } };

    [Fact]
    public void ArrangingTilesIsKeptOnThisMachine()
    {
        var dir = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "hudtest-" + Guid.NewGuid().ToString("N"));
        var path = System.IO.Path.Combine(dir, "hud.json");
        try
        {
            var h = new Controller(new Calls(), path);
            var st = Riding();
            h.Key("o", st, 0);
            var l = h.List()!;
            Assert.Equal("COURSE RIDE", l.Subtitle);
            Assert.Equal(new[] { "1", "2", "3", "4", "5" }, l.Rows.Take(5).Select(r => r.Key));
            Assert.Equal(Layout.Labels[Layout.LookAhead], l.Rows[^2].Label);
            // Climbed (hidden, the 9th choice) to the second place; power
            // down one; grade hidden; the look-ahead strip hidden.
            for (int i = 0; i < 8; i++)
                h.Key("down", st, 0);
            h.Key("2", st, 0);
            Assert.Equal(1, h.List()!.Selected);
            h.Key("up", st, 0);
            h.Key("shift+down", st, 0); // power below climbed, the cursor with it
            h.Key("down", st, 0);
            h.Key("down", st, 0);
            h.Key("down", st, 0);
            h.Key(" ", st, 0); // grade
            for (int i = 0; i < 20; i++)
                h.Key("down", st, 0);
            h.Key("up", st, 0);
            h.Key(" ", st, 0); // the look-ahead
            h.Key("z", st, 0);
            h.Key("enter", st, 0);
            Assert.Null(h.List());
            var want = new[] { "climbed", "power", "heart_rate", "cadence_5s", "time", "to_go" };
            Assert.Equal(want, h.Layout.Shown(Tiles.Ride));
            Assert.False(h.Layout.Showing(Layout.LookAhead));
            Assert.True(h.Layout.Medium);
            Assert.Equal(want, Tiles.For(st, null, h.Layout).Tiles.Select(t => t.Label == "CLIMBED" ? "climbed" : t.Label == "POWER" ? "power" : t.Label == "HEART RATE" ? "heart_rate" : t.Label == "CADENCE 5s" ? "cadence_5s" : t.Label == "TO GO" ? "to_go" : "time"));

            // Saved, and read back by the next start; the dashboard keeps
            // its defaults.
            var again = new Controller(new Calls(), path);
            Assert.Equal(want, again.Layout.Shown(Tiles.Ride));
            Assert.Equal(Layout.Defaults[Tiles.Dashboard], again.Layout.Shown(Tiles.Dashboard));
            Assert.True(again.Layout.Medium);

            // Esc cancels; r goes back to the defaults.
            again.Key("o", st, 0);
            again.Key(" ", st, 0);
            again.Key("esc", st, 0);
            Assert.Equal(want, again.Layout.Shown(Tiles.Ride));
            again.Key("o", st, 0);
            again.Key("r", st, 0);
            again.Key("enter", st, 0);
            Assert.Equal(Layout.Defaults[Tiles.Ride], again.Layout.Shown(Tiles.Ride));
            Assert.True(again.Layout.Showing(Layout.LookAhead));
        }
        finally
        {
            if (System.IO.Directory.Exists(dir))
                System.IO.Directory.Delete(dir, true);
        }
    }

    [Fact]
    public void ArrangingKeepsAtLeastOneTileAndAtMostEight()
    {
        var h = new Controller(new Calls());
        var st = new State();
        h.Key("o", st, 0);
        for (int i = 0; i < 4; i++)
        {
            h.Key(" ", st, 0);
            h.Key("down", st, 0);
        }
        // Three hidden, then the last one stays.
        Assert.Equal("keep at least one tile", h.Notice(0));
        h.Key("enter", st, 0);
        Assert.Equal(new[] { "speed" }, h.Layout.Shown(Tiles.Dashboard));
    }

    [Fact]
    public void BrokenLayoutIsReportedAndNotOverwritten()
    {
        var dir = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "hudtest-" + Guid.NewGuid().ToString("N"));
        var path = System.IO.Path.Combine(dir, "hud.json");
        System.IO.Directory.CreateDirectory(dir);
        try
        {
            System.IO.File.WriteAllText(path, "{ not json");
            var h = new Controller(new Calls(), path);
            Assert.Contains("won't be saved", h.Notice(0));
            h.Key("z", new State(), 0);
            Assert.True(h.Layout.Medium);
            Assert.Equal("{ not json", System.IO.File.ReadAllText(path));
        }
        finally
        {
            System.IO.Directory.Delete(dir, true);
        }
    }
}

public class WorkoutFormTests
{
    [Theory]
    [InlineData("45s", 45)]
    [InlineData("10m", 600)]
    [InlineData("1m30s", 90)]
    [InlineData("1:30", 90)]
    [InlineData("1:00:00", 3600)]
    [InlineData("1h2m3s", 3723)]
    [InlineData("1.5m", 90)]
    public void DurationsAsTheTextFormat(string s, double want)
    {
        Assert.Equal(want, WorkoutForm.ParseDuration(s));
        Assert.Equal(want, WorkoutForm.ParseDuration(WorkoutForm.FormatDuration(want)));
    }

    [Fact]
    public void DurationWithoutUnitAsksForOne()
    {
        var e = Assert.Throws<FormatException>(() => WorkoutForm.ParseDuration("30"));
        Assert.Contains("add a unit", e.Message);
    }

    [Fact]
    public void StepsRetypesAndTypes()
    {
        var f = new WorkoutForm("", null); // warmup, 3 x intervals, cooldown; the cursor on the warmup
        Assert.Equal(WorkoutForm.Field.Type, f.Current);
        f.Key("right", 0); // duration 10m
        f.Key("+", 0);
        Assert.Equal(660, f.Draft.Blocks[0].DurationS);
        f.Key("-", 0);
        f.Key("-", 0); // under 10 minutes the step is 30 s
        Assert.Equal(570, f.Draft.Blocks[0].DurationS);
        f.Key("down", 0); // the intervals: the cursor on its repeat
        Assert.Equal(WorkoutForm.Field.Repeat, f.Current);
        f.Key("5", 0);
        f.Key("enter", 0);
        Assert.Equal(5u, f.Draft.Blocks[1].Repeat);
        f.Key("t", 0); // Intervals → Ramp, keeping its values
        Assert.Equal("Ramp", f.Draft.Blocks[1].Type);
        f.Key("T", 0);
        Assert.Equal("IntervalsT", f.Draft.Blocks[1].Type);
        Assert.Equal(5u, f.Draft.Blocks[1].Repeat);
        // A bad value says why and stays.
        f.Key("right", 0); // on time
        f.Key("3", 0);
        f.Key("0", 0);
        f.Key("enter", 0);
        Assert.Contains("add a unit", f.Notice);
        Assert.Equal("30", f.Buffer);
        f.Key("s", 0);
        f.Key("enter", 0);
        Assert.Null(f.Buffer);
        Assert.Equal(30, f.Draft.Blocks[1].OnDurationS);
        // Add, copy, move, delete.
        f.Key("c", 0);
        Assert.Equal(4, f.Draft.Blocks.Count);
        Assert.Equal(2, f.Block);
        f.Key("shift+up", 0);
        Assert.Equal(1, f.Block);
        f.Key("d", 0);
        f.Key("a", 0);
        Assert.Equal("SteadyState", f.Draft.Blocks[2].Type);
        var t = WorkoutForm.Tidy(f.Draft);
        Assert.Equal(570 + 5 * (30 + 180) + 300 + 300, t.DurationS);
        Assert.Equal(0, t.Blocks[1].DurationS); // the intervals' duration from its ramp days is dropped
    }

    [Fact]
    public void EditorSavesThroughTheCoreAndDiscardsOnTwoEscapes()
    {
        var c = new Calls();
        var h = new Controller(c);
        var st = new State();
        h.Key("w", st, 0);
        h.Key("n", st, 0);
        var v = h.Form(st)!;
        Assert.Equal("NEW WORKOUT", v.Title);
        Assert.Equal(3, v.Blocks.Count);
        Assert.False(v.Bad);
        Assert.Equal("39:00 · 3 blocks · ~71 % FTP average", v.Status);
        h.Key("up", st, 0); // from the first block: the description,
        h.Key("up", st, 0); // the author
        h.Key("enter", st, 0);
        foreach (var k in "me")
            h.Key(k.ToString(), st, 0);
        h.Key("enter", st, 0);
        h.Key("s", st, 0);
        Settle();
        h.Tick(st, 1);
        Assert.Null(h.Form(st));
        Assert.Equal("save workout ", c.Sent[^1]);
        Assert.Equal("me", c.SavedWorkout!.Author);
        Assert.Empty(c.SavedWorkout.Timeline); // derived: the core makes its own
        Assert.Equal("saved my-workout", h.Notice(1));
        Assert.Equal("WORKOUTS", h.List()!.Title);

        // e edits the chosen one; esc once asks, twice discards.
        h.Key("down", st, 2);
        h.Key("e", st, 2);
        Assert.Equal("EDIT intervals", h.Form(st)!.Title);
        h.Key("a", st, 2);
        h.Key("esc", st, 2);
        Assert.NotNull(h.Form(st));
        Assert.Equal("press esc again to discard your changes", h.Asking(2));
        h.Key("esc", st, 3);
        Assert.Null(h.Form(st));
    }

    [Fact]
    public void AnEmptyWorkoutCantBeSaved()
    {
        var f = new WorkoutForm("x", new WorkoutDef { Name = "x" });
        Assert.Equal(WorkoutForm.Action.None, f.Key("s", 0));
        Assert.Equal("can't save: no blocks", f.Notice);
    }

    static void Settle() => Thread.Sleep(50);
}

// Two heads on one core: what the other head does makes this one's
// half-done input stale.
public class OtherHeadTests
{
    static State Free(bool recording = true) => new()
    {
        Profile = new RiderProfile { WeightKg = 75, FtpW = 200 },
        Recording = new Recording { Active = recording },
        Trainer = new Trainer { Sensor = new Sensor { Status = SensorStatus.Connected } },
    };

    [Fact]
    public void ProfileGivenElsewhereClosesOnboarding()
    {
        var h = new Controller(new Calls());
        var st = Free();
        st.Profile = new RiderProfile { SuggestedFtpW = 190 };
        st.Profile.Missing.Add("weight_kg");
        h.Tick(st, 0);
        Assert.NotNull(h.Dialog(st, 0));
        h.Tick(Free(), 1);
        Assert.Null(h.Dialog(Free(), 1));
        Assert.Equal("profile set", h.Notice(1));
    }

    [Fact]
    public void EndQuestionGoesWhenTheRecordingEndedElsewhere()
    {
        var h = new Controller(new Calls());
        h.Key("e", Free(), 0);
        Assert.Equal("END RIDE?", h.Dialog(Free(), 0)!.Title);
        h.Tick(Free(recording: false), 1);
        Assert.Null(h.Dialog(Free(false), 1));
    }

    [Fact]
    public void CountdownStopsWhenARideStartsElsewhereAndTheMenuGives()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("C", Free(), 0);
        var riding = Free();
        riding.Ride = new Ride { Phase = RidePhase.Riding };
        h.Tick(riding, 1);
        h.Tick(riding, 12);
        Assert.DoesNotContain("calibrate", c.Sent);
        Assert.Equal("calibration aborted: the trainer is in use", h.Notice(12));

        var g = new Controller(c);
        g.Key("m", Free(), 0);
        g.Tick(Free(), 0);
        Assert.NotNull(g.List());
        g.Tick(riding, 1);
        Assert.Null(g.List());
    }
}

public class ViewTests
{
    static State With(string view) => new() { Profile = new RiderProfile { WeightKg = 75, FtpW = 200, View = view } };

    [Fact]
    public void ChaseByDefaultAndVSavesItInTheProfile()
    {
        var c = new Calls();
        var h = new Controller(c);
        Assert.True(h.Chase(null)); // without a core: this machine's layout, chase by default
        Assert.True(h.Chase(With("chase")));
        h.Key("v", With("chase"), 0);
        Thread.Sleep(30);
        Assert.Equal("view eyes", c.Sent[^1]);
        Assert.False(h.Chase(With("chase"))); // switched at once, before the core's profile says so
        Assert.False(h.Chase(With("eyes")));
        Assert.True(h.Layout.Chase); // the local layout untouched
        Assert.True(h.Chase(With("chase"))); // the profile changed elsewhere: it wins
    }

    [Fact]
    public void WithoutACoreVKeepsItHere()
    {
        var h = new Controller(new Calls());
        h.Key("v", new State(), 0);
        Assert.False(h.Chase(new State()));
        Assert.Equal("eyes", h.Layout.View);
    }

    [Fact]
    public void ProfileEditEndsWithTheView()
    {
        var c = new Calls();
        c.Profile.View = "chase"; // a core that keeps the view (an older one doesn't: then no view step)
        var h = new Controller(c);
        var st = With("chase");
        h.Key("m", st, 0); // the menu's Profile
        h.Key("p", st, 0);
        h.Key("enter", st, 0); // weight
        Thread.Sleep(30);
        h.Key("enter", st, 0); // height, empty: skipped
        h.Key("enter", st, 0); // FTP
        Thread.Sleep(30);
        Assert.Contains("behind the rider", h.Dialog(st, 0)!.Big);
        h.Key("right", st, 0);
        h.Key("enter", st, 0);
        Thread.Sleep(30);
        Assert.Equal("view eyes", c.Sent[^1]);
        Assert.Null(h.Dialog(st, 1));
    }
}

