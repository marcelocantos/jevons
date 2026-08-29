// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// t568Payload is long enough for turnev.Needle and distinctive enough
// that an unrelated chatlog line cannot match it.
const t568Payload = "[Agent jevons-po responded]\n" +
	"T561 spawn landed; composer-pocket routing is the remaining ask; " +
	"T564 spawn is hours old and already delivered. Do not treat this as fresh."

func t568Server(t *testing.T, dir string, inbox *overseerInbox) *Server {
	t.Helper()
	s := &Server{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	s.SetRecoverBin("", dir)
	return s
}

// 🎯T568: a delivered-then-restart tape produces zero duplicate notifications.
func TestT568DeliveredThenRestartDoesNotReplay(t *testing.T) {
	dir := t.TempDir()
	first := &overseerInbox{}
	s1 := t568Server(t, dir, first)
	res, err := s1.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("first delivery suppressed: %s", res.Message)
	}
	if len(first.texts) != 1 {
		t.Fatalf("first process inbox=%v", first.texts)
	}

	// New process: empty T428 memory ledger, same state dir.
	second := &overseerInbox{}
	s2 := t568Server(t, dir, second)
	res, err = s2.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("post-restart offer: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("post-restart status=%q want %q: %s", res.Status, StatusSuppressedReplay, res.Message)
	}
	if !strings.Contains(res.Message, "🎯T568") {
		t.Fatalf("suppression must name T568: %q", res.Message)
	}
	if len(second.texts) != 0 {
		t.Fatalf("restart replayed %d copies: %v", len(second.texts), second.texts)
	}
}

// 🎯T568: an undelivered-then-restart tape produces exactly one.
func TestT568UndeliveredThenRestartDeliversOnce(t *testing.T) {
	dir := t.TempDir()
	first := &overseerInbox{err: fmt.Errorf("notify queue closed")}
	s1 := t568Server(t, dir, first)
	if _, err := s1.deliverByName("jevons", t568Payload, OriginAgent, false); err == nil {
		t.Fatal("want the first process's seam to fail")
	}
	if len(first.texts) != 0 {
		t.Fatalf("failed seam still recorded %v", first.texts)
	}

	second := &overseerInbox{}
	s2 := t568Server(t, dir, second)
	res, err := s2.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("undelivered batch stayed suppressed after restart: %s", res.Message)
	}
	if len(second.texts) != 1 {
		t.Fatalf("post-restart inbox=%v want exactly 1", second.texts)
	}
}

// First bounce after deploy: no T417 file yet, but the overseer chatlog
// already carries the user-message inject.
func TestT568JournalAlreadyHoldsBatch(t *testing.T) {
	dir := t.TempDir()
	chat := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chat, 0o755); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": t568Payload,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chat, "jevons.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	inbox := &overseerInbox{}
	s := t568Server(t, dir, inbox)
	res, err := s.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("status=%q want suppressed (journal already holds it): %s", res.Status, res.Message)
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("re-pushed a journaled batch: %v", inbox.texts)
	}
}
