using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// Commands are what the head asks of the core (its RPCs). The core
// enforces the rules about riding; the head only asks.
public interface ICommands
{
    Task StopRide();
    // TurnAround turns the rider round onto the course's other way; the
    // course ridden on now, "" when the ride ended instead (on a run-in).
    Task<string> TurnAround();
    Task SetDifficulty(double pct);
    Task StopWorkout();
    Task SkipSegment();
    Task SetIntensity(double pct);

    Task<IList<Course>> ListCourses();
    Task<IList<RideResult>> ListResults();
    Task<IList<Activity>> ListActivities();
    Task<IList<WorkoutDef>> ListWorkouts();
    // SaveWorkout writes a workout to the core's library ("" id: a new
    // one); its id.
    Task<string> SaveWorkout(string id, WorkoutDef w);
    Task StartRide(string courseId);
    Task StartRideAgainst(string courseId, long finishedUnixMs);
    Task StartWorkout(string id);
    Task SetPower(double watts);
    // SetPaused parks the core or carries on (it holds rides, workouts and
    // the trainer; a start carries on).
    Task SetPaused(bool paused);
    // SetSimulated puts a simulated rider in place of the trainer, or
    // takes it away.
    Task SetSimulated(bool on);
    Task SetFtp(double watts);
    Task SetGrade(double pct);
    Task SetLevel(double pct);
    Task ReleaseControl();
    // SetProfile saves what is given (null: unchanged); the profile now.
    Task<RiderProfile> SetProfile(double? weightKg, double? ftpW, double? heightCm, double? maxHrBpm);
    // SetChoice saves one of the profile's settings with a fixed set of
    // values (world_build, world_fetch; SetProfileRequest's field names).
    Task<RiderProfile> SetChoice(string name, string value);
    // SetView saves the rider's view in a 3D renderer ("chase" or "eyes").
    Task<RiderProfile> SetView(string view);
    // EndActivity saves (or discards) the recording; the file saved.
    Task<string> EndActivity(bool discard);
    Task StartCalibration();
    Task CancelCalibration();
    // SaveActivity saves a recording's FIT file on this machine; the path.
    Task<string> SaveActivity(string name);
    // ActivityHistory is an activity's samples from riding second since on:
    // the one being recorded (name "") or a finished one.
    Task<GetActivityHistoryResponse> ActivityHistory(string name, uint since);
    // ActivitySummary sums an activity up, named as for ActivityHistory.
    Task<ActivitySummary> ActivitySummary(string name);
    // RideSummary sums up the last course ride of the activity being
    // recorded: from its start to its finish.
    Task<ActivitySummary> RideSummary();
}

// Controller is the renderer as a head (owner, 2026-10-09: a full
// replacement for the TUI): keys in, commands to the core and what to show
// out, by the TUI's rules (tui/ride.go rideKey, workout.go, help.go).
// Plain C#, apart from Godot: keys are named as the TUI names them ("+",
// "x", "enter", "esc", "f1", ...). Times in seconds of the renderer's clock.
public sealed partial class Controller
{
    public const double AbortConfirmS = 3;  // a first x waits this long for the second
    public const double DifficultyStep = 10, IntensityStep = 1;
    public const double NoticeS = 8;

    readonly ICommands _cmds;
    readonly object _mu = new();
    double _abortUntil = double.NegativeInfinity;
    string _confirmKey = "";
    string _asking = "";
    string _notice = "";
    double _noticeFrom = double.NaN; // when it was first shown (NaN: not yet)

    // The layout is read from layoutPath ("" keeps the defaults, and
    // changes last the session).
    public Controller(ICommands cmds, string layoutPath = "")
    {
        _cmds = cmds;
        LoadLayout(layoutPath);
    }

    // Help is the key map shown (null: none).
    public (string Title, List<(string Keys, string What)> Entries)? Help { get; private set; }

    // Quit is set when the rider asks to leave (the core rides on).
    // Quit: the screen is to close (it may be set after calls to the core
    // finish: check it every frame).
    public bool Quit { get; private set; }

    // Free is whether nothing that takes keys is open (help, a menu or
    // picker, a prompt, the layout editor, the workout form, onboarding,
    // the end-ride question, a spin-down's countdown): keys of the
    // renderer's own (the debug mark's space) are only for then.
    public bool Free => Help == null && _arranging == null && _form == null && _onboard == null && !_ending && !_hardwareUp && Summary == null
        && double.IsNaN(_countdownTo) && _prompt == null && _screen == Screen.None && !_quitting;

    // Note shows a notice from the renderer itself (a mark made).
    public void Note(string text) => Say(text);

    // Asking is a question waiting for its second key ("press x again ...").
    public string Asking(double now) => now < _abortUntil ? _asking : "";

    // Notice is the last thing that went wrong (a command the core refused).
    public string Notice(double now)
    {
        lock (_mu)
        {
            if (_notice != "" && double.IsNaN(_noticeFrom))
                _noticeFrom = now; // shown from the first frame after it came
            return now < _noticeFrom + NoticeS ? _notice : "";
        }
    }

    double _now; // the time of the last key

    public void Key(string key, State? st, double now)
    {
        _now = now;
        if (Help != null)
        {
            Help = null; // any key closes it, without acting
            return;
        }
        if (key == "f1" || key is "?" or "h" && _prompt == null && _form == null)
        {
            Help = _prompt != null ? ("TYPING", new() { ("0-9 . -", "the value"), ("backspace", "delete"), ("enter", "set it"), ("esc", "cancel") })
                : _form != null ? WorkoutForm.Help
                : _arranging != null ? ArrangeHelp
                : _screen != Screen.None ? ScreenHelp() : HelpFor(st, _courses);
            return;
        }
        if (SummaryKey(key))
            return;
        if (_arranging != null)
        {
            ArrangeKey(key);
            return;
        }
        if (HardwareKey(key, st))
            return;
        if (_form != null)
        {
            FormKey(key, now);
            return;
        }
        if (QuitKey(key, st) || PauseKey(key, st))
            return;
        if (SetupKey(key, st, now))
            return;
        if (_prompt != null)
        {
            PromptKey(key, now);
            return;
        }
        if (_screen != Screen.None)
        {
            MenuKey(key, st, now);
            return;
        }
        if (OpenKey(key, st) || LayoutKey(key, st) || GraphKey(key))
            return;
        var r = st?.Ride;
        var w = st?.Workout;
        bool riding = r?.Phase is RidePhase.Armed or RidePhase.Riding;
        bool working = w?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused;

        if (working)
        {
            switch (key)
            {
                case "+" or "=" or "-" or "_":
                    double step = key is "-" or "_" ? -IntensityStep : IntensityStep;
                    double want = Math.Round(w!.IntensityPct + step);
                    Run("set intensity", () => _cmds.SetIntensity(want));
                    return;
                case "n":
                    Run("skip", _cmds.SkipSegment);
                    return;
                case "x":
                    Confirm(now, "x", "press x again to abort the workout", () => Run("abort workout", _cmds.StopWorkout));
                    return;
            }
            return;
        }
        if (w?.Phase is WorkoutPhase.Finished or WorkoutPhase.Aborted && key is "x" or "enter")
        {
            Run("close workout", _cmds.StopWorkout);
            return;
        }
        if (riding)
        {
            switch (key)
            {
                case "+" or "=" or "-" or "_":
                    // To the next multiple of the step in the key's direction
                    // (55 → 60 or 50), as the TUI.
                    double d = r!.DifficultyPct / DifficultyStep;
                    double want = key is "-" or "_" ? (Math.Ceiling(d) - 1) * DifficultyStep : (Math.Floor(d) + 1) * DifficultyStep;
                    want = Math.Clamp(want, 0, 100);
                    Run("set difficulty", () => _cmds.SetDifficulty(want));
                    return;
                case "x":
                    Confirm(now, "x", r!.Loop ? "press x again to end the ride" : "press x again to abort the ride", () => Run("abort ride", _cmds.StopRide));
                    return;
                case "r":
                    TurnKey(now, r!);
                    return;
            }
            return;
        }
        if (r?.Phase is RidePhase.Finished or RidePhase.Aborted && key is "x" or "enter")
            Run("close ride", _cmds.StopRide);
    }

    // Confirm asks for key again (x x, r r): the second press within a
    // few seconds acts; another key's first press doesn't count for it.
    void Confirm(double now, string key, string question, Action act)
    {
        if (now < _abortUntil && _confirmKey == key)
        {
            _abortUntil = double.NegativeInfinity;
            act();
            return;
        }
        (_abortUntil, _asking, _confirmKey) = (now + AbortConfirmS, question, key);
    }

    // TurnKey is r during a course ride: r again turns round onto the
    // course's other way, untimed until a timing line (owner, 2026-10-20:
    // r r, as x x, since the ride so far no longer counts; tui/turn.go).
    void TurnKey(double now, Ride r)
    {
        if (OtherWay(r, _courses) == null)
        {
            Say("this course can't be ridden the other way");
            return;
        }
        string question = OnRunIn(r)
            ? "press r again to end the ride (no way back along the course from the run-in)"
            : "press r again to turn round (this ride won't count; timed again from the next line)";
        Confirm(now, "r", question, () => Run("turn round", () => _cmds.TurnAround()));
    }

    // ReverseSuffix ends a reversed course's ID (course.ReverseSuffix).
    const string ReverseSuffix = "-reverse";

    // OtherWay is the course that rides r's the other way: its reverse, or
    // the forward course of a reverse; null when it has none (a test track,
    // whose lap is the whole course). Without the course list, the core
    // has the last word.
    public static string? OtherWay(Ride r, IList<Course>? courses)
    {
        if (r.Loop && r.LapStartM == 0 && r.LapLengthM >= r.CourseDistanceM - 0.01)
            return null;
        string id = r.CourseId;
        string other = id.EndsWith(ReverseSuffix, StringComparison.Ordinal) ? id[..^ReverseSuffix.Length] : id + ReverseSuffix;
        if (courses == null)
            return other;
        foreach (var c in courses)
            if (c.Id == other)
                return other;
        return null;
    }

    // TurnHelp is r r's line in the key map; none on a test track.
    static List<(string, string)> TurnHelp(Ride r, IList<Course>? courses) => OtherWay(r, courses) == null
        ? new()
        : new() { ("r r", OnRunIn(r) ? "end the ride (no way back along the course from the run-in)" : "turn round: on along the course the other way, untimed until its next timing line") };

    // OnRunIn: on a lapped course's run-in out, which the other way has no
    // way back along: turning round there ends the ride.
    public static bool OnRunIn(Ride r) => r.Loop && r.LapLengthM > 0 && r.DistanceM < r.LapStartM;

    // Run sends a command; a failure shows as a notice.
    void Run(string what, Func<Task> call)
    {
        Task t;
        try
        {
            t = call();
        }
        catch (Exception e)
        {
            Failed(what, e);
            return;
        }
        t.ContinueWith(done =>
        {
            if (done.Exception != null)
                Failed(what, done.Exception.GetBaseException());
        }, TaskContinuationOptions.ExecuteSynchronously);
    }

    void Failed(string what, Exception e) => Say($"{what} failed: {e.Message}");

    // Say shows a notice for a while.
    void Say(string text)
    {
        lock (_mu)
            (_notice, _noticeFrom) = (text, double.NaN);
    }

    // HelpFor is the key map for what is on now (tui/help.go).
    public static (string, List<(string, string)>) HelpFor(State? st, IList<Course>? courses = null)
    {
        var r = st?.Ride;
        var w = st?.Workout;
        var common = new List<(string, string)>
        {
            ("o", "arrange the tiles and strips of this screen"), ("z", "digit size: large or medium"),
            ("t", "the graph of power and heart rate over the ride: show or hide"),
            ("v", "the view: from the rider's eyes, or from behind the rider"),
            ("? h F1", "this help (any key closes it)"), ("q", "the menu (pauses what is under way); quit from there"),
        };
        var pause = new List<(string, string)>
        {
            ("p  space", "pause: the trainer goes flat and the clocks stand (a paused ride never counts as a PB)"),
            ("esc", "pause and open the menu (esc again carries on)"),
        };
        if (w?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused)
            return ("WORKOUT", Concat(new()
            {
                ("+ / -", $"intensity in 1 % steps (now {w.IntensityPct:F0} %)"),
                ("n", "skip to the next part"),
                ("x x", "abort the workout"),
            }, Concat(pause, common)));
        if (r?.Phase is RidePhase.Armed or RidePhase.Riding)
            return (r.Loop ? "TRACK" : "COURSE RIDE", Concat(new()
            {
                ("+ / -", $"trainer difficulty in 10 % steps (now {r.DifficultyPct:F0} %)"),
                ("x x", r.Loop ? "end the ride (each lap is kept; your best lap is the ghost)" : "abort the ride (still recorded; only a finished ride counts as a ghost)"),
            }, Concat(TurnHelp(r, courses), Concat(pause, common))));
        if (r?.Phase is RidePhase.Finished or RidePhase.Aborted || w?.Phase is WorkoutPhase.Finished or WorkoutPhase.Aborted)
            return ("RESULT", Concat(new() { ("x  enter", "close") }, common));
        if (st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level)
            return ("TRAINER CONTROL", Concat(new()
            {
                ("+ / -", "step the target"), ("w", "a new power, or a workout"),
                ("g / l", "a fixed grade / brake level instead"), ("x  0", "back to free riding (the trainer is set flat)"),
            }, Concat(pause, common)));
        return ("FREE RIDE", Concat(new()
        {
            ("m", "the menu: everything you can do from here"),
            ("r", "rides: courses, your history, recordings"),
            ("w", "workouts, or a fixed power (ERG)"),
            ("g / l", "ride at a fixed grade / brake level"),
            ("c / C", "spin-down calibration: when the trainer asks / any time"),
            ("e", "end the ride: save or discard the recording"),
            ("p  space", "pause the recording (anything you start carries on)"),
        }, common));
    }

    static List<(string, string)> Concat(List<(string, string)> a, List<(string, string)> b)
    {
        a.AddRange(b);
        return a;
    }
}
