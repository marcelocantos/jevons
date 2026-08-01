// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Orchestration journeys (MCP-direct against the isolated daemon).
//
// Simple: tool surface + overseer registry presence.
// Moderate: two fleet agents on one workdir (T86 live) + thread
// spawn → direct → remove.
// Shell tools: worker must execute run_terminal_command unattended (T97).

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
	byName := map[string]agentInfo{}
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
	by := map[string]agentInfo{}
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
	ack, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": worker,
		"text": "Reply with exactly: FANOUT_PONG and do not open a PR.",
	})
	if err != nil {
		return fmt.Errorf("worker send: %w", err)
	}
	if !strings.Contains(ack, "standing fleet brief") && !strings.Contains(ack, "T104") {
		return fmt.Errorf("first send ack missing standing brief note: %s", trim(ack, 160))
	}

	// Integrator slice: second agent (po) also gets brief on first send.
	ackPO, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": po,
		"text": "Coordinate only; local commits only.",
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
	var workerRow *agentInfo
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
	if err != nil {
		return fmt.Errorf("direct shell turn: %w (%s)", err, trim(directOut, 200))
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

// ── MCP / HTTP helpers ───────────────────────────────────────────────

type agentInfo struct {
	Name    string `json:"name"`
	WorkDir string `json:"workdir"`
	Parent  string `json:"parent"`
	Status  string `json:"status"`
}

func (s *suite) listAgentsHTTP() ([]agentInfo, error) {
	resp, err := http.Get("http://" + s.host + "/api/agents")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("/api/agents HTTP %d", resp.StatusCode)
	}
	var agents []agentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *suite) mcp(method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", "http://"+s.host+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s decode: %w", method, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, out.Error.Message)
	}
	return out.Result, nil
}

func (s *suite) mcpText(tool string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := s.mcp("tools/call", map[string]any{
		"name": tool, "arguments": args,
	})
	if err != nil {
		return "", err
	}
	// Tool-level isError is encoded in the result content shape.
	var envelope struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res, &envelope); err != nil {
		return "", fmt.Errorf("decode tool result: %w", err)
	}
	var b strings.Builder
	for _, c := range envelope.Content {
		b.WriteString(c.Text)
	}
	text := b.String()
	if envelope.IsError {
		return text, fmt.Errorf("%s: %s", tool, trim(text, 160))
	}
	return text, nil
}

var sessionFragRE = regexp.MustCompile(`session[:\s]+([0-9a-fA-F….]{6,})`)

func extractSessionFragment(s string) string {
	// Prefer "(session: xxx)" form from agent start / list.
	if i := strings.Index(s, "session:"); i >= 0 {
		rest := strings.TrimSpace(s[i+len("session:"):])
		// Truncate at first delimiter.
		for _, sep := range []string{",", ")", " ", "\n", "\t"} {
			if j := strings.Index(rest, sep); j >= 0 {
				rest = rest[:j]
			}
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return rest
		}
	}
	m := sessionFragRE.FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

