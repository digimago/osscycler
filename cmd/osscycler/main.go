// Command osscycler is the one command to ride: it finds the core (one
// already running, such as the systemd service, or one at -core), else
// starts one in the background for as long as the screen is open, and
// opens a screen on it: the 3D view where there is one and a display,
// else the TUI.
//
//	osscycler              ride: the 3D view if installed, else the TUI
//	osscycler 3d           the 3D view
//	osscycler tui          the terminal view
//	osscycler --headless   just the core, for screens elsewhere (e.g. a Pi)
//	osscycler core ...     the same with osscycler-core's own flags
//
// With -core HOST:PORT the screen goes to a core on another machine
// (the TUI over TLS: -tls-ca; the 3D view talks to this machine only for
// now). A core it starts itself logs to ~/osscycler/logs/core.log and is
// stopped (saving the ride) when the screen closes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/digimago/osscycler/internal/api"
	"github.com/digimago/osscycler/internal/home"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "osscycler:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		core      = flag.String("core", "", "use the core at this address (default: the one on this machine, started if none runs)")
		tokenFile = flag.String("token-file", "", "file holding the API token (default: $"+api.TokenEnv+", else "+home.Token()+")")
		caFile    = flag.String("tls-ca", "", "CA certificate to verify a core on another machine (TUI)")
		fake      = flag.Bool("fake", false, "a core started here rides a synthetic rider (to try osscycler without a trainer)")
		local     = flag.String("addr", api.DefaultAddr, "where the core on this machine listens (and one started here)")
		headless  = flag.Bool("headless", false, "run just the core, no screen (e.g. on a Raspberry Pi by the trainer; screens elsewhere connect with -core)")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: osscycler [flags] [3d | tui [screen flags]]
       osscycler --headless [-fake]
       osscycler core [osscycler-core flags]

Opens a screen with its core: the 3D view if it is installed and there is
a display, else the terminal view (tui), and the core on this machine,
started for as long as the screen is open (one already running here, such
as the service on a Pi, is used: two can't share the ANT+ stick). With
-core HOST:PORT the screen uses that core instead and none is started.
--headless runs just the core, for screens elsewhere; "osscycler core"
does the same with osscycler-core's own flags.

`)
		flag.PrintDefaults()
	}
	// "core" hands over to osscycler-core with its own flags.
	if len(os.Args) > 1 && os.Args[1] == "core" {
		return execProgram("osscycler-core", os.Args[2:])
	}
	flag.Parse()
	if *headless {
		// Owner, 2026-10-10: the easiest start; a screen with its own core,
		// a screen on a remote core (-core), or just the core.
		if *core != "" || flag.NArg() > 0 {
			return errors.New("--headless runs just the core here: no -core, no screen")
		}
		return execProgram("osscycler-core", headlessArgs(*local, *fake))
	}
	ui := ""
	var extra []string // after the screen: its own flags
	if flag.NArg() > 0 {
		ui, extra = flag.Arg(0), flag.Args()[1:]
		if ui != "3d" && ui != "tui" {
			flag.Usage()
			os.Exit(2)
		}
	}

	exe, _ := os.Executable()
	view := findRenderer(exe)
	if ui == "" {
		ui = chooseUI(view != nil, hasDisplay())
	}
	if ui == "3d" && view == nil {
		return errors.New("the 3D view isn't installed here: try \"osscycler tui\"")
	}

	addr := *core
	if addr == "" {
		addr = *local
		if !listening(addr) {
			stop, err := startCore(addr, *fake)
			if err != nil {
				return err
			}
			defer stop()
		}
	} else if *fake {
		return errors.New("-fake is for a core started here, not one at -core")
	}

	var cmd *exec.Cmd
	switch ui {
	case "3d":
		host, _, _ := net.SplitHostPort(addr)
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("the 3D view talks to a core on this machine only for now; for %s use \"osscycler -core %s tui\"", addr, addr)
		}
		args := []string{"--core", addr}
		if *tokenFile != "" {
			args = append(args, "--token-file", *tokenFile)
		}
		cmd = view(append(args, extra...))
	default:
		args := []string{"-addr", addr}
		if *tokenFile != "" {
			args = append(args, "-token-file", *tokenFile)
		}
		if *caFile != "" {
			args = append(args, "-tls-ca", *caFile)
		}
		cmd = exec.Command(sibling("osscycler-tui"), append(args, extra...)...)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// ctrl+c goes to the screen (the TUI reads it as a key, the 3D view
	// closes); this command waits for the screen, then stops its core.
	// Caught rather than ignored: an ignored signal would be the screen's
	// too.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		for s := range sig {
			if s == syscall.SIGTERM { // sent to this command alone: pass it on
				_ = cmd.Process.Signal(s)
			}
		}
	}()
	return cmd.Wait()
}

// headlessArgs are osscycler-core's flags for --headless.
func headlessArgs(addr string, fake bool) []string {
	args := []string{"-addr", addr}
	if fake {
		args = append(args, "-fake")
	}
	return args
}

// chooseUI picks the screen: the 3D view where it is installed and a
// display is there to show it, else the TUI.
func chooseUI(have3D, display bool) string {
	if have3D && display {
		return "3d"
	}
	return "tui"
}

func hasDisplay() bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// findRenderer finds the 3D view for a program at exe: $OSSCYCLER_3D;
// osscycler-3d beside it (an unpacked archive) or in ../lib/osscycler
// (packages); in the source tree, the Godot project run with godot from
// the PATH. nil if there is none.
func findRenderer(exe string) func(args []string) *exec.Cmd {
	// Godot hands a program only the arguments after "--".
	program := func(p string) func([]string) *exec.Cmd {
		return func(args []string) *exec.Cmd { return exec.Command(p, append([]string{"--"}, args...)...) }
	}
	if p := os.Getenv("OSSCYCLER_3D"); p != "" {
		return program(p)
	}
	dir := filepath.Dir(exe)
	for _, p := range []string{filepath.Join(dir, "osscycler-3d"), filepath.Join(dir, "..", "lib", "osscycler", "osscycler-3d")} {
		if isFile(p) {
			return program(p)
		}
	}
	project := filepath.Join(dir, "..", "renderers", "godot")
	if isFile(filepath.Join(project, "project.godot")) {
		if godot, err := exec.LookPath("godot"); err == nil {
			return func(args []string) *exec.Cmd {
				return exec.Command(godot, append([]string{"--path", project, "--"}, args...)...)
			}
		}
	}
	return nil
}

// sibling is a program beside this one, else on the PATH.
func sibling(name string) string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); isFile(p) {
			return p
		}
	}
	return name
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// listening tells whether something answers at addr (a core).
func listening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// startCore starts osscycler-core in the background for this screen,
// logging to ~/osscycler/logs/core.log, and waits for its API. stop asks
// it to finish (it saves the ride being recorded) and waits.
func startCore(addr string, fake bool) (stop func(), err error) {
	logDir := filepath.Join(home.Dir(), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, err
	}
	logPath := filepath.Join(logDir, "core.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	args := []string{"-addr", addr}
	if fake {
		args = append(args, "-fake")
	}
	cmd := exec.Command(sibling("osscycler-core"), args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	// Its own process group: ctrl+c in the terminal is the screen's, not
	// the core's.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("starting the core: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait(); logFile.Close() }()
	deadline := time.Now().Add(15 * time.Second)
	for !listening(addr) {
		select {
		case err := <-exited:
			return nil, fmt.Errorf("the core stopped at once (%v): see %s", err, logPath)
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("the core didn't answer at %s: see %s", addr, logPath)
		}
	}
	return func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(20 * time.Second): // it finishes the ride's file first
			_ = cmd.Process.Kill()
			<-exited
		}
	}, nil
}

// execProgram replaces this process with name (beside it, else on the
// PATH).
func execProgram(name string, args []string) error {
	p := sibling(name)
	if !filepath.IsAbs(p) {
		var err error
		if p, err = exec.LookPath(name); err != nil {
			return err
		}
	}
	return syscall.Exec(p, append([]string{name}, args...), os.Environ())
}
