//go:build !linux

package main

import "os/exec"

// setParentDeath: only Linux has it; elsewhere the view is closed when
// this program ends normally.
func setParentDeath(*exec.Cmd) {}
