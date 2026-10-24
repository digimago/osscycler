using System;
using System.Collections.Concurrent;
using System.IO;
using System.Linq;
using System.Net;
using System.Net.Sockets;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Threading;
using System.Threading.Tasks;

namespace Osscycler;

// ControlSocket is the renderer's control socket (--control PORT), for
// debugging sessions: a program (cmd/osscycler-mcp, which an AI assistant
// drives, or nc) moves the view along the course, looks round, takes
// screenshots and measures frames in the renderer that is open, instead of
// starting Godot again for every look (about 10 s each). On loopback only;
// it can do what the keyboard can, and write screenshots where it is told.
//
// The protocol: one JSON object per line each way. A request has "cmd" and
// the command's own fields, and an "id" its reply carries back; a reply has
// "ok" and the command's fields, or "error". Requests are taken on the
// socket's threads and served one at a time on Godot's main thread
// (Main.Control.cs: TryNext once a frame), each connection's in order.
public sealed class ControlSocket : IDisposable
{
    readonly TcpListener _listener;
    readonly ConcurrentQueue<Request> _queue = new();
    readonly CancellationTokenSource _stop = new();
    // Closed when the view does: a request still waiting then is answered
    // that the view closed, also one read off its socket a moment after
    // Dispose emptied the queue (on a slow machine its client had waited
    // for an answer that never came).
    readonly TaskCompletionSource _closed = new(TaskCreationOptions.RunContinuationsAsynchronously);

    public int Port { get; }

    public ControlSocket(int port)
    {
        _listener = new TcpListener(IPAddress.Loopback, port);
        _listener.Start();
        Port = ((IPEndPoint)_listener.LocalEndpoint).Port;
        _ = Task.Run(Accept);
    }

    // TryNext is the next request waiting, if any.
    public bool TryNext(out Request r) => _queue.TryDequeue(out r!);

    // Waiting is how many requests wait to be taken.
    public int Waiting => _queue.Count;

    public void Dispose()
    {
        _closed.TrySetResult();
        _stop.Cancel();
        _listener.Stop();
        while (_queue.TryDequeue(out var r))
            r.Fail("the view closed");
    }

    async Task Accept()
    {
        while (!_stop.IsCancellationRequested)
        {
            TcpClient c;
            try
            {
                c = await _listener.AcceptTcpClientAsync(_stop.Token);
            }
            catch (Exception)
            {
                return; // stopped
            }
            _ = Task.Run(() => Serve(c));
        }
    }

    async Task Serve(TcpClient c)
    {
        using var _ = c;
        try
        {
            var stream = c.GetStream();
            using var reader = new StreamReader(stream, new UTF8Encoding(false));
            using var writer = new StreamWriter(stream, new UTF8Encoding(false)) { AutoFlush = true, NewLine = "\n" };
            while (!_stop.IsCancellationRequested)
            {
                var line = await reader.ReadLineAsync(_stop.Token);
                if (line == null)
                    return;
                if (line.Trim() == "")
                    continue;
                var r = Request.Parse(line);
                if (r.Cmd != "")
                    _queue.Enqueue(r);
                if (await Task.WhenAny(r.Reply, _closed.Task) != r.Reply)
                    r.Fail("the view closed");
                await writer.WriteLineAsync(await r.Reply);
            }
        }
        catch (Exception)
        {
            // The other end went, or the view closed: nothing to tell.
        }
    }

    // Request is a command and its fields, answered once.
    public sealed class Request
    {
        readonly TaskCompletionSource<string> _reply = new(TaskCreationOptions.RunContinuationsAsynchronously);
        JsonNode? _id;

        public string Cmd { get; private set; } = "";
        public JsonObject Args { get; private set; } = new();
        public Task<string> Reply => _reply.Task;

        // Parse reads a request line; one that isn't a request is answered
        // at once with why (and has no command).
        public static Request Parse(string line)
        {
            var r = new Request();
            try
            {
                if (JsonNode.Parse(line) is not JsonObject o)
                    throw new JsonException("not an object");
                r._id = o["id"]?.DeepClone();
                r.Args = o;
                r.Cmd = o["cmd"]?.GetValue<string>() ?? "";
                if (r.Cmd == "")
                    r.Fail("no \"cmd\"");
            }
            catch (Exception e) when (e is JsonException or InvalidOperationException or FormatException)
            {
                r.Cmd = "";
                r.Fail("not a JSON request: " + e.Message);
            }
            return r;
        }

        public string Str(string name, string fallback = "") =>
            Args[name] is JsonValue v && v.TryGetValue<string>(out var s) ? s : fallback;

        public double? Num(string name) =>
            Args[name] is JsonValue v && v.TryGetValue<double>(out var d) && double.IsFinite(d) ? d : null;

        public bool? Bool(string name) =>
            Args[name] is JsonValue v && v.TryGetValue<bool>(out var b) ? b : null;

        public string[] Strs(string name) =>
            Args[name] is JsonArray a ? [.. a.Select(x => x is JsonValue v && v.TryGetValue<string>(out var s) ? s : "")] : [];

        public void Ok(JsonObject? fields = null)
        {
            var o = fields ?? new JsonObject();
            o["ok"] = true;
            Send(o);
        }

        public void Fail(string why) => Send(new JsonObject { ["ok"] = false, ["error"] = why });

        void Send(JsonObject o)
        {
            if (_id != null)
                o["id"] = _id.DeepClone();
            _reply.TrySetResult(o.ToJsonString());
        }
    }
}
