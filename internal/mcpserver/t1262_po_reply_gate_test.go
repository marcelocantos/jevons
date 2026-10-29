// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1262: a product owner's routine reply is held from the overseer; its
// answer to an overseer send, and a typed escalation, are delivered.
func TestT1262ProductOwnerRoutineReplyDoesNotWakeTheOverseer(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Materialized: true, Provider: "anthropic"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Materialized: true, Provider: "anthropic", Parent: "jevons", Role: "product-owner"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	inbox := &overseerInbox{}
	s := &Server{registry: reg}
	s.SetOverseerDeliver(inbox.deliver)

	s.notifyTurn("jevons-po", "Acknowledged. Consistent with prior reports; no action needed.", 1)
	if len(inbox.texts) != 0 {
		t.Fatalf("a routine PO reply woke the overseer: %q", inbox.texts)
	}

	s.noteSendForPOReply("jevons", "jevons-po")
	s.notifyTurn("jevons-po", "T1234 is waiting on claudia T231; nothing else is open.", 1)
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], "waiting on claudia T231") {
		t.Fatalf("the PO's answer to an overseer send was not delivered: %q", inbox.texts)
	}
	s.notifyTurn("jevons-po", "Acknowledged again.", 1)
	if len(inbox.texts) != 1 {
		t.Fatalf("one overseer send let two replies through: %q", inbox.texts)
	}

	escalation := "```jevons\njevons: kind escalation\njevons: target T1244\n```\nThe owner must choose between multi-device pairing and replacing the existing pairing."
	s.notifyTurn("jevons-po", escalation, 1)
	if len(inbox.texts) != 2 || !strings.Contains(inbox.texts[1], "multi-device pairing") {
		t.Fatalf("a typed escalation was held: %q", inbox.texts)
	}

	ack := "```jevons\njevons: kind ack\n```\nNoted."
	s.notifyTurn("jevons-po", ack, 1)
	if len(inbox.texts) != 2 {
		t.Fatalf("a typed ack woke the overseer: %q", inbox.texts)
	}
}
