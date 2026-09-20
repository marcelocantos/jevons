// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T565: `jevons_agent_send name=jevons-po actor=jevons` must land in the
// PO's session and never in the overseer's own transcript. On 2026-08-29 it
// did the latter twice: the overseer's direct contained needs-owner phrasing,
// the 🎯T392.7 worker-report reroute classified it, and the message was
// delivered to the sender.
func TestT565OverseerDirectToPOStaysOnPO(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
	})
	s.fleetBriefed = map[string]bool{"jevons-po": true}

	// Every phrase relayroute.Classify would reroute a worker report on.
	text := "Blocked: needs owner verdict on the spend cap. Also 🎯T10 done, GATE abc GREEN, SHA deadbeef — verify and reap."
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  text,
		"actor": "jevons",
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("overseer→PO: %s", toolText(res))
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], text) {
		t.Fatalf("PO inbox=%v — the overseer's direct must reach the PO", po.sent)
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("overseer→PO leaked into the overseer's own transcript: %v", inbox.texts)
	}
	if got := toolText(res); !strings.Contains(got, `"jevons-po"`) || strings.Contains(got, "overseer") {
		t.Fatalf("result must name the observed receiver jevons-po and nothing else: %q", got)
	}
}

// The PO addressing itself is not a worker report either.
func TestT565POToItselfNotRerouted(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{"jevons-po": "jevons"})
	text := "note to self: blocked on needs owner"
	if _, err := s.deliverByNameAs("jevons-po", "jevons-po", text, OriginAgent, false); err != nil {
		t.Fatal(err)
	}
	if len(po.sent) != 1 || len(inbox.texts) != 0 {
		t.Fatalf("po=%v overseer=%v", po.sent, inbox.texts)
	}
}

// A worker report that is rerouted still names the receiver it was observed
// to reach — the overseer — rather than the PO it was addressed to.
func TestT565RerouteResultNamesObservedReceiver(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"jv-t10":    "jevons-po",
	})
	s.SetAgentReportDir(t.TempDir()) // 🎯T658: a skipped hop needs a stored report
	res, err := s.deliverByNameAs("jv-t10", "jevons-po", "Blocked: needs owner verdict on the cap.", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("worker report must reach the overseer: %v", inbox.texts)
	}
	for _, want := range []string{"rerouted", `overseer "jevons"`, `not "jevons-po" as addressed`} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("result %q missing %q", res.Message, want)
		}
	}
}

// A name that resolves to the overseer seat under a different spelling is a
// receiver≠name mismatch: an error, delivered nowhere — never "accepted for
// overseer".
func TestT565ReceiverMismatchIsError(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const overseerSession = "s-overseer"
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-ceo", WorkDir: "/w", Purpose: claudia.PurposeOverseer, SessionID: overseerSession},
		// A PO row that lost its parent and carries the overseer's session id:
		// isOverseerAgent says yes, the seat name says jevons-ceo.
		{Name: "jevons-po", WorkDir: "/w", Purpose: claudia.PurposeWork, SessionID: overseerSession},
	} {
		d.Materialized = true
		d.Provider = "claude"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = reg
	s.transcript = &TranscriptOps{GetID: func() string { return overseerSession }}

	_, err = s.deliverByName("jevons-po", "hello", OriginAgent, false)
	if err == nil {
		t.Fatal("expected a receiver-mismatch error")
	}
	for _, want := range []string{"receiver mismatch", `"jevons-po"`, `"jevons-ceo"`, "NOT delivered"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if len(inbox.texts) != 0 || len(po.sent) != 0 {
		t.Fatalf("mismatch must deliver nowhere: overseer=%v po=%v", inbox.texts, po.sent)
	}
}

// 🎯T565 part 2: a worker whose last turn declares a blocking wait on a
// tracked background gate is not nudged (jv-t555.1 sent 6+ identical wait
// turns in ~40s under repressure).
func TestT565DeclaresBlockingGateWait(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want bool
	}{
		{"Waiting on the background gate `bin/gate -- make test-go` to finish; nothing to do until the task notification arrives.", true},
		{"Blocked on go test ./... still running in the background (id 3f2a).", true},
		{"Still running: make test-journey in the background — waiting for it before I report.", true},
		{"", false},
		{"Committed 9ba550d3; bin/gate -- make test-go GREEN id=abc.", false},
		{"Waiting for the owner's verdict on the design.", false},
		{"Idle. What next?", false},
	}
	for _, c := range cases {
		if got := DeclaresBlockingGateWait(c.text); got != c.want {
			t.Errorf("DeclaresBlockingGateWait(%q)=%v want %v", c.text, got, c.want)
		}
	}
}

func TestT565IdleNudgeSkipsWaitingOnGate(t *testing.T) {
	t.Parallel()
	o := IdleNudgeObs{
		Name:           "w",
		Purpose:        claudia.PurposeWork,
		ProcessRunning: true,
		Phase:          "idle",
		IdleFor:        DefaultIdleNudgeThreshold * 4,
		HasOpenMission: true,
		WaitingOnGate:  true,
	}
	action, reason := ClassifyIdleNudge(o)
	if action != IdleNudgeSkip || reason != IdleSkipWaitingOnGate {
		t.Fatalf("got %s/%s want skip/%s", action, reason, IdleSkipWaitingOnGate)
	}
	o.WaitingOnGate = false
	if action, _ := ClassifyIdleNudge(o); action != IdleNudgeNudge {
		t.Fatalf("control: without the wait the same observation nudges, got %s", action)
	}
}

// The sweep reads the declaration off the tracker's last terminal text —
// the product path, not a test-only field.
func TestT565SweepReadsGateWaitFromLastTerminal(t *testing.T) {
	t.Parallel()
	tracker := NewIdleActivityTracker()
	tracker.by["jv-t1"] = IdleActivity{
		Phase:        "idle",
		Updated:      tracker.clock().Add(-DefaultIdleNudgeThreshold * 4),
		LastTerminal: "Waiting on bin/gate -- make test-go in the background; will report when the task notification lands.",
	}
	d := claudia.AgentDef{Name: "jv-t1", Purpose: claudia.PurposeWork, TargetID: "T1", AutoStart: true}
	rep := classifyIdleNudgeFor(d, IdleNudgeSweepArgs{
		Activity:       tracker,
		ProcessRunning: func(string) bool { return true },
		MissionOpen:    func(string) bool { return true },
	}, tracker.clock())
	if rep.Action != IdleNudgeSkip || rep.Reason != IdleSkipWaitingOnGate {
		t.Fatalf("got %s/%s want skip/%s", rep.Action, rep.Reason, IdleSkipWaitingOnGate)
	}
}
