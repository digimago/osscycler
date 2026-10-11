using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Osscycler.Hud;
using Osscycler.V1;

namespace Osscycler.Head;

// The ride graph and the end-of-ride summary (owner's list, items 4 and 6;
// tui/graph.go, tui/summary.go). The graph is the activity being recorded,
// a sample a second of riding time: the head fetches the seconds it lacks
// once the recording has run GraphFetchS past them, and t shows or hides
// it (a strip in hud.json). The summary opens after the rider ends a ride
// and saves it, and on i in ACTIVITIES; x, enter or esc close it.
public sealed partial class Controller
{
    public const double GraphFetchS = 2;

    RideHistory _history = new("");
    bool _fetchingHistory;
    ActivitySummary? _summary;
    bool _summaryFromPicker;

    // Graph is the activity being recorded as far as fetched, when the
    // graph is shown and there is something to draw (null otherwise).
    public RideHistory? Graph(State? st)
    {
        lock (_mu)
            return _layout.Showing(Layout.Graph) && st?.Recording is { Active: true } rec &&
                _history.Name == rec.File && _history.Count >= 2 ? _history : null;
    }

    // Summary is the summary on screen (null: none).
    public ActivitySummary? Summary
    {
        get
        {
            lock (_mu)
                return _summary;
        }
    }

    // NeedHistory fetches what the graph lacks; Tick calls it.
    void NeedHistory(State? st)
    {
        var rec = st?.Recording;
        uint since;
        lock (_mu)
        {
            if (_fetchingHistory || !_layout.Showing(Layout.Graph) || rec is not { Active: true })
                return;
            since = _history.Name == rec.File ? (uint)_history.Count : 0;
            if (since > 0 && rec.TimerS < since + GraphFetchS)
                return;
            _fetchingHistory = true;
        }
        Task<GetActivityHistoryResponse> t;
        try
        {
            t = _cmds.ActivityHistory("", since);
        }
        catch (Exception)
        {
            lock (_mu)
                _fetchingHistory = false;
            return;
        }
        t.ContinueWith(done =>
        {
            lock (_mu)
            {
                _fetchingHistory = false;
                if (done.Exception == null)
                    _history = _history.With(done.Result);
            }
        }, TaskContinuationOptions.ExecuteSynchronously);
    }

    // GraphKey is t: the graph shown or hidden, kept in the layout.
    bool GraphKey(string key)
    {
        if (key != "t")
            return false;
        var l = _layout.Clone();
        if (!l.Hide.Remove(Layout.Graph))
            l.Hide.Add(Layout.Graph);
        Keep(l);
        return true;
    }

    // OpenSummary fetches an activity's summary and shows it.
    void OpenSummary(string name, bool fromPicker)
    {
        _cmds.ActivitySummary(name).ContinueWith(done =>
        {
            if (done.Exception != null)
            {
                if (fromPicker) // after ending a ride, "ride saved" stays
                    Failed("the ride's summary", done.Exception.GetBaseException());
                return;
            }
            lock (_mu)
            {
                (_summary, _summaryFromPicker) = (done.Result, fromPicker);
                if (fromPicker)
                    _screen = Screen.None;
            }
        }, TaskContinuationOptions.ExecuteSynchronously);
    }

    // SummaryKey closes the summary; while it is open nothing else takes
    // keys. Whether it was open.
    bool SummaryKey(string key)
    {
        lock (_mu)
        {
            if (_summary == null)
                return false;
            if (key is "x" or "enter" or "esc" or "i" or "q")
            {
                _summary = null;
                if (_summaryFromPicker)
                    _screen = Screen.Picker;
            }
            return true;
        }
    }
}
