using Godot;
using Osscycler.V1;

namespace Osscycler.Hud;

// StripsView draws the strips along the bottom of the HUD (in its
// 1080-pixel-high design units): on a ride the look-ahead in the middle
// and the course's profile at the right, in a workout the workout's
// profile in the middle; each on a panel like the tiles'.
public partial class StripsView : Control
{
    const float Margin = 24, Pad = 10;
    const float CellH = 34;
    const float ProfileH = 96;

    readonly Font _regular, _bold;
    Course? _course;
    double _pos, _ghost = -1;
    WorkoutDef? _workout;
    double _elapsed;

    // Which strips the rider shows (Layout): the look-ahead, and the
    // course's or the workout's profile.
    public bool ShowLookAhead { get; set; } = true;
    public bool ShowProfile { get; set; } = true;

    public StripsView(Font regular, Font bold)
    {
        (_regular, _bold) = (regular, bold);
        MouseFilter = MouseFilterEnum.Ignore;
    }

    // Show sets what to draw: the course (null hides the strips), the
    // rider's distance along it and the ghost's (negative: none).
    public void Show(Course? c, double pos, double ghost)
    {
        (_course, _pos, _ghost, _workout) = (c, pos, ghost, null);
        QueueRedraw();
    }

    // ShowWorkout draws the workout's profile instead, marked at elapsed
    // seconds.
    public void ShowWorkout(WorkoutDef w, double elapsed)
    {
        (_course, _workout, _elapsed) = (null, w, elapsed);
        QueueRedraw();
    }

    public override void _Draw()
    {
        float w = Size.X, h = Size.Y;
        if (_workout != null)
        {
            if (!ShowProfile)
                return;
            float ww = Mathf.Min(1100, w * 0.6f);
            Workout(new Rect2((w - ww) / 2, h - Margin - ProfileH - Pad, ww, ProfileH));
            return;
        }
        if (_course == null)
            return;
        float profileW = Mathf.Min(400, w * 0.22f);
        float aheadW = Mathf.Min(960, w * 0.5f);
        if (ShowLookAhead)
            LookAhead(new Rect2((w - aheadW) / 2, h - Margin - CellH - 30 - Pad, aheadW, CellH + 30));
        if (ShowProfile)
            Profile(new Rect2(w - Margin - profileW, h - Margin - ProfileH - Pad, profileW, ProfileH));
    }

    void Panel(Rect2 r) =>
        DrawStyleBox(new StyleBoxFlat
        {
            BgColor = new Color(0.04f, 0.06f, 0.08f, 0.58f),
            CornerRadiusTopLeft = 12, CornerRadiusTopRight = 12, CornerRadiusBottomLeft = 12, CornerRadiusBottomRight = 12,
        }, r.Grow(Pad));

    void LookAhead(Rect2 r)
    {
        Panel(r);
        var c = _course!;
        double loopLen = c.Loop ? c.DistanceM : 0;
        var (cells, ghostAt) = Strips.LookAhead(c, loopLen > 0 ? _pos % loopLen : _pos, _ghost >= 0 && loopLen > 0 ? _ghost % loopLen : _ghost);
        float cw = r.Size.X / cells.Count;
        for (int i = 0; i < cells.Count; i++)
        {
            var cell = cells[i];
            var box = new Rect2(r.Position.X + i * cw, r.Position.Y, cw + 0.5f, CellH);
            if (cell.Kind == Strips.CellKind.Grade)
                DrawRect(box, Color8(cell.Color));
            else
            {
                // Chequered: two rows of squares.
                for (int q = 0; q < 2; q++)
                    DrawRect(new Rect2(box.Position + new Vector2(0, q * CellH / 2), new Vector2(box.Size.X, CellH / 2)),
                        (i + q) % 2 == 0 ? new Color("e0e0e0") : new Color("404040"));
            }
        }
        // Labels after the cells, so a label may run over the next ones.
        for (int i = 0; i < cells.Count; i++)
        {
            if (cells[i].Label == "")
                continue;
            var at = new Vector2(r.Position.X + i * cw + 4, r.Position.Y + CellH / 2 + 7);
            if (cells[i].Kind != Strips.CellKind.Grade)
                DrawString(_bold, at + Vector2.Down * -1, cells[i].Label, HorizontalAlignment.Left, -1, 20, Colors.Black);
            DrawString(_bold, at, cells[i].Label, HorizontalAlignment.Left, -1, 20, cells[i].Kind == Strips.CellKind.Grade ? Colors.Black : new Color("ff3030"));
        }
        foreach (var (f, text) in Strips.Ticks())
            DrawString(_regular, new Vector2(r.Position.X + (float)f * r.Size.X + 2, r.End.Y - 4), text, HorizontalAlignment.Left, -1, 18, new Color(1, 1, 1, 0.8f));
        if (ghostAt is double g)
        {
            float x = r.Position.X + (float)g * r.Size.X, y = r.End.Y - 12;
            Diamond(new Vector2(x, y), 8, Color8(Palette.Ghost));
            DrawString(_bold, new Vector2(x + 11, y + 7), "PB", HorizontalAlignment.Left, -1, 18, Color8(Palette.Ghost));
        }
    }

    void Profile(Rect2 r)
    {
        Panel(r);
        var c = _course!;
        int n = (int)(r.Size.X / 2);
        var cols = Strips.Profile(c, n);
        if (cols.Count == 0)
            return;
        float cw = r.Size.X / cols.Count;
        double len = c.DistanceM;
        double pos = c.Loop && len > 0 ? _pos % len : _pos;
        int here = (int)(pos / len * (cols.Count - 1));
        for (int x = 0; x < cols.Count; x++)
        {
            float hgt = (float)cols[x].Height * r.Size.Y;
            var col = Color8(cols[x].Color);
            if (x < here)
                col = col.Darkened(0.55f); // ridden
            DrawRect(new Rect2(r.Position.X + x * cw, r.End.Y - hgt, cw + 0.5f, hgt), col);
        }
        if (_ghost >= 0)
        {
            double g = c.Loop && len > 0 ? _ghost % len : _ghost;
            float gx = r.Position.X + (float)(g / len) * r.Size.X;
            DrawLine(new Vector2(gx, r.Position.Y), new Vector2(gx, r.End.Y), Color8(Palette.Ghost), 3);
        }
        float hx = r.Position.X + (float)(pos / len) * r.Size.X;
        DrawLine(new Vector2(hx, r.Position.Y - 4), new Vector2(hx, r.End.Y), Colors.White, 3);
        Diamond(new Vector2(hx, r.Position.Y - 4), 6, Colors.White);
    }

    void Workout(Rect2 r)
    {
        Panel(r);
        var w = _workout!;
        var cols = Strips.Workout(w, (int)(r.Size.X / 2));
        if (cols.Count == 0)
            return;
        float cw = r.Size.X / cols.Count;
        int here = w.DurationS > 0 ? (int)(_elapsed / w.DurationS * cols.Count) : -1;
        for (int x = 0; x < cols.Count; x++)
        {
            float hgt = (float)cols[x].Height * r.Size.Y;
            var col = Color8(cols[x].Color);
            if (x < here)
                col = col.Darkened(0.55f); // done
            DrawRect(new Rect2(r.Position.X + x * cw, r.End.Y - hgt, cw + 0.5f, hgt), col);
        }
        float hx = r.Position.X + (float)Mathf.Clamp(_elapsed / w.DurationS, 0, 1) * r.Size.X;
        DrawLine(new Vector2(hx, r.Position.Y - 4), new Vector2(hx, r.End.Y), Colors.White, 3);
        Diamond(new Vector2(hx, r.Position.Y - 4), 6, Colors.White);
    }

    void Diamond(Vector2 at, float s, Color c) =>
        DrawColoredPolygon(new[] { at + new Vector2(0, -s), at + new Vector2(s, 0), at + new Vector2(0, s), at + new Vector2(-s, 0) }, c);

    static Color Color8(Rgb c) => Godot.Color.Color8(c.R, c.G, c.B);
}
