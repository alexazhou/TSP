package api

import (
	"fmt"
	"gTSP/src/pal"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// DefaultProcRingBytes caps per-stream memory in ProcBuffer so a chatty
	// surviving descendant cannot grow gtsp memory without bound.
	//
	// Phase 1 uses a single contiguous ring: [startOffset, totalBytes) where
	// startOffset = max(0, totalBytes - MaxBytes). There is no gap state, so the
	// same read API maps 1:1 onto a later Phase 2 file-backed buffer.
	DefaultProcRingBytes = 512 * 1024

	// DefaultMaxProcBufferBytes is the historical name of DefaultProcRingBytes,
	// kept so existing call sites keep compiling with identical semantics.
	DefaultMaxProcBufferBytes = DefaultProcRingBytes

	// DefaultProcessReaperInterval is how often the registry reaper scans for
	// completed process entries that no longer have surviving descendants.
	DefaultProcessReaperInterval = 5 * time.Minute

	// DefaultCompletedProcessTTL is how long a completed process entry with no
	// surviving descendants is retained in the registry before removal.
	// Adjust as needed; entries with active descendants are never auto-killed
	// or removed by the reaper.
	DefaultCompletedProcessTTL = 30 * time.Minute
)

// ProcBuffer is a thread-safe bounded ring buffer used for process stdout/stderr
// capture. The readable range is always the single contiguous interval
// [startOffset, totalBytes); bytes before startOffset have been overwritten and
// are reported via overflow / evicted / expired signals instead of being kept.
type ProcBuffer struct {
	// MaxBytes overrides DefaultProcRingBytes (used by tests to force wrapping).
	MaxBytes int

	mu         sync.Mutex
	buf        []byte // physical storage: grows to maxBytes, then overwritten in place
	head       int    // physical index of the oldest retained byte once buf is full
	totalBytes int64  // monotonic count of bytes ever written
	closed     bool
}

// BufferStats is the zero-copy status view used by `include_output: false`.
type BufferStats struct {
	TotalBytes  int64 `json:"total_bytes"`
	StartOffset int64 `json:"start_offset"`
	Overflow    bool  `json:"overflow"`
}

// ReadSliceResult is the result of a bounded sequential window read.
type ReadSliceResult struct {
	Data           []byte
	NextBaseOffset int64 // bytes-absolute offset of Data[0] (already rune-aligned)
	TotalBytes     int64
	SkippedBytes   int64 // bytes skipped between the requested offset and Data[0]
	Overflow       bool
	OffsetExpired  bool // requested offset < startOffset (already overwritten)
}

// HeadTailSnapshot is the result of a head+tail window read.
type HeadTailSnapshot struct {
	Full         []byte // set when the whole retained range fit the request
	Head         []byte // set when split
	Tail         []byte // set when split
	Split        bool
	MiddleBytes  int64 // bytes between Head and Tail that were not copied
	EvictedBytes int64 // total bytes evicted before Head/Full (ring start + rune alignment)
	TotalBytes   int64
	Overflow     bool
}

// TailSnapshot is the result of a tail-only window read.
type TailSnapshot struct {
	Data        []byte
	BeforeBytes int64 // total bytes preceding Data[0] (evicted + outside window)
	TotalBytes  int64
	Overflow    bool
}

func (b *ProcBuffer) maxBytes() int {
	if b.MaxBytes > 0 {
		return b.MaxBytes
	}
	return DefaultProcRingBytes
}

// Write never short-writes and never errors: the pipe drain goroutine must keep
// making progress even when the buffer is full or a single write exceeds the
// whole capacity (in that case only the last MaxBytes bytes are kept, and no
// allocation larger than MaxBytes is made).
func (b *ProcBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n = len(p)
	if n == 0 {
		return 0, nil
	}
	maxBytes := b.maxBytes()
	b.totalBytes += int64(n)

	if n >= maxBytes {
		if cap(b.buf) < maxBytes {
			b.buf = make([]byte, maxBytes)
		} else {
			b.buf = b.buf[:maxBytes]
		}
		copy(b.buf, p[n-maxBytes:])
		b.head = 0
		return n, nil
	}

	space := maxBytes - len(b.buf)
	if n <= space {
		// Grow exactly as needed (no append head-room) so the backing array can
		// never exceed MaxBytes through Go's growth strategy.
		if cap(b.buf)-len(b.buf) < n {
			need := len(b.buf) + n
			grown := make([]byte, len(b.buf), need)
			copy(grown, b.buf)
			b.buf = grown
		}
		base := len(b.buf)
		b.buf = b.buf[:base+n]
		copy(b.buf[base:], p)
		return n, nil
	}

	// This write fills and wraps the ring: size the backing array exactly once so
	// append's growth strategy can never allocate beyond MaxBytes.
	if cap(b.buf) < maxBytes {
		exact := make([]byte, len(b.buf), maxBytes)
		copy(exact, b.buf)
		b.buf = exact
	}

	// Fill up to capacity first, then overwrite from head (ring wrap).
	b.buf = append(b.buf, p[:space]...)
	rest := p[space:]
	for len(rest) > 0 {
		w := copy(b.buf[b.head:], rest)
		b.head = (b.head + w) % maxBytes
		rest = rest[w:]
	}
	return n, nil
}

// startOffsetLocked returns the absolute offset of the oldest retained byte.
func (b *ProcBuffer) startOffsetLocked() int64 {
	start := b.totalBytes - int64(len(b.buf))
	if start < 0 {
		return 0
	}
	return start
}

// physOfLocked maps an absolute logical offset to its physical index.
func (b *ProcBuffer) physOfLocked(offset int64) int {
	size := len(b.buf) // > 0 whenever this is called
	idx := (int64(b.head) + (offset - b.startOffsetLocked())) % int64(size)
	if idx < 0 {
		idx += int64(size)
	}
	return int(idx)
}

// copyRangeLocked copies count bytes starting at absolute offset into dst.
func (b *ProcBuffer) copyRangeLocked(dst []byte, offset int64, count int) int {
	size := len(b.buf)
	idx := b.physOfLocked(offset)
	written := 0
	for written < count {
		chunk := size - idx
		if chunk > count-written {
			chunk = count - written
		}
		copy(dst[written:written+chunk], b.buf[idx:idx+chunk])
		written += chunk
		idx = 0
	}
	return written
}

// alignCeilLocked advances offset past UTF-8 continuation bytes so a window
// never starts in the middle of a multi-byte character.
func (b *ProcBuffer) alignCeilLocked(offset int64) int64 {
	for offset < b.totalBytes && len(b.buf) > 0 {
		if b.buf[b.physOfLocked(offset)]&0xC0 != 0x80 {
			break
		}
		offset++
	}
	return offset
}

// trimPartialRuneSuffix drops a trailing incomplete UTF-8 sequence so a window
// never ends in the middle of a character.
func trimPartialRuneSuffix(p []byte) []byte {
	for i := len(p) - 1; i >= 0 && i >= len(p)-utf8.UTFMax; i-- {
		if p[i]&0xC0 == 0x80 {
			continue // continuation byte: walk back to the lead byte
		}
		want := 1
		switch {
		case p[i] < 0x80:
			want = 1
		case p[i]&0xE0 == 0xC0:
			want = 2
		case p[i]&0xF0 == 0xE0:
			want = 3
		case p[i]&0xF8 == 0xF0:
			want = 4
		}
		if len(p)-i < want {
			return p[:i]
		}
		return p
	}
	return p
}

// ReadFrom copies at most maxReadBytes bytes starting at offset.
//
//   - offset >= TotalBytes: Data == nil, NextBaseOffset == offset (never moves
//     the cursor backwards).
//   - offset < StartOffset: offsetExpired, start from StartOffset.
//   - otherwise start from the rune-aligned offset.
//
// SkippedBytes always equals NextBaseOffset - requestedOffset, so callers can
// report "evicted" and "aligned" skips with one number.
func (b *ProcBuffer) ReadFrom(offset int64, maxReadBytes int) ReadSliceResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	start := b.startOffsetLocked()
	res := ReadSliceResult{TotalBytes: b.totalBytes, Overflow: start > 0}

	if offset >= b.totalBytes {
		res.NextBaseOffset = offset
		return res
	}

	requested := offset
	if offset < start {
		res.OffsetExpired = true
		offset = start
	}
	aligned := b.alignCeilLocked(offset)
	res.NextBaseOffset = aligned
	res.SkippedBytes = aligned - requested

	if maxReadBytes <= 0 {
		return res
	}
	n := int(b.totalBytes - aligned)
	if n > maxReadBytes {
		n = maxReadBytes
	}
	if n <= 0 {
		return res
	}
	// Never end a window in the middle of a rune: if the first byte *after* the
	// window is a continuation byte, a character straddles the cut, so extend by
	// up to UTFMax-1 bytes to finish it. Without this, consecutive pages would
	// silently drop the continuation bytes consumed by the next alignCeil, i.e.
	// page concatenation would not be byte-exact.
	extended := n
	if next := aligned + int64(extended); next < b.totalBytes && b.buf[b.physOfLocked(next)]&0xC0 == 0x80 {
		extra := utf8.UTFMax - 1
		if r := int(b.totalBytes - next); r < extra {
			extra = r
		}
		extended += extra
	}
	buf := make([]byte, extended)
	b.copyRangeLocked(buf, aligned, extended)
	if trimmed := trimPartialRuneSuffix(buf); len(trimmed) > 0 && len(trimmed) < len(buf) {
		buf = buf[:len(trimmed)]
	}
	res.Data = buf
	return res
}

// ReadHeadTail copies the leading headMaxBytes and the trailing tailMaxBytes of
// the retained range. It performs no line scanning: policy lives in tools.
func (b *ProcBuffer) ReadHeadTail(headMaxBytes, tailMaxBytes int) HeadTailSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	start := b.startOffsetLocked()
	res := HeadTailSnapshot{TotalBytes: b.totalBytes, Overflow: start > 0}
	alignedStart := b.alignCeilLocked(start)
	res.EvictedBytes = alignedStart

	avail := b.totalBytes - alignedStart
	if avail <= 0 {
		return res
	}

	if avail <= int64(headMaxBytes+tailMaxBytes) {
		res.Full = make([]byte, int(avail))
		b.copyRangeLocked(res.Full, alignedStart, len(res.Full))
		return res
	}

	res.Split = true

	hn := headMaxBytes
	if int64(hn) > avail {
		hn = int(avail)
	}
	if hn > 0 {
		res.Head = make([]byte, hn)
		b.copyRangeLocked(res.Head, alignedStart, hn)
		res.Head = trimPartialRuneSuffix(res.Head)
	}

	tn := tailMaxBytes
	if int64(tn) > avail {
		tn = int(avail)
	}
	if tn > 0 {
		tailStart := b.alignCeilLocked(b.totalBytes - int64(tn))
		res.Tail = make([]byte, int(b.totalBytes-tailStart))
		b.copyRangeLocked(res.Tail, tailStart, len(res.Tail))
	}

	res.MiddleBytes = b.totalBytes - alignedStart - int64(len(res.Head)) - int64(len(res.Tail))
	if res.MiddleBytes < 0 {
		res.MiddleBytes = 0
	}
	return res
}

// ReadTail copies the trailing tailMaxBytes of the retained range.
func (b *ProcBuffer) ReadTail(tailMaxBytes int) TailSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	start := b.startOffsetLocked()
	res := TailSnapshot{TotalBytes: b.totalBytes, Overflow: start > 0}
	if tailMaxBytes <= 0 || b.totalBytes == 0 {
		res.BeforeBytes = b.totalBytes
		return res
	}

	from := b.totalBytes - int64(tailMaxBytes)
	if from < start {
		from = start
	}
	aligned := b.alignCeilLocked(from)
	n := b.totalBytes - aligned
	if n <= 0 {
		res.BeforeBytes = b.totalBytes
		return res
	}
	res.Data = make([]byte, int(n))
	b.copyRangeLocked(res.Data, aligned, len(res.Data))
	res.BeforeBytes = aligned
	return res
}

// Stats returns the status view without copying any output bytes.
func (b *ProcBuffer) Stats() BufferStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	start := b.startOffsetLocked()
	return BufferStats{TotalBytes: b.totalBytes, StartOffset: start, Overflow: start > 0}
}

// Close releases buffer resources. It is idempotent and a no-op in Phase 1;
// Phase 2 will delete the backing temp file here.
func (b *ProcBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

// String materializes the whole retained range.
//
// TEST/DEBUG ONLY: production polling must use ReadFrom/ReadHeadTail/ReadTail/
// Stats so a single call never copies the full 512 KB.
func (b *ProcBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	start := b.startOffsetLocked()
	size := int(b.totalBytes - start)
	if size <= 0 {
		return ""
	}
	out := make([]byte, size)
	b.copyRangeLocked(out, start, size)
	return string(out)
}

// Truncated reports whether any earlier bytes were overwritten by the ring.
func (b *ProcBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startOffsetLocked() > 0
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

// Close releases platform-specific cleanup handles (e.g., Windows Job Object
// handle) and the output buffers. It is idempotent.
func (bp *BackgroundProcess) Close() {
	if bp.tree != nil {
		bp.tree.Close()
	}
	if bp.Stdout != nil {
		bp.Stdout.Close()
	}
	if bp.Stderr != nil {
		bp.Stderr.Close()
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
