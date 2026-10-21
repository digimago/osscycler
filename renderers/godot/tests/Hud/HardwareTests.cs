using System.Threading;
using Osscycler.Head;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class HardwareTests
{
    static State NoTrainer(bool simulated = false) => new()
    {
        Radio = new Radio { Present = false, Error = "no stick" },
        Trainer = new Trainer { Sensor = new Sensor { Status = simulated ? SensorStatus.Connected : SensorStatus.Searching } },
        HeartRate = new HeartRate { Sensor = new Sensor { Status = SensorStatus.Searching } },
        Profile = new RiderProfile { Complete = true, WeightKg = 80, FtpW = 220 },
        Simulated = simulated,
    };

    // Without a trainer after a few seconds: what is missing, and the ways
    // on; s a simulated rider, which s on the dashboard stops again.
    [Fact]
    public void AsksAfterAFewSecondsWithoutATrainer()
    {
        var c = new Calls();
        var h = new Controller(c);
        h.Tick(NoTrainer(), 0);
        Assert.Null(h.Dialog(NoTrainer(), 0)); // the trainer gets a few seconds
        h.Tick(NoTrainer(), 9);
        var d = h.Dialog(NoTrainer(), 9)!;
        Assert.Equal("NO TRAINER YET", d.Title);
        Assert.Contains(d.Lines, l => l.StartsWith("✕ ANT+ stick"));
        Assert.Contains("s a simulated rider", d.Hint);
        Assert.False(h.Free);
        h.Key("x", NoTrainer(), 9); // other keys wait
        Assert.NotNull(h.Dialog(NoTrainer(), 9));
        h.Key("s", NoTrainer(), 10);
        Thread.Sleep(30);
        Assert.Equal(new[] { "simulated True" }, c.Sent);
        h.Tick(NoTrainer(simulated: true), 11);
        Assert.Null(h.Dialog(NoTrainer(simulated: true), 11));
        h.Key("s", NoTrainer(simulated: true), 12);
        Thread.Sleep(30);
        Assert.Equal("simulated False", c.Sent[^1]);
    }

    // esc carries on without, and it stays away.
    [Fact]
    public void EscCarriesOnWithout()
    {
        var h = new Controller(new Calls());
        h.Tick(NoTrainer(), 0);
        h.Tick(NoTrainer(), 9);
        h.Key("esc", NoTrainer(), 9);
        h.Tick(NoTrainer(), 30);
        Assert.Null(h.Dialog(NoTrainer(), 30));
        Assert.Contains("carrying on without", h.Notice(30));
    }
}
