//go:build windows

package agenthooks

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureDetachedHookCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
