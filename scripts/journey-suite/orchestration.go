// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/spool"
)

// Orchestration journeys (MCP-direct against the isolated daemon).
//
// Simple: tool surface + overseer registry presence.
// Moderate: two fleet agents on one workdir (T86 live) + thread
// spawn → direct → remove.
// Shell tools: worker must execute run_terminal_command unattended (T97).

// jOverseerToolsAttached proves the overseer's own client can reach jevons
// tools under the selected provider (🎯T282). J6 checks the producer side
// (jevonsd serves the tools); this checks the consumer side. Isolates
// attach via AgentDef.MCPServers only — they do not write state_dir/mcp.
func (s *suite) jOverseerToolsAttached() error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}
	token := "journey-tool-effect-" + uuid.NewString()
	before, err := s.journeyIdeas(ctx)
	if err != nil {
		return err
	}
	for _, record := range before {
		if record.Text == token {
			return fmt.Errorf("tool effect exists before the request")
		}
	}
	prompt := "Use the attached jevons_idea_capture MCP tool exactly once with text " +
		fmt.Sprintf("%q", token) + " and source mcp. This is a disposable verification record; do not triage it, spawn work, use a shell, or call the HTTP API. Do not narrate or announce your actions. After the tool succeeds, your only visible reply must be exactly two words separated by one space: the text token, then the idea ID returned by the capture tool."
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": prompt}); err != nil {
		return err
	}
	sawCall := false
	var returnedID string
	err = waitOwnerMuxReplyMatching(ctx, frames, prompt, token, func(text string) bool {
		// Canonical assistant snapshots can include pre-tool commentary in the
		// same row as the final answer. Require one exact terminal proof suffix;
		// API/disk identity decides success, not obedience to a prose-style rule.
		_, id, ok := strings.Cut(text, token+" ")
		if !ok || strings.Count(text, token) != 1 || id == "" || strings.ContainsAny(id, " \t\r\n") {
			return false
		}
		returnedID = id
		return true
	}, func(frame ownerMuxFrame) {
		if matchingIdeaToolCall(frame, token) {
			sawCall = true
		}
	})
	if err != nil {
		return fmt.Errorf("tool turn: %w", err)
	}
	if !sawCall {
		return fmt.Errorf("completed reply had no matching jevons_idea_capture MCP call")
	}
	after, err := s.journeyIdeas(ctx)
	if err != nil {
		return err
	}
	id, err := uniqueIdeaEffect(after, token)
	if err != nil {
		return fmt.Errorf("tool effect API: %w", err)
	}
	if returnedID != id {
		return fmt.Errorf("tool reply identity %q differs from captured identity %q", returnedID, id)
	}
	data, err := os.ReadFile(filepath.Join(s.stateDir, "ideas.json"))
	if err != nil {
		return fmt.Errorf("durable tool effect: %w", err)
	}
	var disk struct {
		Ideas []journeyIdeaRecord `json:"ideas"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		return err
	}
	durableID, err := uniqueIdeaEffect(disk.Ideas, token)
	if err != nil || durableID != id {
		return fmt.Errorf("tool effect not durable under its API identity: id=%q err=%v", durableID, err)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	return queueJourneyProvider(logs, overseerName, string(s.provider))
}

func (s *suite) jMCPToolSurface() error {
	res, err := s.mcp("tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var out struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return fmt.Errorf("decode tools/list: %w", err)
	}
	have := map[string]bool{}
	for _, t := range out.Tools {
		have[t.Name] = true
	}
	required := []string{
		"jevons_agent_list",
		"jevons_agent_start",
		"jevons_agent_stop",
		"jevons_agent_kill",
		"jevons_agent_send",
		"jevons_thread_list",
		"jevons_thread_spawn",
		"jevons_thread_direct",
		"jevons_thread_remove",
		"jevons_mcp_reconnect",
	}
	var missing []string
	for _, n := range required {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing tools: %s", strings.Join(missing, ", "))
	}
	return nil
}

// jMCPReconnect exercises mid-session MCP re-attach (🎯T60 / 🎯T105.1).
// Calls jevons_mcp_reconnect against the live isolate — behavioral cycle
// via grok mcp disable/enable, not tools/list membership alone.
func (s *suite) jMCPReconnect() error {
	text, err := s.mcpText("jevons_mcp_reconnect", map[string]any{})
	combined := text
	if err != nil {
		combined = combined + " " + err.Error()
	}
	low := strings.ToLower(combined)
	// Empty config on a minimal isolate is a valid fail-closed path.
	if strings.Contains(low, "no mcp servers configured") ||
		strings.Contains(low, "nothing to reconnect") {
		return nil
	}
	// Non-Grok overseer: the tool must name its Grok-only control plane
	// rather than cycle a config the caller does not use (🎯T282).
	if strings.Contains(low, "grok control plane") {
		if s.provider == claudia.ProviderGrok {
			return fmt.Errorf("grok overseer refused its own control plane: %s", trim(combined, 200))
		}
		return nil
	}
	if err != nil && !strings.Contains(low, "ok") && !strings.Contains(low, "enable") {
		return fmt.Errorf("mcp reconnect: %v (%s)", err, trim(text, 160))
	}
	if !strings.Contains(low, "ok") &&
		!strings.Contains(low, "enable") &&
		!strings.Contains(low, "reconnect") {
		return fmt.Errorf("mcp reconnect unexpected report: %s", trim(combined, 200))
	}
	// Session not rotated: overseer still running under same name.
	agents, lerr := s.listAgentsHTTP()
	if lerr != nil {
		return lerr
	}
	for _, a := range agents {
		if a.Name == overseerName && a.Status == "running" {
			return nil
		}
	}
	return fmt.Errorf("after mcp reconnect, overseer not running in /api/agents")
}

func (s *suite) jOverseerInRegistry() error {
	agents, err := s.listAgentsHTTP()
	if err != nil {
		return err
	}
	var found bool
	for _, a := range agents {
		if a.Name == overseerName {
			found = true
			if a.Status != "running" {
				return fmt.Errorf("overseer status %q, want running", a.Status)
			}
		}
	}
	if !found {
		return fmt.Errorf("overseer %q not in /api/agents (%d agents)", overseerName, len(agents))
	}
	text, err := s.mcpText("jevons_agent_list", nil)
	if err != nil {
		return err
	}
	if !strings.Contains(text, overseerName) {
		return fmt.Errorf("agent_list missing overseer: %s", trim(text, 120))
	}
	return nil
}

// jTwoAgentsSameWorkdir starts two differently named fleet agents on
// one workdir and requires independent registration (distinct sessions,
// original names preserved) — live check of the T86 EnsureAgent fix.
func (s *suite) jTwoAgentsSameWorkdir() error {
	work := filepath.Join(s.stateDir, "fleet-shared")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	a, b := "jv-orch-a", "jv-orch-b"
	// Always try to stop leftovers if a prior crash left them.
	defer func() {
		_, _ = s.mcpText("jevons_agent_stop", map[string]any{"name": a})
		_, _ = s.mcpText("jevons_agent_stop", map[string]any{"name": b})
	}()

	startA, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": a, "workdir": work,
	})
	if err != nil {
		return fmt.Errorf("start %s: %w", a, err)
	}
	startB, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": b, "workdir": work,
	})
	if err != nil {
		return fmt.Errorf("start %s: %w", b, err)
	}
	sessA := extractSessionFragment(startA)
	sessB := extractSessionFragment(startB)
	if sessA == "" || sessB == "" {
		return fmt.Errorf("start ack missing session: a=%q b=%q", trim(startA, 80), trim(startB, 80))
	}
	if sessA == sessB {
		return fmt.Errorf("session fragments collided (workdir steal?): both %q", sessA)
	}

	list, err := s.mcpText("jevons_agent_list", nil)
	if err != nil {
		return err
	}
	if !strings.Contains(list, a) || !strings.Contains(list, b) {
		return fmt.Errorf("agent_list missing workers:\n%s", list)
	}
	// Original overseer still present under its name.
	if !strings.Contains(list, overseerName) {
		return fmt.Errorf("overseer missing after dual start:\n%s", list)
	}

	agents, err := s.listAgentsHTTP()
	if err != nil {
		return err
	}
	byName := map[string]AgentInfo{}
	for _, ag := range agents {
		byName[ag.Name] = ag
	}
	for _, name := range []string{a, b, overseerName} {
		ag, ok := byName[name]
		if !ok {
			return fmt.Errorf("/api/agents missing %q", name)
		}
		if ag.Status != "running" {
			return fmt.Errorf("%q status %q, want running", name, ag.Status)
		}
	}
	if byName[a].WorkDir != work || byName[b].WorkDir != work {
		return fmt.Errorf("workdir mismatch: a=%q b=%q want %q",
			byName[a].WorkDir, byName[b].WorkDir, work)
	}

	// 🎯T625: two distinct sessions on one workdir is only evidence about
	// this backend if this backend is the one that started them.
	if err := s.assertLaunchedOn(a, b); err != nil {
		return err
	}

	// Stop both; list should drop running status or remove them depending
	// on registry semantics — at least stop must succeed.
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{"name": a}); err != nil {
		return fmt.Errorf("stop %s: %w", a, err)
	}
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{"name": b}); err != nil {
		return fmt.Errorf("stop %s: %w", b, err)
	}
	return nil
}

// jPOWorkerLineageFanout is a multi-slice control-plane path (🎯T108):
// overseer tools start a PO (boss) and a worker under that PO, assert
// /api/agents lineage + completeness, first send injects T104 standing
// brief (ack text), stop leaves registry honest. This is the same MCP
// surface the owner chat overseer uses — not a substitute for typing in
// the browser, but the product spawn/direct path under live Grok.
func (s *suite) jPOWorkerLineageFanout() error {
	work := filepath.Join(s.stateDir, "fanout-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	po, worker := "jv-fanout-po", "jv-fanout-worker"
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": worker, "actor": overseerName})
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": po, "actor": overseerName})
	}()

	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": po, "workdir": work, "actor": overseerName, "parent": overseerName,
	}); err != nil {
		return fmt.Errorf("start po: %w", err)
	}
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": worker, "workdir": work, "actor": po, "parent": po,
	}); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}

	agents, err := s.listAgentsHTTP()
	if err != nil {
		return err
	}
	by := map[string]AgentInfo{}
	for _, a := range agents {
		by[a.Name] = a
	}
	for _, name := range []string{po, worker, overseerName} {
		if _, ok := by[name]; !ok {
			return fmt.Errorf("fan-out list missing %q", name)
		}
	}
	if by[po].Parent != overseerName && by[po].Parent != "" {
		// parent may be empty if not persisted — require worker→po at minimum
	}
	if by[worker].Parent != po {
		return fmt.Errorf("worker parent=%q want %q (who-started-whom)", by[worker].Parent, po)
	}
	if by[po].Status != "running" || by[worker].Status != "running" {
		return fmt.Errorf("want both running: po=%s worker=%s", by[po].Status, by[worker].Status)
	}

	// 🎯T625: lineage is a claim about two agents this backend launched.
	if err := s.assertLaunchedOn(po, worker); err != nil {
		return err
	}

	// First send must inject T104 standing brief (shipped path, not persona grep).
	// 🎯T321: actor names the caller so lineage auth runs on the MCP path.
	ack, err := s.mcpText("jevons_agent_send", map[string]any{
		"name":  worker,
		"text":  "Reply with exactly: FANOUT_PONG and do not open a PR.",
		"actor": "jevons",
	})
	if err != nil {
		return fmt.Errorf("worker send: %w", err)
	}
	if !strings.Contains(ack, "standing fleet brief") && !strings.Contains(ack, "T104") {
		return fmt.Errorf("first send ack missing standing brief note: %s", trim(ack, 160))
	}

	// Integrator slice: second agent (po) also gets brief on first send.
	ackPO, err := s.mcpText("jevons_agent_send", map[string]any{
		"name":  po,
		"text":  "Coordinate only; local commits only.",
		"actor": "jevons",
	})
	if err != nil {
		return fmt.Errorf("po send: %w", err)
	}
	if !strings.Contains(ackPO, "standing fleet brief") && !strings.Contains(ackPO, "T104") {
		return fmt.Errorf("po first send missing brief note: %s", trim(ackPO, 160))
	}

	// Stop worker — must remain listed as stopped (not vanished without kill).
	//
	// 🎯T664: the send above may still be undecided (delivered_unconfirmed) or
	// its turn in flight, and the daemon then refuses an un-forced stop. Try
	// the plain stop first: it either succeeds (delivery decided) or must be
	// refused with the T664 text naming the deciding check. Only then stop
	// with force=true and a reason. Any other stop error still fails the
	// journey, and the refusal text is asserted, so a guard that vanished or
	// changed shape shows up here rather than being papered over by force.
	if stopOut, err := s.mcpText("jevons_agent_stop", map[string]any{"name": worker}); err != nil {
		// err carries a trimmed copy; the full refusal text is the returned string.
		msg := stopOut
		if !strings.Contains(msg, "refusing to stop") || !strings.Contains(msg, "T664") ||
			!strings.Contains(msg, "jevons_transcript_read") {
			return fmt.Errorf("stop worker: %w", err)
		}
		if _, err := s.mcpText("jevons_agent_stop", map[string]any{
			"name": worker, "force": true,
			"reason": "journey teardown: fan-out assertions are complete, delivery verdict irrelevant",
		}); err != nil {
			return fmt.Errorf("forced stop worker: %w", err)
		}
	}
	agents2, err := s.listAgentsHTTP()
	if err != nil {
		return err
	}
	var workerRow *AgentInfo
	for i := range agents2 {
		if agents2[i].Name == worker {
			workerRow = &agents2[i]
			break
		}
	}
	if workerRow == nil {
		return fmt.Errorf("worker disappeared after stop (want still registered)")
	}
	if workerRow.Status == "running" {
		return fmt.Errorf("worker still running after stop")
	}
	return nil
}

// jThreadSpawnDirectRemove is a moderate orchestration path: spawn a
// owned thread, direct a short turn, then remove it cleanly.
func (s *suite) jThreadSpawnDirectRemove() error {
	id := "orch-worker-" + uuid.NewString()
	work, err := os.MkdirTemp(s.stateDir, "thread-work-")
	if err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey orchestration worker",
		"provider": string(s.provider),
	})
	if outage := asOutage("spawn", err); outage != nil {
		return outage
	}
	if err != nil {
		return fmt.Errorf("spawn: %w (%s)", err, trim(spawnOut, 80))
	}
	if !strings.Contains(spawnOut, id) {
		return fmt.Errorf("spawn ack missing id: %s", trim(spawnOut, 100))
	}
	// Session fragment in spawn ack must be present and non-empty.
	if extractSessionFragment(spawnOut) == "" && !strings.Contains(spawnOut, "session") {
		return fmt.Errorf("spawn ack has no session: %s", trim(spawnOut, 100))
	}

	list, err := s.mcpText("jevons_thread_list", nil)
	if err != nil {
		return fmt.Errorf("thread_list: %w", err)
	}
	if !strings.Contains(list, id) {
		return fmt.Errorf("thread_list missing %q: %s", id, trim(list, 160))
	}

	// A fresh request-specific answer must survive the full direct path.
	// Generic activity and token fragments are not successful delivery.
	// Numeric status-shaped fragments deliberately exercise the classifier
	// defect exposed by a UUID containing "500" (T625.3), on every run.
	token := "orch-direct-400-401-402-403-429-500-502-503-504-" + uuid.NewString()
	directOut, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Reply with exactly: " + token,
	})
	if outage := asOutage("direct", err); outage != nil {
		return outage
	}
	if err != nil {
		return fmt.Errorf("direct: %w", err)
	}
	if outage := replyOutage("direct reply", directOut); outage != nil {
		return outage
	}
	if strings.TrimSpace(directOut) != token {
		return fmt.Errorf("direct did not return its exact requested reply: got %q, want %q", trim(directOut, 200), token)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Errorf("read runtime provider evidence: %w", err)
	}
	if err := queueJourneyProvider(logs, id, string(s.provider)); err != nil {
		return err
	}

	if _, err := s.mcpText("jevons_thread_remove", map[string]any{"id": id}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	list2, err := s.mcpText("jevons_thread_list", nil)
	if err != nil {
		return err
	}
	if strings.Contains(list2, id) {
		return fmt.Errorf("thread still listed after remove: %s", trim(list2, 160))
	}
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return fmt.Errorf("registry after remove: %w", err)
	}
	for _, agent := range agents {
		if agent.Name == id {
			return fmt.Errorf("worker %s remains in the agent registry after remove", id)
		}
	}
	return nil
}

// jWorkerShellTool is the T97 regression journey: a fleet worker must
// actually run run_terminal_command (not just start). Prior suite gaps
// only did text-only directs, so ACP permission optionId bugs never fired.
//
// Uses thread_direct (blocks for the turn) rather than agent_send
// (fire-and-forget notify).
func (s *suite) jWorkerShellTool() error {
	id := fmt.Sprintf("orch-shell-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "shell-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	// Marker file the worker creates via shell — oracle independent of model prose.
	marker := filepath.Join(work, "j10-shell-marker.txt")
	_ = os.Remove(marker)
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey shell-permission worker",
	})
	if out := asOutage("spawn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("spawn: %w (%s)", err, trim(spawnOut, 80))
	}

	// 🎯T625: a fresh token per request. A constant marker cannot tell a
	// file this run's shell wrote from one a previous run left behind, and
	// it is exactly the string a chatty model can produce without ever
	// calling the tool.
	token := "J10-SHELL-" + uuid.NewString()
	// Force the shell tool path. Echo both to stdout (for reply) and to a
	// marker file (filesystem oracle if the model is chatty).
	prompt := fmt.Sprintf(
		"You MUST use the run_terminal_command tool. Run exactly:\n"+
			"echo %s | tee %s\n"+
			"Then reply with only the marker string %s (one line).",
		token, marker, token,
	)
	directOut, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": prompt,
	})
	// 🎯T283: thread_direct now classifies provider failures, so a backend
	// outage is distinguishable from the shell tool failing to run. Report the
	// outage instead of asserting a product defect the evidence cannot support.
	if out := asOutage("direct shell turn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("direct shell turn: %w (%s)", err, trim(directOut, 200))
	}
	if out := replyOutage("shell turn reply", directOut); out != nil {
		return out
	}
	if strings.Contains(directOut, "unknown permission option") {
		return fmt.Errorf("shell permission bug still present: %s", trim(directOut, 240))
	}
	if strings.Contains(strings.ToLower(directOut), "permission") &&
		strings.Contains(strings.ToLower(directOut), "failed") {
		return fmt.Errorf("permission failure in reply: %s", trim(directOut, 240))
	}

	// Primary oracle: marker file written by the shell command.
	// Secondary: token appears in the agent reply (whitespace-tolerant).
	markerOK := false
	if data, err := os.ReadFile(marker); err == nil {
		if strings.Contains(string(data), token) {
			markerOK = true
		}
	}
	flat := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			return -1
		}
		return r
	}, directOut)
	replyOK := strings.Contains(flat, token)
	if !markerOK && !replyOK {
		return fmt.Errorf("shell turn did not prove tool ran (no marker file, no token in reply): %s",
			trim(directOut, 200))
	}
	if !markerOK {
		// Reply alone is weak if the model hallucinates the token without shell.
		// Still fail hard when neither path works; soft-pass only if reply has
		// token AND no permission error (tool may have run without tee path).
		// Prefer marker — log when missing.
		return fmt.Errorf("marker file missing/empty at %s; reply had token but filesystem oracle failed — check shell actually ran: %s",
			marker, trim(directOut, 160))
	}

	// 🎯T625: the tool effect is evidence about the backend that ran it.
	if err := s.assertLaunchedOn(id); err != nil {
		return err
	}

	if _, err := s.mcpText("jevons_thread_remove", map[string]any{"id": id}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

// jWorkerTranscriptVisible is the 🎯T282 inspect oracle: after a worker has
// taken a real turn, the product inspect record (the jevons agent journal)
// must have turns. Provider session trees are not a second hydrate.
func (s *suite) jWorkerTranscriptVisible() error {
	id := fmt.Sprintf("orch-tx-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "transcript-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey transcript worker",
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	// 🎯T625: fresh per request, so a journal left by an earlier run cannot
	// stand in for a turn this journey never took.
	token := "JOURNEY-TX-" + uuid.NewString()
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Reply with exactly: " + token,
	}); err != nil {
		return fmt.Errorf("direct: %w", err)
	}

	// The inspect record is the jevons agent journal, so poll briefly
	// rather than assuming it is flushed the instant the turn returns.
	deadline := time.Now().Add(20 * time.Second)
	var lastReason string
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(id)
		if err != nil {
			return fmt.Errorf("transcript API: %w", err)
		}
		if turns, _ := payload["turns"].([]any); len(turns) > 0 {
			// 🎯T625: a populated journal is evidence about the backend
			// whose turn populated it.
			return s.assertLaunchedOn(id)
		}
		lastReason, _ = payload["empty_reason"].(string)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("worker transcript still empty after its turn (empty_reason=%q, provider=%s)",
		lastReason, s.provider)
}

// jProviderMigration is the 🎯T285 continuity oracle. A session cannot
// cross backends, so migration always means a new conversation — the test
// is whether the successor can still do the work.
//
// The probe fact is planted in a DIRECT to the worker, so it exists only
// in that agent's own transcript and then in Claudia's transfer brief the
// successor is seeded with. It must not be in the owner chatlog or the
// isolate persona. If the successor can state it, the brief carried
// predecessor context. A second live agent with a shell will grep the
// host session tree and find the plant — that is not a handover leak.
func migrationJourneyDestination(from claudia.Provider) claudia.Provider {
	if cli.PlanProvider(from) == claudia.ProviderCodex {
		return claudia.ProviderCursor
	}
	return claudia.ProviderCodex
}

func (s *suite) jProviderMigration() error {
	return s.withIsolatedBroker((*suite).providerMigrationWithBroker)
}

func (s *suite) jStoppedProviderMigration() error {
	return s.withIsolatedBroker((*suite).stoppedProviderMigrationWithBroker)
}

// A stopped seat has no in-memory Agent handle. Claudia must still run its
// disposable transfer, persist the destination and handover, then launch
// that same destination without a Jevons handover record.
func (s *suite) stoppedProviderMigrationWithBroker() error {
	id := fmt.Sprintf("orch-stopped-mig-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "stopped-migrate-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	to := migrationJourneyDestination(s.provider)
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey stopped migration worker",
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	const codeword = "AMBERPINE59"
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Remember this mission codeword: " + codeword + ". Reply exactly: STORED",
	}); err != nil {
		return fmt.Errorf("plant codeword: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	source := before[id]
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{
		"name": id, "force": true, "reason": "journey: test migration of a stopped seat",
	}); err != nil {
		return fmt.Errorf("stop predecessor: %w", err)
	}
	if out, err := s.mcpText("jevons_agent_migrate", map[string]any{
		"name": id, "provider": string(to), "owner_asked": true,
	}); err != nil {
		return fmt.Errorf("migrate stopped seat: %w (%s)", err, trim(out, 200))
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	destination := after[id]
	if cli.PlanProvider(destination.Provider) != cli.PlanProvider(to) ||
		destination.SessionID == "" || destination.SessionID == source.SessionID {
		return fmt.Errorf("stopped migration did not persist one destination: source=%+v destination=%+v", source, destination)
	}
	if _, err := os.Stat(s.handoverPath(id)); err == nil {
		return fmt.Errorf("stopped migration wrote a second Jevons handover")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The cold path sends the durable handover into the new sidecar seat.
	// Wait for that turn to finish before probing: a concurrent direct would
	// collide with the seed and say "already processing" rather than test
	// whether context arrived.
	seedDeadline := time.Now().Add(2 * time.Minute)
	seedDone := false
	for time.Now().Before(seedDeadline) {
		recs, err := spool.ReadSeat(filepath.Join(s.stateDir, "spool"), id)
		if err != nil {
			return fmt.Errorf("read successor seed turn: %w", err)
		}
		for _, rec := range recs {
			if rec.Type != "turn" {
				continue
			}
			if rec.Stop != "end_turn" && rec.Stop != "stop_token" {
				return fmt.Errorf("successor seed turn stopped as %q", rec.Stop)
			}
			seedDone = true
			break
		}
		if seedDone {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !seedDone {
		return fmt.Errorf("successor seed turn did not complete within two minutes")
	}
	const probe = "What is the mission codeword? Reply with the codeword only."
	deadline := time.Now().Add(2 * time.Minute)
	var lastReply string
	var lastErr error
	for time.Now().Before(deadline) {
		reply, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		if err == nil && strings.Contains(strings.ToUpper(reply), codeword) {
			return nil
		}
		lastReply, lastErr = reply, err
		time.Sleep(4 * time.Second)
	}
	return fmt.Errorf("stopped successor did not recall context within two minutes: reply=%q err=%v", trim(lastReply, 200), lastErr)
}

func (s *suite) providerMigrationWithBroker() error {
	id := fmt.Sprintf("orch-mig-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "migrate-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	to := migrationJourneyDestination(s.provider)

	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey migration worker",
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}

	// Plant the fact. Deliberately arbitrary: no successor could guess it,
	// and nothing but the predecessor's transcript records it.
	// One unhyphenated token on purpose: a hyphenated probe came back
	// truncated at the first hyphen on the Grok direct path (🎯T286), which
	// would fail this journey for a reply-assembly reason rather than a
	// continuity one.
	const passphrase = "BLUEOTTER42"
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id,
		"text": "Remember this for later — the mission passphrase is " + passphrase +
			". Reply with exactly: STORED",
	}); err != nil {
		return fmt.Errorf("plant fact: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("source registry: %w", err)
	}
	source := before[id]
	if source.SessionID == "" || cli.PlanProvider(source.Provider) != cli.PlanProvider(s.provider) {
		return fmt.Errorf("source identity is not the running provider: %+v", source)
	}

	// owner_asked: the journey plays the owner requesting this move (T561
	// refuses an un-asked leave of a provider with weekly remaining; that
	// refusal is pinned by TestT561MigrateRefusesLeavingClaudeWithWeeklyRemaining).
	migrateOut, err := s.mcpText("jevons_agent_migrate", map[string]any{
		"name": id, "provider": string(to), "owner_asked": true,
	})
	if err != nil {
		return fmt.Errorf("migrate %s → %s: %w (%s)", s.provider, to, err, trim(migrateOut, 200))
	}
	if strings.Contains(strings.ToUpper(migrateOut), "COLD") {
		return fmt.Errorf("migration carried nothing: %s", trim(migrateOut, 200))
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("destination registry: %w", err)
	}
	destination := after[id]
	if cli.PlanProvider(destination.Provider) != cli.PlanProvider(to) ||
		destination.SessionID == "" || destination.SessionID == source.SessionID {
		return fmt.Errorf("migration did not record one distinct destination: source=%+v destination=%+v", source, destination)
	}
	if _, err := os.Stat(s.handoverPath(id)); err == nil {
		return fmt.Errorf("broker-owned migration wrote a second Jevons handover for %s", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check host handover after migration: %w", err)
	}

	// Claudia's disposable transfer agent prepares a bounded brief before
	// moving the work seat. The first probe can still collide with the
	// destination's seed turn; ask again if it is busy.
	// Probe with thread_direct: it queues behind the in-flight seed turn and
	// returns once the successor answers. A busy refusal (Grok ACP) means
	// "the read is still running", not a failure.
	const probe = "What is the mission passphrase? Reply with the passphrase only."
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe}); err != nil &&
		!agenterr.IsPromptBusy(err) {
		return fmt.Errorf("ask successor: %w", err)
	}

	// Assert on the successor's own transcript rather than the direct's
	// return value: the seed turn is still streaming when the probe lands,
	// and the Grok direct path returns only the first chunk of a reply in
	// that window (🎯T286) — a reply-assembly artefact that says nothing
	// about continuity. The passphrase appearing anywhere in the
	// successor's transcript can only have come from its predecessor's.
	deadline := time.Now().Add(90 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		payload, err := s.agentTranscriptHTTP(id)
		if err != nil {
			return fmt.Errorf("successor transcript: %w", err)
		}
		if blob, err := json.Marshal(payload); err == nil &&
			strings.Contains(strings.ToUpper(string(blob)), passphrase) {
			found = true
			break
		}
		_, _ = s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		time.Sleep(5 * time.Second)
	}
	if !found {
		return fmt.Errorf("successor on %s never recovered the passphrase from its predecessor's transcript", to)
	}

	// Control: the plant must not have been in the isolate persona,
	// owner chat, or a handover addressed to anyone but this worker.
	// A second live agent with a shell will grep the host session tree
	// and find BLUEOTTER42; that used to fail this journey for a search
	// reason rather than a continuity one.
	cfg, err := os.ReadFile(filepath.Join(s.stateDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("isolate config: %w", err)
	}
	if strings.Contains(strings.ToUpper(string(cfg)), passphrase) {
		return fmt.Errorf("passphrase leaked into isolate config — the probe proves nothing")
	}
	// Owner chat may mention the plant if the worker notifies upward;
	// that is not a leak of the isolate persona.
	ents, _ := os.ReadDir(filepath.Join(s.stateDir, "handover"))
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".json")
		if name == id {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.stateDir, "handover", e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToUpper(string(raw)), passphrase) {
			return fmt.Errorf("passphrase leaked into handover %s — the probe proves nothing", e.Name())
		}
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("restart after migration: %w", err)
	}
	reopened, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("reopened registry: %w", err)
	}
	if got := reopened[id]; got.SessionID != destination.SessionID ||
		cli.PlanProvider(got.Provider) != cli.PlanProvider(to) {
		return fmt.Errorf("restart moved the seat again: destination=%+v reopened=%+v", destination, got)
	}
	if _, err := os.Stat(s.handoverPath(id)); err == nil {
		return fmt.Errorf("broker-owned migration gained a Jevons handover after restart for %s", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check host handover after restart: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		reply, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		if err == nil && strings.Contains(strings.ToUpper(reply), passphrase) {
			return nil
		}
		if attempt == 2 {
			return fmt.Errorf("destination retained its id but not its context after restart: reply=%q err=%v", trim(reply, 200), err)
		}
		time.Sleep(3 * time.Second)
	}
	return nil
}

// MCP/HTTP helpers live in steps.go (🎯T102 step library).

// jOverseerMigration is the 🎯T285 overseer arm: the owner's CEO agent
// moves backend and keeps working. Unlike a fleet agent it is attached to
// owner chat, so this asserts BOTH halves — the successor recovered its
// predecessor's context, and the chat it answers on is still wired to it.
//
// The probe fact is planted through owner chat, which is also where the
// answer must come back, so the journey exercises exactly the path the
// owner uses.
func (s *suite) jOverseerMigration() error {
	return s.withIsolatedBroker((*suite).overseerMigrationWithBroker)
}

func (s *suite) overseerMigrationWithBroker() error {
	to := migrationJourneyDestination(s.provider)
	const codeword = "TANGERINEHARBOUR77"

	ctx, cancel := context.WithTimeout(context.Background(), 3*turnTimeout)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		conn.CloseNow()
		return err
	}
	plant := "Remember this for the rest of our work — the project codeword is " +
		codeword + ". Reply with exactly: NOTED"
	if err := conn.Write(ctx, websocket.MessageText, []byte(plant)); err != nil {
		conn.CloseNow()
		return err
	}
	if _, _, terminal, err := waitTurn(ctx, frames, codeword, true); err != nil || !terminal {
		conn.CloseNow()
		return fmt.Errorf("plant codeword: terminal=%v err=%v", terminal, err)
	}
	conn.CloseNow()

	body, err := json.Marshal(map[string]any{"provider": string(to)})
	if err != nil {
		return err
	}
	if err := postOverseerMigrateWhenSettled(ctx, "http://"+s.host+"/api/overseer/migrate", body); err != nil {
		return err
	}

	// Owner chat must still reach the successor — a migration that leaves
	// the conversation wired to nothing is the failure this arm exists for.
	conn2, frames2, err := dialChat(ctx, s.host)
	if err != nil {
		return fmt.Errorf("reconnect after migration: %w", err)
	}
	defer conn2.CloseNow()
	if _, err := drainReplay(frames2, 1500*time.Millisecond); err != nil {
		return err
	}
	for attempt := 1; attempt <= 5; attempt++ {
		if err := conn2.Write(ctx, websocket.MessageText,
			[]byte("What is the project codeword? Reply with the codeword only.")); err != nil {
			return err
		}
		_, text, _, err := waitTurn(ctx, frames2, "codeword", true)
		if err == nil && strings.Contains(strings.ToUpper(text), codeword) {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("overseer on %s never recovered the codeword after migration", to)
}

// overseerMigrateRetry is how often a refused migrate is re-asked.
const overseerMigrateRetry = 2 * time.Second

// postOverseerMigrateWhenSettled asks the daemon to migrate the overseer and
// treats its own "turn in flight" refusal as the settle oracle (🎯T625.9).
// The plant turn ending does not mean the overseer is idle: a notice the
// daemon deferred behind that turn starts as it ends (isolate log 15e5f3bf:
// notify_queue drain at 06:53:25.190, migrate refused at 25.3). Any other
// refusal fails at once, and a turn that never settles fails at ctx's
// deadline, so the guard itself stays under test.
func postOverseerMigrateWhenSettled(ctx context.Context, url string, body []byte) error {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("migrate overseer: %w", err)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		msg := fmt.Sprint(out["error"])
		if resp.StatusCode != http.StatusConflict || !strings.Contains(msg, "turn in flight") {
			return fmt.Errorf("migrate overseer HTTP %d: %v", resp.StatusCode, out["error"])
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("migrate overseer: turn never settled: %s", msg)
		case <-time.After(overseerMigrateRetry):
		}
	}
}
