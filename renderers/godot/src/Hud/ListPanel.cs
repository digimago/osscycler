using System;
using System.Collections.Generic;
using Godot;
using Osscycler.Head;

namespace Osscycler.Hud;

// ListPanel draws a menu or picker (Controller.ListView) and a value being
// typed (Controller.PromptView) in the middle of the HUD, in its design
// units: a title, tabs, rows (the selected one lit), a hint.
public partial class ListPanel : Control
{
    const float W = 1100, RowH = 44, Pad = 28;
    const int MaxRows = 12;

    readonly Font _regular, _bold;
    Controller.ListView? _list;
    Controller.PromptView? _prompt;

    public ListPanel(Font regular, Font bold)
    {
        (_regular, _bold) = (regular, bold);
        MouseFilter = MouseFilterEnum.Ignore;
    }

    Controller.DialogView? _dialog;

    Controller.FormView? _form;

    public void Show(Controller.ListView? list, Controller.PromptView? prompt, Controller.DialogView? dialog = null, Controller.FormView? form = null)
    {
        (_list, _prompt, _dialog, _form) = (list, prompt, dialog, form);
        QueueRedraw();
    }

    static readonly Color Gold = new("ffd700"), Dim = new(1, 1, 1, 0.6f), Lit = new(0.2f, 0.45f, 0.85f, 0.9f);

    static readonly string[] ZoneNames = { "recovery", "endurance", "tempo", "threshold", "VO2 max", "anaerobic" };

    // DrawSummary lays a summary out as the TUI's (tui/summary.go): the
    // ride, its averages, the best power and time in each zone.
    void DrawSummary(Osscycler.V1.ActivitySummary s)
    {
        static Color C(Rgb c) => Color.Color8(c.R, c.G, c.B);
        float pw = Mathf.Min(W + 100, Size.X - 80);
        // The height of what is drawn below, line by line.
        int lines = (s.AvgPowerW > 0 ? 1 : 0) + (s.AvgHeartRateBpm > 0 ? 1 : 0) + (s.AvgCadenceRpm > 0 ? 1 : 0);
        float ph = 144 + lines * 36 + (s.PowerCurve.Count > 0 ? 104 : 0)
            + (s.PowerZoneS.Count > 0 ? 52 + s.PowerZoneS.Count * 34 : 0)
            + (s.HeartRateZoneS.Count > 0 ? 52 + s.HeartRateZoneS.Count * 34 : s.AvgHeartRateBpm > 0 ? 50 : 0) + 56;
        // Below the tiles where it fits; else as high as it must.
        float top = Size.Y - ph - 60 >= 170 ? 170 : Mathf.Max(20, (Size.Y - ph) / 2);
        var r = new Rect2((Size.X - pw) / 2, top, pw, ph);
        Panel(r);
        float x = r.Position.X + Pad, y = r.Position.Y + 50;
        string when = "";
        if (DateTime.TryParseExact(s.Name.Replace(".fit", ""), "yyyy-MM-dd-HHmmss", System.Globalization.CultureInfo.InvariantCulture,
                System.Globalization.DateTimeStyles.AssumeLocal, out var t))
            when = "   " + t.ToString("ddd d MMM HH:mm", System.Globalization.CultureInfo.InvariantCulture);
        if (_summaryHeading != "") // a finished course ride: how it went
            DrawString(_bold, new Vector2(x, y), _summaryHeading, HorizontalAlignment.Left, pw - 2 * Pad, 30, Colors.White);
        else
            DrawString(_bold, new Vector2(x, y), "RIDE SUMMARY" + when, HorizontalAlignment.Left, -1, 30, Dim);
        y += 44;
        DrawString(_bold, new Vector2(x, y), $"{Format.Clock(s.TimerS)} riding · {Format.Fixed(s.DistanceM / 1000, 2)} km", HorizontalAlignment.Left, -1, 30, Colors.White);
        y += 50;
        void Line(string label, string text, Color c)
        {
            DrawString(_bold, new Vector2(x, y), label, HorizontalAlignment.Left, -1, 24, Dim);
            DrawString(_regular, new Vector2(x + 170, y), text, HorizontalAlignment.Left, -1, 26, c);
            y += 36;
        }
        if (s.AvgPowerW > 0)
            Line("POWER", $"avg {s.AvgPowerW:F0} W" + (s.NormalizedPowerW > 0 ? $"   NP {s.NormalizedPowerW:F0} W" : "") + $"   max {s.MaxPowerW:F0} W   {s.WorkKj:F0} kJ", C(Palette.Power));
        if (s.AvgHeartRateBpm > 0)
            Line("HEART RATE", $"avg {s.AvgHeartRateBpm:F0}   max {s.MaxHeartRateBpm:F0} bpm", C(Palette.Heart));
        if (s.AvgCadenceRpm > 0)
            Line("CADENCE", $"avg {s.AvgCadenceRpm:F0} rpm", C(Palette.Cadence));
        if (s.PowerCurve.Count > 0)
        {
            y += 14;
            DrawString(_bold, new Vector2(x, y), "BEST POWER", HorizontalAlignment.Left, -1, 24, Dim);
            y += 34;
            float cw = (pw - 2 * Pad) / 10;
            for (int i = 0; i < s.PowerCurve.Count; i++)
            {
                var b = s.PowerCurve[i];
                string d = b.DurationS >= 60 ? $"{b.DurationS / 60}m" : $"{b.DurationS}s";
                var c = s.FtpW > 0 ? C(Palette.Zone(b.PowerW / s.FtpW)) : Colors.White;
                DrawString(_regular, new Vector2(x + i * cw, y), d, HorizontalAlignment.Center, cw, 22, Dim);
                DrawString(_bold, new Vector2(x + i * cw, y + 32), $"{b.PowerW:F0}", HorizontalAlignment.Center, cw, 28, c);
            }
            y += 56;
        }
        void Bars(IList<double> secs, Func<int, Color> color, Func<int, string> name)
        {
            double total = 0, most = 0;
            foreach (var v in secs)
                (total, most) = (total + v, Math.Max(most, v));
            float barW = pw - 2 * Pad - 420;
            for (int i = 0; i < secs.Count; i++)
            {
                DrawString(_regular, new Vector2(x, y), $"Z{i + 1} {name(i)}", HorizontalAlignment.Left, -1, 22, Colors.White);
                float bw = most > 0 ? (float)(secs[i] / most) * barW : 0;
                DrawRect(new Rect2(x + 200, y - 20, bw, 24), color(i));
                DrawString(_regular, new Vector2(x + 200 + barW + 10, y), $"{Format.Clock(secs[i]),8}  {(total > 0 ? secs[i] / total * 100 : 0),3:F0}%",
                    HorizontalAlignment.Left, -1, 22, Colors.White);
                y += 34;
            }
        }
        if (s.PowerZoneS.Count > 0)
        {
            y += 16;
            DrawString(_bold, new Vector2(x, y), $"POWER ZONES   FTP {s.FtpW:F0} W", HorizontalAlignment.Left, -1, 24, Dim);
            y += 36;
            double[] mid = { 0.3, 0.68, 0.83, 0.97, 1.12, 1.5 }; // a power in each zone, for its colour
            Bars(s.PowerZoneS, i => C(Palette.Zone(mid[Math.Min(i, mid.Length - 1)])), i => ZoneNames[Math.Min(i, ZoneNames.Length - 1)]);
        }
        if (s.HeartRateZoneS.Count > 0)
        {
            y += 16;
            var from = s.ThresholdHrBpm > 0 ? $"threshold {s.ThresholdHrBpm:F0} bpm" : $"max {s.MaxHrBpm:F0} bpm";
            DrawString(_bold, new Vector2(x, y), $"HEART RATE ZONES   {from}", HorizontalAlignment.Left, -1, 24, Dim);
            y += 36;
            var up = s.HeartRateZoneUpper;
            Bars(s.HeartRateZoneS, _ => C(Palette.Heart), i => i == 0 ? $"<{up[0] * 100:F0}%" : i >= up.Count ? $">{up[^1] * 100:F0}%" : $"{up[i - 1] * 100:F0}-{up[i] * 100:F0}%");
        }
        else if (s.AvgHeartRateBpm > 0)
        {
            y += 16;
            DrawString(_regular, new Vector2(x, y), "Heart rate zones: add your maximum or threshold heart rate to your profile (m, Profile).", HorizontalAlignment.Left, -1, 22, Dim);
            y += 34;
        }
        DrawString(_regular, new Vector2(r.Position.X, r.End.Y - 22), "x close", HorizontalAlignment.Center, pw, 22, Dim);
    }

    void Panel(Rect2 r) => DrawStyleBox(new StyleBoxFlat
    {
        BgColor = new Color(0.04f, 0.06f, 0.08f, 0.9f),
        CornerRadiusTopLeft = 14, CornerRadiusTopRight = 14, CornerRadiusBottomLeft = 14, CornerRadiusBottomRight = 14,
    }, r);

    Osscycler.V1.ActivitySummary? _summary;

    string _summaryHeading = "";

    // ShowSummary draws a ride's summary (null: none) over everything else
    // this panel shows.
    public void ShowSummary(Osscycler.V1.ActivitySummary? s, string heading = "")
    {
        if (!ReferenceEquals(s, _summary) || heading != _summaryHeading)
            QueueRedraw();
        (_summary, _summaryHeading) = (s, heading);
    }

    public override void _Draw()
    {
        if (_summary is { } sum)
        {
            DrawSummary(sum);
            return;
        }
        if (_form is { } f)
        {
            DrawForm(f);
            return;
        }
        if (_dialog is { } d)
        {
            float dw = Mathf.Min(W, Size.X - 80), dh = 210 + d.Lines.Count * 38;
            var r = new Rect2((Size.X - dw) / 2, 300, dw, dh);
            Panel(r);
            float dy = r.Position.Y + 58;
            DrawString(_bold, new Vector2(r.Position.X, dy), d.Title, HorizontalAlignment.Center, dw, 34, Dim);
            dy += 70;
            DrawString(_bold, new Vector2(r.Position.X, dy), d.Big, HorizontalAlignment.Center, dw, 52,
                Color.Color8(d.BigColor.R, d.BigColor.G, d.BigColor.B));
            dy += 20;
            foreach (var line in d.Lines)
                DrawString(_regular, new Vector2(r.Position.X + Pad, dy += 38), line, HorizontalAlignment.Center, dw - 2 * Pad, 26, Colors.White);
            DrawString(_regular, new Vector2(r.Position.X, r.End.Y - 22), d.Hint, HorizontalAlignment.Center, dw, 22, Dim);
            return;
        }
        if (_prompt is { } p)
        {
            var r = new Rect2((Size.X - 760) / 2, 380, 760, 120);
            Panel(r);
            DrawString(_regular, r.Position + new Vector2(Pad, 46), p.Label, HorizontalAlignment.Left, -1, 28, Dim);
            DrawString(_bold, r.Position + new Vector2(Pad, 96), p.Value + "_", HorizontalAlignment.Left, -1, 44, Colors.White);
            return;
        }
        if (_list is not { } l)
            return;
        int first = Math.Clamp(l.Selected - MaxRows / 2, 0, Math.Max(0, l.Rows.Count - MaxRows));
        int shown = Math.Min(MaxRows, l.Rows.Count);
        float h = 120 + (l.Tabs.Count > 1 ? 50 : 0) + Math.Max(1, shown) * RowH + 60 + (l.Credit != "" ? 34 : 0);
        float w = Mathf.Min(W, Size.X - 80);
        var box = new Rect2((Size.X - w) / 2, Mathf.Max(170, (Size.Y - h) / 2 - 40), w, h);
        Panel(box);
        float x = box.Position.X + Pad, y = box.Position.Y + 58;
        DrawString(_bold, new Vector2(x, y), l.Title, HorizontalAlignment.Left, -1, 40, Colors.White);
        if (l.Subtitle != "")
            DrawString(_regular, new Vector2(x + 300, y), l.Subtitle, HorizontalAlignment.Left, -1, 24, Dim);
        y += 40;
        if (l.Tabs.Count > 1)
        {
            float tx = x;
            for (int i = 0; i < l.Tabs.Count; i++)
            {
                var size = _bold.GetStringSize(l.Tabs[i], HorizontalAlignment.Left, -1, 26);
                if (i == l.Tab)
                    DrawRect(new Rect2(tx - 10, y - 2, size.X + 20, 38), Lit);
                DrawString(_bold, new Vector2(tx, y + 27), l.Tabs[i], HorizontalAlignment.Left, -1, 26, i == l.Tab ? Colors.White : Dim);
                tx += size.X + 44;
            }
            y += 50;
        }
        y += 8;
        float labelW = 0;
        for (int i = first; i < first + shown; i++)
            labelW = Mathf.Max(labelW, _bold.GetStringSize(l.Rows[i].Label, HorizontalAlignment.Left, -1, 28).X);
        labelW = Mathf.Min(labelW, w * 0.5f);
        for (int i = first; i < first + shown; i++)
        {
            var row = l.Rows[i];
            if (i == l.Selected)
                DrawRect(new Rect2(box.Position.X + 10, y - 4, w - 20, RowH - 2), Lit);
            float rx = x;
            if (row.Key != "")
            {
                DrawString(_bold, new Vector2(rx, y + 28), row.Key, HorizontalAlignment.Left, -1, 26, Gold);
                rx += 40;
            }
            if (row.Star)
                DrawStar(new Vector2(rx + 10, y + 18), 11, Gold);
            DrawString(_bold, new Vector2(rx + 28, y + 28), row.Label, HorizontalAlignment.Left, labelW + 4, 28, row.Dim && i != l.Selected ? Dim : Colors.White);
            DrawString(_regular, new Vector2(rx + 28 + labelW + 30, y + 28), row.Detail, HorizontalAlignment.Left,
                box.End.X - Pad - (rx + 28 + labelW + 30), 24, i == l.Selected ? Colors.White : Dim);
            y += RowH;
        }
        if (shown == 0)
            y += RowH;
        if (l.Credit != "")
            DrawString(_regular, new Vector2(x, box.End.Y - 58), l.Credit, HorizontalAlignment.Left, w - 2 * Pad, 22, Dim);
        DrawString(_regular, new Vector2(x, box.End.Y - 24), l.Hint, HorizontalAlignment.Left, w - 2 * Pad, 22, Dim);
    }

    static readonly Color Typing = new("ffd700"), Bad = new("ff5f5f");

    // DrawForm draws the workout editor: the details, the blocks with a
    // cell per value (the one under the cursor lit, gold while typing), the
    // status, the workout's profile in zone colours, the keys.
    void DrawForm(Controller.FormView f)
    {
        const int size = 26;
        float w = Mathf.Min(1300, Size.X - 80), rowH = 40;
        int rows = f.Details.Count + Math.Max(1, f.Blocks.Count);
        float h = 90 + rows * rowH + 30 + 50 + (f.Derived != null ? 110 : 0) + 50;
        var box = new Rect2((Size.X - w) / 2, Mathf.Max(150, (Size.Y - h) / 2 - 20), w, h);
        Panel(box);
        float x = box.Position.X + Pad, y = box.Position.Y + 56;
        DrawString(_bold, new Vector2(x, y), f.Title, HorizontalAlignment.Left, -1, 38, Colors.White);
        y += 26;
        float digit = _regular.GetStringSize("0", HorizontalAlignment.Left, -1, size).X;
        void Row(Controller.FormRow r, float leadW)
        {
            if (r.Cursor)
                DrawString(_bold, new Vector2(x - 2, y + 28), "▸", HorizontalAlignment.Left, -1, size, Gold);
            DrawString(_regular, new Vector2(x + 26, y + 28), r.Lead, HorizontalAlignment.Left, -1, size - 2, Dim);
            float cx = x + 26 + leadW;
            foreach (var c in r.Cells)
            {
                var font = c.Sep ? _regular : _bold;
                string text = c.Typing ? c.Text + "▏" : c.Text;
                float tw = font.GetStringSize(text, HorizontalAlignment.Left, -1, size).X;
                float cw = Mathf.Max(tw, c.Width * digit) + 18;
                if (c.Here)
                    DrawRect(new Rect2(cx - 6, y + 2, Mathf.Min(cw - 6, box.End.X - Pad - cx + 6), rowH - 4), c.Typing ? Typing : Lit);
                var col = c.Typing ? Colors.Black : c.Sep ? Dim : Colors.White;
                DrawString(font, new Vector2(cx, y + 28), text, HorizontalAlignment.Left, box.End.X - Pad - cx, size, col);
                cx += cw;
            }
            y += rowH;
        }
        foreach (var r in f.Details)
            Row(r, 170);
        y += 10;
        if (f.FirstBlock > 0)
            DrawString(_regular, new Vector2(x + 26, y - 2), $"↑ {f.FirstBlock} more", HorizontalAlignment.Left, -1, 20, Dim);
        foreach (var r in f.Blocks)
            Row(r, 50);
        if (f.Blocks.Count == 0)
        {
            DrawString(_regular, new Vector2(x + 26, y + 28), "no blocks yet: press a to add one", HorizontalAlignment.Left, -1, size, Dim);
            y += rowH;
        }
        y += 20;
        DrawString(_regular, new Vector2(x, y + 28), (f.Bad ? "✕ " : "") + f.Status, HorizontalAlignment.Left, w - 2 * Pad, size, f.Bad ? Bad : Colors.White);
        y += 50;
        if (f.Derived is { } d && d.DurationS > 0)
        {
            var area = new Rect2(x, y, w - 2 * Pad, 96);
            var cols = Strips.Workout(d, (int)(area.Size.X / 3));
            float colW = area.Size.X / Math.Max(1, cols.Count);
            for (int i = 0; i < cols.Count; i++)
            {
                float hgt = (float)cols[i].Height * area.Size.Y;
                DrawRect(new Rect2(area.Position.X + i * colW, area.End.Y - hgt, colW + 0.5f, hgt), Color.Color8(cols[i].Color.R, cols[i].Color.G, cols[i].Color.B));
            }
            y += 110;
        }
        DrawString(_regular, new Vector2(x, box.End.Y - 24), f.Hint, HorizontalAlignment.Left, w - 2 * Pad, 22, Dim);
    }

    // DrawStar draws ★ (the font has none).
    void DrawStar(Vector2 c, float r, Color col)
    {
        var pts = new Vector2[10];
        for (int i = 0; i < 10; i++)
        {
            float a = -Mathf.Pi / 2 + i * Mathf.Pi / 5, rr = i % 2 == 0 ? r : r * 0.45f;
            pts[i] = c + new Vector2(Mathf.Cos(a), Mathf.Sin(a)) * rr;
        }
        DrawColoredPolygon(pts, col);
    }
}
