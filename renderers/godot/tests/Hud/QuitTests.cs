using System.Linq;
using Osscycler.Head;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class QuitTests
{
    static void Settle() => System.Threading.Thread.Sleep(50); // commands are sent on other threads

    static State Riding(uint heads = 1, bool paused = false) => new()
    {
        Ride = new Ride { Phase = RidePhase.Riding, CourseName = "Posbank Loop" },
        Recording = new Recording { Active = true },
        Heads = heads,
        Paused = paused,
    };

    static Controller Ridden(Calls c, State st)
    {
        var h = new Controller(c);
        h.Tick(st, 0); // frames tick all ride long (as in Main)
        return h;
    }

    [Fact]
    public void QOnARideOpensTheMenuPaused()
    {
        var c = new Calls();
        var h = Ridden(c, Riding());
        h.Key("q", Riding(), 0);
        Settle();
        Assert.False(h.Quit);
        Assert.NotNull(h.List());
        Assert.Equal(new[] { "paused True" }, c.Sent);
    }

    [Fact]
    public void QuitAsksWhenAloneAndEndsTheRide()
    {
        var c = new Calls();
        var h = Ridden(c, Riding());
        h.Key("q", Riding(), 0);
        h.Key("q", Riding(paused: true), 0); // the menu's Quit
        Assert.False(h.Quit);
        Assert.Equal("QUIT?", h.Dialog(Riding(paused: true), 0)?.Title);
        h.Key("enter", Riding(paused: true), 0);
        Settle();
        Assert.True(h.Quit);
        Assert.Contains("stop ride", c.Sent);
        Assert.Contains(c.Sent, s => s.StartsWith("end"));
    }

    [Fact]
    public void QuitLeavingItPaused()
    {
        var c = new Calls();
        var h = Ridden(c, Riding());
        h.Key("q", Riding(), 0);
        h.Key("q", Riding(paused: true), 0);
        h.Key("l", Riding(paused: true), 0);
        Settle();
        Assert.True(h.Quit);
        Assert.DoesNotContain("stop ride", c.Sent);
        Assert.Equal("paused True", c.Sent.Last());
    }

    [Fact]
    public void QuitAtOnceWithAnotherScreenOrAsAWatcher()
    {
        var h = Ridden(new Calls(), Riding(heads: 2));
        h.Key("q", Riding(heads: 2), 0);
        h.Key("q", Riding(heads: 2, paused: true), 0);
        Assert.True(h.Quit);

        var d = Ridden(new Calls(), Riding(heads: 0));
        d.Watcher = true;
        d.Key("q", Riding(heads: 0), 0);
        d.Key("q", Riding(heads: 0, paused: true), 0);
        Assert.True(d.Quit);
    }

    [Fact]
    public void TheQuestionGoesWhenTheRideEndsElsewhere()
    {
        var h = Ridden(new Calls(), Riding());
        h.Key("q", Riding(), 0);
        h.Key("q", Riding(paused: true), 0);
        h.Tick(new State { Ride = new Ride { Phase = RidePhase.Aborted }, Heads = 1 }, 0);
        Assert.True(h.Quit);
    }
}
