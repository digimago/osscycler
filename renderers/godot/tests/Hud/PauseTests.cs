using System;
using Osscycler.Head;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class PauseTests
{
    static void Settle() => System.Threading.Thread.Sleep(30); // commands are sent on other threads (HeadTests)

    static State Riding(bool paused = false) => new() { Ride = new Ride { Phase = RidePhase.Riding }, Paused = paused };

    [Fact]
    public void PAndSpacePauseAndCarryOn()
    {
        foreach (var key in new[] { "p", "P", " " })
        {
            var c = new Calls();
            var h = new Controller(c);
            h.Key(key, Riding(), 0);
            h.Key(key, Riding(paused: true), 0);
            Settle();
            Assert.Equal(new[] { "paused True", "paused False" }, c.Sent);
        }
    }

    [Fact]
    public void LowercasePPausesAndCarriesOnWhileRiding()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("p", Riding(), 0);
        h.Key("p", Riding(paused: true), 0);
        h.Key("p", new State { Paused = true }, 0); // paused with nothing under way
        Settle();
        Assert.Equal(new[] { "paused True", "paused False", "paused False" }, c.Sent);
    }

    [Fact]
    public void TheProfileIsTheMenusNotAKeyOfItsOwn()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("p", new State { Profile = new RiderProfile { WeightKg = 80, FtpW = 200 } }, 0);
        Settle();
        Assert.Equal(new[] { "paused True" }, c.Sent); // p pauses, also free riding
    }

    [Fact]
    public void EscPausesIntoTheMenuAndOutAgain()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Tick(Riding(), 0);
        h.Key("esc", Riding(), 0);
        h.Tick(Riding(paused: true), 0);
        Assert.NotNull(h.List()); // the menu, staying while paused
        h.Key("esc", Riding(paused: true), 0);
        Settle();
        Assert.Null(h.List());
        Assert.Equal(new[] { "paused True", "paused False" }, c.Sent);
    }

    [Fact]
    public void AnotherScreenCarryingOnClosesTheMenu()
    {
        var h = new Controller(new Calls());
        h.Tick(Riding(), 0); // frames tick all ride long (as in Main)
        h.Key("esc", Riding(), 0);
        h.Tick(Riding(paused: true), 0);
        Assert.NotNull(h.List());
        h.Tick(Riding(paused: false), 0); // the TUI carried on
        Assert.Null(h.List());
    }

    [Fact]
    public void PauseKeysWaitForOpenThings()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Key("m", new State(), 0); // the menu: space chooses there
        h.Key("P", new State(), 0);
        Settle();
        Assert.DoesNotContain(c.Sent, s => s.StartsWith("paused"));
    }

    [Fact]
    public void PausedShowsTheBanner()
    {
        var m = new Moments();
        var st = Riding(paused: true);
        st.PausedSinceUnixMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() - 75_000;
        m.Update(st, null, 0, 0);
        Assert.Equal("Paused", m.Big?.Title);
        Assert.StartsWith("1:15", m.Big?.Line);
    }
}
