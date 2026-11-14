// Package pal provides a Platform Abstraction Layer for OS-specific operations.
//
// Build tags select the correct implementation per platform:
//   - pal_unix.go          (Linux, macOS, BSD) — uses syscall.Kill with process groups & ps
//   - pal_linux.go         (Linux) — sets Pdeathsig on direct children
//   - pal_nonlinux_unix.go (macOS, BSD) — no-op Pdeathsig
//   - pal_windows.go       (Windows) — uses Job Objects & Toolhelp32 snapshot
//
// Usage:
//
//	handle, err := pal.SetProcessTree(cmd) // before cmd.Start()
//	// cmd.Start()
//	handle.BindStartedProcess(cmd)         // immediately after cmd.Start()
//	// ... later ...
//	pal.KillProcessTree(handle)            // terminate process tree
//	handle.Close()                         // release OS handles
package pal

import "sort"

// ProcessNode represents a node in a leftover process tree snapshot.
type ProcessNode struct {
	PID      int            `json:"pid"`
	PPID     int            `json:"ppid"`
	Command  string         `json:"command"`
	Alive    bool           `json:"alive"`
	Children []*ProcessNode `json:"children"`
}

// LeftoverProcesses represents a snapshot of descendant processes still running
// after the main shell command has exited.
type LeftoverProcesses struct {
	Alive int          `json:"alive"`
	Tree  *ProcessNode `json:"tree"`
}

type procEntry struct {
	pid     int
	ppid    int
	command string
}

// buildProcessTree constructs a LeftoverProcesses snapshot from a flat list of
// surviving descendant processes. Entries whose PPID is not in the surviving
// set (e.g., direct children of the exited shell or orphaned grandchildren)
// are attached directly under the root node.
func buildProcessTree(rootPID, rootPPID int, rootCmd string, entries []procEntry) *LeftoverProcesses {
	root := &ProcessNode{
		PID:      rootPID,
		PPID:     rootPPID,
		Command:  rootCmd,
		Alive:    false,
		Children: make([]*ProcessNode, 0),
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].pid < entries[j].pid
	})

	nodes := make(map[int]*ProcessNode, len(entries))
	for _, e := range entries {
		nodes[e.pid] = &ProcessNode{
			PID:      e.pid,
			PPID:     e.ppid,
			Command:  e.command,
			Alive:    true,
			Children: make([]*ProcessNode, 0),
		}
	}

	for _, e := range entries {
		node := nodes[e.pid]
		if parent, ok := nodes[e.ppid]; ok && e.ppid != e.pid {
			parent.Children = append(parent.Children, node)
		} else {
			root.Children = append(root.Children, node)
		}
	}

	return &LeftoverProcesses{
		Alive: len(entries),
		Tree:  root,
	}
}
