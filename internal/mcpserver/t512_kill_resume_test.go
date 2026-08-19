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

// 🎯T512: owner kill/stop/park/reap directives must close open-intent resume
// when later ops evidence shows the named seat is gone or fleet intent matches.
// T344 only closes on SHA/PASS/achieve; ops directives have none of those, so
// before this every bounce re-injected "Kill jevons-po" as owner-intent-resume.

func TestT512ExtractKillCompletedDoesNotRefire(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Kill jevons-po",
			TS:   time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Killed and deregistered jevons-po. agent_list no longer shows that seat; fleet intent state=reaped for jevons-po.",
			TS:   time.Date(2026, 8, 18, 12, 1, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("completed kill must not recover as open work, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT512ExtractKillJevonsPOSpelling(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "Kill Jevons PO"},
		{Role: "assistant", Text: "jevons-po killed and deregistered; gone from the registry."},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("Kill Jevons PO with ops evidence must close, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT512ExtractKillWithoutEvidenceStillRecovers(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "Kill jevons-po"},
		{Role: "assistant", Text: "Looking into the fleet seat now."},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("kill with no later ops evidence must still recover, residual=%q", got.Residual)
	}
	if !strings.Contains(strings.ToLower(got.Text), "kill jevons-po") {
		t.Fatalf("want the kill directive back, got %q", got.Text)
	}
}

func TestT512ExtractKillUnrelatedSeatDoesNotClose(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "Kill jevons-po"},
		{Role: "assistant", Text: "Killed and deregistered jv-t999-other; fleet intent state=reaped for jv-t999-other."},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("ops evidence for a different seat must not close, residual=%q", got.Residual)
	}
}

func TestT512ExtractStopParkCompleted(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "Please stop the `jv-t100-worker` agent"},
		{
			Role: "assistant",
			Text: "Stopped and parked jv-t100-worker — still registered; nothing revives it until fleet intent state=working.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("stop/park completion must close, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT512OwnerFleetDirectiveCompletedHelpers(t *testing.T) {
	t.Parallel()
	if !OwnerFleetDirectiveCompleted("Kill jevons-po", []string{
		"Killed jevons-po and deregistered it from the fleet.",
	}) {
		t.Fatal("expected kill+deregister evidence to complete the directive")
	}
	if OwnerFleetDirectiveCompleted("Kill jevons-po", []string{"working on the kill"}) {
		t.Fatal("progress chatter must not complete a kill directive")
	}
	if OwnerFleetDirectiveCompleted("Please fix the chat wire bug", []string{
		"Killed and deregistered jevons-po.",
	}) {
		t.Fatal("non-fleet directives must not close on ops evidence")
	}
	if OwnerFleetDirectiveCompleted("Kill all workers", []string{
		"Killed and deregistered every worker seat.",
	}) {
		t.Fatal("nameless mass kill must not pretend a concrete seat closed")
	}
}

// TestT512LoadKillCompletedFromChatlog is the restart-resume half: kill
// completion carries no SHA/PASS/achieve markers, so before T512 the chatlog
// loader dropped the assistant turn and every bounce re-fired the kill.
func TestT512LoadKillCompletedFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-18T12:00:00Z","message":{"role":"user","content":"Kill jevons-po"}}`,
		`{"type":"assistant","timestamp":"2026-08-18T12:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Killed and deregistered jevons-po. Current agent_list no longer includes that seat; fleet intent state=reaped for jevons-po."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("completed kill must not re-fire from chatlog, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestT512LoadKillWithoutEvidenceStillRecoversFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := `{"type":"user","timestamp":"2026-08-18T12:00:00Z","message":{"role":"user","content":"Kill jevons-po"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if !got.Recoverable() {
		t.Fatalf("open kill with no later evidence must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(strings.ToLower(got.Text), "kill jevons-po") {
		t.Fatalf("got %q", got.Text)
	}
}

func TestT512LoadKillCompletedFromChatlogToolUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-18T12:00:00Z","message":{"role":"user","content":"Kill jevons-po"}}`,
		`{"type":"assistant","timestamp":"2026-08-18T12:01:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"use_tool","input":{"tool_name":"jevonsmcp__jevons_agent_kill","tool_input":{"name":"jevons-po","actor":"jevons"}}}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("agent_kill tool_use must close the directive, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}
