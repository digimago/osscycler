using System;
using System.Diagnostics;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;

namespace Osscycler;

// WorldAhead builds courses' 3D worlds ahead of a ride, so a course picked
// from the menu starts in its own world rather than a preview (owner,
// 2026-10-13: faster ride starts). While nothing is ridden (no course
// ride, workout or manual control) it takes the core's courses one at a
// time in AheadQueue's order and runs osscycler-world -if-stale -if-cached
// at idle priority: a current world is left as it is, a course whose map
// data or ground model isn't all cached is skipped (built when it is
// ridden, fetching what it lacks: the owner's choice, nothing is
// downloaded for courses not ridden). A ride, workout or manual control
// stops the build going on (WorldSource builds the ride's own world); it
// is tried again when nothing is ridden.
public sealed class WorldAhead
{
    readonly Core _core;
    readonly string _dir;
    readonly object _mu = new();
    readonly Head.AheadQueue _queue = new();
    CancellationTokenSource? _run;
    string _building = "", _how = "";
    DateTime _listedAt = DateTime.MinValue;
    bool _listing;

    // ListEvery: the core's courses are listed again this often (a GPX
    // added to its courses directory joins the queue).
    static readonly TimeSpan ListEvery = TimeSpan.FromMinutes(2);

    public WorldAhead(Core core, string dir) => (_core, _dir) = (core, dir);

    // Status says what the queue is doing ("" nothing worth saying).
    public string Status
    {
        get
        {
            lock (_mu)
                return _queue.Status(_building, _how);
        }
    }

    // Idle is called every frame while nothing is ridden: it lists the
    // core's courses now and then and builds the next one.
    public void Idle()
    {
        lock (_mu)
        {
            if (_run != null || _listing)
                return;
            if (DateTime.UtcNow - _listedAt > ListEvery)
            {
                _listing = true;
                _ = Task.Run(List);
                return;
            }
            if (_queue.Next() is not { } next)
                return;
            _run = new CancellationTokenSource();
            var token = _run.Token;
            (_building, _how) = (next.Name, "");
            _ = Task.Run(() => Build(next.Id, token));
        }
    }

    // Busy stops the build going on: something is ridden. Stop: the
    // renderer quits.
    public void Busy()
    {
        lock (_mu)
            _run?.Cancel();
    }

    public void Stop() => Busy();

    async Task List()
    {
        try
        {
            var courses = await _core.ListCourses();
            var results = await _core.ListResults();
            lock (_mu)
                _queue.Courses(courses.Select(c => (c.Id, c.Name)), results.Select(r => (r.CourseId, r.FinishedUnixMs)));
        }
        catch (Exception)
        {
            // No core just now: listed again later.
        }
        finally
        {
            lock (_mu)
                (_listing, _listedAt) = (false, DateTime.UtcNow);
        }
    }

    async Task Build(string id, CancellationToken token)
    {
        Head.AheadQueue.Outcome? how = null;
        try
        {
            var courses = await WorldSource.FetchCourse(_core, _dir, id, token);
            bool ready = false, skipped = false;
            var (code, _) = await WorldSource.RunBuilder(WorldSource.Args(courses, _dir, id, "-if-cached"), ProcessPriorityClass.Idle, line =>
            {
                if (Head.BuildStatus.Parse(line) is { } h)
                    lock (_mu)
                        _how = h;
                else if (line.StartsWith("skipped: ", StringComparison.Ordinal))
                    skipped = true;
                else if (line.StartsWith("world: ", StringComparison.Ordinal))
                    ready = true;
            }, token);
            how = code == null ? Head.AheadQueue.Outcome.Failed
                : skipped ? Head.AheadQueue.Outcome.Skipped
                : code == 0 && ready ? Head.AheadQueue.Outcome.Ready
                : Head.AheadQueue.Outcome.Failed;
        }
        catch (OperationCanceledException)
        {
            // Stopped by a ride: tried again later.
        }
        catch (Exception)
        {
            how = Head.AheadQueue.Outcome.Failed;
        }
        finally
        {
            lock (_mu)
            {
                if (how is { } o)
                    _queue.Done(id, o);
                (_run, _building, _how) = (null, "", "");
            }
        }
    }
}
