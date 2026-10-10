package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChooseUI(t *testing.T) {
	for _, c := range []struct {
		have3D, display bool
		want            string
	}{{true, true, "3d"}, {true, false, "tui"}, {false, true, "tui"}, {false, false, "tui"}} {
		if got := chooseUI(c.have3D, c.display); got != c.want {
			t.Errorf("3D %v display %v: %s, want %s", c.have3D, c.display, got, c.want)
		}
	}
}

func TestFindRenderer(t *testing.T) {
	t.Setenv("OSSCYCLER_3D", "")
	t.Setenv("PATH", t.TempDir()) // no godot
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0o755)
	exe := filepath.Join(bin, "osscycler")
	if findRenderer(exe) != nil {
		t.Fatal("found a 3D view where there is none")
	}
	// Packages put it in lib/osscycler.
	lib := filepath.Join(dir, "lib", "osscycler")
	os.MkdirAll(lib, 0o755)
	os.WriteFile(filepath.Join(lib, "osscycler-3d"), []byte("#!/bin/sh\n"), 0o755)
	r := findRenderer(exe)
	if r == nil {
		t.Fatal("not found in lib/osscycler")
	}
	if cmd := r([]string{"--core", "127.0.0.1:7420"}); filepath.Base(cmd.Path) != "osscycler-3d" || cmd.Args[1] != "--" || cmd.Args[2] != "--core" {
		t.Errorf("command %v", cmd.Args)
	}
	// Beside the program wins (an unpacked archive), and the environment
	// over both.
	os.WriteFile(filepath.Join(bin, "osscycler-3d"), []byte("#!/bin/sh\n"), 0o755)
	if cmd := findRenderer(exe)(nil); cmd.Path != filepath.Join(bin, "osscycler-3d") {
		t.Errorf("beside: %s", cmd.Path)
	}
	t.Setenv("OSSCYCLER_3D", "/opt/x/osscycler-3d")
	if cmd := findRenderer(exe)(nil); cmd.Path != "/opt/x/osscycler-3d" {
		t.Errorf("environment: %s", cmd.Path)
	}
}

// --headless runs just the core, on this machine's address, fake if asked.
func TestHeadlessArgs(t *testing.T) {
	if got := strings.Join(headlessArgs("127.0.0.1:7420", false), " "); got != "-addr 127.0.0.1:7420" {
		t.Errorf("headless: %s", got)
	}
	if got := strings.Join(headlessArgs("0.0.0.0:7420", true), " "); got != "-addr 0.0.0.0:7420 -fake" {
		t.Errorf("headless -fake: %s", got)
	}
}
