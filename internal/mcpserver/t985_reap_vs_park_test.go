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

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T985 specimens. The first three are the real park reasons jevons-po
// recorded on 2026-09-30 (fleet/intent.json): two own-work-complete seats
// that sat as standing parked rows under the old convention, and one
// genuine cross-repo block that remains a correct park.

// jv-t947-plan-token: own work complete; the remainder is someone else's.
const t985ParkReasonT947 = "jevons-side slice split off and achieved as T973. Remaining T947 scope is claudia-side, routed to claudia-po. Nothing left for this worktree/worker."

// jv-t972-reap-scope: own work complete and integrated.
const t985ParkReasonT972 = "T972 fully achieved and integrated (ledger commit 1e040907, gate 4a8e0270 GREEN); worker has nothing further to do and is looping on idle-nudges with malformed status re-confirmations. Standing down cleanly."

// jv-t765.1-worker: genuinely blocked on another repo, needs the owner.
const t985ParkReasonT765_1 = "Confirmed no bullseye-po exists in the fleet (checked jevons_agent_list). This target is genuinely cross-repo blocked on bullseye's own src/apply.rs, which jevons-po has no mandate or worktree to touch. Parking rather than continuing to spend turns re-confirming the same correct-but-stuck position; needs owner-level coordination to either open a bullseye-repo seat or explicitly descope this sub-target."

// The acceptance's own two cases, as a worker's terminal report.
const t985AlreadyAchievedReport = "T947 was already achieved by someone else. No further acceptance criteria are open. Nothing left to do on this seat."

const t985ExternalBlockReport = "My jevons-side slice is in. Remaining work is the claudia-side remainder, outside its mandate — a genuine external cross-repo block. Park until that lands."

const t985OwnerDecisionBlock = "```jevons\njevons: kind finish-report\njevons: target T765.1\njevons: status blocked\njevons: blocker owner-decision-on-design-gate\njevons: silent-ledger none\n```\nWaiting on an owner decision / design gate before I can continue."

// TestT985ClassifiesOwnWorkCompleteVsBlockedOnExternal is the hermetic
// oracle: classifying a worker's terminal/idle state (or a stop reason)
// assigns reap to own-work-complete and park to blocked-on-external.
func TestT985ClassifiesOwnWorkCompleteVsBlockedOnExternal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want WorkerIdleAction
	}{
		{"acceptance: already achieved by someone else, nothing left → reap", t985AlreadyAchievedReport, IdleActionReap},
		{"acceptance: cross-repo remainder outside mandate → park", t985ExternalBlockReport, IdleActionPark},
		{"acceptance: typed blocked finish-report (owner decision / design gate) → park", t985OwnerDecisionBlock, IdleActionPark},
		{"real park reason jv-t947-plan-token → reap (nothing left outranks the claudia-side mention)", t985ParkReasonT947, IdleActionReap},
		{"real park reason jv-t972-reap-scope → reap", t985ParkReasonT972, IdleActionReap},
		{"real park reason jv-t765.1-worker → park", t985ParkReasonT765_1, IdleActionPark},
		{"superseded → reap", "This worker is superseded; the leaf was folded into T990.", IdleActionReap},
		{"genuine finish shape still reaps", "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T985 PASS", IdleActionReap},
		{"achieved but still blocked on the rest → park (the seat owns a remainder)", "jevons-side achieved; blocked on claudia-po for the rest of the slice.", IdleActionPark},
		{"owner go-ahead wait → park", "Blocked on owner go-ahead for a daemon restart; the seat may resume later.", IdleActionPark},
		{"quoted nothing-left is not a claim (🎯T750)", "The PO wrote \"nothing left for this worker\" but two criteria are still open; continuing.", IdleActionKeep},
		{"mid-work is keep, not a guess", "still reading the tree", IdleActionKeep},
		{"stand-down for the night is keep (parks on the stop path)", "parked for the night", IdleActionKeep},
		{"empty is keep", "", IdleActionKeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyWorkerIdleDisposition(tc.text); got != tc.want {
				t.Fatalf("ClassifyWorkerIdleDisposition = %q, want %q\n%s", got, tc.want, tc.text)
			}
		})
	}
	// The gap this closes: the acceptance's already-achieved report has no
	// Done. finish shape, so T165 alone never reaped it.
	if LooksLikeFinishedWorkReport(t985AlreadyAchievedReport) {
		t.Fatal("the already-achieved specimen now looks like a finish report — the fixture drifted")
	}
}

// Envelope blocked outranks every prose phrase: a seat waiting on the owner
// is not reaped even if the payload also says nothing is left (🎯T938).
func TestT985BlockedEnvelopeOutranksNothingLeft(t *testing.T) {
	t.Parallel()
	text := "```jevons\njevons: kind finish-report\njevons: target T947\njevons: status blocked\njevons: blocker owner-go-ahead\njevons: silent-ledger none\n```\nNothing left on my side; waiting on owner go-ahead to activate."
	if got := ClassifyWorkerIdleDisposition(text); got != IdleActionPark {
		t.Fatalf("blocked+nothing-left classified %q, want park", got)
	}
}

// LooksLikeOwnWorkCompleteReport carries the finish-path vetoes: an ask, a
// forward-looking plan or a report awaiting the overseer is not a reap.
func TestT985OwnWorkCompleteReportVetoes(t *testing.T) {
	t.Parallel()
	if !LooksLikeOwnWorkCompleteReport(t985AlreadyAchievedReport) {
		t.Fatal("already-achieved report must read as own-work-complete")
	}
	for name, text := range map[string]string{
		"forward plan":     "Nothing left on the scout. Next step: wire the stop handler.",
		"local sub-step":   "Nothing left on the scout; implementing now — the stop handler first.",
		"asks a decision":  "Nothing left for this seat unless you want the docs slice too — should I take it?",
		"scout envelope":   "```jevons\njevons: kind scout-report\njevons: target T985\njevons: silent-ledger none\n```\nNothing left to scout.",
		"external block":   t985ExternalBlockReport,
		"blocked envelope": t985OwnerDecisionBlock,
		"empty":            "",
	} {
		if LooksLikeOwnWorkCompleteReport(text) {
			t.Errorf("%s: read as own-work-complete: %q", name, text)
		}
	}
}

// ShouldAutoReapDoneWorkAgent reaps the already-achieved specimen (the
// notify-path half of T985) and keeps a genuine external block.
func TestT985ShouldAutoReapOwnWorkCompleteNotExternalBlock(t *testing.T) {
	t.Parallel()
	reg := t439Registry(t, "jv-t985-reap")
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t985-reap", t985AlreadyAchievedReport, nil)
	if !ok {
		t.Fatalf("already-achieved did not reap (%s)", reason)
	}
	if reason != ownWorkCompleteReapReason {
		t.Fatalf("reap reason = %q, want %s", reason, ownWorkCompleteReapReason)
	}

	reg2 := t439Registry(t, "jv-t985-park")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg2, "jv-t985-park", t985ExternalBlockReport, nil); ok {
		t.Fatalf("external block reaped as %s", reason)
	}
	reg3 := t439Registry(t, "jv-t985-blocked")
	if ok, reason := ShouldAutoReapDoneWorkAgent(reg3, "jv-t985-blocked", t985OwnerDecisionBlock, nil); ok || reason != IdleSkipBlockedOnOwner {
		t.Fatalf("typed blocked reap = %v/%s, want false/%s", ok, reason, IdleSkipBlockedOnOwner)
	}
}

func TestT985MaybeReapOnAlreadyAchievedReport(t *testing.T) {
	t.Parallel()
	reg := t439Registry(t, "jv-t985-sink")
	s := &Server{registry: reg}
	s.maybeReapDoneWorkAgent("jv-t985-sink", t985AlreadyAchievedReport)
	if reg.Def("jv-t985-sink") != nil {
		t.Fatal("already-achieved terminal report must auto-reap (notify path)")
	}
}

// ClassifyStopDisposition is the pure half of the stop path: the caller's
// reason outranks the seat's stored report; a silent reason falls back to
// it; durable roles, descendants and owned uncommitted scope pin park.
func TestT985ClassifyStopDisposition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                                      string
		explicit, reason, stored                  string
		durable, hasDescendants, ownedUncommitted bool
		want                                      WorkerIdleAction
	}{
		{name: "T947 reason → reap", reason: t985ParkReasonT947, want: IdleActionReap},
		{name: "T972 reason → reap", reason: t985ParkReasonT972, want: IdleActionReap},
		{name: "T765.1 reason → park", reason: t985ParkReasonT765_1, want: IdleActionPark},
		{name: "silent reason, stored already-achieved → reap", stored: t985AlreadyAchievedReport, want: IdleActionReap},
		{name: "silent reason, stored blocked → park", stored: t985OwnerDecisionBlock, want: IdleActionPark},
		{name: "silent reason, nothing stored → park", want: IdleActionPark},
		{name: "unclassifiable reason → park", reason: "parked for the night", want: IdleActionPark},
		{name: "reason outranks stored: blocked reason over finished report → park", reason: t985ParkReasonT765_1, stored: t985AlreadyAchievedReport, want: IdleActionPark},
		{name: "explicit reap", explicit: "reap", reason: "parked for the night", want: IdleActionReap},
		{name: "explicit park beats a finished reason", explicit: "park", reason: t985ParkReasonT972, want: IdleActionPark},
		{name: "durable role parks even on explicit reap", explicit: "reap", reason: t985ParkReasonT972, durable: true, want: IdleActionPark},
		{name: "descendants park", reason: t985ParkReasonT972, hasDescendants: true, want: IdleActionPark},
		{name: "owned uncommitted scope parks (🎯T972)", reason: t985ParkReasonT972, ownedUncommitted: true, want: IdleActionPark},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyStopDisposition(tc.explicit, tc.reason, tc.stored, tc.durable, tc.hasDescendants, tc.ownedUncommitted)
			if got.Action != tc.want {
				t.Fatalf("action = %q (%s), want %q", got.Action, got.Why, tc.want)
			}
			if strings.TrimSpace(got.Why) == "" {
				t.Fatal("a disposition names what decided it")
			}
		})
	}
}

// t985Hub is a server with a registry, an intent store and a removal
// account, as the daemon wires them, so a stop-to-reap is observable on the
// registry, the intent record and the accounted removal.
func t985Hub(t *testing.T, names ...string) (*Server, *claudia.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range names {
		if err := reg.Register(claudia.AgentDef{
			Name: n, WorkDir: t.TempDir(), SessionID: "s-" + n, Purpose: claudia.PurposeWork,
			Parent: "jevons-po", TargetID: "T985", Materialized: true, Provider: "grok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	if err := s.OpenFleetIntent(filepath.Join(dir, "state")); err != nil {
		t.Fatal(err)
	}
	return s, reg
}

func t985Stop(t *testing.T, s *Server, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := s.handleAgentStop(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("stop %v: %s", args["name"], toolText(res))
	}
	return toolText(res)
}

// handleAgentStop converts an own-work-complete stop into a reap (the two
// 2026-09-30 specimens), still parks a genuine external block (the third),
// and still parks a silent stop (the T165 residual).
func TestT985StopReapsOwnWorkCompleteAndParksExternalBlock(t *testing.T) {
	t.Parallel()
	const t947, t972, t765, idle = "jv-t947-plan-token", "jv-t972-reap-scope", "jv-t765.1-worker", "jv-t985-idle"
	s, reg := t985Hub(t, t947, t972, t765, idle)

	for name, reason := range map[string]string{t947: t985ParkReasonT947, t972: t985ParkReasonT972} {
		out := t985Stop(t, s, map[string]any{"name": name, "actor": "jevons-po", "reason": reason})
		if !strings.Contains(out, "reaped") || strings.Contains(out, "stopped and parked") {
			t.Fatalf("%s: own-work-complete stop parked instead of reaping: %s", name, out)
		}
		if reg.Def(name) != nil {
			t.Fatalf("%s still registered after an own-work-complete stop — T985 wants reap", name)
		}
		rec, ok := s.fleetIntent().Agents[name]
		if !ok || rec.State != fleetintent.Reaped {
			t.Fatalf("%s intent = %+v, want reaped", name, rec)
		}
		if rec.StopReason != fleetlog.ReasonReapStop {
			t.Fatalf("%s stop_reason = %q, want %s", name, rec.StopReason, fleetlog.ReasonReapStop)
		}
	}

	out := t985Stop(t, s, map[string]any{"name": t765, "actor": "jevons-po", "reason": t985ParkReasonT765_1})
	if !strings.Contains(out, "stopped and parked") {
		t.Fatalf("external-block stop did not park: %s", out)
	}
	if reg.Def(t765) == nil {
		t.Fatal("external-block worker was reaped — parking is reserved for this shape")
	}
	if rec := s.fleetIntent().Agents[t765]; rec.State != fleetintent.Parked {
		t.Fatalf("%s intent = %+v, want parked", t765, rec)
	}

	out = t985Stop(t, s, map[string]any{"name": idle, "actor": "jevons-po"})
	if !strings.Contains(out, "stopped and parked") {
		t.Fatalf("silent stop did not park (T165 residual): %s", out)
	}
	if reg.Def(idle) == nil {
		t.Fatal("silent stop deregistered — the T165 residual is park, not reap")
	}
}

// An explicit disposition fixes the outcome either way.
func TestT985StopExplicitDisposition(t *testing.T) {
	t.Parallel()
	const parkMe, reapMe = "jv-t985-park-me", "jv-t985-reap-me"
	s, reg := t985Hub(t, parkMe, reapMe)
	out := t985Stop(t, s, map[string]any{"name": parkMe, "actor": "jevons-po", "reason": t985ParkReasonT972, "disposition": "park"})
	if !strings.Contains(out, "stopped and parked") || reg.Def(parkMe) == nil {
		t.Fatalf("disposition=park did not park: %s", out)
	}
	out = t985Stop(t, s, map[string]any{"name": reapMe, "actor": "jevons-po", "reason": "parked for the night", "disposition": "reap"})
	if !strings.Contains(out, "reaped") || reg.Def(reapMe) != nil {
		t.Fatalf("disposition=reap did not reap: %s", out)
	}
}

// Durable roles stay: a PO stopped with a finished-work reason parks.
func TestT985DurablePOIsNotReapedOnFullyAchievedStop(t *testing.T) {
	t.Parallel()
	s, reg := t985Hub(t)
	out := t985Stop(t, s, map[string]any{"name": "jevons-po", "actor": "jevons", "reason": t985ParkReasonT972})
	if !strings.Contains(out, "stopped and parked") {
		t.Fatalf("PO stop: %s", out)
	}
	if reg.Def("jevons-po") == nil {
		t.Fatal("PO was reaped — durable roles stay")
	}
}

// A seat with live descendants parks: reaping it would take its children.
func TestT985StopWithDescendantsParks(t *testing.T) {
	t.Parallel()
	const boss, child = "jv-t985-boss", "jv-t985-child"
	s, reg := t985Hub(t, boss)
	if err := reg.Register(claudia.AgentDef{
		Name: child, WorkDir: t.TempDir(), SessionID: "s-child", Purpose: claudia.PurposeWork,
		Parent: boss, Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	out := t985Stop(t, s, map[string]any{"name": boss, "actor": "jevons-po", "reason": t985ParkReasonT972})
	if !strings.Contains(out, "stopped and parked") || !strings.Contains(out, "descendants") {
		t.Fatalf("boss stop: %s", out)
	}
	if reg.Def(boss) == nil || reg.Def(child) == nil {
		t.Fatal("a stop with descendants must not remove anyone")
	}
}
