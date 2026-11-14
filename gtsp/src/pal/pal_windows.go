//go:build windows

package pal

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW          = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject   = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject  = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject        = kernel32.NewProc("TerminateJobObject")
	procQueryInformationJobObject = kernel32.NewProc("QueryInformationJobObject")
	procCreateToolhelp32Snapshot  = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW           = kernel32.NewProc("Process32FirstW")
	procProcess32NextW            = kernel32.NewProc("Process32NextW")
)

const (
	jobInfoBasicProcessIdList       = 3
	jobInfoExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000

	processSetQuota  = 0x0100
	processTerminate = 0x0001
	synchronize      = 0x00100000

	th32csSnapProcess = 0x00000002
	errorMoreData     = syscall.Errno(234)
	maxPath           = 260
)

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type jobObjectBasicProcessIdListHeader struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
}

type processEntry32W struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	CntThreads        uint32
	ParentProcessID   uint32
	PriClassBase      int32
	Flags             uint32
	ExeFile           [maxPath]uint16
}

// ProcessTreeHandle tracks a Windows Job Object for tree-level probing and cleanup.
type ProcessTreeHandle struct {
	mu   sync.Mutex
	job  syscall.Handle
	pid  int
	ppid int
}

// SetProcessTree creates a Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so all processes in the tree are
// automatically terminated if the gtsp process exits or crashes.
func SetProcessTree(_ *exec.Cmd) (*ProcessTreeHandle, error) {
	r1, _, err := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return nil, fmt.Errorf("CreateJobObjectW: %w", err)
	}
	job := syscall.Handle(r1)

	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r2, _, err := procSetInformationJobObject.Call(
		uintptr(job),
		uintptr(jobInfoExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r2 == 0 {
		_ = syscall.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}

	return &ProcessTreeHandle{
		job:  job,
		ppid: os.Getpid(),
	}, nil
}

// BindStartedProcess assigns the newly started process to the Job Object.
// Must be called immediately after cmd.Start() returns.
func (h *ProcessTreeHandle) BindStartedProcess(cmd *exec.Cmd) error {
	if h == nil || cmd == nil || cmd.Process == nil {
		return nil
	}
	h.mu.Lock()
	job := h.job
	h.pid = cmd.Process.Pid
	h.mu.Unlock()

	if job == 0 {
		return nil
	}

	procHandle, err := syscall.OpenProcess(processSetQuota|processTerminate|synchronize, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess(%d): %w", cmd.Process.Pid, err)
	}
	defer syscall.CloseHandle(procHandle)

	r1, _, callErr := procAssignProcessToJobObject.Call(uintptr(job), uintptr(procHandle))
	if r1 == 0 {
		// If the process already exited before binding, Wait() will reap it cleanly.
		if waitRes, _ := syscall.WaitForSingleObject(procHandle, 0); waitRes == syscall.WAIT_OBJECT_0 {
			return nil
		}
		_ = cmd.Process.Kill()
		return fmt.Errorf("AssignProcessToJobObject(%d): %w", cmd.Process.Pid, callErr)
	}
	return nil
}

// KillProcessTree terminates all processes in the Job Object.
func KillProcessTree(h *ProcessTreeHandle) {
	if h == nil {
		return
	}
	h.mu.Lock()
	job := h.job
	h.mu.Unlock()
	if job == 0 {
		return
	}
	r1, _, err := procTerminateJobObject.Call(uintptr(job), 1)
	if r1 == 0 {
		fmt.Fprintf(os.Stderr, "TerminateJobObject warning: %v\n", err)
	}
}

// Close releases the underlying Windows Job Object handle. Idempotent.
func (h *ProcessTreeHandle) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	job := h.job
	h.job = 0
	h.mu.Unlock()
	if job != 0 {
		_ = syscall.CloseHandle(job)
	}
}

func (h *ProcessTreeHandle) queryJobPIDs() []int {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	job := h.job
	rootPID := h.pid
	h.mu.Unlock()
	if job == 0 {
		return nil
	}

	headerSize := int(unsafe.Sizeof(jobObjectBasicProcessIdListHeader{}))
	ptrSize := int(unsafe.Sizeof(uintptr(0)))
	capacity := 64

	for attempt := 0; attempt < 4; attempt++ {
		buf := make([]byte, headerSize+capacity*ptrSize)
		var retLen uint32
		r1, _, err := procQueryInformationJobObject.Call(
			uintptr(job),
			uintptr(jobInfoBasicProcessIdList),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&retLen)),
		)
		hdr := (*jobObjectBasicProcessIdListHeader)(unsafe.Pointer(&buf[0]))
		if r1 == 0 {
			if err == errorMoreData && int(hdr.NumberOfAssignedProcesses) > capacity {
				capacity = int(hdr.NumberOfAssignedProcesses) + 16
				continue
			}
			return nil
		}

		count := int(hdr.NumberOfProcessIdsInList)
		pids := make([]int, 0, count)
		for i := 0; i < count; i++ {
			offset := headerSize + i*ptrSize
			if offset+ptrSize > len(buf) {
				break
			}
			pid := int(*(*uintptr)(unsafe.Pointer(&buf[offset])))
			if pid != 0 && pid != rootPID {
				pids = append(pids, pid)
			}
		}
		return pids
	}
	return nil
}

// HasLeftoverProcesses returns true if any descendant process remains in the Job Object.
func (h *ProcessTreeHandle) HasLeftoverProcesses() bool {
	return len(h.queryJobPIDs()) > 0
}

// EnumerateLeftoverProcesses queries the Job Object's active PIDs and enriches
// them with PPID and executable name from CreateToolhelp32Snapshot.
func (h *ProcessTreeHandle) EnumerateLeftoverProcesses(command string) *LeftoverProcesses {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	rootPID := h.pid
	rootPPID := h.ppid
	h.mu.Unlock()

	pids := h.queryJobPIDs()
	if len(pids) == 0 {
		return buildProcessTree(rootPID, rootPPID, command, nil)
	}

	pidSet := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		pidSet[pid] = struct{}{}
	}

	snapInfo := snapshotProcessInfo(pidSet)
	entries := make([]procEntry, 0, len(pids))
	for _, pid := range pids {
		info, ok := snapInfo[pid]
		if !ok {
			entries = append(entries, procEntry{pid: pid})
			continue
		}
		entries = append(entries, procEntry{
			pid:     pid,
			ppid:    info.ppid,
			command: info.exe,
		})
	}
	return buildProcessTree(rootPID, rootPPID, command, entries)
}

type winProcInfo struct {
	ppid int
	exe  string
}

func snapshotProcessInfo(targetPIDs map[int]struct{}) map[int]winProcInfo {
	result := make(map[int]winProcInfo, len(targetPIDs))
	r1, _, _ := procCreateToolhelp32Snapshot.Call(uintptr(th32csSnapProcess), 0)
	snap := syscall.Handle(r1)
	if snap == syscall.InvalidHandle || snap == 0 {
		return result
	}
	defer syscall.CloseHandle(snap)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ok, _, _ := procProcess32FirstW.Call(uintptr(snap), uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		pid := int(entry.ProcessID)
		if _, wanted := targetPIDs[pid]; wanted {
			result[pid] = winProcInfo{
				ppid: int(entry.ParentProcessID),
				exe:  syscall.UTF16ToString(entry.ExeFile[:]),
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcess32NextW.Call(uintptr(snap), uintptr(unsafe.Pointer(&entry)))
	}
	return result
}
