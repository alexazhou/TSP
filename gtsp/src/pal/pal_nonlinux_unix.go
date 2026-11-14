//go:build !windows && !linux

package pal

import "syscall"

func setPdeathsig(_ *syscall.SysProcAttr) {}
