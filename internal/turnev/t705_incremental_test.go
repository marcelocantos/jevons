// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// 🎯T705: the incremental classifier must answer exactly what the whole-file
// one answers, or it trades correct readings for CPU — a bad bargain for the
// loop that decides whether a seat is stuck.

const (
	t705Enqueue  = `{"type":"queue-operation","operation":"enqueue","content":"work"}`
	t705Drain    = `{"type":"queue-operation","operation":"remove","content":"work"}`
	t705Working  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`
	t705Terminal = `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`
)

func t705Write(t *testing.T, path string, lines []string, trailingNewline bool) {
	t.Helper()
	body := strings.Join(lines, "\n")
	if trailingNewline && len(lines) > 0 {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// Filesystem mtime has coarse granularity; make each write distinct so
	// the cache's append check sees forward motion rather than a tie.
	future := time.Now().Add(time.Duration(len(body)) * time.Millisecond)
	_ = os.Chtimes(path, future, future)
}

// The property that matters: whatever sequence of appends happens, the
// incremental reading equals a cold full read of the same bytes.
func TestT705IncrementalMatchesFullRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps [][]string
	}{
		{"queue opened then drained", [][]string{
			{t705Enqueue},
			{t705Enqueue, t705Working},
			{t705Enqueue, t705Working, t705Drain, t705Terminal},
		}},
		{"terminal then reopened", [][]string{
			{t705Terminal},
			{t705Terminal, t705Enqueue},
			{t705Terminal, t705Enqueue, t705Drain},
		}},
		{"pending survives a long tail", [][]string{
			{t705Enqueue},
			append([]string{t705Enqueue}, repeat(t705Terminal, 50)...),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "updates.jsonl")
			for i, lines := range tc.steps {
				t705Write(t, path, lines, true)

				incremental := ClassifyPhaseFile(path) // resumes the fold
				ResetPhaseCache()
				full := ClassifyPhaseFile(path) // cold, whole file

				if incremental != full {
					t.Fatalf("step %d: incremental=%s full=%s — the cache changed the answer",
						i, incremental, full)
				}
				// Put the cache back where the next step expects it.
				_ = ClassifyPhaseFile(path)
			}
		})
	}
}

// An undrained enqueue from the start of a long tape must still read as
// working. This is the case a tail-window implementation gets wrong, and it
// is the state the fleet most needs to see.
func TestT705PendingFromLongAgoStillReadsWorking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	t705Write(t, path, []string{t705Enqueue}, true)
	if got := ClassifyPhaseFile(path); got != PhaseWorking {
		t.Fatalf("opening reading = %s, want working", got)
	}
	// Thousands of later events, none of them draining that queue.
	t705Write(t, path, append([]string{t705Enqueue}, repeat(t705Terminal, 2000)...), true)
	if got := ClassifyPhaseFile(path); got != PhaseWorking {
		t.Fatalf("after 2000 appended events = %s, want working: the pending enqueue was forgotten", got)
	}
}

// A writer caught mid-line must not have that line counted twice when it
// completes — that would leave `pending` permanently wrong.
func TestT705PartialLineIsNotCountedTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	t705Write(t, path, []string{t705Terminal}, true)
	if got := ClassifyPhaseFile(path); got != PhaseIdle {
		t.Fatalf("baseline = %s, want idle", got)
	}
	// An enqueue lands without its newline yet.
	t705Write(t, path, []string{t705Terminal, t705Enqueue}, false)
	if got := ClassifyPhaseFile(path); got != PhaseWorking {
		t.Fatalf("partial enqueue = %s, want working: a just-written event must be visible", got)
	}
	// The newline arrives, then a drain. The enqueue must be counted once:
	// if the partial line were also folded into the cache, `pending` would
	// stay at one forever and the seat would read working for the rest of
	// its life. Compare against a cold read, which cannot double count.
	t705Write(t, path, []string{t705Terminal, t705Enqueue}, true)
	_ = ClassifyPhaseFile(path)
	t705Write(t, path, []string{t705Terminal, t705Enqueue, t705Drain}, true)
	incremental := ClassifyPhaseFile(path)

	phaseCacheMu.Lock()
	pending := phaseCache[path].fold.pending
	phaseCacheMu.Unlock()
	if pending != 0 {
		t.Fatalf("cached pending = %d after one enqueue and one drain, want 0", pending)
	}

	ResetPhaseCache()
	if full := ClassifyPhaseFile(path); full != incremental {
		t.Fatalf("incremental=%s full=%s — the partial line changed the answer", incremental, full)
	}
}

// A rewritten or rotated file is not an append, and resuming into it would
// read the new bytes against the old fold.
func TestT705ShrunkFileIsReadAfresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	t705Write(t, path, append([]string{t705Enqueue}, repeat(t705Working, 20)...), true)
	if got := ClassifyPhaseFile(path); got != PhaseWorking {
		t.Fatalf("baseline = %s, want working", got)
	}
	t705Write(t, path, []string{t705Terminal}, true)
	if got := ClassifyPhaseFile(path); got != PhaseIdle {
		t.Fatalf("after truncation = %s, want idle: the stale fold survived a rewrite", got)
	}
}

// The point of the exercise: a file that has not changed costs a stat, not
// a parse.
func TestT705UnchangedFileIsNotReparsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	t705Write(t, path, append([]string{t705Enqueue}, repeat(t705Working, 500)...), true)
	first := ClassifyPhaseFile(path)

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	phaseCacheMu.Lock()
	entry, cached := phaseCache[path]
	phaseCacheMu.Unlock()
	if !cached {
		t.Fatal("nothing cached after a read")
	}
	if entry.offset != st.Size() {
		t.Fatalf("cached offset %d, file %d — a complete file should be fully consumed",
			entry.offset, st.Size())
	}
	if again := ClassifyPhaseFile(path); again != first {
		t.Fatalf("second read = %s, first = %s", again, first)
	}

	// An unchanged file must not be opened: mode 000 still stats, but a
	// parse would fail open and collapse to unknown.
	if os.Geteuid() == 0 {
		t.Log("root can open mode-000 files; skipping the stat-not-parse chmod check")
		return
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o600)
	if got := ClassifyPhaseFile(path); got != first {
		t.Fatalf("unchanged unreadable file = %s, want %s: the second call parsed instead of statting",
			got, first)
	}
}

// A tape whose mtime went backwards is a rotation wearing the same name.
// Size going forward is not enough to resume: the prefix is a different tape.
func TestT705MtimeBackwardsIsReadAfresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	t705Write(t, path, append([]string{t705Enqueue}, repeat(t705Working, 20)...), true)
	if got := ClassifyPhaseFile(path); got != PhaseWorking {
		t.Fatalf("baseline = %s, want working", got)
	}

	// Larger than the cached offset so a shrink check cannot save us, and
	// stamped in the past so an append-only resume would keep the stale
	// pending count and still read working.
	body := strings.Join(repeat(t705Terminal, 40), "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if got := ClassifyPhaseFile(path); got != PhaseIdle {
		t.Fatalf("mtime-backwards rewrite = %s, want idle: the stale fold survived a rotated tape", got)
	}
}

// 🎯T705 last clause: a multi-megabyte tape's repeat classification is
// orders of magnitude cheaper than the first, and both agree. Synthetic
// is the hermetic ratchet. A live transcript is used when T705_TRANSCRIPT
// points at one (the closer's measurement), never committed as testdata.
func TestT705MultiMegabyteRepeatIsCheapAndAgrees(t *testing.T) {
	t.Run("synthetic", func(t *testing.T) {
		t705AssertCheapRepeat(t, t705MegabyteTape(t, 3<<20))
	})
	t.Run("live", func(t *testing.T) {
		src := strings.TrimSpace(os.Getenv("T705_TRANSCRIPT"))
		if src == "" {
			t.Skip("set T705_TRANSCRIPT to a real multi-megabyte JSONL to measure the live tape")
		}
		st, err := os.Stat(src)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() < 1<<20 {
			t.Fatalf("%s is %d bytes; want a multi-megabyte transcript", src, st.Size())
		}
		t705AssertCheapRepeat(t, t705CloneFile(t, src))
	})
}

func t705AssertCheapRepeat(t *testing.T, path string) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ResetPhaseCache()

	t0 := time.Now()
	cold := ClassifyPhaseFile(path)
	coldDur := time.Since(t0)
	if cold == PhaseUnknown {
		t.Fatal("cold classify returned unknown on a readable tape")
	}

	warm := make([]time.Duration, 11)
	var again Phase
	for i := range warm {
		t1 := time.Now()
		again = ClassifyPhaseFile(path)
		warm[i] = time.Since(t1)
		if again != cold {
			t.Fatalf("repeat %d = %s, cold = %s", i, again, cold)
		}
	}
	warmDur := t705MedianDuration(warm)
	t.Logf("%d bytes: cold %s (%s) unchanged median %s (%s) ratio %.0fx",
		st.Size(), coldDur, cold, warmDur, again, float64(coldDur)/float64(warmDur+1))

	if coldDur < 500*time.Microsecond {
		t.Fatalf("cold classify of %d bytes took %s — too small to measure", st.Size(), coldDur)
	}
	// Orders of magnitude: a 10× floor is the weakest reading of the
	// clause; the 3.6 MB live tape measured ~18,000× in the landing commit.
	if warmDur*10 >= coldDur {
		t.Fatalf("unchanged %s is not orders of magnitude cheaper than cold %s", warmDur, coldDur)
	}

	// Appending one line must not re-pay the whole tape. If the fold
	// restarted, this classify would cost about as much as cold.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(f, t705Working+"\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t2 := time.Now()
	appended := ClassifyPhaseFile(path)
	appendDur := time.Since(t2)
	if appended != cold && appended != PhaseWorking {
		t.Fatalf("after one-line append = %s, cold = %s", appended, cold)
	}
	if appendDur*5 >= coldDur {
		t.Fatalf("one-line append %s re-paid the tape (cold %s)", appendDur, coldDur)
	}

	ResetPhaseCache()
	full := ClassifyPhaseFile(path)
	if full != appended {
		t.Fatalf("resumed fold %s != cold whole-file %s after the append", appended, full)
	}
}

func t705MegabyteTape(t *testing.T, minBytes int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "updates.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pad := strings.Repeat("a", 2048)
	bulky := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"cmd":"` + pad + `"}}]}}` + "\n"
	if _, err := io.WriteString(f, t705Enqueue+"\n"); err != nil {
		t.Fatal(err)
	}
	written := len(t705Enqueue) + 1
	for written < minBytes {
		n, err := io.WriteString(f, bulky)
		if err != nil {
			t.Fatal(err)
		}
		written += n
	}
	return path
}

func t705CloneFile(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "updates.jsonl")
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func t705MedianDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), ds...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func repeat(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}
