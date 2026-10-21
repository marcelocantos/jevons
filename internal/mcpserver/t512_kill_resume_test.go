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

// 🎯T512: owner kill/stop/park/reap directives close open-intent resume when
// a later structured fleet-op excerpt names the same seat. A killed or
// parked sentence is not that excerpt (🎯T766.4).

func TestT512ExtractKillProseStaysOpen(t *testing.T) {
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
	if !got.Recoverable() {
		t.Fatalf("kill prose must stay open, residual=%q", got.Residual)
	}
}

func TestT512ExtractStructuredKillCloses(t *testing.T) {
	t.Parallel()
	prose := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Kill jevons-po",
			TS:   time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "fleet-op agent_kill jevons-po",
			TS:   time.Date(2026, 8, 18, 12, 1, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(prose)
	if !got.Recoverable() {
		t.Fatalf("a kill sentence is not a fleet-op directive, residual=%q", got.Residual)
	}
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "fleet-op agent_kill jevons-po",
			TS:   time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "fleet-op agent_kill jevons-po",
			TS:   time.Date(2026, 8, 18, 12, 1, 0, 0, time.UTC),
		},
	}
	got = ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("structured agent_kill must not recover, text=%q residual=%q", got.Text, got.Residual)
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
	if !got.Recoverable() {
		t.Fatalf("Kill Jevons PO prose must stay open, residual=%q", got.Residual)
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

func TestT512ExtractStopParkProseStaysOpen(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "Please stop the `jv-t100-worker` agent"},
		{
			Role: "assistant",
			Text: "Stopped and parked jv-t100-worker — still registered; nothing revives it until fleet intent state=working.",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("stop/park prose must stay open, residual=%q", got.Residual)
	}
}

func TestT512OwnerFleetDirectiveCompletedHelpers(t *testing.T) {
	t.Parallel()
	if OwnerFleetDirectiveCompleted("Kill jevons-po", []string{
		"Killed jevons-po and deregistered it from the fleet.",
	}) {
		t.Fatal("kill prose must not complete the directive")
	}
	if OwnerFleetDirectiveCompleted("Kill jevons-po", []string{
		"fleet-op agent_kill jevons-po",
	}) {
		t.Fatal("a kill sentence is not a fleet-op directive")
	}
	if !OwnerFleetDirectiveCompleted("fleet-op agent_kill jevons-po", []string{
		"fleet-op agent_kill jevons-po",
	}) {
		t.Fatal("structured agent_kill must complete the directive")
	}
	if OwnerFleetDirectiveCompleted("fleet-op agent_kill jevons-po", []string{
		"fleet-op agent_kill jv-t999-other",
	}) {
		t.Fatal("structured kill of another seat must not complete the directive")
	}
	if OwnerFleetDirectiveCompleted("fleet-op agent_kill jevons-po", []string{"working on the kill"}) {
		t.Fatal("progress chatter must not complete a kill directive")
	}
	if OwnerFleetDirectiveCompleted("Please fix the chat wire bug", []string{
		"fleet-op agent_kill jevons-po",
	}) {
		t.Fatal("non-fleet directives must not close on a fleet-op excerpt")
	}
	if OwnerFleetDirectiveCompleted("Kill all workers", []string{
		"fleet-op agent_kill jevons-po",
	}) {
		t.Fatal("nameless mass kill must not pretend a concrete seat closed")
	}
	if OwnerFleetDirectiveCompleted("Please stop the `jv-t100-worker` agent", []string{
		"fleet-op agent_stop jv-t100-worker",
	}) {
		t.Fatal("a stop sentence is not a fleet-op directive")
	}
	if !OwnerFleetDirectiveCompleted("fleet-op agent_stop jv-t100-worker", []string{
		"fleet-op agent_stop jv-t100-worker",
	}) {
		t.Fatal("structured agent_stop must complete the stop directive")
	}
}

// TestT512LoadKillProseStaysOpen is the chatlog half: a killed/deregistered
// sentence is not a fleet-op excerpt, so the kill directive still recovers.
func TestT512LoadKillProseStaysOpen(t *testing.T) {
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
	if !got.Recoverable() {
		t.Fatalf("kill prose must re-fire from chatlog, residual=%q", got.Residual)
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

func TestT512LoadKillSentencePlusToolUseStaysOpen(t *testing.T) {
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
	if !got.Recoverable() {
		t.Fatalf("a kill sentence plus tool_use must stay open, residual=%q", got.Residual)
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
		`{"type":"user","timestamp":"2026-08-18T12:00:00Z","message":{"role":"user","content":"fleet-op agent_kill jevons-po"}}`,
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
