// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"
	"testing"
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
	po.inFlight = false
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

// Silent-drop case 2: a busy peer queues (🎯T111.1) — the message is retained
// and the caller is told, rather than the send failing into nothing.
func TestDeliverByNameBusyPeerQueuesNotDrops(t *testing.T) {
	po := &fakeSender{alive: true, inFlight: true}
	s, _ := chainServer(t, map[string]*fakeSender{"jevons-po": po})

	res, err := s.deliverByName("jevons-po", "worker: blocked on review", OriginAgent, false)
	if err != nil {
		t.Fatalf("busy peer must not hard-fail: %v", err)
	}
	if res.Status != "queued" || res.Queued != 1 {
		t.Fatalf("status=%q queued=%d want queued/1", res.Status, res.Queued)
	}
	if got := s.dequeueAgentSend("jevons-po"); got != "worker: blocked on review" {
		t.Fatalf("queued text=%q — message was dropped", got)
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
