// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// t568Payload is long enough for turnev.Needle and distinctive enough
// that an unrelated chatlog line cannot match it.
const t568Payload = envelope.IntentOpen + " " + envelope.IntentRoute + " jevons-po\n" +
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

// A transcript hit does not become durable delivery evidence. After a
// restart the same batch is offered again, because no receipt was written.
func TestT1074OverseerTranscriptDoesNotSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	first := &overseerInbox{}
	s1 := t568Server(t, dir, first)
	res, err := s1.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if res.Status != "delivered_unconfirmed" {
		t.Fatalf("first status=%q want delivered_unconfirmed: %s", res.Status, res.Message)
	}
	if len(first.texts) != 1 {
		t.Fatalf("first process inbox=%v", first.texts)
	}

	second := &overseerInbox{}
	s2 := t568Server(t, dir, second)
	res, err = s2.deliverByName("jevons", t568Payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("post-restart offer: %v", err)
	}
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("transcript evidence suppressed the restart offer: %s", res.Message)
	}
	if len(second.texts) != 1 {
		t.Fatalf("post-restart inbox=%v want exactly 1", second.texts)
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

// A chatlog that already contains the bytes is not a receipt. The offer
// still reaches the seam.
func TestT1074OverseerJournalBytesDoNotConfirm(t *testing.T) {
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
	if res.Status == StatusSuppressedReplay {
		t.Fatalf("journal bytes suppressed the offer: %s", res.Message)
	}
	if res.Status != "delivered_unconfirmed" {
		t.Fatalf("status=%q want delivered_unconfirmed", res.Status)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("inbox=%v want the batch delivered once", inbox.texts)
	}
}
