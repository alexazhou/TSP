package api

import (
	"bytes"
	"fmt"
	"gTSP/src/pal"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// DefaultMaxProcBufferBytes caps per-stream memory in ProcBuffer so a chatty
	// surviving descendant cannot grow gtsp memory without bound.
	DefaultMaxProcBufferBytes = 256 * 1024

	// DefaultProcessReaperInterval is how often the registry reaper scans for
	// completed process entries that no longer have surviving descendants.
	DefaultProcessReaperInterval = 5 * time.Minute

	// DefaultCompletedProcessTTL is how long a completed process entry with no
	// surviving descendants is retained in the registry before removal.
	// Adjust as needed; entries with active descendants are never auto-killed
	// or removed by the reaper.
	DefaultCompletedProcessTTL = 30 * time.Minute
)

// ProcBuffer is a thread-safe, bounded bytes.Buffer used for process stdout/stderr capture.
type ProcBuffer struct {
	MaxBytes  int
	mu        sync.Mutex
	buf       bytes.Buffer
	truncated bool
}

func (b *ProcBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	maxBytes := b.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxProcBufferBytes
	}

	avail := maxBytes - b.buf.Len()
	if avail <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > avail {
		_, _ = b.buf.Write(p[:avail])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *ProcBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Truncated returns true if writes were discarded because the buffer reached MaxBytes.
func (b *ProcBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// BackgroundProcess tracks a running or completed background process.
type BackgroundProcess struct {
	ID        string
	Command   string
	SessionID string
	StartedAt time.Time
	cmd       *exec.Cmd
	tree      *pal.ProcessTreeHandle
	Stdout    *ProcBuffer
	Stderr    *ProcBuffer
	mu        sync.Mutex
	done      bool
	doneAt    time.Time
	exitCode  int
	waitChan  chan struct{}
	ioDone    chan struct{}
}

// IsDone returns true if the main shell process has exited.
func (bp *BackgroundProcess) IsDone() bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.done
}

// DoneAt returns the timestamp when the main shell process exited (zero if still running).
func (bp *BackgroundProcess) DoneAt() time.Time {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.doneAt
}

// GetExitCode returns the process exit code (meaningful only when IsDone() == true).
func (bp *BackgroundProcess) GetExitCode() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.exitCode
}

// WaitChan returns a channel that is closed when the main shell process exits.
func (bp *BackgroundProcess) WaitChan() <-chan struct{} {
	return bp.waitChan
}

// WaitIO waits for stdout/stderr copy goroutines to finish (pipe EOF) or until
// timeout elapses. Returns true if I/O completed before timeout.
func (bp *BackgroundProcess) WaitIO(timeout time.Duration) bool {
	if bp.ioDone == nil {
		return true
	}
	select {
	case <-bp.ioDone:
		return true
	default:
	}
	if timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-bp.ioDone:
		return true
	case <-timer.C:
		return false
	}
}

// HasLeftoverProcesses returns true if any descendant process in the process
// group / Job Object is still alive.
func (bp *BackgroundProcess) HasLeftoverProcesses() bool {
	if bp.tree == nil {
		return false
	}
	return bp.tree.HasLeftoverProcesses()
}

// EnumerateLeftoverProcesses returns a snapshot of surviving descendant processes
// rooted at the exited shell command.
func (bp *BackgroundProcess) EnumerateLeftoverProcesses() *pal.LeftoverProcesses {
	if bp.tree == nil {
		return nil
	}
	return bp.tree.EnumerateLeftoverProcesses(bp.Command)
}

// Kill terminates the entire process tree (process group on Unix, Job Object on Windows).
func (bp *BackgroundProcess) Kill() {
	pal.KillProcessTree(bp.tree)
}

// Close releases platform-specific cleanup handles (e.g., Windows Job Object handle).
func (bp *BackgroundProcess) Close() {
	if bp.tree != nil {
		bp.tree.Close()
	}
}

// ProcessRegistry manages active and handed-over background processes.
type ProcessRegistry struct {
	mu     sync.Mutex
	procs  map[string]*BackgroundProcess
	nextID uint64
}

// NewProcessRegistry creates a new empty ProcessRegistry.
func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{
		procs: make(map[string]*BackgroundProcess),
	}
}

// GlobalProcessRegistry is the singleton process registry.
var GlobalProcessRegistry = NewProcessRegistry()

func init() {
	GlobalProcessRegistry.StartReaper(DefaultProcessReaperInterval, DefaultCompletedProcessTTL)
}

// GenerateID returns a new unique process ID.
func (r *ProcessRegistry) GenerateID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return fmt.Sprintf("proc_%d", r.nextID)
}

// NewProcess wraps a started command in a BackgroundProcess, drains outR/errR into
// stdout/stderr in background goroutines, and begins waiting on cmd.Wait().
func (r *ProcessRegistry) NewProcess(
	id, command, sessionID string,
	cmd *exec.Cmd,
	tree *pal.ProcessTreeHandle,
	outR, errR *os.File,
	stdout, stderr *ProcBuffer,
) *BackgroundProcess {
	ioDone := make(chan struct{})
	bp := &BackgroundProcess{
		ID:        id,
		Command:   command,
		SessionID: sessionID,
		StartedAt: time.Now(),
		cmd:       cmd,
		tree:      tree,
		Stdout:    stdout,
		Stderr:    stderr,
		waitChan:  make(chan struct{}),
		ioDone:    ioDone,
	}

	var ioWG sync.WaitGroup
	if outR != nil {
		ioWG.Add(1)
		go func() {
			defer ioWG.Done()
			defer outR.Close()
			_, _ = io.Copy(stdout, outR)
		}()
	}
	if errR != nil {
		ioWG.Add(1)
		go func() {
			defer ioWG.Done()
			defer errR.Close()
			_, _ = io.Copy(stderr, errR)
		}()
	}
	go func() {
		ioWG.Wait()
		close(ioDone)
	}()

	go func() {
		err := cmd.Wait()
		bp.mu.Lock()
		bp.done = true
		bp.doneAt = time.Now()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				bp.exitCode = exitErr.ExitCode()
			}
		}
		bp.mu.Unlock()
		close(bp.waitChan)
	}()

	return bp
}

// Register adds a BackgroundProcess to the registry.
func (r *ProcessRegistry) Register(bp *BackgroundProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[bp.ID] = bp
}

// Get retrieves a BackgroundProcess by ID.
func (r *ProcessRegistry) Get(id string) (*BackgroundProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bp, ok := r.procs[id]
	return bp, ok
}

// KillAll terminates all registered background processes and releases their handles.
func (r *ProcessRegistry) KillAll() {
	r.mu.Lock()
	procs := make([]*BackgroundProcess, 0, len(r.procs))
	for _, bp := range r.procs {
		procs = append(procs, bp)
	}
	r.mu.Unlock()
	for _, bp := range procs {
		bp.Kill()
		bp.Close()
	}
}

// KillSession terminates all registered processes belonging to sessionID,
// releases their handles, and removes them from the registry.
func (r *ProcessRegistry) KillSession(sessionID string) {
	if sessionID == "" {
		return
	}
	r.mu.Lock()
	var procs []*BackgroundProcess
	for id, bp := range r.procs {
		if bp.SessionID == sessionID {
			procs = append(procs, bp)
			delete(r.procs, id)
		}
	}
	r.mu.Unlock()
	for _, bp := range procs {
		bp.Kill()
		bp.Close()
	}
}

// List returns all registered background processes.
func (r *ProcessRegistry) List() []*BackgroundProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*BackgroundProcess, 0, len(r.procs))
	for _, bp := range r.procs {
		result = append(result, bp)
	}
	return result
}

// ReapOnce removes completed entries whose main process exited more than ttl ago
// and that have no surviving descendant processes. Entries with active descendants
// or still-running main processes are never killed or removed.
func (r *ProcessRegistry) ReapOnce(ttl time.Duration) int {
	r.mu.Lock()
	candidates := make([]*BackgroundProcess, 0, len(r.procs))
	for _, bp := range r.procs {
		candidates = append(candidates, bp)
	}
	r.mu.Unlock()

	reaped := 0
	for _, bp := range candidates {
		if !bp.IsDone() {
			continue
		}
		doneAt := bp.DoneAt()
		if doneAt.IsZero() || time.Since(doneAt) <= ttl {
			continue
		}
		if bp.HasLeftoverProcesses() {
			continue
		}

		r.mu.Lock()
		if cur, ok := r.procs[bp.ID]; ok && cur == bp {
			delete(r.procs, bp.ID)
			reaped++
		}
		r.mu.Unlock()
		bp.Close()
	}
	return reaped
}

// StartReaper starts a background goroutine that periodically calls ReapOnce.
func (r *ProcessRegistry) StartReaper(interval, ttl time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			r.ReapOnce(ttl)
		}
	}()
}
