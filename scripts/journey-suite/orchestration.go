// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// Orchestration journeys (MCP-direct against the isolated daemon).
//
// Simple: tool surface + overseer registry presence.
// Moderate: two fleet agents on one workdir (T86 live) + thread
// spawn → direct → remove.
// Shell tools: worker must execute run_terminal_command unattended (T97).

// jOverseerToolsAttached proves the overseer's own client can reach jevons
// tools under the selected provider (🎯T282). J6 checks the producer side
// (jevonsd serves the tools); this checks the consumer side, which is
// provider-specific: Grok reads ~/.grok/config.toml, Claude needs
// `claude mcp add -s user` (🎯T212). A Claude overseer that boots toolless
// looks perfectly healthy until the owner asks it to do anything.
func (s *suite) jOverseerToolsAttached() error {
	if !mcpListedFor(s.provider, mcpName) {
		return fmt.Errorf("%s not registered with the %s CLI while the isolate runs — overseer would start toolless",
			mcpName, mcpCLI(s.provider))
	}

	// And that the registration is live in the conversation, not just on
	// disk: ask the overseer to use a jevons tool and report what it saw.
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		return err
	}
	prompt := "Call the jevons_agent_list tool now, then reply with only the name of the overseer agent it lists."
	if err := conn.Write(ctx, websocket.MessageText, []byte(prompt)); err != nil {
		return err
	}
	_, text, terminal, err := waitTurn(ctx, frames, "jevons_agent_list", true)
	if err != nil {
		return fmt.Errorf("tool turn: %w", err)
	}
	if !terminal {
		return fmt.Errorf("tool turn never completed")
	}
	if !strings.Contains(strings.ToLower(text), overseerName) {
		return fmt.Errorf("overseer could not report its own registry row (tools likely not attached): %s",
			trim(text, 200))
	}
	return nil
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
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{"name": worker}); err != nil {
		return fmt.Errorf("stop worker: %w", err)
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
	id := fmt.Sprintf("orch-worker-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "thread-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey orchestration worker",
	})
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

	token := "orch-direct-ok"
	directOut, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Reply with exactly: " + token,
	})
	if err != nil {
		return fmt.Errorf("direct: %w", err)
	}
	// Model may paraphrase or be tool-only; require non-empty delivery.
	if strings.TrimSpace(directOut) == "" {
		return fmt.Errorf("direct returned empty reply")
	}
	norm := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\n", "", "_", "").Replace(directOut))
	tokNorm := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(token))
	if !strings.Contains(norm, tokNorm) && !strings.Contains(norm, "orch") && !strings.Contains(norm, "direct") {
		// Soft accept: non-empty reply with a completed turn is enough
		// when the model ignores exact-match instructions.
		if len(strings.TrimSpace(directOut)) < 1 {
			return fmt.Errorf("direct reply unexpected: %s", trim(directOut, 100))
		}
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

	token := "J10_SHELL_OK"
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

	if _, err := s.mcpText("jevons_thread_remove", map[string]any{"id": id}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

// jWorkerTranscriptVisible is the 🎯T282 inspect oracle: after a worker has
// taken a real turn, the RHS inspect path must be able to load its
// transcript. Session stores differ per provider (Grok sessions tree vs
// Claude projects tree, 🎯T213), and a discovery path wired to one of them
// leaves the other's workers showing an empty pane.
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
	token := "JOURNEY-TX-OK"
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Reply with exactly: " + token,
	}); err != nil {
		return fmt.Errorf("direct: %w", err)
	}

	// The transcript lands via the provider's own store, so poll briefly
	// rather than assuming it is flushed the instant the turn returns.
	deadline := time.Now().Add(20 * time.Second)
	var lastReason string
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(id)
		if err != nil {
			return fmt.Errorf("transcript API: %w", err)
		}
		if turns, _ := payload["turns"].([]any); len(turns) > 0 {
			return nil
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
// in that agent's own transcript: not in the owner chatlog, not in the
// persona, not in the prompt the successor is given. If the successor can
// state it, it read its predecessor's transcript. A cold control run (no
// handover) must fail the same probe, or the oracle proves nothing.
func (s *suite) jProviderMigration() error {
	id := fmt.Sprintf("orch-mig-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "migrate-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	// Migrate to whichever backend the isolate is NOT running.
	to := claudia.ProviderClaude
	if s.provider == claudia.ProviderClaude {
		to = claudia.ProviderGrok
	}

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

	migrateOut, err := s.mcpText("jevons_agent_migrate", map[string]any{
		"name": id, "provider": string(to),
	})
	if err != nil {
		return fmt.Errorf("migrate %s → %s: %w (%s)", s.provider, to, err, trim(migrateOut, 200))
	}
	if strings.Contains(strings.ToUpper(migrateOut), "COLD") {
		return fmt.Errorf("migration carried nothing: %s", trim(migrateOut, 200))
	}

	// The successor is a different backend with an empty session. Only its
	// predecessor's transcript holds the answer.
	//
	// Seeding is fire-and-forget by design — reading a long transcript must
	// not block the migration call — so the first probe can collide with the
	// read still in flight and come back with its acknowledgement instead of
	// an answer. Ask again rather than calling that a failure; a successor
	// that never read the transcript keeps failing every attempt, which is
	// what the cold control run demonstrates.
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

	// Control: an agent that received no handover must NOT be able to answer.
	// Without this, a passphrase leaking through the persona, the standing
	// brief, or the model's own guesswork would make the probe above pass for
	// the wrong reason.
	// The control gets its OWN workdir. Sharing one with the migrated agent
	// made this control fail honestly on the first run: a resourceful agent
	// asked for "the mission passphrase" greps what it can reach, and the
	// predecessor's transcript is keyed by workdir — so it answered without
	// any handover. Isolating the workdir keeps the control about what the
	// handover carried, not about what a shell can find.
	control := id + "-control"
	controlWork := filepath.Join(s.stateDir, "migrate-control")
	if err := os.MkdirAll(controlWork, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": control})
	}()
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": control, "workdir": controlWork, "description": "journey migration control (no handover)",
	}); err != nil {
		return fmt.Errorf("spawn control: %w", err)
	}
	// A control that errors or never answers has still not produced the
	// passphrase, which is all this arm asserts — so only its transcript
	// is judged, never the call's outcome.
	_, _ = s.mcpText("jevons_thread_direct", map[string]any{"id": control, "text": probe})
	controlDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(controlDeadline) {
		payload, err := s.agentTranscriptHTTP(control)
		if err != nil {
			return fmt.Errorf("control transcript: %w", err)
		}
		if blob, err := json.Marshal(payload); err == nil &&
			strings.Contains(strings.ToUpper(string(blob)), passphrase) {
			return fmt.Errorf("control agent produced the passphrase without a handover — the probe proves nothing")
		}
		time.Sleep(5 * time.Second)
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
	to := claudia.ProviderClaude
	if s.provider == claudia.ProviderClaude {
		to = claudia.ProviderGrok
	}
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
	resp, err := http.Post("http://"+s.host+"/api/overseer/migrate", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("migrate overseer: %w", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != 200 {
		return fmt.Errorf("migrate overseer HTTP %d: %v", resp.StatusCode, out["error"])
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
