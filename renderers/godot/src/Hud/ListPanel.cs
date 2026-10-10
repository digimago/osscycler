using System;
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

    void Panel(Rect2 r) => DrawStyleBox(new StyleBoxFlat
    {
        BgColor = new Color(0.04f, 0.06f, 0.08f, 0.9f),
        CornerRadiusTopLeft = 14, CornerRadiusTopRight = 14, CornerRadiusBottomLeft = 14, CornerRadiusBottomRight = 14,
    }, r);

    public override void _Draw()
    {
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
