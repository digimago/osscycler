using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// NoCore is the commands without a core (riding a world at a fixed
// speed): each fails, and the head shows that as a notice.
public sealed class NoCore : ICommands
{
    static Task None() => Task.FromException(new InvalidOperationException("no core (start the renderer with --core)"));

    public Task StopRide() => None();
    public Task SetPaused(bool paused) => None();
    public Task SetDifficulty(double pct) => None();
    public Task StopWorkout() => None();
    public Task SkipSegment() => None();
    public Task SetIntensity(double pct) => None();
    public Task<IList<Course>> ListCourses() => Task.FromException<IList<Course>>(new InvalidOperationException("no core"));
    public Task<IList<RideResult>> ListResults() => Task.FromException<IList<RideResult>>(new InvalidOperationException("no core"));
    public Task<IList<Activity>> ListActivities() => Task.FromException<IList<Activity>>(new InvalidOperationException("no core"));
    public Task<IList<WorkoutDef>> ListWorkouts() => Task.FromException<IList<WorkoutDef>>(new InvalidOperationException("no core"));
    public Task<string> SaveWorkout(string id, WorkoutDef w) => Task.FromException<string>(new InvalidOperationException("no core"));
    public Task StartRide(string courseId) => None();
    public Task StartRideAgainst(string courseId, long finishedUnixMs) => None();
    public Task StartWorkout(string id) => None();
    public Task SetPower(double watts) => None();
    public Task SetFtp(double watts) => None();
    public Task SetGrade(double pct) => None();
    public Task SetLevel(double pct) => None();
    public Task ReleaseControl() => None();
    public Task<RiderProfile> SetProfile(double? weightKg, double? ftpW, double? heightCm) => Task.FromException<RiderProfile>(new InvalidOperationException("no core"));
    public Task<RiderProfile> SetView(string view) => Task.FromException<RiderProfile>(new InvalidOperationException("no core"));
    public Task<string> EndActivity(bool discard) => Task.FromException<string>(new InvalidOperationException("no core"));
    public Task StartCalibration() => None();
    public Task CancelCalibration() => None();
    public Task<string> SaveActivity(string name) => Task.FromException<string>(new InvalidOperationException("no core"));
}
