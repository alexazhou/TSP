package api

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// ProcBuffer is a thread-safe bytes.Buffer used for process stdout/stderr capture
type ProcBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *ProcBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *ProcBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// BackgroundProcess tracks a running or completed background process
type BackgroundProcess struct {
	ID        string
	Command   string
	StartedAt time.Time
	cmd       *exec.Cmd
	Stdout    *ProcBuffer
	Stderr    *ProcBuffer
	mu        sync.Mutex
	done      bool
	exitCode  int
	waitChan  chan struct{}
}

// IsDone returns true if the process has exited
func (bp *BackgroundProcess) IsDone() bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.done
}

// GetExitCode returns the process exit code (meaningful only when IsDone() == true)
func (bp *BackgroundProcess) GetExitCode() int {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.exitCode
}

// WaitChan returns a channel that is closed when the process exits
func (bp *BackgroundProcess) WaitChan() <-chan struct{} {
	return bp.waitChan
}

// Kill sends SIGKILL to the process
func (bp *BackgroundProcess) Kill() {
	if bp.cmd != nil && bp.cmd.Process != nil {
		bp.cmd.Process.Kill()
	}
}

// ProcessRegistry manages active background processes
type ProcessRegistry struct {
	mu     sync.Mutex
	procs  map[string]*BackgroundProcess
	nextID uint64
}

// GlobalProcessRegistry is the singleton process registry
var GlobalProcessRegistry = &ProcessRegistry{
	procs: make(map[string]*BackgroundProcess),
}

// GenerateID returns a new unique process ID
func (r *ProcessRegistry) GenerateID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return fmt.Sprintf("proc_%d", r.nextID)
}

// NewProcess wraps a started command in a BackgroundProcess and begins waiting on it.
// The caller must assign cmd.Stdout = stdout and cmd.Stderr = stderr before calling cmd.Start().
func (r *ProcessRegistry) NewProcess(id string, command string, cmd *exec.Cmd, stdout, stderr *ProcBuffer) *BackgroundProcess {
	bp := &BackgroundProcess{
		ID:        id,
		Command:   command,
		StartedAt: time.Now(),
		cmd:       cmd,
		Stdout:    stdout,
		Stderr:    stderr,
		waitChan:  make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		bp.mu.Lock()
		bp.done = true
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

// Register adds a BackgroundProcess to the registry
func (r *ProcessRegistry) Register(bp *BackgroundProcess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.procs[bp.ID] = bp
}

// Get retrieves a BackgroundProcess by ID
func (r *ProcessRegistry) Get(id string) (*BackgroundProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bp, ok := r.procs[id]
	return bp, ok
}

// KillAll terminates all registered background processes
func (r *ProcessRegistry) KillAll() {
	r.mu.Lock()
	procs := make([]*BackgroundProcess, 0, len(r.procs))
	for _, bp := range r.procs {
		procs = append(procs, bp)
	}
	r.mu.Unlock()
	for _, bp := range procs {
		bp.Kill()
	}
}

// List returns all registered background processes
func (r *ProcessRegistry) List() []*BackgroundProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*BackgroundProcess, 0, len(r.procs))
	for _, bp := range r.procs {
		result = append(result, bp)
	}
	return result
}
