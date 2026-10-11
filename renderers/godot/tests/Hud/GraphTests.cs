using System.Linq;
using Osscycler.Head;
using Osscycler.V1;
using Xunit;

namespace Osscycler.Hud.Tests;

public class GraphTests
{
    static GetActivityHistoryResponse Ride(string name, int seconds)
    {
        var r = new GetActivityHistoryResponse { Name = name };
        for (int i = 0; i < seconds; i++)
        {
            r.PowerW.Add(i < seconds / 2 ? 150 : 320);
            r.HeartRateBpm.Add(110 + i / 10);
        }
        return r;
    }

    static State Recording(string file, double timer) => new() { Recording = new Recording { Active = true, File = file, TimerS = timer } };

    // The head fetches only what the graph lacks, as the recording runs
    // on; a new activity starts afresh; t hides the graph.
    [Fact]
    public void FetchesWhatItLacks()
    {
        var c = new Calls { History = Ride("a.fit", 300) };
        var h = new Controller(c);
        h.Tick(Recording("a.fit", 300), 0);
        Assert.Equal(300, h.Graph(Recording("a.fit", 300))!.Count);
        h.Tick(Recording("a.fit", 301), 1); // a second on: not yet
        h.Tick(Recording("a.fit", 302), 2); // two: from 300
        Assert.Equal(new uint[] { 0, 300 }, c.HistoryCalls);

        c.History = Ride("b.fit", 40);
        h.Tick(Recording("b.fit", 40), 3);
        Assert.Equal(40, h.Graph(Recording("b.fit", 40))!.Count);
        Assert.Null(h.Graph(Recording("a.fit", 300))); // not the activity on now

        h.Key("t", Recording("b.fit", 40), 4);
        Assert.Null(h.Graph(Recording("b.fit", 40)));
        Assert.False(h.Layout.Showing(Layout.Graph));
        h.Tick(Recording("b.fit", 50), 5); // hidden: nothing fetched
        Assert.Equal(3, c.HistoryCalls.Count);
        h.Key("t", Recording("b.fit", 50), 6);
        Assert.True(h.Layout.Showing(Layout.Graph));
    }

    [Fact]
    public void ColumnsSqueezeTheRide()
    {
        var (cols, top, minHr, maxHr) = new RideHistory("x").With(Ride("x", 300)).Columns(100);
        Assert.Equal(100, cols.Count);
        Assert.Equal(150, cols[0].PowerW);
        Assert.Equal(320, cols[^1].PowerW);
        Assert.Equal(350, top); // a round 50 W over the highest
        Assert.Equal(110, minHr, 0);
        Assert.Equal(139, maxHr, 0);
        // A piece that doesn't carry on is left out.
        var x = new RideHistory("x").With(Ride("x", 10));
        var gap = new GetActivityHistoryResponse { Name = "x", FromS = 20 };
        gap.PowerW.Add(1);
        Assert.Equal(10, x.With(gap).Count);
    }

    // The summary opens on i in ACTIVITIES and closes back to the list.
    [Fact]
    public void SummaryFromActivities()
    {
        var c = new Calls();
        c.Summaries["2026-10-09-120000.fit"] = new ActivitySummary { Name = "2026-10-09-120000.fit", TimerS = 3600 };
        var h = new Controller(c);
        var st = new State();
        h.Key("r", st, 0);
        h.Key("tab", st, 0);
        h.Key("tab", st, 0); // ACTIVITIES
        h.Key("i", st, 0);
        Assert.Equal(3600, h.Summary!.TimerS);
        Assert.Null(h.List());
        Assert.False(h.Free);
        h.Key("x", st, 1);
        Assert.Null(h.Summary);
        Assert.NotNull(h.List()); // back to the list
    }
}

public class EndToSummaryTests
{
    // From the start: the menu, esc, e, enter: the ride saved, its summary.
    [Fact]
    public void EndingARideShowsItsSummary()
    {
        var c = new Calls();
        c.Summaries["ride.fit"] = new ActivitySummary { Name = "ride.fit", TimerS = 120 };
        var h = new Controller(c);
        var st = new State { Recording = new Recording { Active = true, File = "ride.fit", TimerS = 120 } };
        h.Started(st);
        Assert.NotNull(h.List());
        h.Key("esc", st, 0);
        Assert.Null(h.List());
        h.Key("e", st, 1);
        Assert.Equal("END RIDE?", h.Dialog(st, 1)!.Title);
        h.Key("enter", st, 2);
        System.Threading.Thread.Sleep(30);
        Assert.Equal(120, h.Summary!.TimerS);
    }
}
