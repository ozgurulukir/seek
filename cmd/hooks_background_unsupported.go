//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package cmd

import "os/exec"

func configureDetachedHookCommand(*exec.Cmd) {}
