package main

import (
	"os/exec"
	"syscall"
)

// setParentDeath has the view closed with this program, however it ends
// (the assistant's session closing kills it without a word).
func setParentDeath(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
