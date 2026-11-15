package api

// White-box tests for the Phase-1 contiguous ring ProcBuffer.
//
// 这里是「分页等价性」的最底层判据：ReadFrom 从 0 连续翻页到 totalBytes，
// 各页按序拼接必须与原始字节流逐字节相等（未溢出），或等于原始流的尾部（溢出）。
// 样本刻意包含多字节 UTF-8、>50 KB 超长单行、以及恰好 512 KB 的容量边界。

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// buildStream 造一段可复现的字节流：ASCII 行 + 多字节 UTF-8 + 超长单行。
func buildStream(seed int64, size int, longLine bool) []byte {
	r := rand.New(rand.NewSource(seed))
	var sb bytes.Buffer
	for sb.Len() < size {
		switch {
		case longLine && sb.Len() == size/3:
			// 一条 >50 KB 的超长单行（不带换行）
			sb.WriteString(strings.Repeat("超", 20000)) // 60000 B
			sb.WriteString("\n")
		case r.Intn(3) == 0:
			sb.WriteString(fmt.Sprintf("行 %d 中文内容 μῆνιν\n", sb.Len()))
		default:
			sb.WriteString(fmt.Sprintf("ascii line %d\n", sb.Len()))
		}
	}
	return sb.Bytes()[:size]
}

// writeInChunks 按随机块长写入，逼出 wrap 路径与跨块边界。
func writeInChunks(t *testing.T, b *ProcBuffer, stream []byte, seed int64, maxChunk int) {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	for off := 0; off < len(stream); {
		n := 1 + r.Intn(maxChunk)
		if off+n > len(stream) {
			n = len(stream) - off
		}
		written, err := b.Write(stream[off : off+n])
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
		if written != n {
			t.Fatalf("Write must always report the full length: got %d, want %d", written, n)
		}
		off += n
	}
}

// TestReadFromPaginationEquivalence 是承重判据：连续翻页拼接 == 原始流。
func TestReadFromPaginationEquivalence(t *testing.T) {
	stream512K := buildStream(7, 512*1024, false)

	cases := []struct {
		name    string
		maxB    int
		stream  []byte
		window  int
		maxChnk int
	}{
		{"utf8_small_buffer_wraps", 4096, buildStream(11, 40000, true), 700, 900},
		{"utf8_default_buffer_no_wrap", 0, buildStream(12, 200000, true), 4096, 8192},
		{"exactly_512k_boundary", 0, stream512K, 33333, 65536},
		{"just_over_512k_wraps", 0, buildStream(13, 512*1024+12345, false), 4096, 8192},
		{"single_line_1MB", 0, bytes.Repeat([]byte("x"), 1024*1024), 4096, 16384},
		{"tiny_window_still_progresses", 8192, buildStream(14, 30000, false), 1, 512},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &ProcBuffer{MaxBytes: tc.maxB}
			writeInChunks(t, b, tc.stream, 99, tc.maxChnk)

			stats := b.Stats()
			if stats.TotalBytes != int64(len(tc.stream)) {
				t.Fatalf("TotalBytes = %d, want %d", stats.TotalBytes, len(tc.stream))
			}

			var collected []byte
			offset := int64(0)
			pages := 0
			for offset < stats.TotalBytes {
				res := b.ReadFrom(offset, tc.window)
				if res.TotalBytes != stats.TotalBytes {
					t.Fatalf("page %d: TotalBytes = %d, want %d", pages, res.TotalBytes, stats.TotalBytes)
				}
				if res.NextBaseOffset < offset {
					t.Fatalf("page %d: cursor moved backwards %d -> %d", pages, offset, res.NextBaseOffset)
				}
				if len(res.Data) == 0 {
					t.Fatalf("page %d: empty page at offset %d of %d (no forward progress)", pages, offset, stats.TotalBytes)
				}
				collected = append(collected, res.Data...)
				offset = res.NextBaseOffset + int64(len(res.Data))
				pages++
				if pages > 2000000 {
					t.Fatalf("pagination did not terminate")
				}
			}

			if offset != stats.TotalBytes {
				t.Fatalf("final cursor = %d, want totalBytes = %d", offset, stats.TotalBytes)
			}
			if !utf8.Valid(collected) {
				t.Errorf("concatenated pages are not valid UTF-8")
			}

			if !stats.Overflow {
				if !bytes.Equal(collected, tc.stream) {
					t.Fatalf("non-overflow concatenation differs from the source stream (got %d B, want %d B)",
						len(collected), len(tc.stream))
				}
			} else {
				tail := stats.StartOffset
				// 首个窗口起点可能被对齐到下一个字符边界，因此允许再多跳 <=3 字节。
				if tail < stats.TotalBytes && tail+4 <= stats.TotalBytes {
					// 找到 collected 在原始流中的起点：它必须落在 [StartOffset, StartOffset+3]
					idx := bytes.Index(tc.stream[tail:], collected)
					if idx < 0 || idx > 3 {
						t.Fatalf("overflow concatenation is not a tail of the source stream (StartOffset=%d, idx=%d)", tail, idx)
					}
				}
				if len(collected) != int(stats.TotalBytes-tail) && len(collected) > int(stats.TotalBytes-tail) {
					t.Fatalf("overflow concatenation longer than retained range: %d > %d", len(collected), stats.TotalBytes-tail)
				}
			}
		})
	}
}

// TestWriteKeepsNewestBytesAndNeverOverAllocates 钉住「写满丢新数据」这条原缺陷。
func TestWriteKeepsNewestBytesAndNeverOverAllocates(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 64}

	if n, err := b.Write([]byte("OLDEST")); err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	writeInChunks(t, b, bytes.Repeat([]byte("a"), 100), 5, 17)
	if n, _ := b.Write([]byte("NEWEST")); n != 6 {
		t.Fatalf("Write must report the full length, got %d", n)
	}

	got := b.String()
	if len(got) != 64 {
		t.Fatalf("retained length = %d, want 64", len(got))
	}
	if !strings.HasSuffix(got, "NEWEST") {
		t.Errorf("the newest bytes must be retained, tail = %q", got[len(got)-16:])
	}
	if strings.Contains(got, "OLDEST") {
		t.Errorf("the oldest bytes must be evicted")
	}
	if !b.Truncated() {
		t.Errorf("Truncated() must be true after eviction")
	}
	if cap(b.buf) > 64 {
		t.Errorf("buffer over-allocated: cap = %d, MaxBytes = 64", cap(b.buf))
	}
}

// TestWriteSingleOversizedChunk 单次写超过容量的块：只保留最后 maxBytes，不按 len(p) 分配。
func TestWriteSingleOversizedChunk(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 32}
	payload := append(bytes.Repeat([]byte("x"), 1000), []byte("TAIL")...)

	n, err := b.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(payload))
	}
	got := b.String()
	if len(got) != 32 {
		t.Fatalf("retained length = %d, want 32", len(got))
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Errorf("oversized write must keep the newest bytes, got tail %q", got)
	}
	if cap(b.buf) > 32 {
		t.Errorf("oversized write over-allocated: cap = %d", cap(b.buf))
	}
	if b.Stats().TotalBytes != int64(len(payload)) {
		t.Errorf("TotalBytes = %d, want %d", b.Stats().TotalBytes, len(payload))
	}
}

// TestRingWrapTableDriven 用表驱动方式逐一验证物理 wrap 后的逻辑顺序。
func TestRingWrapTableDriven(t *testing.T) {
	cases := []struct {
		name     string
		maxB     int
		writes   []string
		wantKeep string
	}{
		{"no_wrap_yet", 16, []string{"abc", "def"}, "abcdef"},
		{"exact_fill", 6, []string{"abc", "def"}, "abcdef"},
		{"wrap_by_one", 6, []string{"abcdef", "g"}, "bcdefg"},
		{"wrap_multiple", 6, []string{"abcdefghij"}, "efghij"},
		{"wrap_across_two_writes", 5, []string{"abc", "de", "fgh"}, "defgh"},
		{"wrap_with_big_then_small", 4, []string{"abcdefgh", "ij", "k"}, "hijk"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &ProcBuffer{MaxBytes: tc.maxB}
			for _, w := range tc.writes {
				if _, err := b.Write([]byte(w)); err != nil {
					t.Fatalf("Write(%q) failed: %v", w, err)
				}
			}
			if got := b.String(); got != tc.wantKeep {
				t.Errorf("retained = %q, want %q", got, tc.wantKeep)
			}
			stats := b.Stats()
			total := 0
			for _, w := range tc.writes {
				total += len(w)
			}
			if stats.TotalBytes != int64(total) {
				t.Errorf("TotalBytes = %d, want %d", stats.TotalBytes, total)
			}
			if wantOverflow := total > tc.maxB; stats.Overflow != wantOverflow {
				t.Errorf("Overflow = %v, want %v", stats.Overflow, wantOverflow)
			}
		})
	}
}

// TestReadFromUTF8Alignment 起点落在 rune 中间时必须跳到下一个字符边界并计入 skippedBytes。
func TestReadFromUTF8Alignment(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 64}
	stream := []byte("a中b") // '中' = E4 B8 AD
	if _, err := b.Write(stream); err != nil {
		t.Fatal(err)
	}

	res := b.ReadFrom(2, 16) // 2 是 '中' 的第二个字节（continuation）
	if res.SkippedBytes != 2 {
		t.Errorf("SkippedBytes = %d, want 2", res.SkippedBytes)
	}
	if res.NextBaseOffset != 4 {
		t.Errorf("NextBaseOffset = %d, want 4", res.NextBaseOffset)
	}
	if string(res.Data) != "b" {
		t.Errorf("Data = %q, want %q", res.Data, "b")
	}
	if !utf8.Valid(res.Data) {
		t.Errorf("window must start on a rune boundary")
	}
}

// TestReadFromOffsetBoundaries 过期跳转 / 越界原样回显。
func TestReadFromOffsetBoundaries(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 8}
	if _, err := b.Write([]byte("0123456789AB")); err != nil { // 12 B into 8 B ring ⇒ start=4
		t.Fatal(err)
	}

	expired := b.ReadFrom(1, 4)
	if !expired.OffsetExpired {
		t.Errorf("offset 1 must be reported as expired (startOffset = 4)")
	}
	if expired.NextBaseOffset != 4 {
		t.Errorf("expired read must start at StartOffset 4, got %d", expired.NextBaseOffset)
	}
	if expired.SkippedBytes != 3 {
		t.Errorf("SkippedBytes = %d, want 3 (4-1)", expired.SkippedBytes)
	}
	if string(expired.Data) != "4567" {
		t.Errorf("Data = %q, want %q", expired.Data, "4567")
	}

	beyond := b.ReadFrom(99, 4)
	if beyond.OffsetExpired {
		t.Errorf("offset beyond TotalBytes must not be reported as expired")
	}
	if beyond.NextBaseOffset != 99 {
		t.Errorf("offset beyond TotalBytes must be echoed back, got %d", beyond.NextBaseOffset)
	}
	if beyond.Data != nil {
		t.Errorf("offset beyond TotalBytes must return no data, got %q", beyond.Data)
	}

	exact := b.ReadFrom(b.Stats().TotalBytes, 4)
	if exact.NextBaseOffset != b.Stats().TotalBytes || exact.Data != nil {
		t.Errorf("offset == TotalBytes must be a no-op echo, got next=%d data=%q", exact.NextBaseOffset, exact.Data)
	}
}

// TestReadHeadTailAndReadTail 覆盖窗口读接口的边界与 UTF-8 安全性。
func TestReadHeadTailAndReadTail(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 8192}
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString(fmt.Sprintf("%03d 中文行\n", i))
	}
	stream := []byte(sb.String())
	if _, err := b.Write(stream); err != nil {
		t.Fatal(err)
	}

	full := b.ReadHeadTail(4096, 4096)
	if full.Split {
		t.Errorf("small stream should not split")
	}
	if !bytes.Equal(full.Full, stream) {
		t.Errorf("Full must equal the retained stream")
	}

	split := b.ReadHeadTail(100, 200)
	if !split.Split {
		t.Fatalf("expected split for head=100 tail=200")
	}
	if len(split.Head) > 100 || len(split.Tail) > 200 {
		t.Errorf("head/tail exceeded requested budgets: %d / %d", len(split.Head), len(split.Tail))
	}
	if !utf8.Valid(split.Head) || !utf8.Valid(split.Tail) {
		t.Errorf("head/tail must not split UTF-8 characters")
	}
	if split.MiddleBytes <= 0 {
		t.Errorf("MiddleBytes must be positive when split, got %d", split.MiddleBytes)
	}
	if !bytes.HasSuffix(split.Tail, []byte("中文行\n")) {
		t.Errorf("tail must end on the stream tail, got %q", split.Tail)
	}

	tail := b.ReadTail(64)
	if len(tail.Data) == 0 || len(tail.Data) > 64 {
		t.Fatalf("ReadTail budget violated: %d bytes", len(tail.Data))
	}
	if !utf8.Valid(tail.Data) {
		t.Errorf("tail window must be valid UTF-8")
	}
	if !bytes.HasSuffix(stream, tail.Data) {
		t.Errorf("tail data must be a suffix of the stream")
	}
	if tail.BeforeBytes != int64(len(stream)-len(tail.Data)) {
		t.Errorf("BeforeBytes = %d, want %d", tail.BeforeBytes, len(stream)-len(tail.Data))
	}
}

// TestProcBufferConcurrentReadWrite 供 `go test -race` 抓数据竞争。
func TestProcBufferConcurrentReadWrite(t *testing.T) {
	b := &ProcBuffer{}
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		chunk := bytes.Repeat([]byte("并发写入 concurrency "), 64)
		for i := 0; i < 2000; i++ {
			if _, err := b.Write(chunk); err != nil {
				t.Errorf("Write failed: %v", err)
				return
			}
		}
		close(stop)
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var off int64
			for {
				select {
				case <-stop:
					return
				default:
				}
				res := b.ReadFrom(off, 1024)
				if len(res.Data) > 0 {
					off = res.NextBaseOffset + int64(len(res.Data))
				}
				_ = b.ReadTail(256)
				_ = b.ReadHeadTail(256, 256)
				_ = b.Stats()
			}
		}(i)
	}
	wg.Wait()
}

// TestProcBufferCloseIdempotent Close 必须幂等（Phase 1 为 no-op 钩子）。
func TestProcBufferCloseIdempotent(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 16}
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	b.Close()
	b.Close()
	if got := b.String(); got != "hello" {
		t.Errorf("Close must not drop data in Phase 1, got %q", got)
	}
	if !b.closed {
		t.Errorf("Close must set the closed flag")
	}
}

// TestRegistryRemovalClosesBuffers 钉住 reaper / KillSession / KillAll 对 Close 的调用。
func TestRegistryRemovalClosesBuffers(t *testing.T) {
	newBP := func(session string) *BackgroundProcess {
		return &BackgroundProcess{
			ID:        "proc_x",
			SessionID: session,
			Stdout:    &ProcBuffer{},
			Stderr:    &ProcBuffer{},
		}
	}

	reg := NewProcessRegistry()
	a := newBP("s1")
	reg.Register(a)
	reg.KillSession("s1")
	if !a.Stdout.closed || !a.Stderr.closed {
		t.Errorf("KillSession must Close the stream buffers")
	}

	reg2 := NewProcessRegistry()
	b := newBP("s2")
	reg2.Register(b)
	reg2.KillAll()
	if !b.Stdout.closed || !b.Stderr.closed {
		t.Errorf("KillAll must Close the stream buffers")
	}

	reg3 := NewProcessRegistry()
	c := NewBPForTest("s3")
	reg3.Register(c)
	c.mu.Lock()
	c.done = true
	c.doneAt = c.StartedAt.Add(-2 * DefaultCompletedProcessTTL)
	c.mu.Unlock()
	if reaped := reg3.ReapOnce(DefaultCompletedProcessTTL); reaped != 1 {
		t.Fatalf("expected 1 reaped entry, got %d", reaped)
	}
	if !c.Stdout.closed || !c.Stderr.closed {
		t.Errorf("reaper must Close the stream buffers of removed entries")
	}
}

// NewBPForTest builds a BackgroundProcess with no OS resources for registry tests.
func NewBPForTest(session string) *BackgroundProcess {
	return &BackgroundProcess{
		ID:        "proc_reap",
		SessionID: session,
		StartedAt: time.Now(),
		Stdout:    &ProcBuffer{},
		Stderr:    &ProcBuffer{},
	}
}

// TestReadFromPageSizeDeviationAtMostThreeBytes 钉住「页大小不是精确值」这条契约：
// 为避免跨页切断 rune，ReadFrom 在页边界做终点对齐，因此单页返回可能比
// maxReadBytes 多或少至多 UTFMax-1 = 3 字节。**任何按 len(page) == window 写判据
// 的代码都是错的** —— 这个上界本身就是契约的一部分。
func TestReadFromPageSizeDeviationAtMostThreeBytes(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 8192}
	// 纯多字节流、没有换行：每个窗口边界都会落在 rune 中间。
	stream := []byte(strings.Repeat("中文行", 4000)) // 9 B * 4000
	if _, err := b.Write(stream); err != nil {
		t.Fatal(err)
	}

	for _, window := range []int{1, 2, 3, 4, 7, 10, 100, 1024} {
		total := b.Stats().TotalBytes
		offset := int64(0)
		// 统计实际发生偏差的页数：若一页都没偏差，本用例就是空判据。
		deviatedPages := 0
		for offset < total {
			res := b.ReadFrom(offset, window)
			if len(res.Data) == 0 {
				t.Fatalf("window=%d: empty page at %d", window, offset)
			}
			last := res.NextBaseOffset+int64(len(res.Data)) >= total
			deviation := len(res.Data) - window
			if deviation < 0 {
				deviation = -deviation
			}
			if deviation > 0 {
				deviatedPages++
			}
			if !last && deviation > utf8.UTFMax-1 {
				t.Fatalf("window=%d offset=%d: page returned %d bytes, deviation %d > %d",
					window, offset, len(res.Data), deviation, utf8.UTFMax-1)
			}
			if !utf8.Valid(res.Data) {
				t.Fatalf("window=%d offset=%d: page is not valid UTF-8", window, offset)
			}
			offset = res.NextBaseOffset + int64(len(res.Data))
		}
		// 这些窗口都非 3 的倍数，必然在某个 rune 中间切过，必须观察到偏差。
		if window != 3 && deviatedPages == 0 {
			t.Fatalf("window=%d: no page deviated from the window size; the contract is not exercised", window)
		}
	}
}

// TestReadHeadTailOverflowEvictedBytesASCII verifies that EvictedBytes includes
// the full overwritten ring prefix (not just rune-alignment bytes) on ASCII data.
func TestReadHeadTailOverflowEvictedBytesASCII(t *testing.T) {
	b := &ProcBuffer{MaxBytes: 64}
	if _, err := b.Write([]byte(strings.Repeat("x", 200))); err != nil {
		t.Fatal(err)
	}

	fast := b.ReadHeadTail(100, 100)
	if !fast.Overflow || fast.Split {
		t.Fatalf("expected Overflow=true, Split=false; got %+v", fast)
	}
	if fast.EvictedBytes != 136 {
		t.Fatalf("fast path EvictedBytes = %d, want 136", fast.EvictedBytes)
	}

	split := b.ReadHeadTail(20, 20)
	if !split.Overflow || !split.Split {
		t.Fatalf("expected Overflow=true, Split=true; got %+v", split)
	}
	if split.EvictedBytes != 136 {
		t.Fatalf("split path EvictedBytes = %d, want 136", split.EvictedBytes)
	}
}
