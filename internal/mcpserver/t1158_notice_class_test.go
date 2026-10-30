// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"
	"testing"
)

// t1158NoticeClassCallSites ratchets every production notifyFleetHealth call (🎯T1158).
var t1158NoticeClassCallSites = []struct {
	file    string
	snippet string
	class   FleetHealthNoticeClass
}{
	{"agent_send.go", "Delivery outcome for", noticeAct},
	{"fleet_health.go", "deadAgentOccurrence(reps)", noticeAct},
	{"fleet_mute.go", `mute", reason`, noticeAct},
	{"frontier_consume.go", "noticeInform, fleetHealthEventOccurrence(r.Worker", noticeInform},
	{"frontier_consume.go", "PO %s unreachable (%v) for:", noticeAct},
	{"held_reaped_route.go", "Uncertain route of held message", noticeAct},
	{"held_reaped_route.go", "b2.String()", noticeInform},
	{"mission_protection.go", "Parent delivery failed:", noticeAct},
	{"reap_done.go", "pending.Owes, err, msg", noticeAct},
	{"reap_done.go", "PO %s unreachable (%v) for:", noticeAct},
	{"reap_scope.go", "parent %s unreachable", noticeAct},
	{"send_backlog.go", "noticeAct, strings.Join(keys", noticeAct},
	{"send_backlog.go", "noticeAct, hold, fmt.Sprintf", noticeAct},
	{"send_backlog.go", "noticeAct, head.ID, fmt.Sprintf", noticeAct},
	{"send_backlog.go", "noticeAct, heldBacklogKey(b)", noticeAct},
	{"send_backlog.go", "noticeInform, heldBacklogKey(b)", noticeInform},
	{"send_backlog.go", "FormatSendqPinLine", noticeAct},
	{"send_backlog.go", "pin.AttemptID, line", noticeAct},
	{"send_waiting.go", "Held message %s (attempt", noticeAct},
	{"sendq_survive.go", "noticeInform, fleetHealthEventOccurrence(strings.Join(restarted", noticeInform},
	{"t662_seat_stops.go", "a.Key(), line", noticeAct},
	{"t726_sendq_reconcile.go", "still blocked by entry", noticeAct},
	{"t726_sendq_reconcile.go", "reconciled by %s", noticeAct},
	{"t734_post_reap_commit.go", "PO %s unreachable", noticeAct},
	{"t927_wedged_turn.go", "FormatWedgedTurnLine", noticeAct},
	{"t935_broker_lost.go", "occurrence, text", noticeAct},
}

func TestT1158EveryNotifyFleetHealthCallSiteDeclaresClass(t *testing.T) {
	t.Parallel()
	srcs := t727PackageSources(t)
	const wantCalls = 26
	var found int
	for _, site := range t1158NoticeClassCallSites {
		src, ok := srcs[site.file]
		if !ok {
			t.Fatalf("missing source %s", site.file)
		}
		if !strings.Contains(src, site.snippet) {
			t.Fatalf("%s: snippet missing %q", site.file, site.snippet)
		}
		matched := false
		for _, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, site.snippet) {
				continue
			}
			if !strings.Contains(line, "notifyFleetHealth(") {
				continue
			}
			matched = true
			found++
			want := "noticeInform"
			if site.class == noticeAct {
				want = "noticeAct"
			}
			if !strings.Contains(line, want) {
				t.Errorf("%s: expected %s on %q", site.file, want, strings.TrimSpace(line))
			}
		}
		if !matched {
			t.Fatalf("%s: no notifyFleetHealth line for snippet %q", site.file, site.snippet)
		}
	}
	if found != len(t1158NoticeClassCallSites) {
		t.Fatalf("matched %d rows; want %d", found, len(t1158NoticeClassCallSites))
	}
	if len(t1158NoticeClassCallSites) != wantCalls {
		t.Fatalf("ratchet lists %d sites; want %d", len(t1158NoticeClassCallSites), wantCalls)
	}

	re := regexp.MustCompile(`\.notifyFleetHealth\(`)
	for name, src := range srcs {
		if name == "agents.go" {
			continue
		}
		for i, line := range strings.Split(src, "\n") {
			if !re.MatchString(line) {
				continue
			}
			if strings.Contains(line, "noticeInform") || strings.Contains(line, "noticeAct") {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			t.Errorf("%s:%d unclassified: %s", name, i+1, strings.TrimSpace(line))
		}
	}
}

func TestT1158InformFleetHealthDoesNotDeliverToOverseer(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	s.notifyFleetHealth(noticeInform, "t1158-inform",
		"[frontier-consume] auto-spawned jv-t1158 for T1158 under jevons-po")
	if len(inbox.texts) != 0 {
		t.Fatalf("inform delivered %d overseer messages; want 0", len(inbox.texts))
	}
	s.notifyFleetHealth(noticeAct, "t1158-act", t810Stalled(1, "20m0s"))
	if len(inbox.texts) != 1 {
		t.Fatalf("act delivered %d; want 1", len(inbox.texts))
	}
}
