package unit_test

import (
	"encoding/json"
	"gTSP/src/api"
	"gTSP/src/pal"
	"gTSP/src/tools"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSyncCommandWithBackgroundDescendantReturnsImmediatelyAndHandsOver(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	start := time.Now()
	res, err := tools.ExecuteBashHandler(session, json.RawMessage(`{
		"command": "sleep 30 >/dev/null 2>&1 & echo spawned",
		"task_timeout": 10
	}`))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ExecuteBashHandler failed: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected immediate return without waiting for descendant, took %v", elapsed)
	}

	result, ok := res.(tools.ExecuteBashResult)
	if !ok {
		t.Fatalf("expected ExecuteBashResult, got %T", res)
	}
	if result.ProcessID == "" {
		t.Fatal("expected non-empty process_id for surviving descendant")
	}
	t.Cleanup(func() {
		_, _ = tools.ProcessStopHandler(session, json.RawMessage(`{"process_id":"`+result.ProcessID+`"}`))
	})

	if result.LeftoverProcesses == nil || result.LeftoverProcesses.Alive < 1 {
		t.Fatalf("expected leftover_processes.alive >= 1, got %+v", result.LeftoverProcesses)
	}
	if result.LeftoverProcesses.Tree == nil || result.LeftoverProcesses.Tree.Alive {
		t.Fatalf("expected root tree node to have alive=false, got %+v", result.LeftoverProcesses.Tree)
	}
	if !strings.Contains(result.Message, "process_stop") {
		t.Errorf("expected handover message to mention process_stop, got %q", result.Message)
	}

	// Verify process_list shows status="leftover"
	listRes, err := tools.ProcessListHandler(session, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ProcessListHandler failed: %v", err)
	}
	list := listRes.(tools.ProcessListResult)
	found := false
	for _, p := range list.Processes {
		if p.ProcessID == result.ProcessID {
			found = true
			if p.Status != "leftover" {
				t.Errorf("expected status=leftover in process_list, got %q", p.Status)
			}
		}
	}
	if !found {
		t.Errorf("expected %q to appear in process_list", result.ProcessID)
	}

	// Verify process_output reports running=true and leftover_processes
	outRes, err := tools.ProcessOutputHandler(session, json.RawMessage(`{"process_id":"`+result.ProcessID+`","block":false}`))
	if err != nil {
		t.Fatalf("ProcessOutputHandler failed: %v", err)
	}
	out := outRes.(tools.ProcessOutputResult)
	if !out.Running {
		t.Error("expected process_output running=true while descendant is alive")
	}
	if out.ExitCode != nil {
		t.Errorf("expected exit_code=nil while running, got %v", *out.ExitCode)
	}
	if out.LeftoverProcesses == nil || out.LeftoverProcesses.Alive < 1 {
		t.Errorf("expected leftover_processes in process_output, got %+v", out.LeftoverProcesses)
	}

	// Verify process_output block=true waits instead of returning immediately
	blockStart := time.Now()
	_, err = tools.ProcessOutputHandler(session, json.RawMessage(`{"process_id":"`+result.ProcessID+`","block":true,"timeout":250}`))
	if err != nil {
		t.Fatalf("ProcessOutputHandler(block=true) failed: %v", err)
	}
	if time.Since(blockStart) < 180*time.Millisecond {
		t.Errorf("expected block=true on leftover entry to wait up to timeout, returned in %v", time.Since(blockStart))
	}

	// Verify process_stop terminates the surviving descendant even though main shell is done
	stopRes, err := tools.ProcessStopHandler(session, json.RawMessage(`{"process_id":"`+result.ProcessID+`"}`))
	if err != nil {
		t.Fatalf("ProcessStopHandler failed: %v", err)
	}
	stop := stopRes.(tools.ProcessStopResult)
	if !stop.Success {
		t.Errorf("expected process_stop success=true, got %+v", stop)
	}

	// After stop, process_output should report running=false
	outAfter, err := tools.ProcessOutputHandler(session, json.RawMessage(`{"process_id":"`+result.ProcessID+`","block":false}`))
	if err != nil {
		t.Fatalf("ProcessOutputHandler after stop failed: %v", err)
	}
	if outAfter.(tools.ProcessOutputResult).Running {
		t.Error("expected running=false after process_stop")
	}
}

func TestSyncOutputCompletenessNearPipeBuffer(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	for i := 0; i < 5; i++ {
		res, err := tools.ExecuteBashHandler(session, json.RawMessage(`{
			"command": "awk 'BEGIN{for(i=0;i<2000;i++) printf \"line-%04d\\n\", i; printf \"END_MARKER\\n\"}'"
		}`))
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		result := res.(tools.ExecuteBashResult)
		// Head+Tail 截断保证「无论是否截断，尾部一定保住」：截断不再是丢失
		// 尾部结果的借口。此前这里写成 `!contains && !truncated` 才失败，等于
		// 一旦 truncated=true 就放过任意丢失 —— 判据形同虚设。
		if !strings.Contains(result.Stdout, "END_MARKER") {
			t.Fatalf("iteration %d lost the tail END_MARKER (truncated=%v, len=%d, tail=%q)",
				i, result.Truncated, len(result.Stdout), tailOf(result.Stdout, 160))
		}
	}
}

func TestProcBufferCapsMemoryAndMarksTruncated(t *testing.T) {
	buf := &api.ProcBuffer{MaxBytes: 64}
	n, err := buf.Write([]byte(strings.Repeat("a", 100)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 100 {
		t.Errorf("expected Write to report full length 100 so io.Copy keeps draining, got %d", n)
	}
	if !buf.Truncated() {
		t.Error("expected Truncated() == true")
	}
	if len(buf.String()) != 64 {
		t.Errorf("expected stored length 64, got %d", len(buf.String()))
	}
}

func TestProcessRegistryReapOnceAndKillSession(t *testing.T) {
	reg := api.NewProcessRegistry()

	// Start a quick command for session A
	cmdA, err := pal.ShellCommand("echo done")
	if err != nil {
		t.Fatalf("ShellCommand failed: %v", err)
	}
	treeA, err := pal.SetProcessTree(cmdA)
	if err != nil {
		t.Fatalf("SetProcessTree failed: %v", err)
	}
	if err := cmdA.Start(); err != nil {
		t.Fatalf("cmdA.Start failed: %v", err)
	}
	_ = treeA.BindStartedProcess(cmdA)
	bpA := reg.NewProcess("proc_a", "echo done", "sess_a", cmdA, treeA, nil, nil, &api.ProcBuffer{}, &api.ProcBuffer{})
	reg.Register(bpA)
	<-bpA.WaitChan()

	// Start a long-running command for session B
	cmdB, err := pal.ShellCommand("sleep 30")
	if err != nil {
		t.Fatalf("ShellCommand failed: %v", err)
	}
	treeB, err := pal.SetProcessTree(cmdB)
	if err != nil {
		t.Fatalf("SetProcessTree failed: %v", err)
	}
	if err := cmdB.Start(); err != nil {
		t.Fatalf("cmdB.Start failed: %v", err)
	}
	_ = treeB.BindStartedProcess(cmdB)
	bpB := reg.NewProcess("proc_b", "sleep 30", "sess_b", cmdB, treeB, nil, nil, &api.ProcBuffer{}, &api.ProcBuffer{})
	reg.Register(bpB)
	t.Cleanup(func() {
		bpB.Kill()
		bpB.Close()
	})

	// Running process B must not be reaped; completed process A is reaped once TTL elapses.
	time.Sleep(20 * time.Millisecond)
	reaped := reg.ReapOnce(10 * time.Millisecond)
	if reaped != 1 {
		t.Errorf("expected 1 reaped process, got %d", reaped)
	}
	if _, ok := reg.Get("proc_a"); ok {
		t.Error("expected proc_a to be removed by ReapOnce")
	}
	if _, ok := reg.Get("proc_b"); !ok {
		t.Error("expected running proc_b to remain in registry")
	}

	// KillSession("sess_b") should terminate proc_b and remove it.
	reg.KillSession("sess_b")
	select {
	case <-bpB.WaitChan():
	case <-time.After(2 * time.Second):
		t.Fatal("expected proc_b to terminate on KillSession")
	}
	if _, ok := reg.Get("proc_b"); ok {
		t.Error("expected proc_b to be removed after KillSession")
	}

	// Ensure unused exec import isn't an issue
	_ = (*exec.Cmd)(nil)
}
