using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// Quitting (owner, 2026-10-10: q during a ride must not exit, the menu is
// where to quit from), as the TUI: q off the menu opens the menu, pausing
// what is under way; the menu's Quit (or q there) closes the screen, and
// with something under way and no other screen connected asks first: end
// it and save the ride, or leave it paused (the core pauses a ride no
// screen watches after 30 s and ends it after 30 minutes anyway). A
// scripted run (--shot, --keys) isn't a head and quits without asking.
public sealed partial class Controller
{
    bool _quitting; // the question is on screen

    // Watcher: the renderer follows the core without being a head (a
    // scripted run); it quits without asking.
    public bool Watcher { get; set; }

    // QuitKey: q off the menu, and the question; whether it used the key.
    bool QuitKey(string key, State? st)
    {
        if (_quitting)
        {
            switch (key)
            {
                case "enter" or "q":
                    _quitting = false;
                    EndAndQuit(st);
                    break;
                case "l":
                    _quitting = false;
                    Then(() => _cmds.SetPaused(true));
                    break;
                case "esc":
                    _quitting = false;
                    OpenMenu();
                    _menuSel = MenuItems.Length - 1; // back on Quit
                    break;
            }
            return true;
        }
        if (key != "q" || !Free)
            return false;
        OpenMenu();
        if (UnderWay(st) && st?.Paused != true)
        {
            _menuPaused = true;
            SetPaused(true);
        }
        return true;
    }

    // AskQuit is the menu's Quit.
    void AskQuit(State? st)
    {
        if (!Watcher && UnderWay(st) && (st?.Heads ?? 0) <= 1)
            _quitting = true;
        else
            Quit = true;
    }

    // EndAndQuit ends what is under way, saves the activity, then quits.
    void EndAndQuit(State? st)
    {
        var calls = new List<Func<Task>>();
        if (st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding)
            calls.Add(_cmds.StopRide);
        if (st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused)
            calls.Add(_cmds.StopWorkout);
        if (st?.Control?.Mode is ControlMode.Power or ControlMode.Grade or ControlMode.Level)
            calls.Add(_cmds.ReleaseControl);
        if (st?.Recording?.Active == true)
            calls.Add(() => _cmds.EndActivity(false));
        Then(calls.ToArray());
    }

    // Then makes calls to the core in order, then quits (a call that fails,
    // with the core gone say, is no reason to stay).
    // An async method rather than Task.Run: calls that complete at once
    // (the tests' fake core) finish here and now.
    void Then(params Func<Task>[] calls) => _ = ThenAsync(calls);

    async Task ThenAsync(Func<Task>[] calls)
    {
        foreach (var call in calls)
        {
            try
            {
                await call();
            }
            catch (Exception)
            {
            }
        }
        Quit = true;
    }

    // QuitDialog is the question, when it is on.
    DialogView? QuitDialog(State? st)
    {
        if (!_quitting)
            return null;
        string what = st?.Workout?.Phase is WorkoutPhase.Armed or WorkoutPhase.Running or WorkoutPhase.Paused ? $"the workout {st.Workout.Name}"
            : st?.Ride?.Phase is RidePhase.Armed or RidePhase.Riding ? $"the ride on {st.Ride.CourseName}" : "manual control";
        return new("QUIT?", $"{what} is under way", Hud.Palette.Plain,
            new[] { "and no other screen is connected.", "l quits and leaves it paused: the core ends and saves it after 30 minutes without a screen." },
            "enter end it, save the ride and quit · l leave it paused · esc back");
    }

    // QuitOthers: the question goes once the ride ended, or is watched, on
    // another screen: the screen closes as asked.
    void QuitOthers(State st)
    {
        if (_quitting && (!UnderWay(st) || st.Heads > 1))
            (_quitting, Quit) = (false, true);
    }
}
