using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text;
using System.Text.RegularExpressions;
using Osscycler.V1;

namespace Osscycler.Head;

// WorkoutForm is the workout editor by keys (tui/form.go; $EDITOR doesn't
// suit a TV, so there is no text mode): a row per detail (name, author,
// description) and per block, a cell per value. Arrows move, + / - step
// the value under the cursor, typing or enter edits it, t / T cycle the
// block's type keeping its values, a add, c copy, d delete, shift+↑/↓ (or
// K / J) move the block, s save, esc twice discards. Block messages are
// kept as they are. Plain C#, apart from Godot.
public sealed class WorkoutForm
{
    public enum Field { Name, Author, Description, Type, Duration, Power, Low, High, Repeat, On, OnPower, Off, OffPower, Cadence }

    public enum Action { None, Save, Close }

    public const int HeaderRows = 3;

    // The types in the order t cycles through, and their names.
    static readonly string[] Types = { "Warmup", "SteadyState", "IntervalsT", "Ramp", "FreeRide", "MaxEffort", "Cooldown" };

    public static readonly IReadOnlyDictionary<string, string> TypeNames = new Dictionary<string, string>
    {
        ["Warmup"] = "Warmup", ["SteadyState"] = "Steady", ["IntervalsT"] = "Intervals", ["Ramp"] = "Ramp",
        ["FreeRide"] = "Free ride", ["MaxEffort"] = "Max effort", ["Cooldown"] = "Cooldown",
    };

    public string Id { get; }             // "" for a new workout
    public WorkoutDef Draft { get; private set; }
    public int Row { get; private set; }
    public int Col { get; private set; }
    public string? Buffer { get; private set; } // the value being typed; null when not typing
    public bool Dirty { get; private set; }
    public string Notice { get; private set; } = "";
    double _discardUntil = double.NegativeInfinity;

    public WorkoutForm(string id, WorkoutDef? w)
    {
        Id = id;
        Draft = w?.Clone() ?? NewWorkout();
        Draft.Timeline.Clear();
        Row = Math.Min(HeaderRows, HeaderRows + Draft.Blocks.Count - 1);
    }

    // NewWorkout is the starting point for a new one, as the TUI's.
    public static WorkoutDef NewWorkout()
    {
        var w = new WorkoutDef { Name = "My workout" };
        w.Blocks.Add(new WorkoutBlock { Type = "Warmup", DurationS = 600, PowerLow = 0.40, PowerHigh = 0.75 });
        w.Blocks.Add(new WorkoutBlock { Type = "IntervalsT", Repeat = 3, OnDurationS = 300, OnPower = 0.95, OffDurationS = 180, OffPower = 0.55 });
        w.Blocks.Add(new WorkoutBlock { Type = "Cooldown", DurationS = 300, PowerLow = 0.65, PowerHigh = 0.40 });
        return w;
    }

    public int Rows => HeaderRows + Draft.Blocks.Count;

    // Block is the block under the cursor, or -1 on a detail row.
    public int Block => Row < HeaderRows ? -1 : Row - HeaderRows;

    public static Field[] Fields(WorkoutDef w, int row)
    {
        if (row < HeaderRows)
            return new[] { Field.Name + row };
        return w.Blocks[row - HeaderRows].Type switch
        {
            "SteadyState" => new[] { Field.Type, Field.Duration, Field.Power, Field.Cadence },
            "Warmup" or "Ramp" or "Cooldown" => new[] { Field.Type, Field.Duration, Field.Low, Field.High, Field.Cadence },
            "IntervalsT" => new[] { Field.Type, Field.Repeat, Field.On, Field.OnPower, Field.Off, Field.OffPower, Field.Cadence },
            _ => new[] { Field.Type, Field.Duration },
        };
    }

    public Field Current
    {
        get
        {
            var fs = Fields(Draft, Row);
            return fs[Math.Min(Col, fs.Length - 1)];
        }
    }

    void Move(int drow, int dcol)
    {
        Row = Math.Clamp(Row + drow, 0, Rows - 1);
        int n = Fields(Draft, Row).Length;
        Col = Math.Clamp(Math.Min(Col, n - 1) + dcol, 0, n - 1);
    }

    void Change(Action<WorkoutDef> f)
    {
        var w = Draft.Clone();
        f(w);
        (Draft, Dirty) = (w, true);
    }

    static string Pct(double v) => (Math.Round(v * 1000) / 10).ToString(CultureInfo.InvariantCulture);

    // Value is a field as typed: the starting text when editing it.
    public static string Value(WorkoutDef w, WorkoutBlock b, Field f) => f switch
    {
        Field.Name => w.Name,
        Field.Author => w.Author,
        Field.Description => w.Description,
        Field.Type => TypeNames.GetValueOrDefault(b.Type, b.Type),
        Field.Duration => FormatDuration(b.DurationS),
        Field.On => FormatDuration(b.OnDurationS),
        Field.Off => FormatDuration(b.OffDurationS),
        Field.Power => Pct(b.Power),
        Field.Low => Pct(b.PowerLow),
        Field.High => Pct(b.PowerHigh),
        Field.OnPower => Pct(b.OnPower),
        Field.OffPower => Pct(b.OffPower),
        Field.Repeat => b.Repeat.ToString(CultureInfo.InvariantCulture),
        Field.Cadence => b.Cadence == 0 ? "" : b.Cadence.ToString(CultureInfo.InvariantCulture),
        _ => "",
    };

    static bool IsText(Field f) => f <= Field.Description;

    // FormatDuration writes seconds as the text format does: 1h2m3s.
    public static string FormatDuration(double s)
    {
        if (s == 0)
            return "0s";
        if (s != Math.Floor(s))
            return s.ToString(CultureInfo.InvariantCulture) + "s";
        long t = (long)s, h = t / 3600, m = t % 3600 / 60, sec = t % 60;
        var b = new StringBuilder();
        if (h > 0)
            b.Append(h).Append('h');
        if (m > 0)
            b.Append(m).Append('m');
        if (sec > 0)
            b.Append(sec).Append('s');
        return b.ToString();
    }

    static readonly Regex Units = new(@"^(?:(\d+(?:\.\d+)?)h)?(?:(\d+(?:\.\d+)?)m)?(?:(\d+(?:\.\d+)?)s)?$");

    // ParseDuration reads 45s, 10m, 1m30s, 1.5h, or m:ss and h:mm:ss.
    public static double ParseDuration(string s)
    {
        if (s.Contains(':'))
        {
            var parts = s.Split(':');
            if (parts.Length > 3)
                throw new FormatException($"duration \"{s}\"");
            double total = 0;
            foreach (var p in parts)
            {
                if (!double.TryParse(p, NumberStyles.Float, CultureInfo.InvariantCulture, out var v) || v < 0)
                    throw new FormatException($"duration \"{s}\": want m:ss or h:mm:ss");
                total = total * 60 + v;
            }
            return total;
        }
        var m = Units.Match(s);
        if (s == "" || !m.Success)
        {
            if (double.TryParse(s, NumberStyles.Float, CultureInfo.InvariantCulture, out _))
                throw new FormatException($"{s}: add a unit, like {s}s or {s}m");
            throw new FormatException($"duration \"{s}\": want e.g. 45s, 10m, 1m30s or 1:30");
        }
        double g(int i) => m.Groups[i].Success ? double.Parse(m.Groups[i].Value, CultureInfo.InvariantCulture) : 0;
        double d = g(1) * 3600 + g(2) * 60 + g(3);
        if (d <= 0)
            throw new FormatException($"duration \"{s}\": want e.g. 45s, 10m, 1m30s or 1:30");
        return d;
    }

    // Set parses s into field f of the workout or block.
    static void Set(WorkoutDef w, WorkoutBlock? b, Field f, string s)
    {
        s = s.Trim();
        switch (f)
        {
            case Field.Name:
                w.Name = s;
                return;
            case Field.Author:
                w.Author = s;
                return;
            case Field.Description:
                w.Description = s;
                return;
        }
        b = b!;
        switch (f)
        {
            case Field.Duration:
                b.DurationS = ParseDuration(s);
                break;
            case Field.On:
                b.OnDurationS = ParseDuration(s);
                break;
            case Field.Off:
                b.OffDurationS = s.Trim('0', 's', ':') == "" ? 0 : ParseDuration(s);
                break;
            case Field.Power or Field.Low or Field.High or Field.OnPower or Field.OffPower:
                if (!double.TryParse(s.TrimEnd('%'), NumberStyles.Float, CultureInfo.InvariantCulture, out var v) || v < 0 || v > 500)
                    throw new FormatException($"power \"{s}\": want a percentage of FTP from 0 to 500");
                v /= 100;
                if (f == Field.Power) b.Power = v;
                else if (f == Field.Low) b.PowerLow = v;
                else if (f == Field.High) b.PowerHigh = v;
                else if (f == Field.OnPower) b.OnPower = v;
                else b.OffPower = v;
                break;
            case Field.Repeat:
                if (!uint.TryParse(s.TrimEnd('x'), out var n) || n < 1 || n > 99)
                    throw new FormatException($"repeat \"{s}\": want 1 to 99");
                b.Repeat = n;
                break;
            case Field.Cadence:
                if (s == "" || uint.TryParse(s.Replace("rpm", ""), out var c0) && c0 == 0)
                    b.Cadence = 0;
                else if (!uint.TryParse(s.Replace("rpm", ""), out var c) || c < 30 || c > 200)
                    throw new FormatException($"cadence \"{s}\": want 30 to 200 rpm, or 0 for none");
                else
                    b.Cadence = c;
                break;
        }
    }

    // DurationStep is 5 s under a minute, 30 s under ten, then a minute.
    static double DurationStep(double d, bool up)
    {
        if (!up)
            d -= 1e-9; // stepping down from 1m uses the finer step below it
        return d < 60 ? 5 : d < 600 ? 30 : 60;
    }

    // Step nudges field f of b up or down.
    static void Step(WorkoutBlock b, Field f, bool up)
    {
        int sign = up ? 1 : -1;
        switch (f)
        {
            case Field.Type:
                int i = Array.IndexOf(Types, b.Type);
                Retype(b, Types[(i + Types.Length + sign) % Types.Length]);
                break;
            case Field.Duration or Field.On or Field.Off:
                double cur = f == Field.Duration ? b.DurationS : f == Field.On ? b.OnDurationS : b.OffDurationS;
                double st = DurationStep(cur, up);
                double next = Math.Max(f == Field.Off ? 0 : 5, Math.Floor((cur + sign * st) / st) * st);
                if (f == Field.Duration) b.DurationS = next;
                else if (f == Field.On) b.OnDurationS = next;
                else b.OffDurationS = next;
                break;
            case Field.Power or Field.Low or Field.High or Field.OnPower or Field.OffPower:
                double p(double v) => Math.Clamp(Math.Round(v * 100 + sign) / 100, 0, 5);
                if (f == Field.Power) b.Power = p(b.Power);
                else if (f == Field.Low) b.PowerLow = p(b.PowerLow);
                else if (f == Field.High) b.PowerHigh = p(b.PowerHigh);
                else if (f == Field.OnPower) b.OnPower = p(b.OnPower);
                else b.OffPower = p(b.OffPower);
                break;
            case Field.Repeat:
                b.Repeat = (uint)Math.Clamp((int)b.Repeat + sign, 1, 99);
                break;
            case Field.Cadence:
                if (b.Cadence == 0 && up)
                    b.Cadence = 85;
                else if (b.Cadence <= 50 && !up)
                    b.Cadence = 0;
                else if (b.Cadence > 0)
                    b.Cadence = (uint)Math.Min(200, (int)b.Cadence + sign * 5);
                break;
        }
    }

    // Retype changes a block's type, keeping every field (so cycling
    // through the types keeps what was set); fields the new type needs and
    // that are unset come from the block's overall level and length.
    static void Retype(WorkoutBlock b, string to)
    {
        double level = 0.75, length = 300;
        switch (b.Type)
        {
            case "SteadyState":
                (level, length) = (b.Power, b.DurationS);
                break;
            case "Warmup" or "Ramp" or "Cooldown":
                (level, length) = ((b.PowerLow + b.PowerHigh) / 2, b.DurationS);
                break;
            case "IntervalsT":
                (level, length) = (b.OnPower, b.Repeat * (b.OnDurationS + b.OffDurationS));
                break;
            case "FreeRide" or "MaxEffort":
                length = b.DurationS;
                break;
        }
        level = Math.Round(level * 100) / 100;
        b.Type = to;
        if (b.DurationS == 0)
            b.DurationS = length;
        switch (to)
        {
            case "SteadyState":
                if (b.Power == 0)
                    b.Power = level;
                break;
            case "Warmup" or "Ramp" or "Cooldown":
                if (b.PowerLow == 0 && b.PowerHigh == 0)
                    (b.PowerLow, b.PowerHigh) = (Math.Round(level * 60) / 100, level);
                // Cooldowns are written as ridden, high to low.
                if ((to == "Cooldown") != (b.PowerLow > b.PowerHigh))
                    (b.PowerLow, b.PowerHigh) = (b.PowerHigh, b.PowerLow);
                break;
            case "IntervalsT":
                if (b.Repeat == 0)
                    (b.Repeat, b.OnDurationS, b.OnPower, b.OffDurationS, b.OffPower) = (3, 300, level, 180, 0.55);
                break;
        }
    }

    // Tidy drops the fields a block's type doesn't use (Retype keeps them
    // while editing), and derives the duration and timeline.
    public static WorkoutDef Tidy(WorkoutDef w)
    {
        var t = new WorkoutDef { Id = w.Id, Name = w.Name, Author = w.Author, Description = w.Description };
        foreach (var b in w.Blocks)
        {
            var n = new WorkoutBlock { Type = b.Type, Cadence = b.Cadence };
            n.Texts.AddRange(b.Texts.Select(x => x.Clone()));
            switch (b.Type)
            {
                case "SteadyState":
                    (n.DurationS, n.Power) = (b.DurationS, b.Power);
                    break;
                case "Warmup" or "Ramp" or "Cooldown":
                    (n.DurationS, n.PowerLow, n.PowerHigh) = (b.DurationS, b.PowerLow, b.PowerHigh);
                    break;
                case "IntervalsT":
                    (n.Repeat, n.OnDurationS, n.OnPower) = (b.Repeat, b.OnDurationS, b.OnPower);
                    if (b.OffDurationS > 0)
                        (n.OffDurationS, n.OffPower) = (b.OffDurationS, b.OffPower);
                    break;
                default:
                    n.DurationS = b.DurationS;
                    break;
            }
            t.Blocks.Add(n);
        }
        double at = 0;
        void add(bool free, double dur, double from, double to, string label, uint cadence)
        {
            t.Timeline.Add(new WorkoutSegment { Free = free, StartS = at, DurationS = dur, FromFtp = from, ToFtp = to, Label = label, Cadence = cadence });
            at += dur;
        }
        foreach (var b in t.Blocks)
            switch (b.Type)
            {
                case "SteadyState":
                    add(false, b.DurationS, b.Power, b.Power, "Steady", b.Cadence);
                    break;
                case "Warmup" or "Ramp" or "Cooldown":
                    var (from, to) = b.Type == "Cooldown" ? (Math.Max(b.PowerLow, b.PowerHigh), Math.Min(b.PowerLow, b.PowerHigh)) : (b.PowerLow, b.PowerHigh);
                    add(false, b.DurationS, from, to, b.Type, b.Cadence);
                    break;
                case "IntervalsT":
                    for (int r = 1; r <= b.Repeat; r++)
                    {
                        add(false, b.OnDurationS, b.OnPower, b.OnPower, $"Interval {r}/{b.Repeat} on", b.Cadence);
                        if (b.OffDurationS > 0)
                            add(false, b.OffDurationS, b.OffPower, b.OffPower, $"Interval {r}/{b.Repeat} off", 0);
                    }
                    break;
                case "FreeRide":
                    add(true, b.DurationS, 0, 0, "Free ride", 0);
                    break;
                case "MaxEffort":
                    add(true, b.DurationS, 0, 0, "Max effort", 0);
                    break;
            }
        t.DurationS = at;
        return t;
    }

    // Problem is what keeps the workout from being saved ("" if nothing),
    // as the core's check (workout.Validate).
    public static string Problem(WorkoutDef w)
    {
        if (w.Blocks.Count == 0)
            return "no blocks";
        for (int i = 0; i < w.Blocks.Count; i++)
        {
            var b = w.Blocks[i];
            bool bad(params double[] ps) => ps.Any(p => p < 0 || p > 5 || double.IsNaN(p));
            string? err = b.Type switch
            {
                "IntervalsT" when b.Repeat < 1 || b.OnDurationS <= 0 || b.OffDurationS < 0 => "needs a repeat of 1 or more and an on time",
                "IntervalsT" when bad(b.OnPower, b.OffPower) => "power out of range",
                "SteadyState" or "Warmup" or "Ramp" or "Cooldown" or "FreeRide" or "MaxEffort" when b.DurationS <= 0 => "needs a duration",
                "SteadyState" when bad(b.Power) => "power out of range",
                "Warmup" or "Ramp" or "Cooldown" when bad(b.PowerLow, b.PowerHigh) => "power out of range",
                _ => null,
            };
            if (err != null)
                return $"block {i + 1} ({TypeNames.GetValueOrDefault(b.Type, b.Type)}): {err}";
        }
        return "";
    }

    // Key handles a key at time now (seconds); what the controller should
    // do next. Notice says what went wrong, or asks for the second esc.
    public Action Key(string key, double now)
    {
        Notice = "";
        if (Buffer != null)
        {
            TypingKey(key);
            return Action.None;
        }
        int bi = Block;
        switch (key)
        {
            case "up":
                Move(-1, 0);
                break;
            case "down":
                Move(1, 0);
                break;
            case "left" or "shift+tab":
                Move(0, -1);
                break;
            case "right" or "tab":
                Move(0, 1);
                break;
            case "enter":
                if (Current != Field.Type)
                    Buffer = Value(Draft, bi >= 0 ? Draft.Blocks[bi] : new WorkoutBlock(), Current);
                break;
            case "+" or "=" or "-" or "_" or "t" or "T":
                if (bi < 0)
                    break;
                var f = key is "t" or "T" ? Field.Type : Current;
                bool up = key is "+" or "=" or "t";
                Change(w => Step(w.Blocks[bi], f, up));
                break;
            case "a" or "c":
                if (key == "c" && bi < 0)
                    break;
                int at = bi + 1;
                var nb = key == "c" ? Draft.Blocks[bi].Clone() : new WorkoutBlock { Type = "SteadyState", DurationS = 300, Power = 0.65 };
                Change(w => w.Blocks.Insert(at, nb));
                Row = HeaderRows + at;
                Move(0, 0);
                break;
            case "d" or "delete":
                if (bi < 0)
                    break;
                Change(w => w.Blocks.RemoveAt(bi));
                Move(0, 0);
                break;
            case "K" or "shift+up" or "J" or "shift+down":
                int to = key is "J" or "shift+down" ? bi + 1 : bi - 1;
                if (bi < 0 || to < 0 || to >= Draft.Blocks.Count)
                    break;
                Change(w => (w.Blocks[bi], w.Blocks[to]) = (w.Blocks[to], w.Blocks[bi]));
                Row = HeaderRows + to;
                break;
            case "s":
                var p = Problem(Tidy(Draft));
                if (p != "")
                {
                    Notice = "can't save: " + p;
                    break;
                }
                return Action.Save;
            case "esc":
                if (Dirty && now >= _discardUntil)
                {
                    _discardUntil = now + Controller.AbortConfirmS;
                    Notice = "press esc again to discard your changes";
                    break;
                }
                return Action.Close;
            default:
                // Typing a number on a number field starts replacing it.
                if (key.Length == 1 && char.IsDigit(key[0]) && !IsText(Current) && Current != Field.Type)
                    Buffer = key;
                break;
        }
        return Action.None;
    }

    void TypingKey(string key)
    {
        var buf = Buffer!;
        switch (key)
        {
            case "esc":
                Buffer = null;
                return;
            case "backspace":
                if (buf.Length > 0)
                    buf = buf[..^1];
                break;
            case "enter" or "tab":
                var f = Current;
                int bi = Block;
                try
                {
                    var w = Draft.Clone();
                    Set(w, bi < 0 ? null : w.Blocks[bi], f, buf);
                    (Draft, Dirty) = (w, true);
                }
                catch (FormatException e)
                {
                    Notice = e.Message;
                    return;
                }
                Buffer = null;
                if (key == "tab")
                    Move(0, 1);
                return;
            default:
                bool text = IsText(Current);
                if (key.Length == 1 && !char.IsControl(key[0]) && (text || char.IsDigit(key[0]) || ".:hms%x".Contains(key[0])) && buf.Length < (text ? 80 : 12))
                    buf += key;
                break;
        }
        Buffer = buf;
    }

    // Help is the editor's key map.
    public static readonly (string, List<(string, string)>) Help = ("WORKOUT EDITOR", new()
    {
        ("↑ ↓ ← →  tab", "move between rows and values"),
        ("+ / -", "step the value under the cursor"),
        ("0-9  enter", "type a value (durations 45s 5m 1m30s 1:30; powers in % of FTP)"),
        ("t / T", "the block's type, keeping its values"),
        ("a  c  d", "add a block, copy this one, delete it"),
        ("shift+↑ ↓  K J", "move the block up or down"),
        ("s", "save to the core's workouts"),
        ("esc esc", "discard your changes"),
    });
}
