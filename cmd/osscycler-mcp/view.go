package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// views is the one 3D view open: launched here (and closed with this
// program) or attached to (left open).
type views struct {
	root string

	mu       sync.Mutex
	conn     net.Conn
	rd       *bufio.Reader
	cmd      *exec.Cmd
	exited   chan struct{}
	attached bool
	logPath  string
	nextID   int
}

func newViews(root string) *views { return &views{root: root} }

// openOpts are view_open's arguments.
type openOpts struct {
	world    string // a directory, or a course id
	fromM    float64
	lookDeg  float64
	speedKmh float64
	seed     int
	core     string // follow a core's ride instead of a world
	port     int    // attach to a view already open on this control port
	build    bool   // dotnet build first
}

func (v *views) project() string { return filepath.Join(v.root, "renderers", "godot") }

// open opens a view as o says, closing one open before.
func (v *views) open(o openOpts) error {
	v.close()
	if o.port > 0 {
		return v.dial(o.port, nil, 5*time.Second)
	}
	var world string
	if o.core == "" {
		w, err := v.resolveWorld(o.world)
		if err != nil {
			return err
		}
		world = w
	}
	if o.build {
		if err := v.build(); err != nil {
			return err
		}
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	layout, err := v.layoutCopy()
	if err != nil {
		return err
	}
	godot, err := findTool("OSSCYCLER_GODOT", "godot", filepath.Join(home(), ".local", "bin", "godot"))
	if err != nil {
		return fmt.Errorf("no Godot (set $OSSCYCLER_GODOT; the world-assets skill installs it): %w", err)
	}
	if err := os.MkdirAll(filepath.Join(v.root, "_logs"), 0o755); err != nil {
		return err
	}
	v.logPath = filepath.Join(v.root, "_logs", "osscycler-mcp-godot.log")
	lf, err := os.Create(v.logPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(godot, launchArgs(v.project(), world, o, port, layout)...)
	cmd.Dir = v.root
	cmd.Env = append(os.Environ(), "PWD="+v.root)
	cmd.Stdout, cmd.Stderr = lf, lf
	setParentDeath(cmd)
	if err := cmd.Start(); err != nil {
		lf.Close()
		return err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		lf.Close()
		close(exited)
	}()
	if err := v.dial(port, exited, 120*time.Second); err != nil {
		_ = cmd.Process.Kill()
		<-exited
		return fmt.Errorf("%w\n%s", err, tail(v.logPath, 20))
	}
	v.mu.Lock()
	v.cmd, v.exited = cmd, exited
	v.mu.Unlock()
	return nil
}

// launchArgs are Godot's arguments for a view of world (or of the core's
// ride) on control port port.
func launchArgs(project, world string, o openOpts, port int, layout string) []string {
	f := func(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
	// No --debug: its notice would cover the first shots, and the socket
	// moves and marks itself.
	a := []string{"--path", project, "--", "--control", strconv.Itoa(port)}
	if layout != "" {
		a = append(a, "--layout", layout)
	}
	if o.core != "" {
		a = append(a, "--core", o.core)
	} else {
		a = append(a, "--world", world, "--from", f(o.fromM), "--speed", f(o.speedKmh))
	}
	if o.lookDeg != 0 {
		a = append(a, "--look", f(o.lookDeg))
	}
	if o.seed != 0 {
		a = append(a, "--seed", strconv.Itoa(o.seed))
	}
	return a
}

// dial connects to the control socket on port, waiting for it to open
// (giving up when exited closes: the view didn't start).
func (v *views) dial(port int, exited <-chan struct{}, wait time.Duration) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(wait)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			v.mu.Lock()
			v.conn, v.rd, v.attached = c, bufio.NewReader(c), exited == nil
			v.mu.Unlock()
			return nil
		}
		select {
		case <-exited:
			return errors.New("the view closed before its control socket opened")
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no control socket on %s: %w", addr, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// close closes the view: quits one launched here, leaves one attached to.
func (v *views) close() {
	v.mu.Lock()
	conn, cmd, exited, attached := v.conn, v.cmd, v.exited, v.attached
	v.mu.Unlock()
	if conn != nil && !attached && cmd != nil {
		_, _ = v.call("quit", nil, 5*time.Second)
	}
	if conn != nil {
		conn.Close()
	}
	if cmd != nil {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}
	v.mu.Lock()
	v.conn, v.rd, v.cmd, v.exited, v.attached = nil, nil, nil, nil, false
	v.mu.Unlock()
}

// call sends a command with its fields and waits for the reply's fields.
func (v *views) call(cmd string, fields map[string]any, timeout time.Duration) (map[string]any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.conn == nil {
		return nil, errors.New("no view open: view_open first")
	}
	v.nextID++
	req := map[string]any{"id": v.nextID, "cmd": cmd}
	for k, x := range fields {
		req[k] = x
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	_ = v.conn.SetDeadline(time.Now().Add(timeout))
	if _, err := v.conn.Write(append(b, '\n')); err != nil {
		return nil, v.lost(err)
	}
	line, err := v.rd.ReadBytes('\n')
	if err != nil {
		return nil, v.lost(err)
	}
	var rep map[string]any
	if err := json.Unmarshal(line, &rep); err != nil {
		return nil, fmt.Errorf("the view answered %q: %w", strings.TrimSpace(string(line)), err)
	}
	if ok, _ := rep["ok"].(bool); !ok {
		msg, _ := rep["error"].(string)
		return nil, errors.New(msg)
	}
	delete(rep, "ok")
	delete(rep, "id")
	return rep, nil
}

// lost is err from a connection that broke; the view is taken as gone.
// Called with mu held.
func (v *views) lost(err error) error {
	v.conn.Close()
	v.conn, v.rd = nil, nil
	msg := fmt.Sprintf("the view went (%v)", err)
	if v.logPath != "" && !v.attached {
		msg += "\n" + tail(v.logPath, 15)
	}
	return errors.New(msg)
}

// resolveWorld is world as a directory with a world in it: as given
// (relative to the source tree), else a course id in _worlds/ or
// ~/osscycler/worlds/.
func (v *views) resolveWorld(world string) (string, error) {
	if world == "" {
		return "", errors.New("which world? a directory, or a course id")
	}
	var tried []string
	cands := []string{world}
	if !filepath.IsAbs(world) {
		cands = []string{filepath.Join(v.root, world), filepath.Join(v.root, "_worlds", world), filepath.Join(home(), "osscycler", "worlds", world)}
	}
	for _, d := range cands {
		if isFile(filepath.Join(d, "world.json")) {
			return filepath.Clean(d), nil
		}
		tried = append(tried, d)
	}
	return "", fmt.Errorf("no world.json in %s", strings.Join(tried, ", "))
}

// build compiles the renderer's C# (Godot runs the assembly last built).
func (v *views) build() error {
	dotnet, err := findTool("DOTNET", "dotnet", filepath.Join(home(), ".dotnet", "dotnet"))
	if err != nil {
		return fmt.Errorf("no dotnet to build the view (set $DOTNET, or open with build false): %w", err)
	}
	cmd := exec.Command(dotnet, "build", filepath.Join(v.project(), "OsscyclerGodot.sln"), "-nologo", "-v", "q")
	cmd.Dir = v.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("dotnet build failed: %w\n%s", err, strings.Join(lines[max(0, len(lines)-20):], "\n"))
	}
	return nil
}

// layoutCopy is a copy of the rider's HUD layout for the view to use: it
// looks as the rider's, and v (chase or eyes) and o change the copy only.
func (v *views) layoutCopy() (string, error) {
	dir := filepath.Join(os.TempDir(), "osscycler-mcp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "hud.json")
	b, err := os.ReadFile(filepath.Join(home(), "osscycler", "hud.json"))
	if err != nil {
		b = []byte("{}")
	}
	return dst, os.WriteFile(dst, b, 0o600)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// findTool is a program: $env, else name on the PATH, else fallback.
func findTool(env, name, fallback string) (string, error) {
	if p := os.Getenv(env); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if isFile(fallback) {
		return fallback, nil
	}
	return "", fmt.Errorf("%s not on the PATH nor at %s", name, fallback)
}

// findRoot is the osscycler source tree: dir if given, else the working
// directory or one above it with the Godot project.
func findRoot(dir string) (string, error) {
	if dir != "" {
		return filepath.Abs(dir)
	}
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isFile(filepath.Join(d, "renderers", "godot", "project.godot")) {
			return d, nil
		}
		up := filepath.Dir(d)
		if up == d {
			return "", errors.New("not in the osscycler source tree (no renderers/godot/project.godot above here): give -root")
		}
		d = up
	}
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// tail is the last n lines of the file at p.
func tail(p string, n int) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return "log " + p + ":\n" + strings.Join(lines[max(0, len(lines)-n):], "\n")
}
