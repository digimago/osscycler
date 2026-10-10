using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// The start menu and the pickers (tui/menu.go, ride.go, history.go,
// activities.go, workout.go): what the rider can do, each row the same as
// its key from the dashboard. Lists come from the core when a picker
// opens.
public sealed partial class Controller
{
    enum Screen { None, Menu, Tracks, Picker }

    public enum Tab { Courses, History, Activities, Workouts }

    const int RideTabs = 3; // tab cycles COURSES, HISTORY, ACTIVITIES

    // Row is one line of a list: its key (a number or letter shown before
    // it), what it is, more about it, a star (a personal best), and whether
    // it is dimmed (off).
    public sealed record Row(string Key, string Label, string Detail, bool Star = false, bool Dim = false);

    // ListView is a list to show: a menu or a picker.
    // Credit: a line above the hint (the start menu's map credit).
    public sealed record ListView(string Title, string Subtitle, IReadOnlyList<string> Tabs, int Tab, IReadOnlyList<Row> Rows, int Selected, string Hint, string Credit = "");

    // MapCredit is the OpenStreetMap credit, in the start menu only (owner,
    // 2026-10-10: not on rides; the OSMF attribution guidelines allow a
    // start or menu screen for games and simulations), as the TUI.
    public const string MapCredit = "map data © OpenStreetMap contributors, ODbL";

    // PromptView is a value being typed.
    public sealed record PromptView(string Label, string Value);

    static readonly (string Key, string Label, string Does)[] MenuItems =
    {
        ("f", "Free ride", "just ride: round a test track, or the numbers alone"),
        ("r", "Ride a course", "a GPX course, racing your best time on it"),
        ("w", "Workout", "a structured workout, or a fixed power (ERG)"),
        ("a", "Activities", "your recorded rides: save FIT files, race past rides"),
        ("p", "Profile", "weight, height and FTP"),
        ("c", "Calibrate", "spin-down calibration of the trainer"),
        ("q", "Quit", "close the screen (the core keeps running)"),
    };

    Screen _screen;
    int _menuSel, _trackSel;
    Tab _tab;
    readonly int[] _pick = new int[4];
    bool _menuShownOnce;

    // The lists, as the core last sent them (null: not yet).
    IList<Course>? _courses;
    IList<RideResult>? _results;
    IList<Activity>? _activities;
    IList<WorkoutDef>? _workouts;

    // The value being typed, and what enter does with it.
    // Fresh: the value is a suggestion; the first digit typed replaces it
    // (enter takes it as it is, backspace edits it).
    (string Label, string Value, Func<double, Task> Set, string What, bool Fresh)? _prompt;

    public PromptView? Prompt => _prompt is { } p ? new(p.Label, p.Value) : null;

    // Busy: something under way that the menu would be in the way of.
    static bool Busy(State? st) =>
        st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding ||
        st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused ||
        st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level ||
        st?.Trainer?.Calibration?.Phase is CalibrationPhase.Requested or CalibrationPhase.InProgress;

    // Started opens the menu once, at the first state, when nothing is
    // under way (joining a ride skips it, as in the TUI).
    public void Started(State? st)
    {
        if (_menuShownOnce || st == null)
            return;
        _menuShownOnce = true;
        if (!Busy(st) && st.Ride?.Phase is not (RidePhase.Finished or RidePhase.Aborted))
            OpenMenu();
    }

    void OpenMenu()
    {
        (_screen, _menuSel) = (Screen.Menu, 0);
        Fetch(); // the free ride's tracks are courses
    }

    // OpenKey opens a menu or picker from the dashboard; whether it did.
    bool OpenKey(string key, State? st)
    {
        bool busy = Busy(st), course = st?.Ride is { Phase: RidePhase.Armed or RidePhase.Riding, Loop: false };
        bool working = st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused;
        switch (key)
        {
            case "m" when !busy:
                OpenMenu();
                return true;
            case "r" when !busy:
                OpenPicker(_tab == Tab.Workouts ? Tab.Courses : _tab);
                return true;
            case "w" when !course && !working:
                OpenPicker(Tab.Workouts); // also on a track: a workout rides on it
                return true;
        }
        return false;
    }

    void OpenPicker(Tab tab)
    {
        (_screen, _tab) = (Screen.Picker, tab);
        Fetch();
    }

    void Fetch()
    {
        Load("courses", _cmds.ListCourses, l => _courses = l);
        Load("history", _cmds.ListResults, l => _results = l);
        Load("activities", _cmds.ListActivities, l => _activities = l);
        Load("workouts", _cmds.ListWorkouts, l => _workouts = l);
    }

    void Load<T>(string what, Func<Task<IList<T>>> call, Action<IList<T>> set)
    {
        Task<IList<T>> t;
        try
        {
            t = call();
        }
        catch (Exception e)
        {
            Failed("loading " + what, e);
            return;
        }
        t.ContinueWith(done =>
        {
            if (done.Exception != null)
                Failed("loading " + what, done.Exception.GetBaseException());
            else
                lock (_mu)
                    set(done.Result);
        });
    }

    IList<Course> Tracks()
    {
        lock (_mu)
            return _courses?.Where(c => c.Loop).ToList() ?? new List<Course>();
    }

    void MenuKey(string key, State? st, double now)
    {
        switch (_screen)
        {
            case Screen.Menu:
                switch (key)
                {
                    case "up" or "k":
                        _menuSel = (_menuSel + MenuItems.Length - 1) % MenuItems.Length;
                        return;
                    case "down" or "j" or "tab":
                        _menuSel = (_menuSel + 1) % MenuItems.Length;
                        return;
                    case "esc" or "m":
                        _screen = Screen.None;
                        if (_menuPaused)
                        {
                            _menuPaused = false; // the pause came with the menu: leaving it carries on
                            SetPaused(false);
                        }
                        return;
                    case "enter" or " ":
                        Choose(MenuItems[_menuSel].Key, st);
                        return;
                }
                for (int i = 0; i < MenuItems.Length; i++)
                    if (key == MenuItems[i].Key || key == (i + 1).ToString())
                    {
                        Choose(MenuItems[i].Key, st);
                        return;
                    }
                return;
            case Screen.Tracks:
            {
                int n = Tracks().Count + 1;
                switch (key)
                {
                    case "up" or "k":
                        _trackSel = (_trackSel + n - 1) % n;
                        return;
                    case "down" or "j" or "tab":
                        _trackSel = (_trackSel + 1) % n;
                        return;
                    case "esc":
                        _screen = Screen.Menu;
                        return;
                    case "enter" or " ":
                        RideTrack(_trackSel);
                        return;
                }
                for (int i = 0; i < n; i++)
                    if (key == (i + 1).ToString())
                        RideTrack(i);
                return;
            }
            case Screen.Picker:
                PickerKey(key);
                return;
        }
    }

    void Choose(string item, State? st)
    {
        _screen = Screen.None;
        if (item == "q")
        {
            AskQuit(st); // the menu's pause stays with the question
            return;
        }
        _menuPaused = false; // whatever comes next, the pause stays as it is (a start carries on)
        switch (item)
        {
            case "f":
                (_screen, _trackSel) = (Screen.Tracks, 0);
                return;
            case "r":
                OpenPicker(Tab.Courses);
                return;
            case "w":
                OpenPicker(Tab.Workouts);
                return;
            case "a":
                OpenPicker(Tab.Activities);
                return;
            case "p":
                if (st?.Profile == null)
                    Say("the profile comes from the core: waiting for it");
                else
                    StartOnboarding(st, edit: true);
                return;
            case "c":
                if (!MayCalibrate(st))
                    Say("calibration needs the trainer connected and nothing under way");
                else
                    _countdownTo = _now + CountdownS;
                return;
        }
    }

    // RideTrack: choice 0 is the numbers alone (the dashboard), the rest
    // the tracks.
    void RideTrack(int choice)
    {
        _screen = Screen.None;
        var tracks = Tracks();
        if (choice >= 1 && choice <= tracks.Count)
        {
            var id = tracks[choice - 1].Id;
            Run("start ride", () => _cmds.StartRide(id));
        }
    }

    int Count(Tab t)
    {
        lock (_mu)
            return t switch
            {
                Tab.Courses => _courses?.Count ?? 0,
                Tab.History => _results?.Count ?? 0,
                Tab.Activities => _activities?.Count ?? 0,
                _ => (_workouts?.Count ?? 0) + 1, // Fixed power first
            };
    }

    void PickerKey(string key)
    {
        int t = (int)_tab, n = Count(_tab);
        switch (key)
        {
            case "tab" or "right" when _tab != Tab.Workouts:
                _tab = (Tab)((t + 1) % RideTabs);
                return;
            case "shift+tab" or "left" when _tab != Tab.Workouts:
                _tab = (Tab)((t + RideTabs - 1) % RideTabs);
                return;
            case "up" or "k":
                _pick[t] = Math.Max(0, _pick[t] - 1);
                return;
            case "down" or "j":
                _pick[t] = Math.Max(0, Math.Min(n - 1, _pick[t] + 1));
                return;
            case "esc" or "r" or "w":
                _screen = Screen.None;
                return;
            case "s" when _tab == Tab.Activities:
                SaveSelected();
                return;
            case "n" when _tab == Tab.Workouts:
                OpenForm("", null);
                return;
            case "e" when _tab == Tab.Workouts:
                lock (_mu)
                {
                    int i = _pick[t] - 1; // Fixed power first
                    if (_workouts != null && i >= 0 && i < _workouts.Count)
                        OpenForm(_workouts[i].Id, _workouts[i]);
                    else
                        Say("choose a workout to edit, or n for a new one");
                }
                return;
            case "f" when _tab == Tab.Workouts:
                Ask("FTP in watts", "", w => _cmds.SetFtp(w), "set FTP");
                return;
            case "enter":
                Enter();
                return;
        }
    }

    void Enter()
    {
        int i = _pick[(int)_tab];
        lock (_mu)
        {
            switch (_tab)
            {
                case Tab.Courses when _courses != null && i < _courses.Count:
                {
                    var id = _courses[i].Id;
                    _screen = Screen.None;
                    Run("start ride", () => _cmds.StartRide(id));
                    return;
                }
                case Tab.History when _results != null && i < _results.Count:
                    Race(_results[i]);
                    return;
                case Tab.Activities when _activities != null && i < _activities.Count:
                    // A course ride in it is raced; anything else is saved.
                    var name = _activities[i].Name;
                    var r = _results?.FirstOrDefault(x => x.File == name);
                    if (r != null)
                        Race(r);
                    else
                        SaveSelected();
                    return;
                case Tab.Workouts when i == 0:
                    Ask("Fixed power in watts", "", w => _cmds.SetPower(w), "set power");
                    return;
                case Tab.Workouts when _workouts != null && i - 1 < _workouts.Count:
                {
                    var id = _workouts[i - 1].Id;
                    _screen = Screen.None;
                    Run("start workout", () => _cmds.StartWorkout(id));
                    return;
                }
            }
        }
    }

    void Race(RideResult r)
    {
        _screen = Screen.None;
        string id = r.CourseId;
        long at = r.FinishedUnixMs;
        Run("start ride", () => _cmds.StartRideAgainst(id, at));
    }

    void SaveSelected()
    {
        string name;
        lock (_mu)
        {
            int i = _pick[(int)Tab.Activities];
            if (_activities == null || i >= _activities.Count)
                return;
            name = _activities[i].Name;
        }
        Say("saving " + name + " ...");
        _cmds.SaveActivity(name).ContinueWith(done => Say(done.Exception != null
            ? "saving " + name + " failed: " + done.Exception.GetBaseException().Message
            : "saved " + done.Result));
    }

    // Ask opens a prompt for a number; enter hands it to set.
    void Ask(string label, string value, Func<double, Task> set, string what)
    {
        _screen = Screen.None;
        _prompt = (label, value, set, what, value != "");
    }

    void PromptKey(string key, double now)
    {
        var p = _prompt!.Value;
        switch (key)
        {
            case "esc":
                _prompt = null;
                return;
            case "backspace":
                if (p.Value.Length > 0)
                    _prompt = p with { Value = p.Value[..^1], Fresh = false };
                return;
            case "enter":
                _prompt = null;
                if (double.TryParse(p.Value, NumberStyles.Float, CultureInfo.InvariantCulture, out var v))
                    Run(p.What, () => p.Set(v));
                else if (p.Value != "")
                    Say($"\"{p.Value}\" isn't a number");
                return;
        }
        string value = p.Fresh ? "" : p.Value;
        if (key.Length == 1 && (char.IsDigit(key[0]) || key == "." || key == "-" && value == "") && value.Length < 8)
            _prompt = p with { Value = value + key, Fresh = false };
    }

    // List is the menu or picker to show (null: none).
    public ListView? List()
    {
        if (_arranging != null)
            return Arranging();
        switch (_screen)
        {
            case Screen.Menu:
                return new("OSSCYCLER", "what now?", Array.Empty<string>(), 0,
                    MenuItems.Select((it, i) => new Row((i + 1).ToString(), it.Label, it.Does)).ToList(), _menuSel,
                    "↑ ↓ choose · enter or the number · esc back", MapCredit);
            case Screen.Tracks:
            {
                var rows = new List<Row> { new("1", "Just the numbers", "the dashboard: power, heart rate, cadence, speed") };
                int i = 2;
                foreach (var c in Tracks())
                {
                    string what = $"{Hud.Format.Fixed(c.DistanceM / 1000, 2)} km round, " + (c.GainM >= 1 ? $"{Hud.Format.Fixed(c.GainM, 0)} m up and down" : "flat");
                    if (Pb(c.Id) is { } r)
                        what += ", best lap " + Hud.Format.LapTime(r.ElapsedS);
                    rows.Add(new((i++).ToString(), c.Name, what));
                }
                return new("FREE RIDE", "where to?", Array.Empty<string>(), 0, rows, _trackSel,
                    "the trainer follows the track's grade; w starts a workout on it");
            }
            case Screen.Picker:
                return Picker();
        }
        return null;
    }

    RideResult? Pb(string courseId)
    {
        lock (_mu)
            return _results?.FirstOrDefault(r => r.CourseId == courseId && r.StartM < 1 && r.PersonalBest);
    }

    ListView Picker()
    {
        var rows = new List<Row>();
        string hint;
        lock (_mu)
        {
            switch (_tab)
            {
                case Tab.Courses:
                    foreach (var c in _courses ?? new List<Course>())
                    {
                        var pb = c.Loop ? null : _results?.FirstOrDefault(r => r.CourseId == c.Id && r.StartM < 1 && r.PersonalBest);
                        rows.Add(new("", (c.Loop ? "↻ " : "") + c.Name,
                            $"{Hud.Format.Fixed(c.DistanceM / 1000, 1)} km · {Hud.Format.Fixed(c.GainM, 0)} m up" + (pb != null ? " · PB " + Hud.Format.Clock(pb.ElapsedS) : ""), pb != null));
                    }
                    hint = "enter ride · tab history, activities · esc back";
                    break;
                case Tab.History:
                    foreach (var r in _results ?? new List<RideResult>())
                        rows.Add(new("", $"{When(r.FinishedUnixMs)}  {r.CourseName}",
                            $"{Hud.Format.Clock(r.ElapsedS)} · {Hud.Format.Fixed(r.AvgPowerW, 0)} W" + (r.StartM >= 1 ? $" · from {Hud.Format.Fixed(r.StartM / 1000, 2)} km" : ""), r.PersonalBest));
                    hint = "enter race it again · tab · esc back";
                    break;
                case Tab.Activities:
                    foreach (var a in _activities ?? new List<Activity>())
                        rows.Add(new("", When(a.StartUnixMs), a.Error != "" ? a.Error :
                            $"{Hud.Format.Clock(a.TimerS)} · {Hud.Format.Fixed(a.DistanceM / 1000, 1)} km · {Hud.Format.Fixed(a.AvgPowerW, 0)} W · {(a.Virtual ? "course" : "indoor")}" + (a.Laps > 1 ? $" · {a.Laps} laps" : "")));
                    hint = "enter race the course ride in it, or save the FIT file · s save · esc back";
                    break;
                default:
                    rows.Add(new("", "Fixed power (ERG)", "hold one power: type the watts"));
                    foreach (var w in _workouts ?? new List<WorkoutDef>())
                        rows.Add(new("", w.Name, w.Error != "" ? w.Error : Hud.Format.Clock(w.DurationS)));
                    hint = "enter start · n new · e edit · f FTP · esc back";
                    break;
            }
        }
        var tabs = _tab == Tab.Workouts ? new[] { "WORKOUTS" } : new[] { "COURSES", "HISTORY", "ACTIVITIES" };
        int sel = Math.Min(_pick[(int)_tab], Math.Max(0, rows.Count - 1));
        _pick[(int)_tab] = sel;
        return new(_tab == Tab.Workouts ? "WORKOUTS" : "RIDES", rows.Count == 0 ? "loading, or nothing yet" : "", tabs,
            _tab == Tab.Workouts ? 0 : (int)_tab, rows, sel, hint);
    }

    (string, List<(string, string)>) ScreenHelp() => _screen switch
    {
        Screen.Picker when _tab == Tab.Workouts => ("WORKOUTS", new()
        {
            ("↑ ↓  k j", "choose"), ("enter", "start the workout; on Fixed power: hold one power (ERG)"),
            ("n", "a new workout, in the editor"), ("e", "edit the chosen workout"),
            ("f", "set your FTP: targets are relative to it"), ("esc  w", "back"),
        }),
        Screen.Picker => ("RIDES", new()
        {
            ("tab ← →", "COURSES, HISTORY, ACTIVITIES"), ("↑ ↓  k j", "choose"),
            ("enter", "ride the course; race a past ride again; an activity: race it, else save its FIT file"),
            ("s", "save the activity's FIT file to Downloads"), ("esc  r", "back"),
        }),
        _ => ("MENU", new() { ("↑ ↓", "choose"), ("enter  1-7", "do it"), ("the letter", "the same as its key from the dashboard"), ("esc  m", "close") }),
    };

    static string When(long unixMs) =>
        DateTimeOffset.FromUnixTimeMilliseconds(unixMs).LocalDateTime.ToString("d MMM HH:mm", CultureInfo.InvariantCulture);
}
