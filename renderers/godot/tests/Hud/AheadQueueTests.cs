using Osscycler.Head;
using Xunit;

namespace Osscycler.Hud.Tests;

public class AheadQueueTests
{
    static readonly (string, string)[] Menu = { ("figure-8", "Figure 8"), ("oval-400", "Oval"), ("posbank", "Posbank Loop"), ("velp", "Velp loop") };

    [Fact]
    public void BuildsRecentlyRiddenCoursesFirstThenTheMenusOrder()
    {
        var q = new AheadQueue();
        q.Courses(Menu, new[] { ("posbank", 100L), ("velp", 300L), ("posbank", 200L) });
        var order = new System.Collections.Generic.List<string>();
        while (q.Next() is { } c)
        {
            order.Add(c.Id);
            q.Done(c.Id, AheadQueue.Outcome.Ready);
        }
        Assert.Equal(new[] { "velp", "posbank", "figure-8", "oval-400" }, order);
    }

    [Fact]
    public void TriesEachCourseOnceASessionEvenWhenListedAgain()
    {
        var q = new AheadQueue();
        q.Courses(Menu, System.Array.Empty<(string, long)>());
        q.Done("figure-8", AheadQueue.Outcome.Ready);
        q.Done("oval-400", AheadQueue.Outcome.Skipped);
        q.Courses(Menu, System.Array.Empty<(string, long)>()); // listed again a while later
        Assert.Equal("posbank", q.Next()?.Id);
    }

    [Fact]
    public void YieldsToARideUnderWayButNotToOneThatHasEnded()
    {
        Assert.False(AheadQueue.Riding(null));
        Assert.False(AheadQueue.Riding(new Osscycler.V1.State()));
        Assert.True(AheadQueue.Riding(new Osscycler.V1.State { Ride = new() { Phase = Osscycler.V1.RidePhase.Armed, CourseId = "velp" } }));
        Assert.True(AheadQueue.Riding(new Osscycler.V1.State { Ride = new() { Phase = Osscycler.V1.RidePhase.Riding, CourseId = "velp" } }));
        // An aborted or finished ride stays in the state, its result shown.
        Assert.False(AheadQueue.Riding(new Osscycler.V1.State { Ride = new() { Phase = Osscycler.V1.RidePhase.Aborted, CourseId = "velp" } }));
        Assert.False(AheadQueue.Riding(new Osscycler.V1.State { Ride = new() { Phase = Osscycler.V1.RidePhase.Finished, CourseId = "velp" } }));
        Assert.True(AheadQueue.Riding(new Osscycler.V1.State { Workout = new() { Phase = Osscycler.V1.WorkoutPhase.Paused } }));
        Assert.False(AheadQueue.Riding(new Osscycler.V1.State { Workout = new() { Phase = Osscycler.V1.WorkoutPhase.Finished } }));
        Assert.True(AheadQueue.Riding(new Osscycler.V1.State { Control = new() { Mode = Osscycler.V1.ControlMode.Grade } }));
    }

    [Fact]
    public void SaysWhatItIsBuildingAndWhatWaitsForTheRide()
    {
        var q = new AheadQueue();
        Assert.Equal("", q.Status("", ""));
        q.Courses(Menu, System.Array.Empty<(string, long)>());
        q.Done("figure-8", AheadQueue.Outcome.Ready);
        Assert.Equal("3D worlds ahead: Oval (2 of 4) · map data 3/8", q.Status("Oval", "map data 3/8"));
        Assert.Equal("", q.Status("", "")); // between builds: quiet
        q.Done("oval-400", AheadQueue.Outcome.Ready);
        q.Done("posbank", AheadQueue.Outcome.Skipped);
        q.Done("velp", AheadQueue.Outcome.Ready);
        Assert.Equal("3D worlds ahead: 3 of 4 ready, 1 built when ridden (map data to fetch)", q.Status("", ""));
        var all = new AheadQueue();
        all.Courses(Menu, System.Array.Empty<(string, long)>());
        foreach (var (id, _) in Menu)
            all.Done(id, AheadQueue.Outcome.Ready);
        Assert.Equal("", all.Status("", "")); // all ready: nothing to say
    }
}
