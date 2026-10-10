using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Osscycler.V1;

namespace Osscycler.Head;

// The workout editor (tui/form.go): n in WORKOUTS starts a new workout, e
// edits the selected one; s saves it to the core's library and goes back
// to the list.
public sealed partial class Controller
{
    WorkoutForm? _form;
    string? _formSaved; // a save that came back: its id ("" when it failed)

    // FormCell is one value of a row: its text, whether the cursor is on
    // it (and typing), or a separator between values; width in characters.
    public sealed record FormCell(string Text, bool Here = false, bool Typing = false, bool Sep = false, int Width = 0);

    // FormRow is one line: the cursor, its lead (a detail's name, a block's
    // number) and its cells.
    public sealed record FormRow(bool Cursor, string Lead, IReadOnlyList<FormCell> Cells);

    // FormView is the editor to show: the derived workout (null when it
    // can't be saved) for its profile, a status line (Bad: what keeps it
    // from saving), a hint.
    public sealed record FormView(string Title, IReadOnlyList<FormRow> Details, IReadOnlyList<FormRow> Blocks, int FirstBlock,
        string Status, bool Bad, WorkoutDef? Derived, string Hint);

    void OpenForm(string id, WorkoutDef? w)
    {
        if (w?.Error is { Length: > 0 } err)
        {
            Say($"{id} doesn't load: {err}");
            return;
        }
        _form = new WorkoutForm(id, w);
        _screen = Screen.None;
    }

    void FormKey(string key, double now)
    {
        var f = _form!;
        var act = f.Key(key, now);
        if (f.Notice.StartsWith("press esc again"))
        {
            (_abortUntil, _asking) = (now + AbortConfirmS, f.Notice);
        }
        else if (f.Notice != "")
            Say(f.Notice);
        switch (act)
        {
            case WorkoutForm.Action.Close:
                _form = null;
                _abortUntil = double.NegativeInfinity;
                Say("edit discarded");
                OpenPicker(Tab.Workouts);
                return;
            case WorkoutForm.Action.Save:
                var def = WorkoutForm.Tidy(f.Draft);
                def.Timeline.Clear();
                def.DurationS = 0;
                string id = f.Id;
                Say("saving ...");
                Task<string> t;
                try
                {
                    t = _cmds.SaveWorkout(id, def);
                }
                catch (Exception e)
                {
                    Failed("saving", e);
                    return;
                }
                t.ContinueWith(done =>
                {
                    lock (_mu)
                        _formSaved = done.Exception == null ? done.Result : "";
                    if (done.Exception != null)
                        Failed("saving", done.Exception.GetBaseException());
                });
                return;
        }
    }

    // FormDone applies a save that came back (from Tick): saved, the form
    // closes on the workouts list.
    void FormDone()
    {
        string? saved;
        lock (_mu)
            (saved, _formSaved) = (_formSaved, null);
        if (saved is not { Length: > 0 } || _form == null)
            return;
        _form = null;
        Say("saved " + saved);
        OpenPicker(Tab.Workouts);
    }

    public FormView? Form(State? st)
    {
        if (_form is not { } f)
            return null;
        var w = f.Draft;
        FormCell Cell(int row, WorkoutForm.Field field, string text, int width)
        {
            bool here = row == f.Row && field == f.Current;
            if (here && f.Buffer != null)
                return new(f.Buffer, true, true, Width: width);
            return new(text, here, Width: width);
        }
        var details = new List<FormRow>();
        string[] names = { "name", "author", "description" };
        for (int r = 0; r < WorkoutForm.HeaderRows; r++)
        {
            var field = WorkoutForm.Field.Name + r;
            var v = WorkoutForm.Value(w, new WorkoutBlock(), field);
            details.Add(new(r == f.Row, names[r], new[] { Cell(r, field, v == "" ? "–" : v, 0) }));
        }
        var blocks = new List<FormRow>();
        for (int i = 0; i < w.Blocks.Count; i++)
        {
            int row = WorkoutForm.HeaderRows + i;
            var b = w.Blocks[i];
            var cells = new List<FormCell>();
            FormCell Dur(WorkoutForm.Field x) => Cell(row, x, WorkoutForm.Value(w, b, x), 6);
            FormCell Pct(WorkoutForm.Field x) => Cell(row, x, WorkoutForm.Value(w, b, x) + "%", 5);
            FormCell Sep(string s) => new(s, Sep: true);
            cells.Add(Cell(row, WorkoutForm.Field.Type, WorkoutForm.TypeNames.GetValueOrDefault(b.Type, b.Type), 10));
            switch (b.Type)
            {
                case "SteadyState":
                    cells.AddRange(new[] { Dur(WorkoutForm.Field.Duration), Sep("@"), Pct(WorkoutForm.Field.Power) });
                    break;
                case "Warmup" or "Ramp" or "Cooldown":
                    cells.AddRange(new[] { Dur(WorkoutForm.Field.Duration), Pct(WorkoutForm.Field.Low), Sep("→"), Pct(WorkoutForm.Field.High) });
                    break;
                case "IntervalsT":
                    cells.AddRange(new[]
                    {
                        Cell(row, WorkoutForm.Field.Repeat, WorkoutForm.Value(w, b, WorkoutForm.Field.Repeat) + "×", 3),
                        Dur(WorkoutForm.Field.On), Sep("@"), Pct(WorkoutForm.Field.OnPower), Sep("/"),
                        Dur(WorkoutForm.Field.Off), Sep("@"), Pct(WorkoutForm.Field.OffPower),
                    });
                    break;
                default:
                    cells.Add(Dur(WorkoutForm.Field.Duration));
                    break;
            }
            if (WorkoutForm.Fields(w, row).Contains(WorkoutForm.Field.Cadence))
                cells.Add(Cell(row, WorkoutForm.Field.Cadence, b.Cadence > 0 ? $"{b.Cadence} rpm" : "– rpm", 7));
            if (b.Texts.Count > 0)
                cells.Add(Sep($"{b.Texts.Count} message{(b.Texts.Count == 1 ? "" : "s")}"));
            blocks.Add(new(row == f.Row, (i + 1).ToString(), cells));
        }
        const int visible = 10;
        int first = blocks.Count <= visible ? 0 : Math.Clamp(f.Block - visible / 2, 0, blocks.Count - visible);
        var tidy = WorkoutForm.Tidy(w);
        var problem = WorkoutForm.Problem(tidy);
        string status;
        if (problem != "")
            status = problem;
        else
        {
            double sum = 0, t = 0;
            foreach (var s in tidy.Timeline.Where(s => !s.Free))
                (sum, t) = (sum + (s.FromFtp + s.ToFtp) / 2 * s.DurationS, t + s.DurationS);
            double avg = t > 0 ? sum / t : 0, ftp = st?.Workout?.FtpW is > 0 and var fw ? fw : st?.Profile?.FtpW ?? 0;
            string a = ftp > 0 ? $"~{Hud.Format.Fixed(avg * ftp, 0)} W average at {Hud.Format.Fixed(ftp, 0)} W FTP" : $"~{Hud.Format.Fixed(avg * 100, 0)} % FTP average";
            status = $"{Hud.Format.Clock(tidy.DurationS)} · {w.Blocks.Count} block{(w.Blocks.Count == 1 ? "" : "s")} · {a}";
        }
        string hint = f.Buffer != null
            ? "enter set · tab set and next · esc cancel · durations 45s 5m 1m30s 1:30 · powers in % of FTP"
            : "↑↓←→ move · +/- adjust · type or enter to edit · t type · a add · c copy · d delete · shift+↑↓ move · s save · esc discard";
        return new(f.Id == "" ? "NEW WORKOUT" : "EDIT " + f.Id, details, blocks.Skip(first).Take(visible).ToList(), first,
            status, problem != "", problem == "" ? tidy : null, hint);
    }
}
