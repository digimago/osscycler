using System;
using System.Globalization;

namespace Osscycler.Hud;

// Rgb is a colour as the TUI writes it (sRGB bytes), apart from Godot so
// the HUD's model is tested without the engine.
public readonly record struct Rgb(byte R, byte G, byte B)
{
    public static Rgb Hex(uint rgb) => new((byte)(rgb >> 16), (byte)(rgb >> 8), (byte)rgb);
}

// Format writes numbers as the TUI does (tui/ride.go, history.go,
// loop.go), so both views read alike.
public static class Format
{
    static readonly CultureInfo C = CultureInfo.InvariantCulture;

    // Clock is m:ss, or h:mm:ss from an hour.
    public static string Clock(double s)
    {
        var t = TimeSpan.FromSeconds(Math.Max(0, s));
        return t.TotalHours >= 1
            ? string.Format(C, "{0}:{1:00}:{2:00}", (int)t.TotalHours, t.Minutes, t.Seconds)
            : string.Format(C, "{0}:{1:00}", (int)t.TotalMinutes, t.Seconds);
    }

    // LapTime has tenths: 0:39.4.
    public static string LapTime(double s)
    {
        int tenths = (int)Math.Round(Math.Max(0, s) * 10, MidpointRounding.AwayFromZero);
        return string.Format(C, "{0}:{1:00}.{2}", tenths / 600, tenths / 10 % 60, tenths % 10);
    }

    // Secs is a positive gap: 4.2 s, or 1:05 from a minute.
    public static string Secs(double s) => s >= 60 ? Clock(s) : string.Format(C, "{0:0.0} s", s);

    public static string Fixed(double v, int decimals) => v.ToString("F" + decimals, C);
}

// Palette is the TUI's colours (tui/model.go, ride.go, workout.go,
// history.go).
public static class Palette
{
    public static readonly Rgb Power = Rgb.Hex(0xffd700);   // xterm 220
    public static readonly Rgb Heart = Rgb.Hex(0xff5f5f);   // 203
    public static readonly Rgb Cadence = Rgb.Hex(0x5fd7ff); // 81
    public static readonly Rgb Speed = Rgb.Hex(0x87d787);   // 114
    public static readonly Rgb Dim = Rgb.Hex(0x8a8a8a);     // 240 is #585858: too dark over a scene
    public static readonly Rgb Plain = Rgb.Hex(0xf2f2f2);
    public static readonly Rgb Ahead = Rgb.Hex(0x59bf59);
    public static readonly Rgb Behind = Rgb.Hex(0xff6639);
    public static readonly Rgb Ghost = Rgb.Hex(0xd070ff);
    public static readonly Rgb Descent = Rgb.Hex(0x6c7a89);

    static readonly (double Grade, Rgb C)[] Heat =
    {
        (0, Rgb.Hex(0x2c7bb6)), (3, Rgb.Hex(0x00a6ca)), (5, Rgb.Hex(0x00ccbc)), (7, Rgb.Hex(0x90eb9d)),
        (9, Rgb.Hex(0xffff8c)), (11, Rgb.Hex(0xf9d057)), (13, Rgb.Hex(0xf29e2e)), (14.5, Rgb.Hex(0xd7191c)),
        (16, Rgb.Hex(0x7f0000)),
    };

    // Grade is the TUI's heat scale: blue on the flat to deep red at 16 %,
    // slate for any descent.
    public static Rgb Grade(double g)
    {
        if (g < -0.5)
            return Descent;
        g = Math.Clamp(g, 0, Heat[^1].Grade);
        for (int i = 1; i < Heat.Length; i++)
        {
            var (ga, a) = Heat[i - 1];
            var (gb, b) = Heat[i];
            if (g <= gb)
            {
                double t = (g - ga) / (gb - ga);
                byte mix(byte x, byte y) => (byte)Math.Round(x + (y - x) * t, MidpointRounding.AwayFromZero); // as Go's math.Round
                return new Rgb(mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B));
            }
        }
        return Heat[^1].C;
    }

    static readonly (double UpTo, Rgb C)[] Zones =
    {
        (0.60, Rgb.Hex(0x7f7f7f)), (0.76, Rgb.Hex(0x338cff)), (0.90, Rgb.Hex(0x59bf59)),
        (1.05, Rgb.Hex(0xffcc3f)), (1.19, Rgb.Hex(0xff6639)),
    };

    // Zone is the power zone's colour for a fraction of FTP (Zwift's).
    public static Rgb Zone(double frac)
    {
        foreach (var (upTo, c) in Zones)
            if (frac < upTo)
                return c;
        return Rgb.Hex(0xff330c);
    }
}
