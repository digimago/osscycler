package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// rpc runs the server over the lines given and returns its replies.
func rpc(t *testing.T, s *server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("reply %q: %v", l, err)
		}
		replies = append(replies, m)
	}
	return replies
}

func TestProtocol(t *testing.T) {
	s := newServer(t.TempDir())
	r := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"view_state","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fly"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
		`{"jsonrpc":"2.0","id":6,"method":"ping"}`,
	)
	if len(r) != 6 { // the notification has no reply
		t.Fatalf("%d replies, want 6: %v", len(r), r)
	}
	init := r[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" || init["capabilities"].(map[string]any)["tools"] == nil {
		t.Errorf("initialize: %v", init)
	}
	var names []string
	for _, x := range r[1]["result"].(map[string]any)["tools"].([]any) {
		tl := x.(map[string]any)
		names = append(names, tl["name"].(string))
		if tl["inputSchema"].(map[string]any)["type"] != "object" {
			t.Errorf("%s: schema %v", tl["name"], tl["inputSchema"])
		}
	}
	for _, want := range []string{"view_open", "view_shot", "view_goto", "view_state", "view_stats", "view_reload", "view_close"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools/list lacks %s: %v", want, names)
		}
	}
	// No view open: a tool error the assistant reads, not a protocol error.
	call := r[2]["result"].(map[string]any)
	if call["isError"] != true || !strings.Contains(call["content"].([]any)[0].(map[string]any)["text"].(string), "view_open first") {
		t.Errorf("view_state with no view: %v", r[2])
	}
	if r[3]["error"] == nil || r[4]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Errorf("unknown tool %v, unknown method %v", r[3], r[4])
	}
	if r[5]["result"] == nil || r[5]["id"].(float64) != 6 {
		t.Errorf("ping: %v", r[5])
	}
}

// fakeView is a control socket as the renderer's: it answers state and
// shot (writing a small PNG where it is told), and records the requests.
func fakeView(t *testing.T) (port int, got chan map[string]any) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got = make(chan map[string]any, 16)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			var req map[string]any
			_ = json.Unmarshal(sc.Bytes(), &req)
			got <- req
			rep := map[string]any{"id": req["id"], "ok": true, "course_id": "posbank", "along_m": 3350.0, "look_deg": 90.0, "view": "chase"}
			switch req["cmd"] {
			case "shot": // as the renderer, making the directory
				_ = os.MkdirAll(filepath.Dir(req["path"].(string)), 0o755)
				f, _ := os.Create(req["path"].(string))
				_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2)))
				f.Close()
			case "goto":
				rep = map[string]any{"id": req["id"], "ok": false, "error": "goto: no world shown"}
			}
			b, _ := json.Marshal(rep)
			c.Write(append(b, '\n'))
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, got
}

func TestShotThroughTheControlSocket(t *testing.T) {
	root := t.TempDir()
	port, got := fakeView(t)
	s := newServer(root)
	defer s.views.close()
	r := rpc(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"view_open","arguments":{"port":`+itoa(port)+`}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view_shot","arguments":{"m":3350,"look_deg":90,"save_to":"_scratch/s.png"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"view_goto","arguments":{"m":1}}}`,
	)
	if res := r[0]["result"].(map[string]any); res["isError"] == true {
		t.Fatalf("attach: %v", res)
	}
	<-got // the state view_open asks for
	req := <-got
	if req["cmd"] != "shot" || req["m"] != 3350.0 || req["look_deg"] != 90.0 || req["path"] != filepath.Join(root, "_scratch", "s.png") || req["width"] != 1280.0 {
		t.Errorf("shot request %v", req)
	}
	content := r[1]["result"].(map[string]any)["content"].([]any)
	img := content[0].(map[string]any)
	if img["type"] != "image" {
		t.Fatalf("shot: %v", content)
	}
	data, _ := base64.StdEncoding.DecodeString(img["data"].(string))
	if img["type"] != "image" || img["mimeType"] != "image/png" || !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Errorf("shot content %v", img["type"])
	}
	if cap := content[1].(map[string]any)["text"].(string); !strings.Contains(cap, "3.350 km of posbank") || !strings.Contains(cap, "saved to") {
		t.Errorf("caption %q", cap)
	}
	// The view's own error comes back as the tool's.
	if res := r[2]["result"].(map[string]any); res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "no world shown") {
		t.Errorf("goto error: %v", res)
	}
}

func TestLaunchArgs(t *testing.T) {
	a := launchArgs("/src/renderers/godot", "/w/posbank", openOpts{fromM: 1545, lookDeg: -90, seed: 2026285}, 7461, "/tmp/hud.json")
	want := []string{"--path", "/src/renderers/godot", "--", "--control", "7461", "--layout", "/tmp/hud.json",
		"--world", "/w/posbank", "--from", "1545", "--speed", "0", "--look", "-90", "--seed", "2026285"}
	if !slices.Equal(a, want) {
		t.Errorf("args\n%v\nwant\n%v", a, want)
	}
	c := launchArgs("/p", "", openOpts{core: "127.0.0.1:7420"}, 1, "")
	if !slices.Contains(c, "--core") || slices.Contains(c, "--world") {
		t.Errorf("following a core: %v", c)
	}
}

func TestWorldsByCourseID(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "_worlds", "velp-posbank")
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "world.json"), []byte("{}"), 0o644)
	v := newViews(root)
	if got, err := v.resolveWorld("velp-posbank"); err != nil || got != d {
		t.Errorf("by id: %q, %v", got, err)
	}
	if got, err := v.resolveWorld("_worlds/velp-posbank"); err != nil || got != d {
		t.Errorf("relative: %q, %v", got, err)
	}
	if _, err := v.resolveWorld("nowhere"); err == nil || !strings.Contains(err.Error(), "no world.json") {
		t.Errorf("missing: %v", err)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
