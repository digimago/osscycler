using System;
using System.IO;
using System.Net.Sockets;
using System.Text.Json.Nodes;
using System.Threading.Tasks;
using Xunit;

namespace Osscycler.Hud.Tests;

// The control socket (--control): one JSON request per line, served one at
// a time where the frame loop takes them, each reply carrying its id.
public class ControlSocketTests
{
    sealed class Conn : IDisposable
    {
        readonly TcpClient _c = new();
        public readonly StreamReader In;
        public readonly StreamWriter Out;

        public Conn(int port)
        {
            _c.Connect("127.0.0.1", port);
            In = new StreamReader(_c.GetStream());
            Out = new StreamWriter(_c.GetStream()) { AutoFlush = true, NewLine = "\n" };
        }

        public async Task<JsonObject> Read() =>
            (JsonObject)JsonNode.Parse((await In.ReadLineAsync().WaitAsync(TimeSpan.FromSeconds(5)))!)!;

        public void Dispose() => _c.Dispose();
    }

    // Next waits for the frame loop's next request.
    static async Task<ControlSocket.Request> Next(ControlSocket s)
    {
        for (int i = 0; i < 500; i++)
        {
            if (s.TryNext(out var r))
                return r;
            await Task.Delay(10);
        }
        throw new TimeoutException("no request");
    }

    [Fact]
    public async Task ARequestIsServedWhereTheLoopTakesIt()
    {
        using var s = new ControlSocket(0);
        using var c = new Conn(s.Port);
        c.Out.WriteLine("""{"id": 7, "cmd": "goto", "m": 1545.5, "look_deg": -90, "keys": ["v", "z"]}""");
        var r = await Next(s);
        Assert.Equal("goto", r.Cmd);
        Assert.Equal(1545.5, r.Num("m"));
        Assert.Equal(-90, r.Num("look_deg"));
        Assert.Null(r.Num("speed_kmh"));
        Assert.Equal(new[] { "v", "z" }, r.Strs("keys"));
        r.Ok(new JsonObject { ["along_m"] = 1545.5 });
        var reply = await c.Read();
        Assert.True((bool)reply["ok"]!);
        Assert.Equal(7, (int)reply["id"]!);
        Assert.Equal(1545.5, (double)reply["along_m"]!);
    }

    [Fact]
    public async Task WhatIsNoRequestIsAnsweredAtOnce()
    {
        using var s = new ControlSocket(0);
        using var c = new Conn(s.Port);
        c.Out.WriteLine("not json");
        var a = await c.Read();
        Assert.False((bool)a["ok"]!);
        Assert.Contains("not a JSON request", (string)a["error"]!);
        c.Out.WriteLine("""{"id": "x"}""");
        var b = await c.Read();
        Assert.False((bool)b["ok"]!);
        Assert.Equal("x", (string)b["id"]!);
        Assert.False(s.TryNext(out _)); // neither reached the frame loop
    }

    [Fact]
    public async Task AFailedCommandSaysWhy()
    {
        using var s = new ControlSocket(0);
        using var c = new Conn(s.Port);
        c.Out.WriteLine("""{"cmd": "fly"}""");
        (await Next(s)).Fail("unknown command \"fly\"");
        var a = await c.Read();
        Assert.False((bool)a["ok"]!);
        Assert.Equal("unknown command \"fly\"", (string)a["error"]!);
        Assert.Null(a["id"]);
    }

    [Fact]
    public async Task RequestsWaitingWhenTheViewClosesAreAnswered()
    {
        var s = new ControlSocket(0);
        using var c = new Conn(s.Port);
        c.Out.WriteLine("""{"id": 2, "cmd": "state"}""");
        // Queued, not taken: wait for it (a fixed 200 ms wasn't always
        // enough on CI, and the request came in after the view closed).
        for (var waited = 0; s.Waiting == 0 && waited < 5000; waited += 10)
            await Task.Delay(10);
        Assert.Equal(1, s.Waiting);
        s.Dispose();
        var a = await c.Read();
        Assert.False((bool)a["ok"]!);
        Assert.Equal(2, (int)a["id"]!);
        Assert.Equal("the view closed", (string)a["error"]!);
    }
}
