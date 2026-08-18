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

// 🎯T477: an already-answered owner *question* must not re-fire as open intent
// after a daemon bounce. T344 closes resume only on product evidence
// (SHA/PASS/achieve); a question answered with an explanation — the live
// incident was "why did jevons-po mint on Opus 5", answered twice with the
// config-precedence explanation — matched none of those markers, so every
// bounce re-injected the same chatlog turn as owner-intent-resume.

func TestT477ExtractAnsweredQuestionDoesNotRefire(t *testing.T) {
	t.Parallel()
	// Live incident shape: owner why-question, later assistant explanation
	// citing knobs/files — no SHA, no PASS, no achieve attestation.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why did jevons-po mint on Opus 5? The config says grok.",
			TS:   time.Date(2026, 8, 15, 10, 12, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Because the grok entry in ~/.jevons/config.yaml loses to a leftover llm-portfolio.json override: the portfolio file takes precedence at mint time, and it still pinned Opus 5. Filed T475 (precedence fix) and T476 (stale portfolio cleanup).",
			TS:   time.Date(2026, 8, 15, 10, 14, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("answered question must not recover as open work, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT477ProgressChatterDoesNotCloseQuestion(t *testing.T) {
	t.Parallel()
	// An in-progress ack is not an answer: the owner is still owed one, and
	// the resume must still fire.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why did jevons-po mint on Opus 5? The config says grok.",
		},
		{
			Role: "assistant",
			Text: "Looking into the mint path now — checking config.yaml.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("question with only progress chatter must still recover, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "Opus 5") {
		t.Fatalf("want the question back, got %q", got.Text)
	}
}

func TestT477DirectiveStillNeedsProductEvidence(t *testing.T) {
	t.Parallel()
	// T477 relaxes closure for questions only. A directive answered with an
	// explanation but no product evidence keeps the T344 contract: recover.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Please switch jevons-po minting back to grok and clean up the override",
		},
		{
			Role: "assistant",
			Text: "Because the grok entry in config.yaml loses to a leftover llm-portfolio.json override, the minting change needs the precedence fix first.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("directive without product evidence must recover, residual=%q", got.Residual)
	}
}

func TestT477ReAskOfAnsweredQuestionStillRecovers(t *testing.T) {
	t.Parallel()
	// A brand-new owner re-ask of the same question is the newest turn with
	// no later answer: it recovers.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why did jevons-po mint on Opus 5?",
			TS:   time.Date(2026, 8, 15, 10, 12, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Because the grok entry in config.yaml loses to a leftover llm-portfolio.json override that pinned Opus 5.",
			TS:   time.Date(2026, 8, 15, 10, 14, 0, 0, time.UTC),
		},
		{
			Role: "user",
			Text: "Why is jevons-po still minting on Opus 5 after that explanation?",
			TS:   time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("fresh re-ask must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(strings.ToLower(got.Text), "still minting") {
		t.Fatalf("want the re-ask text, got %q", got.Text)
	}
}

func TestT477UnrelatedExplanationDoesNotCloseQuestion(t *testing.T) {
	t.Parallel()
	// Topical link is required: an explanation about something else entirely
	// leaves the question open.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why did jevons-po mint on Opus 5?",
		},
		{
			Role: "assistant",
			Text: "Because the watchdog plist carries its own PATH snapshot, the restart script rebuilds bin/detach from committed HEAD before re-execing.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("unrelated explanation must not close the question, residual=%q", got.Residual)
	}
}

// TestT477LoadAnsweredQuestionFromChatlog is the restart-resume half: the
// explanation answer carries no product-evidence markers, so before T477 the
// chatlog loader dropped it and the extractor saw only the question — the
// re-fire happened on the daily path even if extraction were fixed.
func TestT477LoadAnsweredQuestionFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-15T10:12:00Z","message":{"role":"user","content":"Why did jevons-po mint on Opus 5? The config says grok."}}`,
		`{"type":"assistant","timestamp":"2026-08-15T10:14:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Because the grok entry in ~/.jevons/config.yaml loses to a leftover llm-portfolio.json override: the portfolio file takes precedence at mint time, and it still pinned Opus 5."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("answered question must not re-fire from chatlog, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}
