// Command osscycler-mcp is an MCP server (Model Context Protocol, over
// stdio) for development sessions with an AI assistant: it opens the 3D
// view on a world, or attaches to one already open, and drives it through
// the view's control socket (--control: renderers/godot/src/ControlSocket.cs):
// move along the course, look round, take screenshots (returned as images),
// measure frames, reload a rebuilt world, press keys, mark a spot. One view
// stays open between calls, so a look costs a fraction of a second instead
// of a Godot start-up.
//
// The repository's .mcp.json registers it for Claude Code (go run). Not in
// release archives: it needs the source tree (the Godot project, Godot and
// .NET as the world-assets skill installs them).
//
// The protocol is JSON-RPC 2.0, one message per line; only what an MCP
// client needs of it is here (initialize, ping, tools/list, tools/call), no
// dependency.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
)

// protocolVersions are the MCP versions this server speaks, newest first.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

const instructions = `Drives osscycler's 3D view (the Godot renderer) for checking worlds by eye. view_open opens a world (a directory, or a course id from _worlds/ or ~/osscycler/worlds/) at a distance along the course, or attaches to a view the rider started with --control PORT; then view_shot (go somewhere, look, get a screenshot), view_goto, view_state, view_stats, view_reload (after rebuilding the world), view_hide, view_keys, view_mark, view_close. Distances are metres along the course, as the HUD's km marks; look_deg turns the view right of the way ahead.`

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	root := flag.String("root", "", "the osscycler source tree (default: found from the working directory)")
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix("osscycler-mcp: ")
	r, err := findRoot(*root)
	if err != nil {
		log.Fatal(err)
	}
	s := newServer(r)
	defer s.views.close()
	if err := s.serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	views *views
}

func newServer(root string) *server { return &server{views: newViews(root)} }

// serve answers requests from in on out until in ends.
func (s *server) serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			if err := enc.Encode(message{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error: " + err.Error()}}); err != nil {
				return err
			}
			continue
		}
		if len(m.ID) == 0 || m.Method == "" {
			continue // a notification, or a reply to nothing we asked
		}
		res, rerr := s.handle(m.Method, m.Params)
		reply := message{JSONRPC: "2.0", ID: m.ID, Result: res, Error: rerr}
		if rerr == nil && res == nil {
			reply.Result = struct{}{}
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *server) handle(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		v := protocolVersions[0]
		for _, known := range protocolVersions {
			if p.ProtocolVersion == known {
				v = known
			}
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "osscycler-view", "version": "1"},
			"instructions":    instructions,
		}, nil
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params: " + err.Error()}
		}
		t, ok := tools[p.Name]
		if !ok {
			return nil, &rpcError{-32602, fmt.Sprintf("unknown tool %q", p.Name)}
		}
		args := map[string]any{}
		if len(p.Arguments) > 0 && string(p.Arguments) != "null" {
			if err := json.Unmarshal(p.Arguments, &args); err != nil {
				return nil, &rpcError{-32602, "invalid arguments: " + err.Error()}
			}
		}
		content, err := t.run(s.views, args)
		if err != nil {
			return map[string]any{"content": []any{text(err.Error())}, "isError": true}, nil
		}
		return map[string]any{"content": content}, nil
	}
	return nil, &rpcError{-32601, "method not found: " + method}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
