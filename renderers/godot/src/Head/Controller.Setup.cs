using System;
using System.Collections.Generic;
using System.Globalization;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// The rider's setup and the trainer outside rides (tui/onboard.go,
// control.go, end.go, calibration.go): the profile (asked for while the
// core lacks it), manual control, ending an activity, spin-down
// calibration.
public sealed partial class Controller
{
    public const double CountdownS = 10;     // before a spin-down starts
    public const double CalibrationShownS = 10;

    // DialogView is a panel in the middle: a title, a big line in its
    // colour (a number, STOP PEDALLING), lines and a hint.
    public sealed record DialogView(string Title, string Big, Hud.Rgb BigColor, IReadOnlyList<string> Lines, string Hint);

    // Onboarding: the profile's steps, as the TUI's.
    enum Step { Weight, Height, Ftp, View }

    sealed class Onboarding
    {
        public Step Step;
        public string Value = "";
        public bool Edit; // opened with p, not because the core asked
    }

    Onboarding? _onboard;
    bool _onboardLater; // put off with esc: rides and workouts wait for it
    double _discardUntil = double.NegativeInfinity;
    bool _ending;
    double _countdownTo = double.NaN; // when the spin-down starts (NaN: no countdown)

    static readonly Dictionary<ControlMode, double> ControlStep = new()
    {
        [ControlMode.Power] = 5, [ControlMode.Grade] = 0.5, [ControlMode.Level] = 5,
    };

    static bool ControlOn(State? st) => st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level;

    static bool Calibrating(State? st) => st?.Trainer?.Calibration?.Phase is CalibrationPhase.Requested or CalibrationPhase.InProgress;

    bool MayCalibrate(State? st) =>
        st?.Trainer?.Sensor?.Status == SensorStatus.Connected && !Calibrating(st) && double.IsNaN(_countdownTo) &&
        st.Ride?.Phase is not (RidePhase.Armed or RidePhase.Riding) &&
        st.Workout?.Phase is not (WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused);

    // ControlText is manual control as the status line shows it ("" off).
    public static string ControlText(State? st) => st?.Control?.Mode switch
    {
        ControlMode.Power => $"ERG {Hud.Format.Fixed(st.Control.Target, 0)} W",
        ControlMode.Grade => $"GRADE {Hud.Format.Fixed(st.Control.Target, 1)} %",
        ControlMode.Level => $"LEVEL {Hud.Format.Fixed(st.Control.Target, 0)} %",
        _ => "",
    };

    bool _wasBusy;
    string _doing = "";

    // Doing is what is under way, as a name that changes when it does
    // ("" for nothing).
    static string Doing(State st)
    {
        if (st.Ride?.Phase is RidePhase.Armed or RidePhase.Riding)
            return "ride " + st.Ride.CourseId;
        if (st.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused)
            return "workout " + st.Workout.Id;
        if (st.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level)
            return "control " + st.Control.Mode;
        if (st.Trainer?.Calibration?.Phase is CalibrationPhase.Requested or CalibrationPhase.InProgress)
            return "calibration";
        return "";
    }

    // Tick runs what time drives: the onboarding when the core lacks the
    // profile, the countdown to a spin-down. Call it every frame.
    public void Tick(State? st, double now)
    {
        FormDone();
        Others(st);
        var p = st?.Profile;
        if (_onboard == null && !_onboardLater && p != null && p.Missing.Count > 0 && !Busy(st) &&
            _prompt == null && _form == null && !_ending && _screen != Screen.Picker && double.IsNaN(_countdownTo))
        {
            _screen = Screen.None;
            StartOnboarding(st, edit: false);
        }
        if (!double.IsNaN(_countdownTo) && now >= _countdownTo)
        {
            _countdownTo = double.NaN;
            Run("start calibration", _cmds.StartCalibration);
        }
    }

    // Others closes what another head made stale (owner, 2026-10-09: the
    // TUI and the renderer can both be connected to the core, each able to
    // start or end what the other shows): the profile asked for and then
    // given there, the end-ride question once the recording ended or a
    // ride began, the countdown to a spin-down once something else drives
    // the trainer, the menu once a ride or workout started.
    void Others(State? st)
    {
        if (st == null)
            return;
        bool busy = Busy(st);
        if (_onboard is { Edit: false } && st.Profile is { } p && p.Missing.Count == 0)
        {
            _onboard = null;
            Say("profile set");
        }
        bool working = st.Ride?.Phase is RidePhase.Armed or RidePhase.Riding ||
            st.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused;
        if (_ending && (st.Recording is not { Active: true } || working))
        {
            _ending = false;
            Say("the ride was ended, or one started, on another screen");
        }
        if (!double.IsNaN(_countdownTo) && busy)
        {
            _countdownTo = double.NaN;
            Say("calibration aborted: the trainer is in use");
        }
        // Something else under way than a moment ago (a ride, a workout,
        // another kind of trainer control: owner, 2026-10-10, a script took
        // over the trainer and the menu stayed until a ride started): show
        // it.
        string doing = Doing(st);
        if (doing != "" && doing != _doing && (_screen is Screen.Menu or Screen.Tracks || _screen == Screen.Picker && _tab != Tab.Workouts))
            _screen = Screen.None;
        _doing = doing;
        _wasBusy = busy;
        if (_menuPaused && _wasPaused && !st.Paused && _screen == Screen.Menu)
        {
            (_screen, _menuPaused) = (Screen.None, false);
            Say("carried on on another screen");
        }
        _wasPaused = st.Paused;
        QuitOthers(st);
    }

    void StartOnboarding(State? st, bool edit)
    {
        var p = st?.Profile ?? new RiderProfile();
        var o = new Onboarding { Edit = edit };
        if (p.WeightKg > 0)
        {
            o.Value = Hud.Format.Fixed(p.WeightKg, 0);
            if (!edit) // only the FTP is missing
                (o.Step, o.Value) = (Step.Ftp, Hud.Format.Fixed(p.SuggestedFtpW, 0));
        }
        _onboard = o;
    }

    static string StepValue(RiderProfile? p, Step s) => s switch
    {
        Step.View => p?.View == "eyes" ? "eyes" : "chase",
        Step.Weight => p is { WeightKg: > 0 } ? Hud.Format.Fixed(p.WeightKg, 0) : "",
        Step.Height => p is { HeightCm: > 0 } ? Hud.Format.Fixed(p.HeightCm, 0) : "",
        _ => p is { FtpW: > 0 } ? Hud.Format.Fixed(p.FtpW, 0) : p != null ? Hud.Format.Fixed(p.SuggestedFtpW, 0) : "",
    };

    // SetupKey handles the dialogs and their keys from the dashboard;
    // whether it used the key.
    bool SetupKey(string key, State? st, double now)
    {
        if (_onboard is { } o)
        {
            OnboardKey(o, key, st);
            return true;
        }
        if (_ending)
        {
            switch (key)
            {
                case "enter" or "s":
                    _ending = false;
                    End(false);
                    break;
                case "d" when now < _discardUntil:
                    _ending = false;
                    End(true);
                    break;
                case "d":
                    _discardUntil = now + AbortConfirmS;
                    break;
                case "esc":
                    _ending = false;
                    break;
            }
            return true;
        }
        if (!double.IsNaN(_countdownTo))
        {
            if (key is "esc" or "c")
                _countdownTo = double.NaN; // aborted
            return true;
        }
        if (Calibrating(st))
        {
            if (key == "esc")
                Run("cancel calibration", _cmds.CancelCalibration);
            return true;
        }
        if (_screen != Screen.None || _prompt != null)
            return false;

        bool course = st?.Ride is { Phase: RidePhase.Armed or RidePhase.Riding, Loop: false };
        bool working = st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused;
        switch (key)
        {
            case "e" when st?.Recording?.Active == true && st.Ride?.Phase is not (RidePhase.Armed or RidePhase.Riding) && !working:
                (_ending, _discardUntil) = (true, double.NegativeInfinity);
                return true;
            case "c" when MayCalibrate(st) && st!.Trainer.ResistanceCalibrationRequired:
            case "C" when MayCalibrate(st):
                _countdownTo = now + CountdownS;
                return true;
            case "g" or "l" when !course && !working && !Calibrating(st):
            {
                bool grade = key == "g";
                var mode = grade ? ControlMode.Grade : ControlMode.Level;
                double v = st?.Control?.Mode == mode ? st.Control.Target : grade ? 3 : 30;
                Ask(grade ? "Grade in %" : "Brake level in % of maximum", Num(v),
                    grade ? x => _cmds.SetGrade(x) : x => _cmds.SetLevel(x), "trainer control");
                return true;
            }
        }
        if (ControlOn(st) && !course && !working)
        {
            var c = st!.Control;
            switch (key)
            {
                case "+" or "=" or "-" or "_":
                {
                    double step = ControlStep[c.Mode];
                    double want = key is "-" or "_"
                        ? (Math.Ceiling(c.Target / step - 1e-9) - 1) * step
                        : (Math.Floor(c.Target / step + 1e-9) + 1) * step;
                    Func<double, Task> set = c.Mode switch
                    {
                        ControlMode.Power => _cmds.SetPower,
                        ControlMode.Grade => _cmds.SetGrade,
                        _ => _cmds.SetLevel,
                    };
                    Run("trainer control", () => set(want));
                    return true;
                }
                case "x" or "0":
                    Run("release trainer", _cmds.ReleaseControl);
                    return true;
            }
        }
        return false;
    }

    static string Num(double v) => v == Math.Truncate(v) ? Hud.Format.Fixed(v, 0) : Hud.Format.Fixed(v, 1);

    void OnboardKey(Onboarding o, string key, State? st)
    {
        switch (key)
        {
            case "esc":
                if (o.Step > Step.Weight && o.Edit)
                {
                    o.Step--;
                    o.Value = StepValue(st?.Profile, o.Step);
                    return;
                }
                _onboard = null;
                if (!o.Edit)
                {
                    _onboardLater = true;
                    Say("rides and workouts wait for your profile: m, then p (Profile), when ready");
                }
                return;
            case "left" or "right" or " " or "space" or "up" or "down" when o.Step == Step.View:
                o.Value = o.Value == "eyes" ? "chase" : "eyes";
                return;
            case "c" or "e" when o.Step == Step.View:
                o.Value = key == "c" ? "chase" : "eyes";
                return;
            case "enter" when o.Step == Step.View:
            {
                var view = o.Value;
                _cmds.SetView(view).ContinueWith(done =>
                {
                    if (done.Exception != null)
                    {
                        Failed("saving the view", done.Exception.GetBaseException());
                        return;
                    }
                    lock (_mu)
                        if (_onboard == o)
                        {
                            _onboard = null;
                            Say("profile saved");
                        }
                }, TaskContinuationOptions.ExecuteSynchronously);
                return;
            }
            case "backspace":
                if (o.Value.Length > 0)
                    o.Value = o.Value[..^1];
                return;
            case "enter":
            {
                if (o.Step == Step.Height && o.Value == "")
                {
                    (o.Step, o.Value) = (Step.Ftp, StepValue(st?.Profile, Step.Ftp)); // optional: skipped
                    return;
                }
                var (lo, hi, what) = o.Step switch
                {
                    Step.Weight => (30.0, 200.0, "weight in kg"),
                    Step.Height => (120.0, 220.0, "height in cm"),
                    _ => (50.0, 600.0, "FTP in watts"),
                };
                if (!double.TryParse(o.Value, NumberStyles.Float, CultureInfo.InvariantCulture, out var v) || v < lo || v > hi)
                {
                    Say($"{what}: {lo:F0} to {hi:F0}");
                    return;
                }
                var step = o.Step;
                Task<RiderProfile> save = step switch
                {
                    Step.Weight => _cmds.SetProfile(v, null, null),
                    Step.Height => _cmds.SetProfile(null, null, v),
                    _ => _cmds.SetProfile(null, v, null),
                };
                save.ContinueWith(done =>
                {
                    if (done.Exception != null)
                    {
                        Failed("saving the profile", done.Exception.GetBaseException());
                        return;
                    }
                    lock (_mu)
                    {
                        if (_onboard != o)
                            return;
                        if (step < Step.Ftp || step == Step.Ftp && o.Edit && done.Result.View != "")
                            (o.Step, o.Value) = (step + 1, StepValue(done.Result, step + 1)); // editing: the view too, if the core keeps it
                        else
                        {
                            _onboard = null;
                            Say(done.Result.WeightForced || done.Result.FtpForced
                                ? "profile set; what flags on the core force applies to this run only" : "profile saved");
                        }
                    }
                }, TaskContinuationOptions.ExecuteSynchronously);
                return;
            }
        }
        if (o.Step != Step.View && key.Length == 1 && (char.IsDigit(key[0]) || key == ".") && o.Value.Length < 5)
            o.Value += key;
    }

    void End(bool discard) =>
        _cmds.EndActivity(discard).ContinueWith(done => Say(done.Exception != null
            ? "ending the ride failed: " + done.Exception.GetBaseException().Message
            : discard ? "ride discarded" : "ride saved: " + done.Result), TaskContinuationOptions.ExecuteSynchronously);

    // Dialog is the setup panel to show (null: none).
    public DialogView? Dialog(State? st, double now)
    {
        if (QuitDialog(st) is { } q)
            return q;
        if (_onboard is { Step: Step.View } ov)
        {
            bool chase = ov.Value != "eyes";
            return new("RIDER PROFILE", chase ? "View: ▸ behind the rider ◂   your eyes" : "View:   behind the rider   ▸ your eyes ◂", Hud.Palette.Plain,
                new[] { "How the 3D view shows your ride: from behind and above you (you see yourself and your ghost), or through your own eyes. v switches while riding." },
                "step 4 of 4 · ← → choose · enter save · esc back");
        }
        if (_onboard is { } o)
        {
            var p = st?.Profile;
            var (prompt, unit, why, forced) = o.Step switch
            {
                Step.Weight => ("Your weight", "kg", "It sets how climbs feel on the trainer and your speed on a course.", p?.WeightForced == true),
                Step.Height => ("Your height", "cm", "Optional: with your weight it sizes how much air you push on a course. Leave it empty to skip.", p?.HeightForced == true),
                _ => ("Your FTP", "W", "The power you can hold for about an hour: workouts are set from it. Not sure? Take the suggestion.", p?.FtpForced == true),
            };
            var lines = new List<string> { why };
            if (forced)
                lines.Add("a flag on the core forces this for its run: what you enter is saved for later");
            return new(o.Edit ? "RIDER PROFILE" : "WELCOME TO OSSCYCLER", $"{prompt}: {o.Value}_ {unit}", Hud.Palette.Plain, lines,
                $"step {(int)o.Step + 1} of {(o.Edit && st?.Profile?.View is { Length: > 0 } ? 4 : 3)} · enter save and go on · esc {(o.Edit && o.Step > Step.Weight ? "back" : "later")}");
        }
        if (_ending)
        {
            var r = st?.Recording ?? new Recording();
            bool discard = now < _discardUntil;
            return new("END RIDE?", discard ? "press d again to delete this ride" : $"{Hud.Format.Clock(r.TimerS)} riding · {Hud.Format.Fixed(r.DistanceM / 1000, 2)} km",
                discard ? Hud.Palette.Behind : Hud.Palette.Plain, Array.Empty<string>(),
                discard ? "enter save instead · esc keep riding" : "enter save · d discard · esc keep riding");
        }
        if (!double.IsNaN(_countdownTo))
            return new("SPIN-DOWN CALIBRATION", $"{Math.Ceiling(_countdownTo - now):F0}", Hud.Palette.Power,
                new[] { "When it starts: pedal up until the screen says STOP,", "then stop pedalling and let the trainer coast to a stop." }, "esc abort");
        var c = st?.Trainer?.Calibration;
        switch (c?.Phase)
        {
            case CalibrationPhase.Requested:
                return new("SPIN-DOWN CALIBRATION", "waiting for the trainer to start", Hud.Palette.Plain, Array.Empty<string>(), "esc cancel");
            case CalibrationPhase.InProgress:
            {
                var tr = st!.Trainer;
                string speed = tr.HasSpeedMps ? Hud.Format.Fixed(tr.SpeedMps * 3.6, 1) + " km/h" : "-- km/h";
                var lines = new List<string>();
                if (c.TemperatureCondition == CalibrationCondition.TooLow)
                    lines.Add("trainer too cold: ride about 10 minutes to warm up, then retry");
                if (c.Message != "")
                    lines.Add(c.Message);
                return c.SpeedCondition == CalibrationCondition.Ok
                    ? new("SPIN-DOWN CALIBRATION", "STOP PEDALLING · " + speed, Hud.Palette.Ahead, lines.Count > 0 ? lines : new List<string> { "let the trainer coast to a stop" }, "esc cancel")
                    : new("SPIN-DOWN CALIBRATION", "PEDAL UP" + (c.HasTargetSpeedMps ? $" to {c.TargetSpeedMps * 3.6:F0} km/h" : "") + " · " + speed, Hud.Palette.Power, lines, "esc cancel");
            }
            case CalibrationPhase.Succeeded or CalibrationPhase.Failed or CalibrationPhase.Cancelled
                when (st!.CoreTimeNs - c.PhaseChangedNs) / 1e9 < CalibrationShownS:
                return c.Phase switch
                {
                    CalibrationPhase.Succeeded => new("SPIN-DOWN CALIBRATION", "CALIBRATED", Hud.Palette.Ahead,
                        new[] { c.HasSpinDownMs ? $"spin-down {c.SpinDownMs} ms" + (c.HasTargetSpinDownMs ? $" (target {c.TargetSpinDownMs} ms)" : "") : "the trainer reports no spin-down time" }, ""),
                    CalibrationPhase.Failed => new("SPIN-DOWN CALIBRATION", "CALIBRATION FAILED", Hud.Palette.Behind, new[] { c.Message }, "C retry"),
                    _ => new("SPIN-DOWN CALIBRATION", "calibration cancelled", Hud.Palette.Dim, Array.Empty<string>(), ""),
                };
        }
        return null;
    }
}
