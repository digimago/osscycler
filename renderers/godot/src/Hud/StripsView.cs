using System;
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
    public bool ShowMap { get; set; } = true;

    Minimap? _map; // the course's, made when it changes

    // The ride graph at the bottom left (null: none): the activity being
    // recorded, power by zone (ftp; one colour without it) and heart rate.
    RideHistory? _graph;
    double _ftp;

    public void Graph(RideHistory? h, double ftp)
    {
        if (!ReferenceEquals(h, _graph) || ftp != _ftp)
            QueueRedraw();
        (_graph, _ftp) = (h, ftp);
    }

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
        if (_graph != null)
        {
            // Left of the look-ahead (which takes the middle half), as high
            // as the profile.
            float gw = Mathf.Min(420, (w - Mathf.Min(960, w * 0.5f)) / 2 - 2 * Margin - Pad);
            if (gw > 160)
                RideGraph(new Rect2(Margin + Pad, h - Margin - ProfileH - Pad - 60, gw, ProfileH));
        }
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
        if (ShowMap)
        {
            if (_map?.CourseId != _course.Id)
                _map = new Minimap(_course);
            float side = Mathf.Min(280, w * 0.15f);
            Map(new Rect2(w - Margin - Pad - side, 236, side, side));
        }
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

    // RideGraph draws the whole activity in r: power as bars coloured by
    // zone, heart rate as a line over them, the scale at the top left.
    void RideGraph(Rect2 r)
    {
        Panel(r);
        var h = _graph!;
        var labels = r.Position;
        r = new Rect2(r.Position + new Vector2(0, 26), r.Size - new Vector2(0, 26)); // the scale above the bars
        int n = (int)(r.Size.X / 3);
        var (cols, top, minHr, maxHr) = h.Columns(n);
        float cw = r.Size.X / cols.Count;
        for (int x = 0; x < cols.Count; x++)
        {
            float hgt = (float)(cols[x].PowerW / top) * r.Size.Y;
            var col = _ftp > 0 ? Color8(Palette.Zone(cols[x].PowerW / _ftp)) : Color8(Palette.Power);
            DrawRect(new Rect2(r.Position.X + x * cw, r.End.Y - hgt, cw + 0.5f, hgt), col);
        }
        if (maxHr > minHr)
        {
            var heart = new Color(1f, 0.67f, 0.78f); // lighter than zones 5 and 6, which it crosses
            Vector2? last = null;
            for (int x = 0; x < cols.Count; x++)
            {
                if (cols[x].HeartRate <= 0)
                {
                    last = null;
                    continue;
                }
                float f = (float)((cols[x].HeartRate - minHr) / (maxHr - minHr));
                var at = new Vector2(r.Position.X + (x + 0.5f) * cw, r.End.Y - 6 - f * (r.Size.Y - 18));
                if (last is Vector2 l)
                    DrawLine(l, at, heart, 3);
                last = at;
            }
            DrawString(_bold, labels + new Vector2(110, 18), $"{maxHr:F0} bpm", HorizontalAlignment.Left, -1, 18, heart);
        }
        DrawString(_bold, labels + new Vector2(2, 18), $"{top:F0} W", HorizontalAlignment.Left, -1, 18, Color8(Palette.Power));
        DrawString(_regular, new Vector2(r.Position.X, r.End.Y + 22), Format.Clock(h.Count), HorizontalAlignment.Right, r.Size.X, 18, new Color(1, 1, 1, 0.8f));
    }

    // Map draws the course from above in r, north up: the line ahead
    // light, the part ridden dim, the start chequered, the ghost and the
    // rider (an arrow the way the course runs).
    void Map(Rect2 r)
    {
        Panel(r);
        var m = _map!;
        if (m.Points.Count < 2)
            return;
        var c = _course!;
        double len = c.DistanceM, pos = c.Loop && len > 0 ? _pos % len : _pos;
        Vector2 P(double x, double y) => r.Position + new Vector2((float)x, (float)y) * r.Size.X;
        Color ahead = new(1, 1, 1, 0.9f), ridden = new(1, 1, 1, 0.35f);
        for (int i = 1; i < m.Points.Count; i++)
        {
            var (ax, ay, ad) = m.Points[i - 1];
            var (bx, by, _) = m.Points[i];
            DrawLine(P(ax, ay), P(bx, by), ad < pos ? ridden : ahead, 3, true);
        }
        var (sx, sy, _) = m.Points[0];
        var s0 = P(sx, sy);
        for (int q = 0; q < 4; q++)
            DrawRect(new Rect2(s0 + new Vector2(q % 2 * 5 - 5, q / 2 * 5 - 5), new Vector2(5, 5)), (q == 0 || q == 3) ? Colors.White : Colors.Black);
        if (_ghost >= 0)
        {
            var (gx, gy, _) = m.At(_ghost);
            DrawCircle(P(gx, gy), 6, Color8(Palette.Ghost));
        }
        var (hx, hy, heading) = m.At(_pos);
        var at = P(hx, hy);
        var dir = new Vector2((float)Math.Sin(heading), -(float)Math.Cos(heading));
        var side = new Vector2(-dir.Y, dir.X);
        DrawColoredPolygon(new[] { at + dir * 13, at - dir * 7 + side * 8, at - dir * 7 - side * 8 }, Color8(Palette.Power));
        DrawPolyline(new[] { at + dir * 13, at - dir * 7 + side * 8, at - dir * 7 - side * 8, at + dir * 13 }, Colors.Black, 2, true);
    }

    void Diamond(Vector2 at, float s, Color c) =>
        DrawColoredPolygon(new[] { at + new Vector2(0, -s), at + new Vector2(s, 0), at + new Vector2(0, s), at + new Vector2(-s, 0) }, c);

    static Color Color8(Rgb c) => Godot.Color.Color8(c.R, c.G, c.B);
}
