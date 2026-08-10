// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The instrument itself, tested directly, because four callers now depend on
// it and a shared reader that only gets exercised through its callers is one
// whose edges nobody has looked at.

type log struct {
	t    *testing.T
	path string
}

func newLog(t *testing.T) *log {
	t.Helper()
	return &log{t: t, path: filepath.Join(t.TempDir(), "session.jsonl")}
}

func (l *log) write(rec map[string]any) {
	l.t.Helper()
	line, err := json.Marshal(rec)
	if err != nil {
		l.t.Fatal(err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		l.t.Fatal(err)
	}
}

func (l *log) size() (int64, bool) { return Size(l.path) }

const payload = "Continue 🎯T416: all four callers land on the same turn-begin predicate, " +
	"read from the receiver's own transcript. Local master only."

func TestScanReadsHowFarThePayloadGot(t *testing.T) {
	for _, tc := range []struct {
		name string
		recs []map[string]any
		want Fate
	}{{
		name: "authored user message is the strongest answer",
		recs: []map[string]any{{
			"type":    "user",
			"message": map[string]any{"role": "user", "content": payload},
		}},
		want: FateUserMessage,
	}, {
		name: "enqueue alone is queued, not delivered",
		recs: []map[string]any{{
			"type": "queue-operation", "operation": "enqueue", "content": payload,
		}},
		want: FateQueued,
	}, {
		name: "a drained queue entry entered the turn though no user message carries it",
		recs: []map[string]any{
			{"type": "queue-operation", "operation": "enqueue", "content": payload},
			{"type": "queue-operation", "operation": "remove", "content": payload},
			{"attachment": map[string]any{"type": "queued_command", "prompt": payload}},
		},
		want: FateEnteredTurn,
	}, {
		name: "a pane capture quoting the payload is not delivery",
		recs: []map[string]any{{
			"type": "user",
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "content": "$ tmux capture-pane -p\n> " + payload},
			}},
		}},
		want: FateUnseen,
	}, {
		name: "a subagent's own conversation is not the agent we addressed",
		recs: []map[string]any{{
			"type": "user", "isSidechain": true,
			"message": map[string]any{"role": "user", "content": payload},
		}},
		want: FateUnseen,
	}, {
		name: "the agent saying it back is not delivery",
		recs: []map[string]any{{
			"type": "assistant",
			"message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "acknowledged: " + payload},
			}},
		}},
		want: FateUnseen,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLog(t)
			l.write(map[string]any{
				"type":    "user",
				"message": map[string]any{"role": "user", "content": "earlier traffic"},
			})
			baseline, had := l.size()
			for _, rec := range tc.recs {
				l.write(rec)
			}
			if got := Scan(l.path, baseline, had, Needle(payload)); got != tc.want {
				t.Fatalf("fate=%s want %s", got, tc.want)
			}
		})
	}
}

// Clause 1(d), asserted as itself: payload-match at user-message level returns
// ABSENT for a delivery the queue records confirm.
//
// This is pinned rather than merely worked around because the next reader who
// gets ABSENT will otherwise do what the overseer did on 2026-08-10 — reach for
// the hand-flush workaround and press Enter on a message the receiver had
// already taken into its turn, duplicating it. The two instruments are read
// together, and "not at user-message level" is not an answer on its own.
func TestPayloadMatchAloneIsAbsentForADeliveryTheQueueRecordsConfirm(t *testing.T) {
	l := newLog(t)
	l.write(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": "the turn that was already running"},
	})
	baseline, had := l.size()
	l.write(map[string]any{"type": "queue-operation", "operation": "enqueue", "content": payload})
	l.write(map[string]any{"type": "queue-operation", "operation": "remove", "content": payload})
	l.write(map[string]any{"attachment": map[string]any{"type": "queued_command", "prompt": payload}})

	// What clause 1(b) alone would report: user messages only.
	raw, err := os.ReadFile(l.path)
	if err != nil {
		t.Fatal(err)
	}
	needle := Needle(payload)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var rec record
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if text, ok := authoredUserText(rec); ok && strings.Contains(Normalize(text), needle) {
			t.Fatal("fixture has a user message carrying the payload; it is no longer the queued case")
		}
	}

	if got := Scan(l.path, baseline, had, needle); got != FateEnteredTurn {
		t.Fatalf("fate=%s want entered_turn — the queue records are the only evidence there is", got)
	}
}

// The baseline is what stops a message that landed BEFORE this send from
// confirming this one — the same payload delivered twice is two deliveries.
func TestScanIgnoresAnEarlierCopyOfTheSamePayload(t *testing.T) {
	l := newLog(t)
	l.write(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": payload},
	})
	baseline, had := l.size()
	if got := Scan(l.path, baseline, had, Needle(payload)); got != FateUnseen {
		t.Fatalf("fate=%s — a resend was confirmed by the copy that landed before it", got)
	}
}

// A session whose file does not exist has never submitted anything: the
// positive born-stuck test, and the one instrument here that asserts rather
// than fails to observe.
func TestMissingIsOnlyAboutDurableBackends(t *testing.T) {
	l := newLog(t)
	if !Missing(l.path) {
		t.Fatal("a named session with no file on disk must read as born-stuck")
	}
	if Missing("") {
		t.Fatal("a live-stream backend has no transcript to be missing — every Grok send would read born-stuck")
	}
	l.write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "x"}})
	if Missing(l.path) {
		t.Fatal("an existing transcript reported missing")
	}
}

// Needle refuses to identify a message it cannot tell apart, rather than
// matching on noise. A confirmation that fires on "ok" would confirm from
// whatever unrelated message happens to contain it.
func TestNeedleGivesUpOnTextItCannotIdentify(t *testing.T) {
	if Needle("ok") != "" {
		t.Error("a two-letter payload is not identifiable")
	}
	if Needle("  \n\t ") != "" {
		t.Error("whitespace is not identifiable")
	}
	long := strings.Repeat("standing fleet brief. ", 500) + "and the caller's own instruction at the end."
	n := Needle(long)
	if !strings.Contains(n, "the caller's own instruction at the end.") {
		t.Fatalf("the needle dropped the distinguishing tail: %q", n)
	}
	if len([]rune(n)) > needleLen {
		t.Fatalf("needle is %d runes, over the bound", len([]rune(n)))
	}
}
