using Osscycler.V1;

namespace Osscycler.Head;

// Pausing (owner, 2026-10-10: park the core for a coffee), as the TUI:
// p or P (and space, outside the renderer's debug mode, where space
// marks a spot while riding) pauses and carries on while nothing that
// takes keys is open (owner, 2026-10-10: a paused ride didn't carry on: p
// was the profile, refused while riding; the profile is the menu's now,
// so p is pause everywhere else); esc
// during a ride, a workout or manual control pauses and opens the menu,
// and esc out of that menu carries on. The core holds everything while
// paused (SetPaused); the head only asks.
public sealed partial class Controller
{
    bool _menuPaused; // the menu was opened by esc, pausing the core
    bool _wasPaused;

    // UnderWay: something drives the trainer (a ride, a workout, manual
    // control).
    static bool UnderWay(State? st) =>
        st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding ||
        st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused ||
        st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level;

    bool PauseKey(string key, State? st)
    {
        if (!Free || Calibrating(st))
            return false;
        bool paused = st?.Paused == true;
        switch (key)
        {
            case "p" or "P" or " " or "space":
                SetPaused(!paused);
                return true;
            case "esc" when UnderWay(st) && !paused:
                OpenMenu();
                _menuPaused = true;
                SetPaused(true);
                return true;
        }
        return false;
    }

    void SetPaused(bool on) => Run(on ? "pause" : "carry on", () => _cmds.SetPaused(on));
}
