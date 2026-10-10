using System;
using System.Collections.Generic;
using System.Linq;
using Osscycler.Hud;
using Osscycler.V1;

namespace Osscycler.Head;

// Arranging the HUD (tui/layout.go): o opens the editor for the screen on
// now (its tiles, and the strips), z switches the digits between large and
// medium, v the view between the rider's eyes and behind the rider. Kept
// in hud.json on the renderer's machine.
public sealed partial class Controller
{
    Layout _layout = new();
    string _layoutPath = "";
    Arranger? _arranging;

    // Layout is the arrangement the HUD shows.
    public Layout Layout => _layout;

    string? _viewWanted; // a view switched to, until the core's profile says so

    // Chase tells whether the view is behind the rider: the rider's
    // profile says (owner, 2026-10-09: a preference in the profile), else,
    // without a core or with one that doesn't keep it, this machine's
    // layout; chase by default.
    public bool Chase(Osscycler.V1.State? st)
    {
        var v = st?.Profile?.View;
        if (v is "chase" or "eyes")
        {
            if (_viewWanted == v)
                _viewWanted = null;
            return (_viewWanted ?? v) == "chase";
        }
        return _layout.Chase;
    }

    void LoadLayout(string path)
    {
        if (path == "")
            return;
        try
        {
            _layout = Layout.Load(path);
            _layoutPath = path;
        }
        catch (Exception e)
        {
            // A broken file is never overwritten: changes last the session.
            Say($"{path}: {e.Message} (changes to the HUD won't be saved)");
        }
    }

    // ArrangeScreen is the screen whose tiles o arranges now (null: none,
    // as on a result, a calibration or a countdown).
    static string? ArrangeScreen(State? st)
    {
        if (st?.Ride?.Phase is RidePhase.Finished or RidePhase.Aborted || st?.Workout?.Phase is WorkoutPhase.Finished or WorkoutPhase.Aborted)
            return null;
        if (st?.Trainer?.Calibration?.Phase is CalibrationPhase.Requested or CalibrationPhase.InProgress)
            return null;
        return Tiles.Screen(st);
    }

    // LayoutKey handles o and z from a screen without a list or prompt.
    bool LayoutKey(string key, State? st)
    {
        switch (key)
        {
            case "o" when ArrangeScreen(st) is { } screen:
                _arranging = new Arranger(_layout, screen);
                return true;
            case "z":
                var l = _layout.Clone();
                l.Digits = l.Medium ? "" : "medium";
                Keep(l);
                return true;
            case "v":
                string next = Chase(st) ? "eyes" : "chase";
                if (st?.Profile?.View is "chase" or "eyes")
                {
                    // The rider's preference, in the core's profile.
                    _viewWanted = next;
                    Run("saving the view", async () =>
                    {
                        try
                        {
                            await _cmds.SetView(next);
                        }
                        catch
                        {
                            _viewWanted = null; // still as the profile has it
                            throw;
                        }
                    });
                    return true;
                }
                var v = _layout.Clone();
                v.View = next == "eyes" ? "eyes" : "";
                Keep(v);
                return true;
        }
        return false;
    }

    void ArrangeKey(string key)
    {
        var a = _arranging!;
        if (key == "z")
        {
            LayoutKey(key, null);
            return;
        }
        if (a.Key(key, out var result))
        {
            _arranging = null;
            if (result != null)
            {
                (result.Digits, result.View) = (_layout.Digits, _layout.View); // z while arranging
                Keep(result);
            }
            return;
        }
        if (a.Notice != "")
            Say(a.Notice);
    }

    // Keep uses a layout and saves it.
    void Keep(Layout l)
    {
        _layout = l;
        if (_layoutPath == "")
            return;
        try
        {
            l.Save(_layoutPath);
        }
        catch (Exception e)
        {
            Failed("saving the HUD's layout", e);
        }
    }

    ListView Arranging()
    {
        var a = _arranging!;
        var rows = new List<Row>();
        int n = 0;
        foreach (var id in a.Order)
        {
            bool on = a.On.Contains(id), strip = Layout.Strips.Contains(id);
            string key = strip ? " " : on ? (++n).ToString() : "·"; // " ": aligned with the tiles
            string detail = strip ? (on ? "shown" : "hidden") : on ? "" : "hidden";
            rows.Add(new Row(key, Layout.Labels[id], detail, Dim: !on));
        }
        return new("TILES", Layout.ScreenNames[a.Screen], Array.Empty<string>(), 0, rows, a.Cursor,
            $"space show/hide · shift+↑↓ or 1-8 move · r defaults · enter keep · esc cancel · z digits: {(_layout.Medium ? "medium" : "large")}");
    }

    static readonly (string, List<(string, string)>) ArrangeHelp = ("TILES", new()
    {
        ("↑ / ↓", "choose a tile or strip"),
        ("shift+↑ / ↓  K / J", "move the tile up or down the order"),
        ("1-8", "put the tile in that place (the rest move down; a hidden one is shown)"),
        ("space  x", "show or hide it"),
        ("r", "back to the defaults"),
        ("enter", "keep the arrangement (saved on this machine)"),
        ("esc  o", "cancel"),
    });
}
