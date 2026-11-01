package unit_test

import (
	"encoding/json"
	"testing"
	"time"

	"gTSP/src/api"
	"gTSP/src/tools"
)

// TestRunInBackgroundIsNotTaskTimeoutZero 钉住一个容易说错的行为差异：
// `run_in_background:true` 与 `task_timeout:0` **不等价**。
//
// 历史坑：schema 的 run_in_background 描述曾写「Equivalent to task_timeout=0
// with timeout_action='background'」。实测不成立：
//   - task_timeout=0 在 handler 里被改成默认 180s，命令跑完了就直接返回同步结果，
//     根本走不到 timeout_action；
//   - 只有「跑满 timeout 仍未完成」才会提升为后台。
// 于是照着描述写 task_timeout:0 的人会意外阻塞（本测试用 sleep 3 卡 3 秒），
// 而且拿到的是 ExecuteBashResult 不是 process_id。
//
// 本测试用 3 秒的命令把差异压到可接受耗时，断言的是「返回类型」这个稳定特征，
// 不断言精确耗时（CI 机器负载波动大）。
func TestRunInBackgroundIsNotTaskTimeoutZero(t *testing.T) {
	pinTestShell(t)
	session := setupShellTestSession()

	t.Run("task_timeout_zero_blocks_and_returns_sync_result", func(t *testing.T) {
		start := time.Now()
		res, err := tools.ExecuteBashHandler(session,
			json.RawMessage(`{"command":"sleep 3","task_timeout":0,"timeout_action":"background"}`))
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, ok := res.(tools.ExecuteBashResult); !ok {
			t.Fatalf("task_timeout=0 应返回同步结果 ExecuteBashResult，实际 %T", res)
		}
		// 命令 3 秒就结束了，远早于默认 180s 超时，所以一定是同步等待。
		// 阈值取 2s：既证明「等了」，又留足余量给慢机器。
		if elapsed < 2*time.Second {
			t.Errorf("task_timeout=0 应阻塞到命令结束（约 3s），实际只用了 %v", elapsed)
		}
	})

	t.Run("run_in_background_returns_immediately", func(t *testing.T) {
		start := time.Now()
		res, err := tools.ExecuteBashHandler(session,
			json.RawMessage(`{"command":"sleep 3","run_in_background":true}`))
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		bg, ok := res.(tools.BashBackgroundResult)
		if !ok {
			t.Fatalf("run_in_background:true 应返回 BashBackgroundResult，实际 %T", res)
		}
		if bg.ProcessID == "" {
			t.Error("run_in_background:true 应返回非空 process_id")
		}
		// 关键差异：立刻返回，不等 3 秒。
		if elapsed > 2*time.Second {
			t.Errorf("run_in_background:true 应立即返回，实际耗时 %v（更像 task_timeout=0 的行为）", elapsed)
		}

		// 收尾：别把 3 秒的进程留给后面的用例。
		t.Cleanup(func() {
			if bp, found := api.GlobalProcessRegistry.Get(bg.ProcessID); found {
				bp.Kill()
			}
		})
	})
}
