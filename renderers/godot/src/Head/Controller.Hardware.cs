using System;
using Osscycler.V1;

namespace Osscycler.Head;

// Starting without the hardware (owner's list, item 12; tui/hardware.go):
// when no trainer has come within HardwareGraceS of the first state, a
// dialog says what is missing and offers the ways on: wait (it closes
// once the trainer comes), s a simulated rider in its place (the core's
// SetSimulated: nothing is recorded meanwhile), esc carry on without. Not
// for a core without a stick (-fake). On the dashboard s starts or stops
// the simulated rider later.
public sealed partial class Controller
{
    public const double HardwareGraceS = 8;

    double _firstStateAt = double.NaN;
    bool _hardwareLater, _hardwareUp;

    static bool TrainerMissing(State? st) =>
        st?.Radio != null && !st.Simulated && st.Trainer?.Sensor?.Status != SensorStatus.Connected;

    // HardwareTick decides, each frame, whether the dialog is up.
    void HardwareTick(State? st, double now)
    {
        if (st != null && double.IsNaN(_firstStateAt))
            _firstStateAt = now;
        _hardwareUp = !_hardwareLater && now - _firstStateAt >= HardwareGraceS && TrainerMissing(st) && _onboard == null && !Busy(st);
    }

    // HardwareKey: s or esc on the dialog (it takes every key but q); s on
    // the dashboard. Whether it used the key.
    bool HardwareKey(string key, State? st)
    {
        if (_hardwareUp)
        {
            switch (key)
            {
                case "s":
                    (_hardwareLater, _hardwareUp) = (true, false);
                    Run("the simulated rider", () => _cmds.SetSimulated(true));
                    break;
                case "esc":
                    (_hardwareLater, _hardwareUp) = (true, false);
                    Say("carrying on without a trainer: s rides a simulated rider");
                    break;
            }
            return key != "q";
        }
        if (key == "s" && _screen == Screen.None && _prompt == null)
        {
            if (st?.Simulated == true)
            {
                Run("stopping the simulated rider", () => _cmds.SetSimulated(false));
                return true;
            }
            if (TrainerMissing(st))
            {
                Run("the simulated rider", () => _cmds.SetSimulated(true));
                return true;
            }
        }
        return false;
    }

    DialogView? HardwareDialog(State? st)
    {
        if (!_hardwareUp || st == null)
            return null;
        static string Mark(bool ok, string yes, string no) => ok ? "● " + yes : "✕ " + no;
        return new("NO TRAINER YET", "waiting for the trainer", Hud.Palette.Plain, new[]
        {
            Mark(st.Radio?.Present == true, "ANT+ stick", "ANT+ stick: not found; plug it in"),
            Mark(st.Trainer?.Sensor?.Status == SensorStatus.Connected, "trainer", "trainer: not found; pedal to wake it"),
            Mark(st.HeartRate?.Sensor?.Status == SensorStatus.Connected, "heart rate strap", "heart rate strap: not found"),
            "Power, cadence and speed come from the trainer.",
            "This closes by itself when the trainer is found.",
        }, "s a simulated rider (nothing is recorded) · esc carry on without");
    }
}
