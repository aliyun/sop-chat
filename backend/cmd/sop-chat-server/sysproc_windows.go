//go:build windows

package main

import "os/exec"

// setSysProcAttr 在 Windows 上为空操作（Windows 无 Setsid 概念）。
func setSysProcAttr(cmd *exec.Cmd) {}
