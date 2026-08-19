// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/relayroute"
)

// chainServer builds a Server whose fleet arm resolves to fake senders and
// whose overseer arm records what the owner would see, so the whole
// deliver-by-name path runs without a provider process (🎯T309.3).
type overseerInbox struct {
	texts   []string
	origins []SendOrigin
	err     error
}

func (o *overseerInbox) deliver(text string, origin SendOrigin) error {
	if o.err != nil {
		return o.err
	}
	o.texts = append(o.texts, text)
	o.origins = append(o.origins, origin)
	return nil
}

func chainServer(t *testing.T, fleet map[string]*fakeSender) (*Server, *overseerInbox) {
	t.Helper()
	inbox := &overseerInbox{}
	s := &Server{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		fs, ok := fleet[name]
		if !ok {
			// Same shape ensureAgentProcess returns for an unknown peer.
			return nil, false, fmt.Errorf("agent %q is not running", name)
		}
		return fs, false, nil
	})
	// 🎯T416: delivery is now reported from evidence about the agent, and a
	// fakeSender is not an agent — with no witness every leg here would come
	// back delivered_unconfirmed. These tests are about ROUTING (which arm a
	// name resolves to, whose origin is carried), so they witness the payload
	// landing and let the confirmation be pinned where it belongs, in
	// send_turn_begin_test.go.
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	return s, inbox
}

// The mission chain: a worker reports to its PO, the PO reports to the
// overseer, and the overseer's arm is what the owner sees. Every leg is the
// SAME call addressed by name — acceptance 1 and 3.
func TestDeliverByNameWorkerToPOToOverseerChain(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})

	// Leg 1: worker → PO. Fleet arm, agent origin.
	res, err := s.deliverByName("jevons-po", "worker: slice landed, tests green", OriginAgent, false)
	if err != nil {
		t.Fatalf("worker→PO: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("worker→PO status=%q want sent", res.Status)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "slice landed") {
		t.Fatalf("PO inbox=%v", po.sent)
	}

	// Leg 2: PO → overseer, by name, through the same call. Before 🎯T309.3
	// this resolved through the registry and bypassed the owner chat journal.
	res, err = s.deliverByName("jevons", "po: T309.3 converging", OriginAgent, false)
	if err != nil {
		t.Fatalf("PO→overseer: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("PO→overseer status=%q want sent", res.Status)
	}
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], "T309.3 converging") {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
	if inbox.origins[0] != OriginAgent {
		t.Fatalf("origin=%q want agent", inbox.origins[0])
	}

	// Leg 3: owner → any agent, same call, owner origin. The owner reaches
	// the overseer and a worker by name with no privileged wire either way.
	if _, err := s.deliverByName("jevons", "owner: status?", OriginOwner, false); err != nil {
		t.Fatalf("owner→overseer: %v", err)
	}
	if inbox.origins[1] != OriginOwner {
		t.Fatalf("origin=%q want owner", inbox.origins[1])
	}
	// The turn started by leg 1 ends. Both halves are needed since 🎯T416:
	// the fake stops refusing sends, AND the daemon observes the boundary —
	// in production that second half is the event sink's terminal stop, and
	// without it the daemon holds this message in its own queue rather than
	// stacking it in a composer.
	po.inFlight = false
	s.noteTurnEnded("jevons-po")
	if _, err := s.deliverByName("jevons-po", "owner: direct to PO", OriginOwner, false); err != nil {
		t.Fatalf("owner→PO: %v", err)
	}
	if len(po.sent) != 2 {
		t.Fatalf("PO inbox=%v want 2", po.sent)
	}
}

// Case-insensitive name resolution reaches the overseer arm, not the registry
// arm — otherwise "Jevons" would be looked up as a fleet agent and 404.
func TestDeliverByNameOverseerCaseInsensitive(t *testing.T) {
	s, inbox := chainServer(t, nil)
	if _, err := s.deliverByName("Jevons", "hello", OriginAgent, false); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
}

// Silent-drop case 1: an unregistered peer is an error the caller sees.
func TestDeliverByNameUnregisteredPeerErrors(t *testing.T) {
	s, _ := chainServer(t, map[string]*fakeSender{})
	_, err := s.deliverByName("ghost-worker", "are you there", OriginAgent, false)
	if err == nil {
		t.Fatal("unregistered peer must error, not vanish")
	}
	if !strings.Contains(err.Error(), "ghost-worker") {
		t.Fatalf("err=%v should name the peer", err)
	}
}

func TestT3927RelayReportSkipsPOHop(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"jv-t10":    "jevons-po",
	})
	logPath := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	journal, err := eventlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	s.SetEventJournal(journal)

	text := "Blocked: needs owner verdict on the provider spend cap before I can proceed.\nDetails stay with the overseer."
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  text,
		"actor": "jv-t10",
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("relay send: %v", err)
	}
	if res.IsError {
		t.Fatalf("relay send: %s", toolText(res))
	}
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], text) {
		t.Fatalf("overseer inbox=%v — full report must skip the PO hop", inbox.texts)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "routed to overseer") {
		t.Fatalf("PO inbox=%v want one record line", po.sent)
	}
	for _, want := range []string{"jv-t10", "needs_owner", "Blocked: needs owner verdict on the provider spend cap"} {
		if !strings.Contains(po.sent[0], want) {
			t.Errorf("PO record %q missing %q", po.sent[0], want)
		}
	}
	if strings.Contains(po.sent[0], "\n") {
		t.Fatalf("PO record is not one line: %q", po.sent[0])
	}
	if strings.Contains(po.sent[0], IdentityHeaderMarker) {
		t.Fatalf("PO record summarized the delivery envelope, not the report: %q", po.sent[0])
	}

	events, err := eventlog.Tail(logPath, eventlog.TailOptions{
		Component: "relayroute",
		Decision:  "overseer",
		Contains:  "jv-t10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("relayroute events=%d want 1", len(events))
	}
	ev := events[0]
	for key, want := range map[string]string{
		"worker":      "jv-t10",
		"route_class": "overseer",
		"reason":      "needs_owner",
		"summary":     relayroute.ReportSummary(text),
	} {
		if got := ev.Fields[key]; got != want {
			t.Errorf("event field %s=%v want %q", key, got, want)
		}
	}
}

func TestT3927OwnerDirectToPONotRerouted(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	text := "🎯T10 done. GATE abc GREEN. SHA deadbeef."
	if _, err := s.deliverByName("jevons-po", text, OriginOwner, false); err != nil {
		t.Fatal(err)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "GATE abc") {
		t.Fatalf("owner→PO must stay on the PO: %v", po.sent)
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("owner→PO leaked to overseer: %v", inbox.texts)
	}
}

// 🎯T515: first agent_send injects identity + standing brief (which itself
// contains "needs-owner" / "class-3"). Classification and the PO record
// summary must use the sender's report body, not the doctrine envelope —
// otherwise every first hop to a PO is falsely direct-routed, and the record
// line summarizes the brief instead of the report.
func TestT515EnvelopeDoesNotFakeRouteOrPolluteSummary(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"jv-t515":   "jevons-po",
	})
	logPath := filepath.Join(t.TempDir(), "logs", "events.jsonl")
	journal, err := eventlog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	s.SetEventJournal(journal)

	// Routine parent-bound report: must stay on the PO even though the
	// injected standing brief mentions needs-owner.
	routine := "slice landed; continuing on the remaining leaf."
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  routine,
		"actor": "jv-t515",
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("routine send: %v", err)
	}
	if res.IsError {
		t.Fatalf("routine send: %s", toolText(res))
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("routine report leaked to overseer via brief phrases: %v", inbox.texts)
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], routine) {
		t.Fatalf("PO inbox=%v want the routine report (with brief)", po.sent)
	}

	// Real needs_owner report on a subsequent send (brief already injected):
	// record line + durable relayroute event must name the worker and summarize
	// the report body, not the doctrine.
	po.inFlight = false
	s.noteTurnEnded("jevons-po")
	po.sent = nil
	inbox.texts = nil
	report := "Blocked: needs owner verdict on the provider spend cap before I can proceed."
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  report,
		"actor": "jv-t515",
	}
	res, err = s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("needs_owner send: %v", err)
	}
	if res.IsError {
		t.Fatalf("needs_owner send: %s", toolText(res))
	}
	if len(inbox.texts) != 1 || !strings.Contains(inbox.texts[0], report) {
		t.Fatalf("overseer inbox=%v want full needs_owner report", inbox.texts)
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO inbox=%v want one record line", po.sent)
	}
	for _, want := range []string{"jv-t515", "needs_owner", "Blocked: needs owner verdict on the provider spend cap"} {
		if !strings.Contains(po.sent[0], want) {
			t.Errorf("PO record %q missing %q", po.sent[0], want)
		}
	}
	if strings.Contains(po.sent[0], IdentityHeaderMarker) || strings.Contains(po.sent[0], "Jevons fleet standing brief") {
		t.Fatalf("PO record summarized the delivery envelope: %q", po.sent[0])
	}
	events, err := eventlog.Tail(logPath, eventlog.TailOptions{
		Component: "relayroute",
		Decision:  "overseer",
		Contains:  "jv-t515",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("relayroute events=%d want 1", len(events))
	}
	ev := events[0]
	if ev.Fields["worker"] != "jv-t515" || ev.Fields["route_class"] != "overseer" {
		t.Fatalf("event fields=%v", ev.Fields)
	}
	if got := ev.Fields["summary"]; got != relayroute.ReportSummary(report) {
		t.Fatalf("event summary=%v want %q", got, relayroute.ReportSummary(report))
	}
}

// Silent-drop case 2: a busy peer queues (🎯T111.1) — the message is retained
// and the caller is told, rather than the send failing into nothing.
func TestDeliverByNameBusyPeerQueuesNotDrops(t *testing.T) {
	po := &fakeSender{alive: true, inFlight: true}
	s, _ := chainServer(t, map[string]*fakeSender{"jevons-po": po})

	res, err := s.deliverByName("jevons-po", "worker: still mid-slice, continue", OriginAgent, false)
	if err != nil {
		t.Fatalf("busy peer must not hard-fail: %v", err)
	}
	if res.Status != "queued" || res.Queued != 1 {
		t.Fatalf("status=%q queued=%d want queued/1", res.Status, res.Queued)
	}
	if got := s.dequeueAgentSend("jevons-po"); got.Text != "worker: still mid-slice, continue" {
		t.Fatalf("queued text=%q — message was dropped", got.Text)
	}
}

// Silent-drop case 3: a failing overseer arm surfaces as an error. The
// 🎯T62 regression was exactly this outcome being logged and forgotten.
func TestDeliverByNameOverseerFailureSurfaces(t *testing.T) {
	s, inbox := chainServer(t, nil)
	inbox.err = fmt.Errorf("overseer not running")

	_, err := s.deliverByName("jevons", "worker done report", OriginAgent, false)
	if err == nil {
		t.Fatal("overseer delivery failure must surface")
	}
	if !strings.Contains(err.Error(), "overseer not running") {
		t.Fatalf("err=%v should carry the cause", err)
	}
}

// With no seam wired at all, an overseer-addressed send is a loud error
// rather than a message posted into the void.
func TestDeliverByNameNoOverseerSeamErrors(t *testing.T) {
	s := &Server{}
	if _, err := s.deliverByName("jevons", "report", OriginAgent, false); err == nil {
		t.Fatal("unwired overseer seam must error")
	}
}

// The legacy notifyJevon injection still carries agent-origin text (existing
// wiring keeps working), but cannot carry an owner turn: delivering the
// owner's words as an unmarked agent note would misattribute them.
func TestDeliverByNameLegacyNotifySeam(t *testing.T) {
	var got []string
	s := &Server{}
	s.SetNotify(func(text string) { got = append(got, text) })

	if _, err := s.deliverByName("jevons", "worker reply", OriginAgent, false); err != nil {
		t.Fatalf("agent origin via legacy seam: %v", err)
	}
	if len(got) != 1 || got[0] != "worker reply" {
		t.Fatalf("legacy inbox=%v", got)
	}
	if _, err := s.deliverByName("jevons", "owner words", OriginOwner, false); err == nil {
		t.Fatal("owner turn must not be delivered through the unframed legacy seam")
	}
}

// notify() is a shim over the single path: a worker's terminal report reaches
// the overseer arm with the same framing it had before the unification.
func TestNotifyRoutesThroughDeliverByName(t *testing.T) {
	s, inbox := chainServer(t, nil)

	s.notify("jv-t309.3-fleet-deliver", "commit abc123; tests green")
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
	if !strings.Contains(inbox.texts[0], "[Agent jv-t309.3-fleet-deliver responded]") {
		t.Fatalf("framing lost: %q", inbox.texts[0])
	}
	if !strings.Contains(inbox.texts[0], "commit abc123") {
		t.Fatalf("body lost: %q", inbox.texts[0])
	}
	if inbox.origins[0] != OriginAgent {
		t.Fatalf("origin=%q want agent", inbox.origins[0])
	}

	// [silent] ops chatter is still suppressed before delivery.
	s.notify("jv-ops", "[silent] workers fine")
	if len(inbox.texts) != 1 {
		t.Fatalf("silent reply leaked: %v", inbox.texts)
	}
}

// sendToAgent keeps its contract as a shim, including the registry-absent
// error for a fleet name (the overseer arm does not need a registry).
func TestSendToAgentShimRegistryAbsent(t *testing.T) {
	s := &Server{}
	if _, err := s.sendToAgent("some-worker", "hi", false); err == nil {
		t.Fatal("fleet send without registry must error")
	}
	if _, err := s.sendToAgent("", "hi", false); err == nil {
		t.Fatal("empty name must error")
	}
	if _, err := s.sendToAgent("some-worker", "", false); err == nil {
		t.Fatal("empty text must error")
	}
}

// 🎯T309.3 acceptance 2: authorization is decided on the single path by
// lineage/policy. Every lineage relationship may talk; what is refused is the
// identity claim — only the owner surface may speak as the owner.
func TestAuthorizeDeliverOwnerOriginIsOwnerSurfaceOnly(t *testing.T) {
	isOverseer := func(n string) bool { return n == "jevons" }

	// The owner surface may assert owner origin.
	rel, err := AuthorizeDeliver(nil, ActorOwnerSurface, "jevons", OriginOwner, isOverseer)
	if err != nil {
		t.Fatalf("owner surface denied: %v", err)
	}
	if rel != RelationOwnerSurface {
		t.Fatalf("relation=%q", rel)
	}

	// A fleet agent may not — that would put words in the owner's mouth
	// inside the overseer's session.
	if _, err := AuthorizeDeliver(nil, "jevons-po", "jevons", OriginOwner, isOverseer); err == nil {
		t.Fatal("a fleet agent must not be able to send as the owner")
	}

	// The same agent reaching the same target as itself is fine.
	rel, err = AuthorizeDeliver(nil, "jevons-po", "jevons", OriginAgent, isOverseer)
	if err != nil {
		t.Fatalf("report up denied: %v", err)
	}
	if rel != RelationReportUp {
		t.Fatalf("relation=%q want report_up", rel)
	}
}

// Lineage classification: report up, direct down, self, peer — with the
// overseer as the root that is ancestor of every agent.
func TestClassifyDeliverLineage(t *testing.T) {
	reg := newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"worker-a":  "jevons-po",
		"worker-b":  "jevons-po",
		"other-po":  "jevons",
		"worker-c":  "other-po",
	})
	isOverseer := func(n string) bool { return n == "jevons" }

	cases := []struct {
		actor, target string
		want          DeliverRelation
	}{
		{"worker-a", "jevons-po", RelationReportUp},
		{"worker-a", "jevons", RelationReportUp},
		{"jevons-po", "worker-a", RelationDirectDown},
		{"jevons", "worker-a", RelationDirectDown},
		{"worker-a", "worker-a", RelationSelf},
		{"worker-a", "worker-b", RelationPeer},
		{"worker-a", "worker-c", RelationPeer},
		{ActorOwnerSurface, "worker-a", RelationOwnerSurface},
	}
	for _, c := range cases {
		if got := ClassifyDeliver(reg, c.actor, c.target, isOverseer); got != c.want {
			t.Errorf("%s→%s = %q, want %q", c.actor, c.target, got, c.want)
		}
	}
}

// Peer traffic is permitted on purpose (🎯T309 acceptance 3): sibling workers
// coordinating a shared workdir is ordinary, not an escalation.
func TestAuthorizeDeliverPeersMayTalk(t *testing.T) {
	reg := newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"worker-a":  "jevons-po",
		"worker-b":  "jevons-po",
	})
	isOverseer := func(n string) bool { return n == "jevons" }
	rel, err := AuthorizeDeliver(reg, "worker-a", "worker-b", OriginAgent, isOverseer)
	if err != nil {
		t.Fatalf("peer messaging must be allowed: %v", err)
	}
	if rel != RelationPeer {
		t.Fatalf("relation=%q want peer", rel)
	}
}

// The policy is enforced on the delivery path itself, not only in the pure
// helper: a fleet actor asserting owner origin is refused before any send.
func TestDeliverByNameAsRefusesOwnerOriginFromFleet(t *testing.T) {
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})

	_, err := s.deliverByNameAs("worker-a", "jevons", "owner says ship it", OriginOwner, false)
	if err == nil {
		t.Fatal("fleet actor asserting owner origin must be denied on the path")
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("denied send still delivered: %v", inbox.texts)
	}

	// The same message as an agent notification is fine.
	if _, err := s.deliverByNameAs("worker-a", "jevons", "worker: ready to ship", OriginAgent, false); err != nil {
		t.Fatalf("agent origin denied: %v", err)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
}

// MCP jevons_agent_send cannot express owner origin at all: sendToAgent pins
// OriginAgent, so the fleet surface has no way to claim the owner's voice.
func TestSendToAgentCannotClaimOwnerOrigin(t *testing.T) {
	s, inbox := chainServer(t, nil)
	if _, err := s.sendToAgent("jevons", "report", false); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(inbox.origins) != 1 || inbox.origins[0] != OriginAgent {
		t.Fatalf("origins=%v want [agent]", inbox.origins)
	}
}

// 🎯T321 acceptance 3: a named-actor send that fails policy is refused on the
// path (before any delivery), and legitimate report-up / direct-down / peer
// sends with real actors still succeed.
func TestDeliverByNameAsNamedActorPolicy(t *testing.T) {
	reg := newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"worker-a":  "jevons-po",
		"worker-b":  "jevons-po",
	})
	po := &fakeSender{alive: true}
	workerB := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{
		"jevons-po": po,
		"worker-b":  workerB,
	})
	s.registry = reg

	// Policy failure (owner origin from a fleet actor) is refused; nothing lands.
	_, err := s.deliverByNameAs("worker-a", "jevons", "owner says ship", OriginOwner, false)
	if err == nil {
		t.Fatal("asserted actor failing policy must be refused on the path")
	}
	if !strings.Contains(err.Error(), "may not send as the owner") {
		t.Fatalf("err=%v should name the identity refusal", err)
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("denied send still delivered: %v", inbox.texts)
	}

	// Report up: worker → PO.
	if _, err := s.deliverByNameAs("worker-a", "jevons-po", "worker: report up", OriginAgent, false); err != nil {
		t.Fatalf("report_up: %v", err)
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO inbox=%v", po.sent)
	}

	// Direct down: PO → worker-b.
	if _, err := s.deliverByNameAs("jevons-po", "worker-b", "po: direct down", OriginAgent, false); err != nil {
		t.Fatalf("direct_down: %v", err)
	}
	if len(workerB.sent) != 1 {
		t.Fatalf("worker-b inbox=%v", workerB.sent)
	}

	// Peer: worker-a → worker-b (policy permits on purpose). The previous
	// turn ends first — see the note in the chain test on why the daemon has
	// to be told as well as the fake (🎯T416).
	workerB.inFlight = false
	s.noteTurnEnded("worker-b")
	if _, err := s.deliverByNameAs("worker-a", "worker-b", "peer: coordinate", OriginAgent, false); err != nil {
		t.Fatalf("peer: %v", err)
	}
	if len(workerB.sent) != 2 {
		t.Fatalf("worker-b inbox after peer=%v", workerB.sent)
	}

	// Report up to overseer by name under a real actor.
	if _, err := s.deliverByNameAs("worker-a", "jevons", "worker: done report", OriginAgent, false); err != nil {
		t.Fatalf("report_up overseer: %v", err)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
}

// 🎯T321 acceptance 1–2: MCP jevons_agent_send carries actor and calls the
// lineage path with it (not the owner-surface blank).
func TestHandleAgentSendRequiresActorAndUsesLineage(t *testing.T) {
	reg := newLineageRegistry(t, map[string]string{
		"jevons-po": "jevons",
		"worker-a":  "jevons-po",
	})
	po := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{"jevons-po": po})
	s.registry = reg

	// Empty actor, no session default → refused before delivery.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "jevons-po",
		"text": "hello from nowhere",
	}
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("empty actor must error")
	}
	if !strings.Contains(toolText(res), "actor is required") {
		t.Fatalf("got %q", toolText(res))
	}
	if len(po.sent) != 0 {
		t.Fatalf("send without actor still delivered: %v", po.sent)
	}

	// Legitimate report-up with named actor succeeds on the MCP path.
	req.Params.Arguments = map[string]any{
		"name":  "jevons-po",
		"text":  "worker: slice landed",
		"actor": "worker-a",
	}
	res, err = s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("report_up MCP send: %s", toolText(res))
	}
	if len(po.sent) != 1 || !strings.Contains(po.sent[0], "slice landed") {
		t.Fatalf("PO inbox=%v", po.sent)
	}

	// Same actor reporting to overseer by name.
	req.Params.Arguments = map[string]any{
		"name":  "jevons",
		"text":  "worker: ready for review",
		"actor": "worker-a",
	}
	res, err = s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("overseer MCP send: %s", toolText(res))
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer inbox=%v", inbox.texts)
	}
	if inbox.origins[0] != OriginAgent {
		t.Fatalf("origin=%q want agent", inbox.origins[0])
	}
}

// 🎯T321: denials on the MCP path log actor + relation (AuthorizeDeliver teeth).
func TestHandleAgentSendDenialLogsActorAndRelation(t *testing.T) {
	// Force a policy denial through the path that MCP uses: sendToAgentAs
	// always pins OriginAgent, so we exercise deliverByNameAs directly with
	// owner origin under a named actor — the only identity refusal today —
	// and assert the structured log carries actor + relation.
	cap := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s, inbox := chainServer(t, nil)
	_, err := s.deliverByNameAs("worker-a", "jevons", "owner voice", OriginOwner, false)
	if err == nil {
		t.Fatal("expected denial")
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("denied delivery leaked: %v", inbox.texts)
	}

	var found map[string]any
	for _, r := range cap.records {
		m := attrsMap(r)
		if m["status"] == "denied" && m["component"] == "agent_send" {
			found = m
			break
		}
	}
	if found == nil {
		t.Fatal("expected denied agent_send slog record")
	}
	if found["actor"] != "worker-a" {
		t.Fatalf("actor=%v want worker-a", found["actor"])
	}
	if found["relation"] != string(RelationReportUp) {
		// worker-a → overseer classifies as report_up even when origin is denied.
		t.Fatalf("relation=%v want report_up", found["relation"])
	}
	if found["name"] != "jevons" {
		t.Fatalf("name=%v", found["name"])
	}
}

// newLineageRegistry builds a registry with the given name→parent tree.
func newLineageRegistry(t *testing.T, parents map[string]string) *claudia.Registry {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, parent := range parents {
		if _, err := reg.EnsureAgentWithParent(name, "/work/"+name, "", parent, false); err != nil {
			t.Fatalf("EnsureAgentWithParent %s→%s: %v", name, parent, err)
		}
	}
	return reg
}
