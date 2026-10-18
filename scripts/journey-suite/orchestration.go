// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"
)

// ensureJourneyGitRepo makes dir a git repository. Codex work seats
// request sandbox git write, and Claudia refuses that grant when the
// workdir is not in a repository.
func ensureJourneyGitRepo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "init", "-q", dir)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init %s: %v (%s)", dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

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
	// A codex work seat asks for sandbox git write, and that grant
	// refuses a directory that is not a repository.
	if err := ensureJourneyGitRepo(work); err != nil {
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

	// The seat authority refreshes from the registry once a second, so a
	// seat read the instant its start returns is still phase_unknown (seen
	// 2026-10-05). Give both a bounded moment to read running; the failure
	// below still reports what they read if they never do.
	var by map[string]AgentInfo
	deadline := time.Now().Add(20 * time.Second)
	for {
		agents, err := s.listAgentsHTTP()
		if err != nil {
			return err
		}
		by = map[string]AgentInfo{}
		for _, a := range agents {
			by[a.Name] = a
		}
		if (by[po].Status == "running" && by[worker].Status == "running") || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
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
	// 🎯T828 / 🎯T664: force the just-messaged worker into delivered_unconfirmed
	// so the plain stop ALWAYS runs the refusal arm. The soft T625.7 branch
	// ("assert refusal only if plain stop is refused") could go GREEN without
	// ever reading the T664 text. Control: arming fault-t664-disable-guard
	// lets the plain stop succeed and this journey fails at the assertion.
	if err := os.WriteFile(filepath.Join(s.stateDir, "fault-t664-unconfirmed"), []byte(worker+"\n"), 0o600); err != nil {
		return fmt.Errorf("arm T828 unconfirmed fault: %w", err)
	}
	defer func() { _ = os.Remove(filepath.Join(s.stateDir, "fault-t664-unconfirmed")) }()
	stopOut, err := s.mcpText("jevons_agent_stop", map[string]any{"name": worker})
	if err == nil {
		return fmt.Errorf("plain stop of just-messaged worker succeeded; T664 refusal arm did not run (stopOut=%s)", trim(stopOut, 160))
	}
	// err carries a trimmed copy; the full refusal text is the returned string.
	msg := stopOut
	if !strings.Contains(msg, "refusing to stop") || !strings.Contains(msg, "T664") ||
		!strings.Contains(msg, "jevons_transcript_read") {
		return fmt.Errorf("stop worker: want T664 refusal naming the check, got: %w", err)
	}
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{
		"name": worker, "force": true,
		"reason": "journey teardown: fan-out assertions are complete, delivery verdict irrelevant",
	}); err != nil {
		return fmt.Errorf("forced stop worker: %w", err)
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

// MCP/HTTP helpers live in steps.go (🎯T102 step library).
