// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

func t1054Finish() string {
	return envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T1054", SHA: "aabbccddeeff", Verdict: envelope.VerdictGreen, Status: envelope.ProgressLanded, SilentLedger: envelope.SilentLedgerEmpty})
}

func TestT1054OnlyParentDeliveredMetadataStaysOutOfOwnerChat(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = t1054Registry(t, "jevons-po")
	s.SetAgentReportDir(t.TempDir())
	report := t1054Finish()
	if !s.routineMetadataReport("worker", report) {
		t.Fatalf("metadata report not classified: %q", report)
	}
	s.notify("worker", report)
	if len(po.sent) != 1 || len(inbox.texts) != 0 {
		t.Fatalf("parent=%v owner=%v", po.sent, inbox.texts)
	}
	if stored, err := agentreport.Latest(s.agentReportStateDir(), "worker"); err != nil || stored.Text != report {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	// A metadata-only scout also goes to the parent, not owner.
	scout := envelope.Format(&envelope.Message{Kind: envelope.KindScoutReport, Target: "T1054", Phase: envelope.PhaseScout, SilentLedger: envelope.SilentLedgerEmpty})
	s.notify("worker", scout)
	// Duplicate report-id suppression on the parent path cannot license owner suppression.
	if len(po.sent) != 1 || len(inbox.texts) != 1 {
		t.Fatalf("scout parent=%v owner=%v", po.sent, inbox.texts)
	}
}

func TestT1054UnexpectedProseAlwaysReachesOwner(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = t1054Registry(t, "jevons-po")
	s.SetAgentReportDir(t.TempDir())
	base := t1054Finish()
	cases := []string{
		base + "\nUnusual safety incident: poisoned process unexpectedly restarted.",
		base + "\nThe owner must decide whether to continue.",
		strings.Replace(base, "silent-ledger none", "silent-ledger ranked\njevons: silent-decision confidence=0.3 choice=hold why=unexpected", 1),
		strings.Replace(base, "verdict GREEN", "verdict SUSPECT", 1),
		strings.Replace(base, "status landed", "status blocked\njevons: blocker owner-decision", 1),
		strings.Replace(base, "kind finish-report", "kind escalation", 1),
		strings.Replace(base, "silent-ledger none", "silent-ledger none\njevons: unknown-field anomaly", 1),
		"Unexpected safety incident!\n" + base,
		"Done GREEN but something went wrong.",
	}
	for i, report := range cases {
		if s.routineMetadataReport("worker", report) {
			t.Fatalf("case %d wrongly classified routine", i)
		}
		n := len(inbox.texts)
		s.notify("worker", report)
		if len(inbox.texts) != n+1 {
			t.Fatalf("case %d lost owner message", i)
		}
	}
}

func TestT1054ParentUnavailableAndOverseerParentPreserveDelivery(t *testing.T) {
	for _, parent := range []string{"jevons-po", "jevons"} {
		s, inbox := chainServer(t, nil)
		s.registry = t1054Registry(t, parent)
		s.SetAgentReportDir(t.TempDir())
		s.notify("worker", t1054Finish())
		if len(inbox.texts) != 1 {
			t.Fatalf("parent=%s owner=%v", parent, inbox.texts)
		}
	}

}

func t1054Registry(t *testing.T, parent string) *claudia.Registry {
	t.Helper()
	reg := newLineageRegistry(t, map[string]string{"worker": parent, "jevons-po": "jevons"})
	if err := reg.Remove("worker"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "worker", SessionID: uuid.NewString(), Parent: parent, Purpose: claudia.PurposeWork, WorkDir: "/work/worker"}); err != nil {
		t.Fatal(err)
	}
	return reg
}
