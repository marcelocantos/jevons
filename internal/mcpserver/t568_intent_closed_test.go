// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T568 owner-intent-resume half: a closed owner instruction must not re-fire
// as MANDATORY open work after a bounce. The agent-reply half is
// t568_restart_replay_test.go (landed 1912fa53) — do not weaken it here.
//
// Three hermetic tapes:
//  1. instruction + bare ack → still open (T512:61 stays green)
//  2. instruction + later substantive reply → closed
//  3. instruction + reply naming an achieved TargetID (Cursor-monthly / 🎯T550)

func TestT568InstructionPlusBareAckStillOpen(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Cursor monthly cycle?",
			TS:   time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Looking into the fleet seat now.",
			TS:   time.Date(2026, 8, 26, 10, 1, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("bare ack must not close, residual=%q text=%q", got.Residual, got.Text)
	}
	if !strings.Contains(strings.ToLower(got.Text), "cursor monthly") {
		t.Fatalf("want the instruction back, got %q", got.Text)
	}
}

func TestT568InstructionPlusSubstantiveReplyCloses(t *testing.T) {
	t.Parallel()
	// No SHA / PASS / achieve, no TargetID, no T477 "because" connective —
	// T344/T477/T528 miss this. T568 (a) closes on the later substantive reply.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Please relabel Cursor's plan window as monthly, not weekly.",
			TS:   time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Cursor's included-usage window is a billing-cycle month. The ticker should say monthly, not weekly; unused does not roll over.",
			TS:   time.Date(2026, 8, 26, 10, 2, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("later substantive reply must close, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT568CursorMonthlyAchievedTargetCloses(t *testing.T) {
	t.Parallel()
	// Live incident shape: owner question names no TargetID; overseer files
	// 🎯T550; ledger later shows achieved. T528 does not close (owner text has
	// no id). T344 does not close (reply never says achieved). T568 (b) does.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Cursor monthly cycle?",
			TS:   time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Filed 🎯T550.",
			TS:   time.Date(2026, 8, 26, 12, 5, 0, 0, time.UTC),
		},
	}
	status := map[string]string{"T550": "achieved"}
	got := ExtractOpenOwnerIntentWithLedger(turns, status)
	if got.Recoverable() {
		t.Fatalf("reply naming achieved T550 must close, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
	// Control: same tape without ledger still recovers — short "Filed 🎯T550."
	// is not a substantive reply and T528 sees no owner-named id.
	open := ExtractOpenOwnerIntent(turns)
	if !open.Recoverable() {
		t.Fatalf("without ledger the short T550 filing must still recover, residual=%q", open.Residual)
	}
}

func TestT568LoadCursorMonthlyFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := []byte(`targets:
  T550:
    name: Cursor plan window is labeled monthly, not weekly
    status: achieved
`)
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	chatDir := filepath.Join(state, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-26T12:00:00Z","message":{"role":"user","content":"Cursor monthly cycle?"}}`,
		`{"type":"assistant","timestamp":"2026-08-26T12:05:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Filed 🎯T550."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "jevons.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	closed := LoadOpenOwnerIntentWithLedger(state, "jevons", dir)
	if closed.Recoverable() {
		t.Fatalf("chatlog+ledger T550 must not resume, text=%q residual=%q", closed.Text, closed.Residual)
	}
	if closed.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, closed.Residual)
	}

	// Without the ledger the short filing is kept (TargetID) but does not close.
	open := LoadOpenOwnerIntent(state, "jevons")
	if !open.Recoverable() {
		t.Fatalf("chatlog without ledger must still recover, residual=%q", open.Residual)
	}
}

func TestT568LoadSubstantiveReplyFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-26T10:00:00Z","message":{"role":"user","content":"Please relabel Cursor's plan window as monthly, not weekly."}}`,
		`{"type":"assistant","timestamp":"2026-08-26T10:02:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Cursor's included-usage window is a billing-cycle month. The ticker should say monthly, not weekly; unused does not roll over."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "jevons.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("chatlog substantive reply must close, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}
