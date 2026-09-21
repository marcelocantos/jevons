// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/sendq"
)

func codexLogFixture(t *testing.T, rows ...[3]any) string {
	t.Helper()
	home := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(home, "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`create table logs (id integer primary key autoincrement, ts integer not null,
		ts_nanos integer not null default 0, level text not null default 'DEBUG', target text not null default '',
		feedback_log_body text, module_path text, file text, line integer, thread_id text, process_uuid text,
		estimated_bytes integer not null default 0)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`insert into logs (ts, thread_id, feedback_log_body) values (?, ?, ?)`,
			r[0].(time.Time).Unix(), r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func turnInputRow(payload string) string {
	return `session_loop{thread_id=th}: Submission sub=Submission { id: "x", op: TurnInput { request: ` +
		`TurnInputRequest { input: UserInput { content: [Text { text: "` + rustDebugString(payload) + `" }] } } } }`
}

// held appends texts and drives each to Uncertain, the state a drain leaves
// when its confirmation window expires.
func held(t *testing.T, q *sendq.Store, at time.Time, texts ...string) []sendq.Entry {
	t.Helper()
	for _, text := range texts {
		if _, _, err := q.Append("po", text, at); err != nil {
			t.Fatal(err)
		}
	}
	var out []sendq.Entry
	for range texts {
		e, ok, err := q.ClaimFront("po")
		if err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
		if err := q.Resolve("po", e, sendq.Unverified, "no session event within 45s"); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func TestT768CodexSessionLogConfirmsHeldEntries(t *testing.T) {
	enq := time.Now().Add(-10 * time.Minute)
	delivered := "NUDGE — you are phase=idle.\n\"quoted\" and a \\ backslash\tend"
	unseen := "never reached codex"
	twin := "same text twice"
	home := codexLogFixture(t,
		[3]any{enq.Add(-time.Hour), "th", turnInputRow(twin)},                  // before enqueue: not a receipt
		[3]any{enq.Add(time.Minute), "th", turnInputRow(delivered)},            // the receipt
		[3]any{enq.Add(time.Minute), "other", turnInputRow(unseen)},            // another thread
		[3]any{enq.Add(time.Minute), "th", "tool output mentioning " + unseen}, // not a TurnInput
		[3]any{enq.Add(2 * time.Minute), "th", turnInputRow(twin)},             // one submission of the twin
	)
	q := sendq.NewStore(t.TempDir())
	entries := held(t, q, enq, delivered, unseen, twin, twin)

	ids, err := codexReceipts(q, "po", home, "th", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != entries[0].ID || ids[1] != entries[2].ID {
		t.Fatalf("confirmed %v, want exactly the delivered entry and ONE twin (%s, %s)", ids, entries[0].ID, entries[2].ID)
	}
	left, err := q.Snapshot("po")
	if err != nil || len(left) != 2 {
		t.Fatalf("want the unseen entry and the second twin still held, got %+v %v", left, err)
	}
	for _, e := range left {
		if e.State != sendq.Uncertain {
			t.Fatalf("an entry with no receipt was released: %+v", e)
		}
	}
}

func TestT768NoCodexLogLeavesEntriesHeld(t *testing.T) {
	q := sendq.NewStore(t.TempDir())
	held(t, q, time.Now(), "anything")
	ids, err := codexReceipts(q, "po", t.TempDir(), "th", time.Now())
	if err != nil || len(ids) != 0 {
		t.Fatalf("absence of a log confirmed something: %v %v", ids, err)
	}
}
