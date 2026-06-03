// Package pal provides a Platform Abstraction Layer for OS-specific operations.
//
// Build tags select the correct implementation per platform:
//   - pal_unix.go   (Linux, macOS, BSD) — uses syscall.Kill with process groups
//   - pal_windows.go (Windows) — uses taskkill /T /F for process tree cleanup
//
// Usage:
//
//	pal.SetProcessGroup(cmd)  // before cmd.Start()
//	// ... later ...
//	pal.KillProcessGroup(pid) // terminate process tree
package pal
