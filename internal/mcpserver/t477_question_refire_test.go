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

// 🎯T477: an already-answered owner question must not re-fire as open intent
// after a daemon bounce. An explanation sentence is not that answer
// (🎯T766.4). A declared finish on the same topic is.

func TestT477ExtractAnsweredQuestionDoesNotRefire(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why did jevons-po mint on Opus 5? The config says grok. (T475)",
			TS:   time.Date(2026, 8, 15, 10, 12, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Because the grok entry in ~/.jevons/config.yaml loses to a leftover llm-portfolio.json override: the portfolio file takes precedence at mint time, and it still pinned Opus 5. Filed T475 (precedence fix) and T476 (stale portfolio cleanup).",
			TS:   time.Date(2026, 8, 15, 10, 14, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("an explanation sentence must stay open, residual=%q", got.Residual)
	}
	turns[1].Text = declaredFinishReport("T475", "jevons-po mint config says grok opus")
	got = ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("declared finish on the same question must not recover, text=%q residual=%q", got.Text, got.Residual)
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
	// An explanation sentence does not close a directive. A declared finish
	// on the same complaint does. The helper still refuses a non-question.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Please switch jevons-po minting back to grok and clean up the override (T475)",
		},
		{
			Role: "assistant",
			Text: "Because the grok entry in config.yaml loses to a leftover llm-portfolio.json override, the minting change needs the precedence fix first.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("an explanation sentence must not close the directive, residual=%q", got.Residual)
	}
	if OwnerQuestionAnsweredWithExplanation(turns[0].Text, []string{turns[1].Text}) {
		t.Fatal("T477 helper must still refuse a directive (not a question)")
	}
	turns[1].Text = declaredFinishReport("T475", "switch jevons-po minting back to grok and clean the override")
	got = ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("declared finish on the directive must close, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
	if OwnerQuestionAnsweredWithExplanation(turns[0].Text, []string{turns[1].Text}) {
		t.Fatal("T477 helper must still refuse a directive (not a question)")
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
	// An explanation about something else is not an answer, and a sentence
	// is not a close (🎯T766.4).
	owner := "Why did jevons-po mint on Opus 5?"
	reply := "Because the watchdog plist carries its own PATH snapshot, the restart script rebuilds bin/detach from committed HEAD before re-execing."
	if OwnerQuestionAnsweredWithExplanation(owner, []string{reply}) {
		t.Fatal("T477 helper must still require a topical link")
	}
	got := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Role: "user", Text: owner},
		{Role: "assistant", Text: reply},
	})
	if !got.Recoverable() {
		t.Fatalf("an unrelated explanation sentence must stay open, residual=%q", got.Residual)
	}
}

// TestT477LoadAnsweredQuestionFromChatlog is the restart-resume half.
// An explanation sentence is dropped. A declared finish on the same
// question is retained and closes.
func TestT477LoadAnsweredQuestionFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	user := `{"type":"user","timestamp":"2026-08-15T10:12:00Z","message":{"role":"user","content":"Why did jevons-po mint on Opus 5? The config says grok. (T475)"}}`
	prose := `{"type":"assistant","timestamp":"2026-08-15T10:14:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Because the grok entry in ~/.jevons/config.yaml loses to a leftover llm-portfolio.json override: the portfolio file takes precedence at mint time, and it still pinned Opus 5."}]}}`
	body := strings.Join([]string{user, prose}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if !got.Recoverable() {
		t.Fatalf("an explanation sentence must re-fire from chatlog, residual=%q", got.Residual)
	}
	ts := time.Date(2026, 8, 15, 10, 14, 0, 0, time.UTC)
	finish := t7664AssistantLine(ts, declaredFinishReport("T475", "jevons-po mint config says grok opus"))
	body = strings.Join([]string{user, finish}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got = LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("declared finish must not re-fire from chatlog, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}
