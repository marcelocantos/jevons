// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

func TestT1054RoutineCompletionAdmittedToParentNotOwner(t *testing.T) {
	po := &fakeSender{alive: true}
	s, owner := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
	dir := t.TempDir()
	s.SetAgentReportDir(dir)
	routine := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T1054", Verdict: envelope.VerdictGreen, Status: envelope.ProgressLanded, SHA: "abcdef0123456", SilentLedger: envelope.SilentLedgerEmpty, Payload: "Implementation landed. Named test green."})
	if !routineWorkerCompletion(routine) {
		t.Fatal("fixture not classified routine")
	}
	s.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: routine, StopReason: "end_turn"})
	if len(owner.texts) != 0 {
		t.Fatalf("routine painted in owner stream: %v", owner.texts)
	}
	if len(po.sent) != 1 {
		t.Fatalf("parent did not receive routine report: %v", po.sent)
	}
	if rec, err := agentreport.Latest(dir, "worker"); err != nil || rec.Text != routine {
		t.Fatalf("durable report=%+v err=%v", rec, err)
	}

	// A decision request, failure, direct PO answer and unknown-timeout
	// escalation all keep their route to the owner. No prose can turn a
	// direct owner request into a suppressible worker report.
	cases := []string{
		envelope.Format(&envelope.Message{Kind: envelope.KindEscalation, Target: "T1054", Payload: "Owner decision required: choose trust root."}),
		strings.Replace(routine, "jevons: verdict GREEN", "jevons: verdict RED", 1),
		strings.Replace(routine, "Named test green.", "Unknown timeout: cannot determine whether the worker completed.", 1),
	}
	for _, report := range cases {
		s.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})
	}
	s.agentEventSink("jevons-po")(claudia.Event{Type: "assistant", Text: "The owner requested this status: here is the direct answer.", StopReason: "end_turn"})
	if len(owner.texts) != 4 {
		t.Fatalf("decision/error/timeout/direct answer not delivered: %v", owner.texts)
	}
}

func TestT1054UncertainOrUndurableReportFailsOpen(t *testing.T) {
	routine := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T1054", Verdict: envelope.VerdictGreen, Status: envelope.ProgressLanded, SHA: "abcdef0123456", SilentLedger: envelope.SilentLedgerEmpty, Payload: "Named test green."})
	for _, text := range []string{"Unenveloped report complete", strings.Replace(routine, "jevons: status landed", "jevons: status in-progress", 1), strings.Replace(routine, "jevons: status landed", "", 1), strings.Replace(routine, "jevons: verdict GREEN", "jevons: verdict UNKNOWN", 1), strings.Replace(routine, "Named test green.", "A security regression needs review.", 1)} {
		if routineWorkerCompletion(text) {
			t.Fatalf("uncertain report suppressed: %q", text)
		}
	}
	po := &fakeSender{alive: true}
	s, owner := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
	// Without the durable store even a valid routine is forwarded.
	s.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: routine, StopReason: "end_turn"})
	if len(owner.texts) != 1 {
		t.Fatalf("missing durable store silently lost report: %v", owner.texts)
	}
	// A failed parent delivery is not a reason to hide the overseer copy.
	failingParent := &fakeSender{alive: true, sendErr: fmt.Errorf("parent unavailable")}
	failed, failedOwner := chainServer(t, map[string]*fakeSender{"jevons-po": failingParent})
	failed.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
	failed.SetAgentReportDir(t.TempDir())
	failed.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: routine, StopReason: "end_turn"})
	if len(failedOwner.texts) != 1 {
		t.Fatalf("failed parent send hid report: %v", failedOwner.texts)
	}
}

func TestT1054RootParentAndFalseGreenRemainVisible(t *testing.T) {
	routine := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T1054", Status: envelope.ProgressLanded, Verdict: envelope.VerdictGreen, SHA: "abcdef0123456", SilentLedger: envelope.SilentLedgerEmpty, Payload: "Named test green."})
	t.Run("root parent", func(t *testing.T) {
		s, owner := chainServer(t, nil)
		s.registry = newLineageRegistry(t, map[string]string{"worker": "jevons"})
		s.SetAgentReportDir(t.TempDir())
		s.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: routine, StopReason: "end_turn"})
		if len(owner.texts) != 1 {
			t.Fatalf("root-parent report hidden: %v", owner.texts)
		}
	})
	t.Run("false green", func(t *testing.T) {
		po := &fakeSender{alive: true}
		s, owner := chainServer(t, map[string]*fakeSender{"jevons-po": po})
		s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "worker": "jevons-po"})
		s.SetAgentReportDir(t.TempDir())
		bad := strings.Replace(routine, "Named test green.", "    make test-web 2>&1 | tail -25; echo \"EXIT=${PIPESTATUS[0]}\"\n    EXIT=\n", 1)
		if len(FalseGreenFlagsForReport(bad, "")) == 0 {
			t.Fatal("fixture has no false-green flags")
		}
		s.agentEventSink("worker")(claudia.Event{Type: "assistant", Text: bad, StopReason: "end_turn"})
		if len(owner.texts) != 1 || !strings.Contains(owner.texts[0], "FALSE-GREEN") {
			t.Fatalf("false green hidden: %v", owner.texts)
		}
	})
}
