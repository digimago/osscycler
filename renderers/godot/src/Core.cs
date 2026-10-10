using System;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Threading;
using System.Threading.Tasks;
using Grpc.Core;
using Grpc.Net.Client;
using Osscycler.V1;

namespace Osscycler;

// Core follows a core's state stream in the background, reconnecting with
// backoff, and keeps the newest state for the frame loop to read. Like
// every osscycler client it sends the API token with each call and talks
// to a core beyond this machine only over TLS (not in the spike: loopback
// only).
public sealed class Core : IDisposable, Head.ICommands
{
    readonly CancellationTokenSource _stop = new();
    readonly object _mu = new();
    State? _state;
    string _status = "connecting";

    readonly string _address, _token;
    // Courses as the core lists them (their profiles, for the HUD's
    // strips), fetched once per course; asked: being fetched or fetched.
    readonly Dictionary<string, Course> _courses = new();
    readonly HashSet<string> _asked = new();
    readonly bool _head;

    // head: this renderer counts as a screen the rider rides with
    // (StreamStateRequest.head); not in debug mode.
    public Core(string address, string token, bool head = true)
    {
        if (!IsLoopback(address))
            throw new ArgumentException($"{address}: the spike talks to a core on this machine only (no TLS yet)");
        (_address, _token, _head) = (address, token, head);
        _ = Task.Run(() => Run(address, token, _stop.Token));
    }

    // Course is course id as the core lists it, or null until it has been
    // fetched (the first call starts that, in the background).
    public Course? Course(string id)
    {
        lock (_mu)
        {
            if (_courses.TryGetValue(id, out var c))
                return c;
            if (!_asked.Add(id))
                return null;
        }
        _ = Task.Run(() => FetchCourses(id));
        return null;
    }

    readonly Dictionary<string, WorkoutDef> _workouts = new();
    readonly HashSet<string> _askedWorkouts = new();

    // Workout is workout id as the core lists it (its timeline, for the
    // HUD's workout strip), or null until fetched, as Course.
    public WorkoutDef? Workout(string id)
    {
        lock (_mu)
        {
            if (_workouts.TryGetValue(id, out var w))
                return w;
            if (!_askedWorkouts.Add(id))
                return null;
        }
        _ = Task.Run(async () =>
        {
            try
            {
                using var channel = GrpcChannel.ForAddress("http://" + _address);
                var client = new TelemetryService.TelemetryServiceClient(channel);
                var resp = await client.ListWorkoutsAsync(new ListWorkoutsRequest(), new Metadata { { "authorization", "Bearer " + _token } }, cancellationToken: _stop.Token);
                lock (_mu)
                    foreach (var w in resp.Workouts)
                        _workouts[w.Id] = w;
            }
            catch (Exception)
            {
                try
                {
                    await Task.Delay(TimeSpan.FromSeconds(5), _stop.Token);
                }
                catch (OperationCanceledException)
                {
                    return;
                }
            }
            lock (_mu)
                if (!_workouts.ContainsKey(id))
                    _askedWorkouts.Remove(id);
        });
        return null;
    }

    async Task FetchCourses(string id)
    {
        try
        {
            // A course carries its scenery: more than gRPC's 4 MB default.
            using var channel = GrpcChannel.ForAddress("http://" + _address, new GrpcChannelOptions { MaxReceiveMessageSize = 64 << 20 });
            var client = new TelemetryService.TelemetryServiceClient(channel);
            var headers = new Metadata { { "authorization", "Bearer " + _token } };
            var resp = await client.ListCoursesAsync(new ListCoursesRequest(), headers, cancellationToken: _stop.Token);
            lock (_mu)
                foreach (var c in resp.Courses)
                    _courses[c.Id] = c;
        }
        catch (Exception)
        {
            // Asked again on a later frame, after a pause.
            try
            {
                await Task.Delay(TimeSpan.FromSeconds(5), _stop.Token);
            }
            catch (OperationCanceledException)
            {
                return;
            }
        }
        lock (_mu)
            if (!_courses.ContainsKey(id))
                _asked.Remove(id);
    }

    // Latest is the newest state (null until one arrives or while
    // disconnected) and a word on the connection.
    public (State? State, string Status) Latest()
    {
        lock (_mu)
            return (_state, _status);
    }

    void Set(State? s, string status)
    {
        lock (_mu)
            (_state, _status) = (s, status);
    }

    async Task Run(string address, string token, CancellationToken ct)
    {
        var backoff = TimeSpan.FromSeconds(0.5);
        while (!ct.IsCancellationRequested)
        {
            try
            {
                using var channel = GrpcChannel.ForAddress("http://" + address);
                var client = new TelemetryService.TelemetryServiceClient(channel);
                var headers = new Metadata { { "authorization", "Bearer " + token } };
                using var call = client.StreamState(new StreamStateRequest { MaxRateHz = 60, Head = _head }, headers, cancellationToken: ct);
                await foreach (var resp in call.ResponseStream.ReadAllAsync(ct))
                {
                    Set(resp.State, "connected");
                    backoff = TimeSpan.FromSeconds(0.5);
                }
                Set(null, "stream ended");
            }
            catch (OperationCanceledException) when (ct.IsCancellationRequested)
            {
                return;
            }
            catch (RpcException e)
            {
                // Never show numbers we can no longer vouch for.
                Set(null, e.StatusCode == StatusCode.Unauthenticated ? "the core refused the API token" : $"core unavailable: {e.Status.Detail}");
            }
            catch (Exception e)
            {
                Set(null, $"core unavailable: {e.Message}");
            }
            try
            {
                await Task.Delay(backoff, ct);
            }
            catch (OperationCanceledException)
            {
                return;
            }
            backoff = TimeSpan.FromSeconds(Math.Min(10, backoff.TotalSeconds * 2));
        }
    }

    static bool IsLoopback(string address)
    {
        var host = address;
        if (host.StartsWith('['))
            host = host[1..host.IndexOf(']')];
        else if (host.Contains(':'))
            host = host[..host.LastIndexOf(':')];
        return host == "localhost" || (IPAddress.TryParse(host, out var ip) && IPAddress.IsLoopback(ip));
    }

    // Token finds the API token as the TUI does: an explicit file, then
    // $OSSCYCLER_API_TOKEN, then osscycler's folder ($OSSCYCLER_HOME or
    // ~/osscycler).
    public static string Token(string file)
    {
        if (file != "")
            return File.ReadAllText(file).Trim();
        var env = System.Environment.GetEnvironmentVariable("OSSCYCLER_API_TOKEN");
        if (!string.IsNullOrWhiteSpace(env))
            return env.Trim();
        return File.ReadAllText(Path.Combine(Home(), "api-token")).Trim();
    }

    public static string Home()
    {
        var h = System.Environment.GetEnvironmentVariable("OSSCYCLER_HOME");
        return string.IsNullOrEmpty(h) ? Path.Combine(System.Environment.GetFolderPath(System.Environment.SpecialFolder.UserProfile), "osscycler") : h;
    }

    // Commands, over one channel kept for them.
    GrpcChannel? _commands;

    TelemetryService.TelemetryServiceClient Client()
    {
        lock (_mu)
            _commands ??= GrpcChannel.ForAddress("http://" + _address);
        return new TelemetryService.TelemetryServiceClient(_commands);
    }

    Metadata Auth() => new() { { "authorization", "Bearer " + _token } };

    // Call makes one command; a refusal comes back as the core's own words.
    static async Task<T> Call<T>(Func<Task<T>> rpc)
    {
        try
        {
            return await rpc();
        }
        catch (RpcException e)
        {
            throw new InvalidOperationException(e.Status.Detail != "" ? e.Status.Detail : e.StatusCode.ToString());
        }
    }

    CallOptions Opts() => new(Auth(), cancellationToken: _stop.Token);

    public Task StopRide() => Call(() => Client().StopRideAsync(new StopRideRequest(), Opts()).ResponseAsync);
    public Task SetDifficulty(double pct) => Call(() => Client().SetDifficultyAsync(new SetDifficultyRequest { DifficultyPct = pct }, Opts()).ResponseAsync);
    public Task StopWorkout() => Call(() => Client().StopWorkoutAsync(new StopWorkoutRequest(), Opts()).ResponseAsync);
    public Task SkipSegment() => Call(() => Client().SkipSegmentAsync(new SkipSegmentRequest(), Opts()).ResponseAsync);
    public Task SetIntensity(double pct) => Call(() => Client().SetIntensityAsync(new SetIntensityRequest { IntensityPct = pct }, Opts()).ResponseAsync);
    public Task StartRide(string courseId) => Call(() => Client().StartRideAsync(new StartRideRequest { CourseId = courseId }, Opts()).ResponseAsync);
    public Task StartRideAgainst(string courseId, long finishedUnixMs) =>
        Call(() => Client().StartRideAsync(new StartRideRequest { CourseId = courseId, AgainstFinishedUnixMs = finishedUnixMs }, Opts()).ResponseAsync);
    public async Task<string> SaveWorkout(string id, WorkoutDef w)
    {
        var r = await Call(() => Client().SaveWorkoutAsync(new SaveWorkoutRequest { Id = id, Workout = w }, Opts()).ResponseAsync);
        lock (_mu)
        {
            // The HUD's strip fetches it afresh.
            _workouts.Remove(r.Id);
            _askedWorkouts.Remove(r.Id);
        }
        return r.Id;
    }

    // CourseFile is a course's GPX file as the core has it (null: none to
    // give, as for the tracks and included routes every client has).
    public async Task<(string Name, byte[] Data)?> CourseFile(string courseId)
    {
        try
        {
            // A GPX can be megabytes: more than gRPC's 4 MB default.
            using var channel = GrpcChannel.ForAddress("http://" + _address, new GrpcChannelOptions { MaxReceiveMessageSize = 64 << 20 });
            var client = new TelemetryService.TelemetryServiceClient(channel);
            var r = await client.GetCourseFileAsync(new GetCourseFileRequest { CourseId = courseId }, Opts()).ResponseAsync;
            return (r.Name, r.Data.ToByteArray());
        }
        catch (RpcException e) when (e.StatusCode is StatusCode.NotFound or StatusCode.Unimplemented)
        {
            return null;
        }
    }

    public Task StartWorkout(string id) => Call(() => Client().StartWorkoutAsync(new StartWorkoutRequest { Id = id }, Opts()).ResponseAsync);
    public Task SetPaused(bool paused) => Call(() => Client().SetPausedAsync(new SetPausedRequest { Paused = paused }, Opts()).ResponseAsync);
    public Task SetPower(double watts) => Call(() => Client().SetTrainerControlAsync(new SetTrainerControlRequest { PowerW = watts }, Opts()).ResponseAsync);
    public Task SetFtp(double watts) => Call(() => Client().SetFtpAsync(new SetFtpRequest { FtpW = watts }, Opts()).ResponseAsync);

    public Task SetGrade(double pct) => Call(() => Client().SetTrainerControlAsync(new SetTrainerControlRequest { GradePct = pct }, Opts()).ResponseAsync);
    public Task SetLevel(double pct) => Call(() => Client().SetTrainerControlAsync(new SetTrainerControlRequest { LevelPct = pct }, Opts()).ResponseAsync);
    public Task ReleaseControl() => Call(() => Client().ReleaseTrainerControlAsync(new ReleaseTrainerControlRequest(), Opts()).ResponseAsync);
    public Task StartCalibration() => Call(() => Client().StartCalibrationAsync(new StartCalibrationRequest { Type = CalibrationType.SpinDown }, Opts()).ResponseAsync);
    public Task CancelCalibration() => Call(() => Client().CancelCalibrationAsync(new CancelCalibrationRequest(), Opts()).ResponseAsync);

    public async Task<RiderProfile> SetView(string view) =>
        (await Call(() => Client().SetProfileAsync(new SetProfileRequest { View = view }, Opts()).ResponseAsync)).Profile;

    public async Task<RiderProfile> SetProfile(double? weightKg, double? ftpW, double? heightCm)
    {
        var req = new SetProfileRequest();
        if (weightKg is double w)
            req.WeightKg = w;
        if (ftpW is double f)
            req.FtpW = f;
        if (heightCm is double h)
            req.HeightCm = h;
        return (await Call(() => Client().SetProfileAsync(req, Opts()).ResponseAsync)).Profile;
    }

    public async Task<string> EndActivity(bool discard) =>
        (await Call(() => Client().EndActivityAsync(new EndActivityRequest { Discard = discard }, Opts()).ResponseAsync)).File;

    public async Task<IList<Course>> ListCourses()
    {
        // A course carries its scenery: more than gRPC's 4 MB default.
        using var channel = GrpcChannel.ForAddress("http://" + _address, new GrpcChannelOptions { MaxReceiveMessageSize = 64 << 20 });
        var resp = await Call(() => new TelemetryService.TelemetryServiceClient(channel).ListCoursesAsync(new ListCoursesRequest(), Opts()).ResponseAsync);
        lock (_mu)
            foreach (var c in resp.Courses)
                _courses[c.Id] = c;
        return resp.Courses;
    }

    public async Task<IList<RideResult>> ListResults() => (await Call(() => Client().ListResultsAsync(new ListResultsRequest(), Opts()).ResponseAsync)).Results;
    public async Task<IList<Activity>> ListActivities() => (await Call(() => Client().ListActivitiesAsync(new ListActivitiesRequest(), Opts()).ResponseAsync)).Activities;
    public async Task<IList<WorkoutDef>> ListWorkouts() => (await Call(() => Client().ListWorkoutsAsync(new ListWorkoutsRequest(), Opts()).ResponseAsync)).Workouts;

    // SaveActivity saves a recording's FIT file on this machine, as the
    // TUI does: into ~/Downloads (else home), through a temporary file
    // renamed when complete; an existing file is never overwritten.
    public async Task<string> SaveActivity(string name)
    {
        var home = System.Environment.GetFolderPath(System.Environment.SpecialFolder.UserProfile);
        var dir = Path.Combine(home, "Downloads");
        if (!Directory.Exists(dir))
            dir = home;
        var path = Path.Combine(dir, Path.GetFileName(name));
        if (File.Exists(path))
            throw new IOException(path + " is already there");
        var tmp = path + ".part";
        try
        {
            using (var call = Client().ExportActivity(new ExportActivityRequest { Name = name }, Opts()))
            await using (var f = new FileStream(tmp, FileMode.CreateNew))
            {
                try
                {
                    await foreach (var chunk in call.ResponseStream.ReadAllAsync(_stop.Token))
                        chunk.Chunk.WriteTo(f);
                }
                catch (RpcException e)
                {
                    throw new InvalidOperationException(e.Status.Detail);
                }
            }
            File.Move(tmp, path);
            return path;
        }
        finally
        {
            if (File.Exists(tmp))
                File.Delete(tmp);
        }
    }

    public void Dispose()
    {
        _stop.Cancel();
        _commands?.Dispose();
    }
}
