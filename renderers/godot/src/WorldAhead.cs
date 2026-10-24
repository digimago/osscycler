using System;
using System.Diagnostics;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;

namespace Osscycler;

// WorldAhead builds courses' 3D worlds ahead of a ride, so a course picked
// from the menu starts in its own world rather than a preview (owner,
// 2026-10-13: faster ride starts). It takes the core's courses one at a
// time in AheadQueue's order and runs osscycler-world -if-stale at idle
// priority: a current world is left as it is. The rider's profile says
// when and how (owner, 2026-10-13: preferences in the profile): by
// default only while nothing is ridden (a ride, workout or manual control
// stops the build going on, tried again later) and from the caches only
// (-if-cached: a course whose map data or ground model isn't all cached
// is skipped, built when it is ridden); or during rides too (the course
// being ridden left to WorldSource, which builds its own world), and
// downloading what a course lacks.
public sealed class WorldAhead
{
    readonly Core _core;
    readonly string _dir;
    readonly object _mu = new();
    readonly Head.AheadQueue _queue = new();
    CancellationTokenSource? _run;
    string _building = "", _buildingId = "", _how = "";
    bool _fetch;
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

    // Of is how course id's world turned out this session (null: not tried
    // yet), for the course picker.
    public Head.AheadQueue.Outcome? Of(string id)
    {
        lock (_mu)
            return _queue.Of(id);
    }

    // Ready records that course id's world is current (the ride's own,
    // built or found by WorldSource), so it isn't tried again.
    public void Ready(string id)
    {
        lock (_mu)
            _queue.Done(id, Head.AheadQueue.Outcome.Ready);
    }

    // Fetch says whether builds may download what a course lacks (the
    // profile's world_fetch); allowing it tries the skipped courses again.
    public void Fetch(bool allowed)
    {
        lock (_mu)
        {
            if (allowed && !_fetch)
                _queue.Retry(Head.AheadQueue.Outcome.Skipped);
            _fetch = allowed;
        }
    }

    // Idle is called every frame while worlds may be built: it lists the
    // core's courses now and then and builds the next one, other than
    // riding (the course being ridden, "" none), whose build it stops.
    public void Idle(string riding = "")
    {
        lock (_mu)
        {
            if (riding != "" && _buildingId == riding)
                _run?.Cancel();
            if (_run != null || _listing)
                return;
            if (DateTime.UtcNow - _listedAt > ListEvery)
            {
                _listing = true;
                _ = Task.Run(List);
                return;
            }
            if (_queue.Next(riding) is not { } next)
                return;
            _run = new CancellationTokenSource();
            var token = _run.Token;
            (_building, _buildingId, _how) = (next.Name, next.Id, "");
            var extra = _fetch ? System.Array.Empty<string>() : new[] { "-if-cached" };
            _ = Task.Run(() => Build(next.Id, extra, token));
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

    async Task Build(string id, string[] extra, CancellationToken token)
    {
        Head.AheadQueue.Outcome? how = null;
        try
        {
            var courses = await WorldSource.FetchCourse(_core, _dir, id, token);
            bool ready = false, skipped = false;
            var (code, _) = await WorldSource.RunBuilder(WorldSource.Args(courses, _dir, id, extra), ProcessPriorityClass.Idle, line =>
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
                (_run, _building, _buildingId, _how) = (null, "", "", "");
            }
        }
    }
}
