using System;
using System.Collections.Generic;
using System.Linq;
using Osscycler.V1;

namespace Osscycler.Head;

// AheadQueue is the order in which the 3D view builds courses' worlds
// ahead of a ride, and what it says about it (owner, 2026-10-13: faster
// ride starts; every course in the menu, while nothing is being ridden,
// from the caches only). Recently ridden courses come first, newest first,
// then the rest in the menu's order. Each course is tried once a session:
// built (or found current), skipped (its data not all cached: built when
// it is ridden, fetching what it lacks) or failed. A build stopped by a
// ride is tried again. Plain C# apart from Godot, tested.
public sealed class AheadQueue
{
    public enum Outcome { Ready, Skipped, Failed }

    readonly Dictionary<string, Outcome> _done = new();
    List<(string Id, string Name)> _courses = new();

    // Courses sets the menu's courses (id, name) and the ride history
    // (course id, finished, unix ms): the order to build them in.
    public void Courses(IEnumerable<(string Id, string Name)> courses, IEnumerable<(string CourseId, long FinishedUnixMs)> results)
    {
        var last = new Dictionary<string, long>();
        foreach (var (id, at) in results)
            if (!last.TryGetValue(id, out var t) || at > t)
                last[id] = at;
        var list = courses.ToList();
        var index = list.Select((c, i) => (c.Id, i)).GroupBy(x => x.Id).ToDictionary(g => g.Key, g => g.First().i);
        _courses = list.GroupBy(c => c.Id).Select(g => g.First())
            .OrderByDescending(c => last.TryGetValue(c.Id, out var t) ? t : long.MinValue)
            .ThenBy(c => index[c.Id])
            .ToList();
    }

    // Riding tells whether something is ridden, so no world is built
    // ahead: a course ride armed or under way (its own world comes first),
    // a workout armed, running or paused, or manual control. A ride or
    // workout that has ended, its result still shown, is nothing ridden.
    public static bool Riding(State? st) =>
        st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding
        || st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused
        || st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level;

    // Next is the next course to build (null when every one has been tried).
    public (string Id, string Name)? Next()
    {
        foreach (var c in _courses)
            if (!_done.ContainsKey(c.Id))
                return c;
        return null;
    }

    // Done records how a course's build ended.
    public void Done(string id, Outcome how) => _done[id] = how;

    // Status says how far the queue is, for the status line while nothing
    // is ridden: building is the course being built and how far ("" none).
    public string Status(string building, string how)
    {
        int n = _courses.Count;
        if (n == 0)
            return "";
        int ready = _courses.Count(c => _done.TryGetValue(c.Id, out var o) && o == Outcome.Ready);
        if (building != "")
        {
            int at = _courses.Count(c => _done.ContainsKey(c.Id)) + 1;
            return $"3D worlds ahead: {building} ({Math.Min(at, n)} of {n}){(how != "" ? " · " + how : "")}";
        }
        if (Next() != null)
            return "";
        int skipped = _courses.Count(c => _done.TryGetValue(c.Id, out var o) && o == Outcome.Skipped);
        return skipped > 0 ? $"3D worlds ahead: {ready} of {n} ready, {skipped} built when ridden (map data to fetch)" : "";
    }
}
