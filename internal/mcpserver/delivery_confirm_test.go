// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// ── pure oracles (must FAIL pre-fix without delivery_confirm.go) ─────────

func TestClassifyAgentListStatusNeverBriefed(t *testing.T) {
	t.Parallel()
	if got := ClassifyAgentListStatus(true, false, false); got != AgentStatusNeverBriefed {
		t.Fatalf("live zero-turn: got %q want %s", got, AgentStatusNeverBriefed)
	}
	if got := ClassifyAgentListStatus(true, true, false); got != AgentStatusRunning {
		t.Fatalf("turn began: got %q", got)
	}
	if got := ClassifyAgentListStatus(true, false, true); got != AgentStatusRunning {
		t.Fatalf("materialized: got %q", got)
	}
	if got := ClassifyAgentListStatus(false, true, true); got != AgentStatusStopped {
		t.Fatalf("dead: got %q", got)
	}
}

func TestConfirmSendBeganTurn(t *testing.T) {
	t.Parallel()
	if err := ConfirmSendBeganTurn("sent", nil); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmSendBeganTurn("rehydrated_sent", nil); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmSendBeganTurn("queued", nil); err == nil {
		t.Fatal("queued must not count as turn began")
	}
	if err := ConfirmSendBeganTurn("sent", fmt.Errorf("turn not submitted")); err == nil {
		t.Fatal("send error must surface")
	}
}

// 🎯T305 (a): start-with-prompt delivers via send path and marks turn began.
func TestDeliverStartPromptMarksTurnBegan(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t305-a", WorkDir: dir, SessionID: "s1",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "jv-t305-a" {
			return nil, false, fmt.Errorf("unknown %s", name)
		}
		return fs, false, nil
	})

	prompt := "Execute 🎯T305 — fix both delivery failures."
	if err := s.deliverStartPrompt("jv-t305-a", prompt); err != nil {
		t.Fatalf("deliverStartPrompt: %v", err)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sends=%d want 1; got %v", len(fs.sent), fs.sent)
	}
	// Standing brief prepended on first deliver.
	if !strings.Contains(fs.sent[0], "Jevons fleet standing brief") {
		t.Fatal("start prompt must carry standing fleet brief on first deliver")
	}
	if !strings.Contains(fs.sent[0], "🎯T305") {
		t.Fatal("start prompt body missing")
	}
	if !s.agentHasTurnBegan("jv-t305-a") {
		t.Fatal("turn began not marked after start prompt")
	}
	if got := ClassifyAgentListStatus(true, s.agentHasTurnBegan("jv-t305-a"), false); got != AgentStatusRunning {
		t.Fatalf("status after start prompt: %s", got)
	}
}

// 🎯T305 (b): multi-KB send (above paste-block threshold) still marks turn
// began when the sender succeeds (claudia press-through is unit-tested in
// tmuxagent; here we assert the host marks delivery).
func TestDeliverLargeSendMarksTurnBegan(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t305-b", WorkDir: dir, SessionID: "s2",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		return fs, false, nil
	})

	// Above claudia pasteBlockThreshold (400) and multi-line — same class
	// as fleet standing brief + mission brief.
	large := strings.Repeat("mission line\n", 80) // ~1040 bytes
	if len(large) < 400 {
		t.Fatalf("fixture too small: %d", len(large))
	}
	res, err := deliverToSender(s, "jv-t305-b", large, false, fs, false)
	if err != nil {
		t.Fatalf("deliverToSender: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%s", res.Status)
	}
	if !s.agentHasTurnBegan("jv-t305-b") {
		t.Fatal("large send must mark turn began")
	}
	if len(fs.sent) != 1 || fs.sent[0] != large {
		t.Fatalf("sent payload wrong: len=%d", len(fs.sent[0]))
	}
}

// 🎯T305 (c): failed submission errors and leaves no phantom running record
// (start path stops the process; list status never_briefed / stopped).
func TestDeliverStartPromptFailureNoPhantomRunning(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t305-c", WorkDir: dir, SessionID: "s3",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "claude",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true}
	fs.sendErr = fmt.Errorf("turn not submitted: paste block still visible")
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		return fs, false, nil
	})

	err = s.deliverStartPrompt("jv-t305-c", "please do the work\n"+strings.Repeat("x", 500))
	if err == nil {
		t.Fatal("want start prompt delivery error")
	}
	if !strings.Contains(err.Error(), "start prompt not delivered") {
		t.Fatalf("error should name start prompt: %v", err)
	}
	if s.agentHasTurnBegan("jv-t305-c") {
		t.Fatal("failed delivery must not mark turn began")
	}
	// Still registered but not "running" for supervisors.
	if got := ClassifyAgentListStatus(true, s.agentHasTurnBegan("jv-t305-c"), false); got != AgentStatusNeverBriefed {
		t.Fatalf("failed deliver while process up: %s want never_briefed", got)
	}
}

// agent_list surface shows never_briefed for live zero-turn seats.
func TestAgentListShowsNeverBriefed(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t305-list", WorkDir: dir, SessionID: "s4",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "claude",
		// Materialized false = zero durable turns.
	}); err != nil {
		t.Fatal(err)
	}
	// No live process → stopped (never_briefed only when alive).
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	res, err := s.handleAgentList(nil, mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, "jv-t305-list") {
		t.Fatalf("list missing agent: %s", text)
	}
	if !strings.Contains(text, AgentStatusStopped) {
		t.Fatalf("stopped agent should show stopped: %s", text)
	}

	// Simulate: process up, no turn — pure status column.
	status := ClassifyAgentListStatus(true, false, false)
	if status != AgentStatusNeverBriefed {
		t.Fatalf("alive zero-turn status=%s", status)
	}
}

