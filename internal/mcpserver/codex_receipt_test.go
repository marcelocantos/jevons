// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
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

func TestT772UncertainResolveChecksReceiptImmediately(t *testing.T) {
	// 🎯T772: when an entry first becomes Uncertain, the codex session log is
	// checked at once — not deferred to the next drain (which may never come
	// on an idle seat, and by then codex may have pruned the TurnInput row).
	s, _ := t401Server(t)
	sid := "th-t772-immediate"
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	if err := s.registry.Register(claudia.AgentDef{
		Name: "codex-po", WorkDir: t.TempDir(), SessionID: sid,
		Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	enq := time.Now().Add(-2 * time.Minute)
	payload := "held brief that codex already accepted"
	home := filepath.Join(state, "claudia", "codex-homes", sid)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// Build the log under the exclusive home confirmCodexReceipts will open.
	_ = codexLogFixtureAt(t, home,
		[3]any{enq.Add(time.Minute), sid, turnInputRow(payload)},
	)
	q := s.sendQueue()
	if _, _, err := q.Append("codex-po", payload, enq); err != nil {
		t.Fatal(err)
	}
	e, ok, err := q.ClaimFront("codex-po")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := q.Resolve("codex-po", e, sendq.Unverified, "no session event within 45s"); err != nil {
		t.Fatal(err)
	}
	// Product hook after first Uncertain resolve (drain path):
	if n := s.confirmCodexReceipts("codex-po"); n != 1 {
		t.Fatalf("immediate check settled %d, want 1", n)
	}
	left, err := q.Snapshot("codex-po")
	if err != nil || len(left) != 0 {
		t.Fatalf("want empty queue after immediate receipt check, got %+v err=%v", left, err)
	}
}

func TestT772SweepSettlesCodexHeldWithoutDrain(t *testing.T) {
	// 🎯T772: SweepSendBacklogs re-checks held codex entries on its existing
	// cadence so an idle seat does not lose receipts waiting for a drain.
	s, _ := t401Server(t)
	sid := "th-t772-sweep"
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	if err := s.registry.Register(claudia.AgentDef{
		Name: "codex-idle", WorkDir: t.TempDir(), SessionID: sid,
		Provider: claudia.ProviderCodex, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	enq := time.Now().Add(-5 * time.Minute)
	payload := "still held while the seat stays idle"
	home := filepath.Join(state, "claudia", "codex-homes", sid)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = codexLogFixtureAt(t, home,
		[3]any{enq.Add(30 * time.Second), sid, turnInputRow(payload)},
	)
	q := s.sendQueue()
	if _, _, err := q.Append("codex-idle", payload, enq); err != nil {
		t.Fatal(err)
	}
	e, ok, err := q.ClaimFront("codex-idle")
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if err := q.Resolve("codex-idle", e, sendq.Unverified, "no session event within 45s"); err != nil {
		t.Fatal(err)
	}
	// No drain, no turn boundary — only the backlog sweep.
	s.SweepSendBacklogs()
	left, err := q.Snapshot("codex-idle")
	if err != nil || len(left) != 0 {
		t.Fatalf("sweep should settle the held codex entry from the session log; got %+v err=%v", left, err)
	}
}

// codexLogFixtureAt writes logs_2.sqlite under an existing exclusive home.
func codexLogFixtureAt(t *testing.T, home string, rows ...[3]any) string {
	t.Helper()
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
