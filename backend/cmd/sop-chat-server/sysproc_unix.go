//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr 在 Unix 上将子进程放入新的会话（Setsid），
// 使其与父进程的控制终端脱离，成为独立的守护进程。
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
