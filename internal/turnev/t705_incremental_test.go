// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"os"
	"path/filepath"
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
}

func repeat(line string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}
