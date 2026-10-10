using System.Collections.Generic;
using System.Globalization;

namespace Osscycler;

// Args are the renderer's own arguments ("--name value" pairs), the ones
// after "--" on Godot's command line.
public sealed class Args
{
    readonly Dictionary<string, string> _values = new();

    public static Args Parse(string[] argv)
    {
        var a = new Args();
        for (int i = 0; i < argv.Length; i++)
        {
            if (!argv[i].StartsWith("--"))
                continue;
            var name = argv[i][2..];
            var value = "";
            if (name.Contains('='))
                (name, value) = (name[..name.IndexOf('=')], name[(name.IndexOf('=') + 1)..]);
            else if (i + 1 < argv.Length && !argv[i + 1].StartsWith("--"))
                value = argv[++i];
            a._values[name] = value;
        }
        return a;
    }

    public string Get(string name, string fallback) => _values.TryGetValue(name, out var v) ? v : fallback;

    // Has is whether --name was given (with a value or without: --debug).
    public bool Has(string name) => _values.ContainsKey(name);

    // File is a path argument. Godot runs in the project's directory, not
    // the one it was started from: a relative path is taken from where the
    // command was given ($PWD, which the shell sets), as the rider means it.
    public string File(string name, string fallback)
    {
        var v = Get(name, "");
        if (v == "")
            return fallback;
        var from = System.Environment.GetEnvironmentVariable("PWD");
        return System.IO.Path.IsPathRooted(v) || string.IsNullOrEmpty(from) ? v : System.IO.Path.GetFullPath(System.IO.Path.Combine(from, v));
    }

    public double Number(string name, double fallback) =>
        _values.TryGetValue(name, out var v) && double.TryParse(v, NumberStyles.Float, CultureInfo.InvariantCulture, out var d) ? d : fallback;
}
