using System.Collections.Generic;
using Godot;
using Osscycler.V1;

namespace Osscycler.Hud;

// HudView draws the HUD: 2D panels over the 3D view (owner, 2026-10-09:
// overlays, not things in the world), laid out for a 1080-pixel-high
// screen and scaled to the real one, so it reads the same from a TV's
// distance at 1080p and 4K. The tiles go across the top, leaving the road
// in view; the course strips along the bottom (StripsView), a small
// status line bottom left.
public sealed class HudView
{
    const float DesignH = 1080;
    const float TileMinW = 200; // tiles don't change width as their numbers do
    const int LargeDigits = 72, MediumDigits = 52;

    readonly CanvasLayer _layer = new() { Layer = 10 };
    readonly Control _root = new();
    readonly HBoxContainer _row = new();
    readonly Label _status = new();
    readonly StripsView _strips;
    readonly ListPanel _list;
    readonly Font _regular, _bold;
    readonly List<(PanelContainer Panel, Label Label, Label Value, Label Unit)> _tiles = new();
    readonly Cadence5s _cadence = new();
    readonly Moments _moments = new();
    readonly PanelContainer _banner, _warn, _message, _place, _laps;
    string _note = "";
    readonly Label _bannerTitle, _bannerSub, _bannerLine, _warnText, _messageText, _placeText, _credit;
    readonly VBoxContainer _lapRows = new();
    readonly PanelContainer _help;
    readonly Label _helpTitle;
    readonly GridContainer _helpRows = new() { Columns = 2 };
    string _asking = "", _notice = "";
    (string Title, List<(string Keys, string What)> Entries)? _helpShown;
    ulong _seq = ulong.MaxValue;

    public Node Node => _layer;

    public HudView(Viewport viewport)
    {
        _regular = Fonts.Hud(500);
        _bold = Fonts.Hud(700);
        _layer.AddChild(_root);
        _root.MouseFilter = Control.MouseFilterEnum.Ignore;

        _strips = new StripsView(_regular, _bold);
        _root.AddChild(_strips);

        _row.AddThemeConstantOverride("separation", 14);
        _row.Alignment = BoxContainer.AlignmentMode.Center;
        _root.AddChild(_row);

        _status.AddThemeFontOverride("font", _regular);
        _status.AddThemeFontSizeOverride("font_size", 20);
        _status.AddThemeColorOverride("font_color", new Color(1, 1, 1, 0.85f));
        _status.AddThemeConstantOverride("outline_size", 6);
        _status.AddThemeColorOverride("font_outline_color", new Color(0, 0, 0, 0.6f));
        _root.AddChild(_status);

        // Moments: the banner in the middle, warnings and workout messages
        // under the tiles, the place top left, the laps at the left, the
        // map credit above the status line.
        (_banner, var bannerBox) = Panel();
        _bannerTitle = Text(bannerBox, _bold, 56, Colors.White);
        _bannerSub = Text(bannerBox, _bold, 30, Colors.White);
        _bannerLine = Text(bannerBox, _regular, 24, new Color(1, 1, 1, 0.8f));
        (_warn, var warnBox) = Panel();
        _warnText = Text(warnBox, _bold, 26, new Color("ffaf00"));
        (_message, var messageBox) = Panel();
        _messageText = Text(messageBox, _bold, 30, Colors.White);
        // Long messages (announcements) wrap rather than run off the screen.
        _messageText.AutowrapMode = TextServer.AutowrapMode.WordSmart;
        _messageText.CustomMinimumSize = new Vector2(1400, 0);
        (_place, var placeBox) = Panel();
        _placeText = Text(placeBox, _bold, 30, Colors.White);
        (_laps, var lapsBox) = Panel();
        lapsBox.AddChild(_lapRows);
        _list = new ListPanel(_regular, _bold);
        _root.AddChild(_list);
        (_help, var helpBox) = Panel();
        _helpTitle = Text(helpBox, _bold, 34, Colors.White);
        _helpRows.AddThemeConstantOverride("h_separation", 36);
        _helpRows.AddThemeConstantOverride("v_separation", 6);
        helpBox.AddChild(_helpRows);
        _credit = new Label();
        _credit.AddThemeFontOverride("font", _regular);
        _credit.AddThemeFontSizeOverride("font_size", 18);
        _credit.AddThemeColorOverride("font_color", new Color(1, 1, 1, 0.8f));
        _credit.AddThemeConstantOverride("outline_size", 6);
        _credit.AddThemeColorOverride("font_outline_color", new Color(0, 0, 0, 0.6f));
        _root.AddChild(_credit);

        viewport.SizeChanged += () => Fit(viewport);
        Fit(viewport);
    }

    // Fit scales the design to the screen's height.
    void Fit(Viewport viewport)
    {
        var size = viewport.GetVisibleRect().Size;
        float k = size.Y / DesignH;
        _root.Scale = new Vector2(k, k);
        _root.Size = size / k;
        _row.Position = new Vector2(0, 18);
        _row.Size = new Vector2(_root.Size.X, 0);
        _status.Position = new Vector2(20, _root.Size.Y - 44);
        _strips.Position = Vector2.Zero;
        _strips.Size = _root.Size;
        _list.Position = Vector2.Zero;
        _list.Size = _root.Size;
        _credit.Position = new Vector2(20, _root.Size.Y - 72);
        _place.Position = new Vector2(24, 24);
        _laps.Position = new Vector2(24, 300);
    }

    // Update shows the state (null: none, as without a core) and a status
    // line; now is the renderer's clock in seconds.
    // course, pos and ghost are for the strips: the ride's course (null:
    // none shown), the rider's distance along it (carried forward between
    // the core's updates) and the ghost's (negative: none); workout is the
    // workout under way, whose profile replaces them. note is a line
    // above the status line, under the map credit (riding a preview while
    // the world is built).
    public void Update(State? st, double now, string status, Course? course = null, double pos = 0, double ghost = -1, WorkoutDef? workout = null, string note = "")
    {
        _note = note;
        _strips.ShowLookAhead = _layout.Showing(Layout.LookAhead);
        _strips.ShowProfile = _layout.Showing(Layout.Profile);
        if (Tiles.WorkoutActive(st) && workout != null)
            _strips.ShowWorkout(workout, st!.Workout.ElapsedS);
        else
            _strips.Show(Tiles.RideActive(st) && !Tiles.WorkoutActive(st) ? course : null, pos, ghost);
        if (st != null && st.Sequence != _seq)
        {
            _seq = st.Sequence;
            var tr = st.Trainer;
            _cadence.Add(now, tr?.CadenceRpm ?? 0, tr != null && tr.HasCadenceRpm);
        }
        double? cadence = _cadence.Average(now, out var rpm) ? rpm : null;
        var (_, tiles) = st == null ? (Tiles.Dashboard, new List<Tile>()) : Tiles.For(st, cadence, _layout);
        while (_tiles.Count < tiles.Count)
            _tiles.Add(NewTile());
        for (int i = 0; i < _tiles.Count; i++)
        {
            var (panel, label, value, unit) = _tiles[i];
            panel.Visible = i < tiles.Count;
            if (!panel.Visible)
                continue;
            value.AddThemeFontSizeOverride("font_size", _layout.Medium ? MediumDigits : LargeDigits);
            panel.CustomMinimumSize = new Vector2(_layout.Medium ? TileMinW * 0.8f : TileMinW, 0);
            var t = tiles[i];
            label.Text = t.Label;
            value.Text = t.Value;
            value.AddThemeColorOverride("font_color", Readable(Color(t.ValueColor)));
            unit.Text = t.Unit == "" ? " " : t.Unit;
            unit.AddThemeColorOverride("font_color", t.UnitColor is Rgb u ? Color(u) : new Color(1, 1, 1, 0.7f));
        }
        _status.Text = status;

        _moments.Update(st, course, pos, now);
        var b = _moments.Big;
        _banner.Visible = b != null;
        if (b != null)
        {
            _bannerTitle.Text = b.Title;
            _bannerSub.Text = b.Sub;
            _bannerSub.AddThemeColorOverride("font_color", Readable(Color(b.SubColor)));
            _bannerLine.Text = b.Line;
            _bannerLine.Visible = b.Line != "";
            Center(_banner, 330);
        }
        var lines = new List<string>();
        if (_asking != "")
            lines.Add(_asking);
        if (_notice != "")
            lines.Add(_notice);
        lines.AddRange(_moments.Warnings);
        _warn.Visible = lines.Count > 0;
        if (_warn.Visible)
        {
            _warnText.Text = string.Join("   ·   ", lines);
            Center(_warn, PanelTop);
        }
        _help.Visible = _helpShown != null;
        if (_helpShown is { } h)
            Center(_help, _warn.Visible ? PanelTop + 80 : PanelTop + 30);
        _message.Visible = _moments.Message != "";
        if (_message.Visible)
        {
            _messageText.Text = _moments.Message;
            Center(_message, _warn.Visible ? PanelTop + 70 : PanelTop);
        }
        _place.Visible = _moments.Place != "";
        _placeText.Text = _moments.Place;
        // The note, bottom up from above the status line (the map's credit
        // is the start menu's: owner, 2026-10-10, not on rides).
        var above = new List<string>();
        if (_note != "")
            above.Add(_note);
        _credit.Text = string.Join("\n", above);
        _credit.Position = new Vector2(20, _root.Size.Y - 44 - 26 * System.Math.Max(1, above.Count) - 2);
        _laps.Visible = _moments.Laps.Count > 0;
        if (_laps.Visible)
        {
            while (_lapRows.GetChildCount() < _moments.Laps.Count)
            {
                var row = new HBoxContainer();
                row.AddThemeConstantOverride("separation", 24);
                Text(row, _regular, 26, Colors.White).CustomMinimumSize = new Vector2(190, 0);
                Text(row, _bold, 26, Colors.White);
                _lapRows.AddChild(row);
            }
            for (int i = 0; i < _lapRows.GetChildCount(); i++)
            {
                var row = (HBoxContainer)_lapRows.GetChild(i);
                row.Visible = i < _moments.Laps.Count;
                if (!row.Visible)
                    continue;
                var l = _moments.Laps[i];
                var c = l.Fastest ? Color(Palette.Power) : l.Running ? new Color(1, 1, 1, 0.65f) : Colors.White;
                foreach (var (label, text) in new[] { ((Label)row.GetChild(0), l.Label), ((Label)row.GetChild(1), l.Time) })
                {
                    label.Text = text;
                    label.AddThemeColorOverride("font_color", c);
                }
            }
        }
    }

    Layout _layout = new();

    // Arrange sets the rider's layout: the tiles, the strips shown, the
    // digits' size.
    public void Arrange(Layout l) => _layout = l;

    // Head sets what the head itself shows: a question waiting for its
    // second key, a notice (a command the core refused) and the key help
    // (null: none). Call it before Update.
    public void Head(string asking, string notice, (string Title, List<(string Keys, string What)> Entries)? help,
        Head.Controller.ListView? list = null, Head.Controller.PromptView? prompt = null, Head.Controller.DialogView? dialog = null,
        Head.Controller.FormView? form = null)
    {
        (_asking, _notice) = (asking, notice);
        _list.Show(help == null ? list : null, help == null ? prompt : null, help == null ? dialog : null, help == null ? form : null);
        if (help?.Title == _helpShown?.Title && help?.Entries.Count == _helpShown?.Entries.Count)
        {
            _helpShown = help;
            return;
        }
        _helpShown = help;
        foreach (var c in _helpRows.GetChildren())
            c.QueueFree();
        if (help is not { } h)
            return;
        _helpTitle.Text = "KEYS: " + h.Title;
        foreach (var (keys, what) in h.Entries)
        {
            var k = Text(_helpRows, _bold, 26, new Color("ffd700"));
            k.HorizontalAlignment = HorizontalAlignment.Right;
            k.Text = keys;
            var w = Text(_helpRows, _regular, 26, Colors.White);
            w.HorizontalAlignment = HorizontalAlignment.Left;
            w.Text = what;
        }
    }

    // Center puts a panel in the middle across, its top at y.
    // PanelTop: where the panels under the tiles start, clear of them
    // (owner, 2026-10-10: messages sat right against the numbers; the
    // tiles end at about 175).
    const float PanelTop = 215;

    void Center(Control c, float y)
    {
        c.ResetSize();
        c.Position = new Vector2((_root.Size.X - c.Size.X) / 2, y);
    }

    (PanelContainer, VBoxContainer) Panel()
    {
        var panel = new PanelContainer { Visible = false };
        panel.AddThemeStyleboxOverride("panel", new StyleBoxFlat
        {
            BgColor = new Color(0.04f, 0.06f, 0.08f, 0.82f), // text over anything: a light wall showed through at 0.66
            CornerRadiusTopLeft = 12, CornerRadiusTopRight = 12, CornerRadiusBottomLeft = 12, CornerRadiusBottomRight = 12,
            ContentMarginLeft = 24, ContentMarginRight = 24, ContentMarginTop = 10, ContentMarginBottom = 12,
        });
        var box = new VBoxContainer();
        box.AddThemeConstantOverride("separation", 2);
        panel.AddChild(box);
        _root.AddChild(panel);
        return (panel, box);
    }

    static Label Text(Container parent, Font f, int size, Color c)
    {
        var l = new Label { HorizontalAlignment = HorizontalAlignment.Center };
        l.AddThemeFontOverride("font", f);
        l.AddThemeFontSizeOverride("font_size", size);
        l.AddThemeColorOverride("font_color", c);
        parent.AddChild(l);
        return l;
    }

    (PanelContainer, Label, Label, Label) NewTile()
    {
        var panel = new PanelContainer { CustomMinimumSize = new Vector2(TileMinW, 0) };
        panel.AddThemeStyleboxOverride("panel", new StyleBoxFlat
        {
            BgColor = new Color(0.04f, 0.06f, 0.08f, 0.58f),
            CornerRadiusTopLeft = 12, CornerRadiusTopRight = 12, CornerRadiusBottomLeft = 12, CornerRadiusBottomRight = 12,
            ContentMarginLeft = 18, ContentMarginRight = 18, ContentMarginTop = 8, ContentMarginBottom = 10,
        });
        var box = new VBoxContainer();
        box.AddThemeConstantOverride("separation", -6);
        panel.AddChild(box);
        Label make(Font f, int size, Color c)
        {
            var l = new Label { HorizontalAlignment = HorizontalAlignment.Center };
            l.AddThemeFontOverride("font", f);
            l.AddThemeFontSizeOverride("font_size", size);
            l.AddThemeColorOverride("font_color", c);
            box.AddChild(l);
            return l;
        }
        var label = make(_regular, 22, new Color(1, 1, 1, 0.75f));
        var value = make(_bold, LargeDigits, Colors.White);
        var unit = make(_regular, 22, new Color(1, 1, 1, 0.7f));
        _row.AddChild(panel);
        return (panel, label, value, unit);
    }

    static Color Color(Rgb c) => Godot.Color.Color8(c.R, c.G, c.B);

    // Readable lifts a colour too dark to read on the HUD's dark panels
    // (the heat scale's blue on the flat, slate for descents) towards
    // white, keeping its hue.
    static Color Readable(Color c)
    {
        const float minLuma = 0.55f;
        for (int i = 0; i < 8 && c.Luminance < minLuma; i++)
            c = c.Lerp(Colors.White, 0.2f);
        return c;
    }
}

// Fonts are the HUD's: Atkinson Hyperlegible Next (fonts/, OFL), loaded
// from the file at run time, with tabular figures so numbers hold still.
public static class Fonts
{
    static FontFile? _file;

    public static Font Hud(int weight)
    {
        _file ??= LoadFile();
        var ts = TextServerManager.GetPrimaryInterface();
        return new FontVariation
        {
            BaseFont = _file,
            VariationOpentype = new Godot.Collections.Dictionary { { ts.NameToTag("wght"), weight } },
            OpentypeFeatures = new Godot.Collections.Dictionary { { ts.NameToTag("tnum"), 1 } },
        };
    }

    static FontFile LoadFile()
    {
        // As Godot imported it: in an exported build the font is in the
        // pack as that resource, not as the .ttf.
        var f = ResourceLoader.Load<FontFile>("res://fonts/AtkinsonHyperlegibleNext-wght.ttf");
        if (f == null)
        {
            GD.PushError("osscycler: the HUD font didn't load");
            f = new FontFile();
        }
        return f;
    }
}
