//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package agenthooks

import (
	"os/exec"
	"syscall"
)

func configureDetachedHookCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
