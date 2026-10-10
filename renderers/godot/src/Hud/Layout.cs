using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Osscycler.Hud;

// Layout is the rider's arrangement of the HUD (owner, 2026-10-09): the
// tiles of each screen and their order (the TUI's choices and defaults,
// tui/layout.go), which strips show along the bottom, and the digits'
// size. Kept in a file on the renderer's machine (hud.json in osscycler's
// folder): it is about this screen, not the core.
public sealed class Layout
{
    public const int MaxTiles = 8; // two rows of four in the TUI; across the top here

    // Choices are each screen's tiles, defaults first.
    public static readonly IReadOnlyDictionary<string, string[]> Choices = new Dictionary<string, string[]>
    {
        [Tiles.Dashboard] = new[] { "power", "heart_rate", "cadence", "speed", "cadence_5s", "distance" },
        [Tiles.Ride] = new[] { "power", "heart_rate", "cadence_5s", "grade", "time", "to_go", "cadence", "speed", "climbed", "avg_power" },
        [Tiles.Workout] = new[] { "target", "power", "cadence_5s", "interval", "heart_rate", "cadence", "workout_left", "avg_power", "speed" },
    };

    public static readonly IReadOnlyDictionary<string, string[]> Defaults = new Dictionary<string, string[]>
    {
        [Tiles.Dashboard] = new[] { "power", "heart_rate", "cadence", "speed" },
        // Heart rate too (owner, 2026-10-10: it disappeared once a ride
        // took over from the dashboard).
        [Tiles.Ride] = new[] { "power", "heart_rate", "cadence_5s", "grade", "time", "to_go" },
        [Tiles.Workout] = new[] { "target", "power", "cadence_5s", "interval", "heart_rate" },
    };

    public static readonly IReadOnlyDictionary<string, string> ScreenNames = new Dictionary<string, string>
    {
        [Tiles.Dashboard] = "DASHBOARD", [Tiles.Ride] = "COURSE RIDE", [Tiles.Workout] = "WORKOUT",
    };

    // The strips along the bottom, each shown unless hidden.
    public const string LookAhead = "look_ahead", Profile = "profile";
    public static readonly string[] Strips = { LookAhead, Profile };

    public static readonly IReadOnlyDictionary<string, string> Labels = new Dictionary<string, string>
    {
        ["power"] = "Power", ["heart_rate"] = "Heart rate", ["cadence"] = "Cadence", ["cadence_5s"] = "Cadence, 5 s average",
        ["speed"] = "Speed", ["distance"] = "Distance", ["grade"] = "Grade", ["time"] = "Time (and the gap to your ghost)",
        ["to_go"] = "Distance to go", ["climbed"] = "Climbed", ["avg_power"] = "Average power", ["target"] = "Target power",
        ["interval"] = "Time left in this part", ["workout_left"] = "Time left in the workout",
        [LookAhead] = "Look-ahead strip (the next 250 m)", [Profile] = "Course or workout profile",
    };

    // TilesBy lists the shown tiles per screen, in order (a screen not in
    // it shows its defaults).
    [JsonPropertyName("tiles")] public Dictionary<string, List<string>> TilesBy { get; set; } = new();
    [JsonPropertyName("hide")] public List<string> Hide { get; set; } = new();
    // Digits is "medium" for smaller numbers; large otherwise.
    [JsonPropertyName("digits")] public string Digits { get; set; } = "";

    [JsonIgnore] public bool Medium => Digits == "medium";

    // View is "eyes" for the rider's own view; behind and above the rider
    // (chase, the default: owner, 2026-10-09) otherwise. Used without a
    // core: with one, the rider's profile keeps the view.
    [JsonPropertyName("view")] public string View { get; set; } = "";
    [JsonIgnore] public bool Chase => View != "eyes";

    public IReadOnlyList<string> Shown(string screen) =>
        TilesBy.TryGetValue(screen, out var ids) && ids.Count > 0 ? ids : Defaults[screen];

    public bool Showing(string strip) => !Hide.Contains(strip);

    public Layout Clone() => new()
    {
        TilesBy = TilesBy.ToDictionary(kv => kv.Key, kv => kv.Value.ToList()),
        Hide = Hide.ToList(),
        Digits = Digits,
        View = View,
    };

    // Load reads the arrangement; a missing file is the defaults. Unknown
    // tiles and strips are dropped, so an older or newer renderer copes.
    public static Layout Load(string path)
    {
        if (!File.Exists(path))
            return new Layout();
        var l = JsonSerializer.Deserialize<Layout>(File.ReadAllText(path)) ?? new Layout();
        var tiles = new Dictionary<string, List<string>>();
        foreach (var (screen, ids) in l.TilesBy ?? new())
        {
            if (!Choices.TryGetValue(screen, out var choices))
                continue;
            var keep = new List<string>();
            foreach (var id in ids ?? new())
                if (choices.Contains(id) && !keep.Contains(id) && keep.Count < MaxTiles)
                    keep.Add(id);
            tiles[screen] = keep;
        }
        l.TilesBy = tiles;
        l.Hide = (l.Hide ?? new()).Where(Strips.Contains).Distinct().ToList();
        l.Digits ??= "";
        l.View = l.View == "eyes" ? "eyes" : "";
        return l;
    }

    // Save writes it atomically.
    public void Save(string path)
    {
        var dir = Path.GetDirectoryName(Path.GetFullPath(path))!;
        Directory.CreateDirectory(dir);
        var tmp = Path.Combine(dir, $".hud-{Guid.NewGuid():N}.json");
        try
        {
            File.WriteAllText(tmp, JsonSerializer.Serialize(this, new JsonSerializerOptions { WriteIndented = true }) + "\n");
            File.Move(tmp, path, true);
        }
        finally
        {
            File.Delete(tmp);
        }
    }
}

// Arranger is the editor for one screen's tiles and the strips, by keys
// as the TUI's (tui/layout.go): ↑/↓ choose, shift+↑/↓ (or K/J) move the
// tile, 1-8 put it in that place, space shows or hides, r defaults, enter
// keeps, esc (or o) cancels.
public sealed class Arranger
{
    public string Screen { get; }
    public List<string> Order { get; private set; } = new(); // every choice, shown ones first; then the strips
    public HashSet<string> On { get; private set; } = new();
    public int Cursor { get; private set; }
    public string Notice { get; private set; } = "";

    readonly Layout _from;

    public Arranger(Layout from, string screen)
    {
        (_from, Screen) = (from, screen);
        Fill(from);
    }

    void Fill(Layout l)
    {
        Order = new();
        On = new();
        foreach (var id in l.Shown(Screen))
        {
            Order.Add(id);
            On.Add(id);
        }
        foreach (var id in Layout.Choices[Screen])
            if (!On.Contains(id))
                Order.Add(id);
        foreach (var s in Layout.Strips)
        {
            Order.Add(s);
            if (l.Showing(s))
                On.Add(s);
        }
    }

    static bool IsStrip(string id) => Layout.Strips.Contains(id);

    int TileCount => Order.Count(id => !IsStrip(id) && On.Contains(id));

    // Key handles a key; done is set when the editor closes, with the new
    // layout (null: cancelled).
    public bool Key(string key, out Layout? result)
    {
        result = null;
        Notice = "";
        string id = Order[Cursor];
        int tiles = Order.Count(o => !IsStrip(o));
        switch (key)
        {
            case "up" or "k":
                Cursor = Math.Max(0, Cursor - 1);
                break;
            case "down" or "j":
                Cursor = Math.Min(Order.Count - 1, Cursor + 1);
                break;
            case "shift+up" or "K" or "shift+down" or "J":
                int to = key is "shift+up" or "K" ? Cursor - 1 : Cursor + 1;
                if (!IsStrip(id) && to >= 0 && to < tiles)
                {
                    (Order[Cursor], Order[to]) = (Order[to], Order[Cursor]);
                    Cursor = to;
                }
                break;
            case " " or "space" or "x":
                if (IsStrip(id))
                {
                    if (!On.Remove(id))
                        On.Add(id);
                }
                else if (On.Contains(id) && TileCount == 1)
                    Notice = "keep at least one tile";
                else if (!On.Contains(id) && TileCount >= Layout.MaxTiles)
                    Notice = $"at most {Layout.MaxTiles} tiles";
                else if (!On.Remove(id))
                    On.Add(id);
                break;
            case "1" or "2" or "3" or "4" or "5" or "6" or "7" or "8":
                if (IsStrip(id))
                    break;
                if (!On.Contains(id) && TileCount >= Layout.MaxTiles)
                {
                    Notice = $"at most {Layout.MaxTiles} tiles";
                    break;
                }
                On.Add(id);
                Cursor = Place(id, key[0] - '0');
                break;
            case "r":
                var def = _from.Clone();
                def.TilesBy.Remove(Screen);
                def.Hide.Clear();
                Fill(def);
                break;
            case "enter":
                var l = _from.Clone();
                var ids = Order.Where(o => !IsStrip(o) && On.Contains(o)).ToList();
                if (ids.SequenceEqual(Layout.Defaults[Screen]))
                    l.TilesBy.Remove(Screen);
                else
                    l.TilesBy[Screen] = ids;
                l.Hide = Layout.Strips.Where(s => !On.Contains(s)).ToList();
                result = l;
                return true;
            case "esc" or "o":
                return true;
        }
        return false;
    }

    // Place moves id to be the nth shown tile (1-based), the tiles from
    // there on moving down one; past the last it goes last. Its new index.
    int Place(string id, int n)
    {
        Order.Remove(id);
        int at = Order.Count(o => !IsStrip(o)), seen = 0;
        for (int i = 0; i < Order.Count && !IsStrip(Order[i]); i++)
        {
            if (!On.Contains(Order[i]))
                continue;
            if (++seen == n)
            {
                at = i;
                break;
            }
            at = i + 1;
        }
        Order.Insert(at, id);
        return at;
    }
}
