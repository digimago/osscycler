package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type tool struct {
	description string
	props       map[string]any // the arguments' JSON schemas
	required    []string
	run         func(v *views, a map[string]any) ([]any, error)
}

func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func strs(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

var (
	mArg     = num("metres along the course (the HUD's km × 1000); a loop wraps")
	lookArg  = num("degrees the view turns right of the way ahead (negative: left; 0 ahead, 180 behind)")
	speedArg = num("riding speed in km/h without a core (0 holds the view still)")
)

var tools = map[string]tool{
	"view_open": {
		description: "Open the 3D view (Godot) on a world at a spot, or attach to a view already open with --control PORT (port). A view opened here closes with view_close or this server; one attached to is left open. Builds the renderer's C# first unless build is false. Replaces a view open before.",
		props: map[string]any{
			"world":     str("the world: a directory (absolute or from the source tree), or a course id found in _worlds/ or ~/osscycler/worlds/"),
			"from_m":    mArg,
			"look_deg":  lookArg,
			"speed_kmh": num("riding speed in km/h (default 0: the view holds still)"),
			"seed":      num("the light, haze and heather's flowering (as --seed; marks record theirs)"),
			"core":      str("follow the ride on a core at host:port instead of a world (goto then moves the view, not the ride)"),
			"port":      num("attach to a view the rider started with --control PORT (no world needed)"),
			"build":     map[string]any{"type": "boolean", "description": "dotnet build the renderer first (default true)"},
		},
		run: func(v *views, a map[string]any) ([]any, error) {
			o := openOpts{world: argStr(a, "world"), fromM: argNum(a, "from_m", 0), lookDeg: argNum(a, "look_deg", 0),
				speedKmh: argNum(a, "speed_kmh", 0), seed: int(argNum(a, "seed", 0)), core: argStr(a, "core"),
				port: int(argNum(a, "port", 0)), build: true}
			if b, ok := a["build"].(bool); ok {
				o.build = b
			}
			if err := v.open(o); err != nil {
				return nil, err
			}
			return state(v, 2*time.Second)
		},
	},
	"view_close": {
		description: "Close the view opened with view_open (one attached to is only let go).",
		run: func(v *views, a map[string]any) ([]any, error) {
			v.close()
			return []any{text("closed")}, nil
		},
	},
	"view_state": {
		description: "Where the view is (along the course, the rider's and camera's place in the world's frame, lat/lon), the world (directory, stamp, course) and the view's settings.",
		run:         func(v *views, a map[string]any) ([]any, error) { return state(v, 5*time.Second) },
	},
	"view_goto": {
		description: "Move the view along the course, turn it, or set the speed; returns the state.",
		props:       map[string]any{"m": mArg, "look_deg": lookArg, "speed_kmh": speedArg},
		run: func(v *views, a map[string]any) ([]any, error) {
			rep, err := v.call("goto", pick(a, "m", "look_deg", "speed_kmh"), 30*time.Second)
			if err != nil {
				return nil, err
			}
			return []any{text(pretty(rep))}, nil
		},
	},
	"view_shot": {
		description: "A screenshot of the view, returned as an image: optionally moved (m) and turned (look_deg) first, after settle frames for the grass, shadows and haze to catch up.",
		props: map[string]any{
			"m":        mArg,
			"look_deg": lookArg,
			"settle":   num("frames to wait before taking it (default 10; 60 after a long jump if grass looks unfinished)"),
			"width":    num("pixels across (default 1280; the screen's own if larger)"),
			"save_to":  str("keep it at this path too (.png or .jpg; relative: from the source tree)"),
		},
		run: shot,
	},
	"view_stats": {
		description: "Measure the frame over some frames (as --stats): fps, render CPU and GPU time, draw calls, triangles, objects, the frame loop by section and the world's nodes by kind.",
		props:       map[string]any{"frames": num("frames to measure (default 60)")},
		run: func(v *views, a map[string]any) ([]any, error) {
			rep, err := v.call("stats", pick(a, "frames"), 60*time.Second)
			if err != nil {
				return nil, err
			}
			lines, _ := rep["lines"].([]any)
			delete(rep, "lines")
			out := pretty(rep)
			for _, l := range lines {
				out += "\n" + fmt.Sprint(l)
			}
			return []any{text(out)}, nil
		},
	},
	"view_reload": {
		description: "Load the world again (after rebuilding it), or another world, keeping the view where it is.",
		props:       map[string]any{"world": str("a directory or course id (default: the world shown)")},
		run: func(v *views, a map[string]any) ([]any, error) {
			f := map[string]any{}
			if w := argStr(a, "world"); w != "" {
				d, err := v.resolveWorld(w)
				if err != nil {
					return nil, err
				}
				f["world"] = d
			}
			rep, err := v.call("load", f, 120*time.Second)
			if err != nil {
				return nil, err
			}
			return []any{text(pretty(rep))}, nil
		},
	},
	"view_hide": {
		description: "Leave parts of the world out of the view, as --hide: world node name prefixes (\"plants\", \"plants bush\", \"terrain\", \"roads\", \"buildings\", \"water\"), \"grass\" (grown near the rider) or \"shadows\". An empty list shows everything again.",
		props:       map[string]any{"parts": strs("what to leave out")},
		required:    []string{"parts"},
		run: func(v *views, a map[string]any) ([]any, error) {
			rep, err := v.call("hide", map[string]any{"parts": argStrs(a, "parts")}, 10*time.Second)
			if err != nil {
				return nil, err
			}
			return []any{text(pretty(rep))}, nil
		},
	},
	"view_keys": {
		description: "Press keys in the view, named as its head names them: \"v\" (chase or eyes view; the view's own layout copy, not the rider's), \"z\", \"o\", \"esc\", \"enter\", arrows (\"up\", \"ctrl+right\"), letters.",
		props:       map[string]any{"keys": strs("the keys, in order")},
		required:    []string{"keys"},
		run: func(v *views, a map[string]any) ([]any, error) {
			rep, err := v.call("keys", map[string]any{"keys": argStrs(a, "keys")}, 10*time.Second)
			if err != nil {
				return nil, err
			}
			return []any{text(pretty(rep))}, nil
		},
	},
	"view_mark": {
		description: "Mark the spot looked at as flawed, as space does in debug mode: a line in the marks file (~/osscycler/marks/marks.jsonl unless the view was given --marks) with a screenshot; the world-faults skill's marks.py reads them.",
		run: func(v *views, a map[string]any) ([]any, error) {
			rep, err := v.call("mark", nil, 10*time.Second)
			if err != nil {
				return nil, err
			}
			return []any{text("marked; marks in " + fmt.Sprint(rep["marks"]))}, nil
		},
	},
}

// toolList is tools as tools/list gives them, by name.
func toolList() []any {
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, n := range names {
		t := tools[n]
		schema := map[string]any{"type": "object", "properties": t.props}
		if t.props == nil {
			schema["properties"] = map[string]any{}
		}
		if len(t.required) > 0 {
			schema["required"] = t.required
		}
		out = append(out, map[string]any{"name": n, "description": t.description, "inputSchema": schema})
	}
	return out
}

func state(v *views, timeout time.Duration) ([]any, error) {
	rep, err := v.call("state", nil, timeout)
	if err != nil {
		return nil, err
	}
	return []any{text(pretty(rep))}, nil
}

func shot(v *views, a map[string]any) ([]any, error) {
	path := argStr(a, "save_to")
	keep := path != ""
	if keep && !filepath.IsAbs(path) {
		path = filepath.Join(v.root, path)
	}
	if !keep {
		dir := filepath.Join(os.TempDir(), "osscycler-mcp")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(dir, fmt.Sprintf("shot-%d.jpg", time.Now().UnixNano()))
	}
	f := pick(a, "m", "look_deg", "settle")
	f["path"] = path
	f["width"] = argNum(a, "width", 1280)
	rep, err := v.call("shot", f, 60*time.Second)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !keep {
		os.Remove(path)
	}
	mime := "image/jpeg"
	if strings.EqualFold(filepath.Ext(path), ".png") {
		mime = "image/png"
	}
	caption := fmt.Sprintf("%.3f km of %v, look %v°, %v view", num0(rep["along_m"])/1000, rep["course_id"], rep["look_deg"], rep["view"])
	if keep {
		caption += ", saved to " + path
	}
	return []any{
		map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(b), "mimeType": mime},
		text(caption),
	}, nil
}

// pick is the named arguments that are given, for the view.
func pick(a map[string]any, names ...string) map[string]any {
	f := map[string]any{}
	for _, n := range names {
		if x, ok := a[n]; ok && x != nil {
			f[n] = x
		}
	}
	return f
}

func argStr(a map[string]any, n string) string {
	s, _ := a[n].(string)
	return s
}

func argNum(a map[string]any, n string, fallback float64) float64 {
	if x, ok := a[n].(float64); ok {
		return x
	}
	return fallback
}

func argStrs(a map[string]any, n string) []string {
	out := []string{}
	xs, _ := a[n].([]any)
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func num0(x any) float64 {
	f, _ := x.(float64)
	return f
}

func pretty(m map[string]any) string {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Sprint(m)
	}
	return string(b)
}
