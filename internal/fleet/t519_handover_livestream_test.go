// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

// 🎯T519 — handover seed arrival on a live-stream successor must not be
// decided by a phantom Claude JSONL.
//
// Same surface bug T501 fixed on the mint path, at the handover call site:
// claudia advertises a Claude-shaped JSONLPath for Codex/Grok; watchSeedArrival
// treated Missing(path) as decidable "never begun"; Deliver also returned
// "codex app-server: turn already in flight"; the record stayed pending and
// every T418 sweep ERROR-spammed. T517 exempts the PO; this is the worker half.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

// t519WorkerFixture is a Claude worker with a durable predecessor transcript,
// ready to PrepareMigration onto Codex (live-stream successor).
func t519WorkerFixture(t *testing.T) (*Claudia, *handover.Store) {
	t.Helper()
	dir := t.TempDir()
	sessionID := "019fd13d-e500-7913-b96c-981e50aa2e51"
	claudeProjects := filepath.Join(dir, "claude-projects")
	bucket := filepath.Join(claudeProjects, discovery.EncodeCWDBucket("/work/repo"))
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(bucket, sessionID+".jsonl")
	if err := os.WriteFile(transcript, []byte(`{"type":"user","message":{"role":"user","content":"hello"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t519-w", WorkDir: "/work/repo", SessionID: sessionID,
		Provider: claudia.ProviderClaude, Materialized: true,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}

	store := handover.NewStore(filepath.Join(dir, "handover"))
	f := NewClaudia(reg)
	f.SetSessionRoots(discovery.Roots{ClaudeProjects: claudeProjects})
	f.SetHandoverStore(store)
	return f, store
}

// TestT519CodexPhantomJSONLBusyIsNotNeverBegun: the live specimen — Codex
// successor, JSONLPath at a file the backend never writes, Deliver returns
// turn-already-in-flight. The seed must not be condemned as
// "no transcript was ever created at …jsonl".
func TestT519CodexPhantomJSONLBusyIsNotNeverBegun(t *testing.T) {
	f, store := t519WorkerFixture(t)
	if _, err := f.PrepareMigration("jv-t519-w", claudia.ProviderCodex, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	pending, ok, err := store.Get("jv-t519-w")
	if err != nil || !ok {
		t.Fatalf("no pending record: ok=%v err=%v", ok, err)
	}
	if def := f.reg.Def("jv-t519-w"); def == nil || def.Provider != claudia.ProviderCodex {
		t.Fatalf("successor provider = %v, want codex", def)
	}

	phantom := filepath.Join(t.TempDir(), "projects", "phantom-session.jsonl")
	f.seedTranscript = func(string) string { return phantom }
	f.seedDeliver = func(string, string) (string, error) {
		return "", fmt.Errorf("deliver turn to agent %q: codex app-server: turn already in flight", "jv-t519-w")
	}

	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	f.handOffSeed("jv-t519-w", pending)
	slog.SetDefault(prev)

	if rec, found := sink.find(slog.LevelError, "handover hand-off failed; it stays pending for the next launch"); found {
		var detail string
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				detail = fmt.Sprint(a.Value.Any())
			}
			return true
		})
		if strings.Contains(detail, "no transcript was ever created") {
			t.Fatalf("phantom Claude JSONL decided the seed: err=%q", detail)
		}
		t.Fatalf("turn-already-in-flight on a live-stream successor must not ERROR-spam; err=%q", detail)
	}

	saved, ok, err := store.Get("jv-t519-w")
	if err != nil || !ok {
		t.Fatalf("record lost while deferred: ok=%v err=%v", ok, err)
	}
	if !saved.Usable() {
		t.Fatal("busy live-stream defer must leave the seed pending for the next turn boundary")
	}
}

// TestT519CodexPhantomJSONLSuccessfulDeliverMarksDelivered: undecidable
// transcript + a clean Deliver still clears the pending record (live-stream
// evidence is the reply, not the Claude JSONL).
func TestT519CodexPhantomJSONLSuccessfulDeliverMarksDelivered(t *testing.T) {
	f, store := t519WorkerFixture(t)
	if _, err := f.PrepareMigration("jv-t519-w", claudia.ProviderCodex, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	pending, ok, err := store.Get("jv-t519-w")
	if err != nil || !ok {
		t.Fatalf("no pending record: ok=%v err=%v", ok, err)
	}

	phantom := filepath.Join(t.TempDir(), "projects", "phantom-session.jsonl")
	f.seedTranscript = func(string) string { return phantom }
	f.seedDeliver = func(_, seed string) (string, error) {
		return "seeded; resuming", nil
	}

	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	f.handOffSeed("jv-t519-w", pending)
	slog.SetDefault(prev)

	if _, found := sink.find(slog.LevelError, "handover hand-off failed; it stays pending for the next launch"); found {
		t.Fatal("successful live-stream deliver reported as failed")
	}
	saved, ok, err := store.Get("jv-t519-w")
	if err != nil {
		t.Fatal(err)
	}
	if ok && saved.Usable() {
		t.Fatal("successful deliver left the seed pending-for-retry")
	}
}

// TestT519GrokPhantomJSONLSameRule: Grok is the other live-stream surface.
func TestT519GrokPhantomJSONLSameRule(t *testing.T) {
	f, store := t519WorkerFixture(t)
	if _, err := f.PrepareMigration("jv-t519-w", claudia.ProviderGrok, false); err != nil {
		t.Fatalf("PrepareMigration: %v", err)
	}
	pending, _, err := store.Get("jv-t519-w")
	if err != nil {
		t.Fatal(err)
	}
	phantom := filepath.Join(t.TempDir(), "projects", "phantom-session.jsonl")
	f.seedTranscript = func(string) string { return phantom }
	f.seedDeliver = func(string, string) (string, error) {
		return "", fmt.Errorf("deliver turn to agent %q: grok acp: prompt already in flight", "jv-t519-w")
	}

	sink := &logSink{}
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	f.handOffSeed("jv-t519-w", pending)
	slog.SetDefault(prev)

	if rec, found := sink.find(slog.LevelError, "handover hand-off failed; it stays pending for the next launch"); found {
		var detail string
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "err" {
				detail = fmt.Sprint(a.Value.Any())
			}
			return true
		})
		t.Fatalf("grok live-stream busy must not ERROR; err=%q", detail)
	}
}

func TestT519ProviderKeepsClaudeTranscript(t *testing.T) {
	for provider, durable := range map[claudia.Provider]bool{
		"":                     true,
		claudia.ProviderClaude: true,
		claudia.ProviderCodex:  false,
		claudia.ProviderGrok:   false,
	} {
		if got := providerKeepsClaudeTranscript(provider); got != durable {
			t.Errorf("providerKeepsClaudeTranscript(%q) = %v, want %v", provider, got, durable)
		}
	}
}
