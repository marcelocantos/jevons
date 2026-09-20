// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// 🎯T709 (Jevons secondary) — Claude product-owner seats (ge-po) died
// seconds after jevons_agent_start. Primary hole is Claudia
// WaitReady / MatchStartupMenu (claudia 🎯T87, marcelocantos/claudia#57):
// the loop already auto-Enters resume menus and comments a follow-on
// trust-folder screen, but the matcher does not see
// "Quick safety check: Is this a project you created or one you trust",
// so the pane stays no_composer.
//
// Jevons then classified that as generic startup_stall, T387 treated
// the opening brief as proven-undelivered, T433 retired the row as
// unbriefed_seat, and POST /api/agents/ge-po/send returned reaped_held.
// These oracles pin the host half: classify the frame as workspace_trust
// and never silent-reap. They fail on the pre-fix tree.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/claudetrust"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/mark3labs/mcp-go/mcp"
)

// Live Colossus 2026-09-20 start error for ge-po (owner-formatted).
const t709GePoStartErr = "start prompt not delivered to \"ge-po\": send failed: " +
	"Agent CLI stalled on startup (startup_stall / no_composer): no idle input box within the ready timeout — not a cloud outage; the seat is retried. Last frame: " +
	"Quick safety check: Is this a project you created or one you trust? (Like your\n" +
	" own code, a well-known open source project, or work from your team). If not,\n" +
	" take a moment to review what's in this folder first.\n\n" +
	" Claude Code'll be able to read, edit, and execute files here.\n\n" +
	" ⚠ This folder pre-approves 10 tool permissions in .claude/settings.local.json:"

func TestT709LiveFrameIsWorkspaceTrustNotGenericStall(t *testing.T) {
	t.Parallel()
	if !agenterr.IsWorkspaceTrust(t709GePoStartErr) {
		t.Fatal("live ge-po last frame must classify as workspace_trust")
	}
	if !WorkspaceTrustBlocksReady(fmt.Errorf("%s", t709GePoStartErr)) {
		t.Fatal("teardown fork must recognise the live start error")
	}
	if got := agenterr.ClassifyText(t709GePoStartErr); got != agenterr.ClassStartupStall {
		t.Fatalf("class=%q want startup_stall", got)
	}
	_, msg := agenterr.ClassifyAndFormat(fmt.Errorf("%s", t709GePoStartErr))
	if !strings.Contains(msg, "workspace_trust") {
		t.Fatalf("owner copy must name workspace_trust, not a silent retry: %q", msg)
	}
	if strings.Contains(msg, "the seat is retried") && !strings.Contains(msg, "keeps the seat") {
		t.Fatalf("owner copy still claims a retry-then-reap: %q", msg)
	}
	if !strings.Contains(msg, "not a silent reap") && !strings.Contains(msg, "keeps the seat registered") {
		t.Fatalf("owner copy must name the recoverable keep-seat action: %q", msg)
	}
}

func t709Harness(t *testing.T, name string) (*Server, *claudia.Registry, *fakeSender, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	wd := filepath.Join(dir, "ge")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: wd, SessionID: "s-" + name,
		Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork,
		Parent: "jevons",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true, sendErr: fmt.Errorf("%s", t709GePoStartErr)}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetRemovalAccount(fleetlog.New(nil))
	if err := s.OpenFleetIntent(dir); err != nil {
		t.Fatal(err)
	}
	s.SetSendQueueDir(dir)
	s.SetClaudeTrustConfig(filepath.Join(dir, ".claude.json"))
	s.SetSenderResolver(func(got string) (agentSender, bool, error) {
		if got != name {
			return nil, false, fmt.Errorf("unexpected send to %q", got)
		}
		return fs, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, Durable: true, TranscriptAbsent: true,
		Detail: "no transcript was ever created",
	}))
	return s, reg, fs, wd
}

// The T387/T433 incident: a fresh mint whose opening brief dies on the
// trust dialog must keep the registry row and must not stamp reaped.
func TestT709TrustStallKeepsSeatNotReapedHeld(t *testing.T) {
	const name = "ge-po"
	s, reg, _, wd := t709Harness(t, name)

	err := s.deliverStartPrompt(name, "You are ge-po.")
	if err == nil {
		t.Fatal("trust-blocked brief must still fail confirmation")
	}
	if BriefInFlight(err) {
		t.Fatalf("trust stall is not a queued brief: %v", err)
	}
	released, kept := s.startBriefFailureTeardown(name, false, err)
	if released || kept {
		t.Fatalf("teardown = released=%v kept=%v, want neither (held, not T518)", released, kept)
	}
	if def := reg.Def(name); def == nil {
		t.Fatal("fresh mint retired as unbriefed_seat — the 🎯T709 incident")
	}
	if _, ok := LookupReapedRecord(s.fleetIntent(), name); ok {
		t.Fatal("unbriefed_seat removal stamped reaped — send would return reaped_held")
	}
	data, err := os.ReadFile(s.claudeTrustConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(data, wd) {
		t.Fatalf("trust stall must write hasTrustDialogAccepted: %s", data)
	}
}

// mint→send→alive: after the trust stall keeps the seat, a later send is
// ordinary delivery, never reaped_held.
func TestT709MintSendAliveAfterTrustStall(t *testing.T) {
	const name = "ge-po"
	s, reg, fs, _ := t709Harness(t, name)

	err := s.deliverStartPrompt(name, "You are ge-po.")
	if err == nil {
		t.Fatal("fixture: trust stall must fail the opening brief")
	}
	if released, _ := s.startBriefFailureTeardown(name, false, err); released {
		t.Fatal("fixture: seat was reaped")
	}
	if reg.Def(name) == nil {
		t.Fatal("fixture: seat missing")
	}

	fs.sendErr = nil
	fs.inFlight = false
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))
	res, serr := s.deliverByName(name, "ping after mint", OriginAgent, false)
	if serr != nil {
		t.Fatalf("send to kept seat: %v", serr)
	}
	if res.Status == StatusReapedHeld {
		t.Fatalf("send status=%q — the ge-po reaped_held incident", res.Status)
	}
	if res.Status != "sent" && res.Status != "rehydrated_sent" && res.Status != "interrupted_sent" {
		// deliverToSender may confirm via the witness; queued is still alive.
		if res.Status == "" {
			t.Fatalf("empty send status: %+v", res)
		}
	}
	if _, ok := LookupReapedRecord(s.fleetIntent(), name); ok {
		t.Fatal("reaped intent appeared after send")
	}
	if len(fs.sent) == 0 {
		t.Fatal("kept seat must accept a later send")
	}
}

// handleAgentStart of a Claude PO: Launch pre-accepts trust, a trust-dialog
// brief fails loudly, the seat stays sendable.
func TestT709HandleAgentStartKeepsClaudePO(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	wd := filepath.Join(dir, "squz-ge")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	var overseer []string
	fs := &fakeSender{alive: true, sendErr: fmt.Errorf("%s", t709GePoStartErr)}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetRemovalAccount(fleetlog.New(nil))
	if err := s.OpenFleetIntent(dir); err != nil {
		t.Fatal(err)
	}
	s.SetSendQueueDir(dir)
	trustPath := filepath.Join(dir, ".claude.json")
	s.SetClaudeTrustConfig(trustPath)
	s.SetOverseerDeliver(func(text string, _ SendOrigin) error {
		overseer = append(overseer, text)
		return nil
	})
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		return nil, nil
	}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "ge-po" {
			return nil, false, fmt.Errorf("unexpected %s", name)
		}
		return fs, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, Durable: true, TranscriptAbsent: true,
		Detail: "no transcript",
	}))

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "ge-po",
		"workdir":  wd,
		"provider": string(claudia.ProviderClaude),
		"parent":   "jevons",
		"purpose":  "work",
		"role":     "product-owner",
		"prompt":   "You are the product owner for squz/ge.",
	}
	res, err := s.handleAgentStart(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !res.IsError {
		t.Fatal("trust-blocked start must return an MCP error (brief did not land)")
	}
	if !strings.Contains(toolText(res), "Quick safety check") && !strings.Contains(toolText(res), "workspace_trust") {
		t.Fatalf("start error must name the trust dialog: %s", toolText(res))
	}
	if reg.Def("ge-po") == nil {
		t.Fatal("handleAgentStart reaped ge-po — the 🎯T709 incident")
	}
	if _, ok := LookupReapedRecord(s.fleetIntent(), "ge-po"); ok {
		t.Fatal("start stamped reaped_held")
	}
	data, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(data, wd) {
		t.Fatalf("Launch must pre-accept owner workdir trust: %s", data)
	}
	heard := strings.Join(overseer, "\n")
	if !strings.Contains(heard, "T709") || !strings.Contains(heard, "T87") ||
		!strings.Contains(heard, "wrong outcome") {
		t.Fatalf("overseer must hear the recoverable T709/T87 action, got %q", heard)
	}

	fs.sendErr = nil
	fs.inFlight = false
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))
	send, serr := s.deliverByName("ge-po", "still here", OriginAgent, false)
	if serr != nil {
		t.Fatalf("send after kept start: %v", serr)
	}
	if send.Status == StatusReapedHeld {
		t.Fatal("send after kept start was reaped_held")
	}
}

// Control: a generic no_composer stall (no trust dialog) still reaps a
// freshly minted row — T387's phantom-seat teardown is unchanged.
func TestT709GenericNoComposerStillReaps(t *testing.T) {
	const name = "jv-t709-generic"
	s, reg, fs, _ := t709Harness(t, name)
	fs.sendErr = fmt.Errorf("claude not ready (no_composer): no idle input box after 30s; last frame:\n● Rebuilding…")

	err := s.deliverStartPrompt(name, "Execute 🎯T709 control.")
	if err == nil {
		t.Fatal("generic stall must fail confirmation")
	}
	if WorkspaceTrustBlocksReady(err) {
		t.Fatalf("generic no_composer classified as trust: %v", err)
	}
	released, kept := s.startBriefFailureTeardown(name, false, err)
	if !released || kept {
		t.Fatalf("teardown = released=%v kept=%v, want released (T387)", released, kept)
	}
	if reg.Def(name) != nil {
		t.Fatal("generic stall must still reap a fresh unbriefed row")
	}
}

func TestT709LaunchAgentPreAcceptsClaudeTrust(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	wd := filepath.Join(dir, "ge")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "ge-po", WorkDir: wd, SessionID: "s1",
		Provider: claudia.ProviderClaude,
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	trustPath := filepath.Join(dir, ".claude.json")
	s.SetClaudeTrustConfig(trustPath)
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		return nil, nil
	}
	if _, err := s.launchAgent(context.Background(), "ge-po"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(data, wd) {
		t.Fatalf("launchAgent must pre-accept trust: %s", data)
	}
}

func TestT709FormatNoticeIsRecoverableNotReap(t *testing.T) {
	t.Parallel()
	got := FormatWorkspaceTrustNotice("ge-po", "/tmp/ge", t709GePoStartErr)
	for _, want := range []string{"🎯T709", "ge-po", "did not reap", "Remint or send", "🎯T87"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "No briefless seat was retained") {
		t.Fatal("must not reuse the T433 reap notice")
	}
}

func TestT709ReapedHeldRequiresActualReap(t *testing.T) {
	// Sanity: the reaped_held address is still what a real T433 remove
	// produces — this file's other tests are only meaningful if that
	// stamp is what send reads.
	s, reg, _, _ := t709Harness(t, "ghost-po")
	if _, err := s.RemovalAccount().Remove(reg, "ghost-po", fleetlog.Removal{
		Reason: fleetlog.ReasonUnbriefedSeat,
		Detail: "retired a seat whose opening brief never landed (🎯T433)",
	}); err != nil {
		t.Fatal(err)
	}
	s.MarkAgentReaped("ghost-po", "product:unbriefed_seat", "retired a seat whose opening brief never landed (🎯T433)")
	if _, ok := LookupReapedRecord(s.fleetIntent(), "ghost-po"); !ok {
		t.Fatal("fixture: accounted unbriefed remove must stamp reaped")
	}
	res, err := s.deliverByName("ghost-po", "hello", OriginAgent, false)
	if err != nil {
		t.Fatalf("reaped send is not a transport error: %v", err)
	}
	if res.Status != StatusReapedHeld {
		t.Fatalf("status=%q want %s", res.Status, StatusReapedHeld)
	}
	_ = fleetintent.Reaped
}
