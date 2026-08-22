// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package main journey step library (🎯T102).
//
// Named helpers for setup / act / assert shared by owner-chat and
// orchestration journeys. No formal journey DSL — write journeys in Go
// and call these steps. See README "Step library".
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

// ── Fleet / HTTP steps ────────────────────────────────────────────────

// AgentInfo is one row from GET /api/agents.
type AgentInfo struct {
	Name    string `json:"name"`
	WorkDir string `json:"workdir"`
	Parent  string `json:"parent"`
	Status  string `json:"status"`
	Phase   string `json:"phase,omitempty"`
}

// ListAgentsHTTP GETs /api/agents on the isolate.
func (s *suite) ListAgentsHTTP() ([]AgentInfo, error) {
	resp, err := http.Get("http://" + s.host + "/api/agents")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("/api/agents HTTP %d", resp.StatusCode)
	}
	var agents []AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// listAgentsHTTP is a legacy alias used by existing journeys.
func (s *suite) listAgentsHTTP() ([]AgentInfo, error) { return s.ListAgentsHTTP() }

// agentTranscriptHTTP reads the product inspect record: the jevons
// per-agent journal. GET /api/agents/{name}/transcript is gone.
func (s *suite) agentTranscriptHTTP(name string) (map[string]any, error) {
	path := filepath.Join(s.stateDir, "agent-chatlogs", name+".jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"turns": []any{}, "empty": true, "journal": path}, nil
		}
		return nil, err
	}
	var turns []any
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(ln), &ev); err != nil {
			continue
		}
		typ, _ := ev["type"].(string)
		if typ == "user" || typ == "assistant" || typ == "agent_note" {
			role := typ
			if msg, ok := ev["message"].(map[string]any); ok {
				if r, _ := msg["role"].(string); r != "" {
					role = r
				}
			}
			turns = append(turns, map[string]any{"role": role, "raw": ev})
		}
	}
	return map[string]any{
		"turns":   turns,
		"empty":   len(turns) == 0,
		"journal": string(b),
	}, nil
}

// ── MCP steps ─────────────────────────────────────────────────────────

// MCPJSONRPC posts a JSON-RPC method to the isolate /mcp endpoint.
func (s *suite) MCPJSONRPC(method string, params any) (json.RawMessage, error) {
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

func (s *suite) mcp(method string, params any) (json.RawMessage, error) {
	return s.MCPJSONRPC(method, params)
}

// MCPToolCall invokes tools/call and returns concatenated text content.
func (s *suite) MCPToolCall(tool string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	res, err := s.MCPJSONRPC("tools/call", map[string]any{
		"name": tool, "arguments": args,
	})
	if err != nil {
		return "", err
	}
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

func (s *suite) mcpText(tool string, args map[string]any) (string, error) {
	return s.MCPToolCall(tool, args)
}

// AgentStart starts a fleet agent via jevons_agent_start.
func (s *suite) AgentStart(name, workdir, actor, parent string) (string, error) {
	args := map[string]any{"name": name, "workdir": workdir}
	if actor != "" {
		args["actor"] = actor
	}
	if parent != "" {
		args["parent"] = parent
	}
	return s.MCPToolCall("jevons_agent_start", args)
}

// AgentSend sends text to a running agent as actor (🎯T321). Empty actor defaults
// to "jevons" (journey harness speaks as the overseer surface).
func (s *suite) AgentSend(name, text string) (string, error) {
	return s.AgentSendAs("jevons", name, text)
}

// AgentSendAs is AgentSend with an explicit lineage actor.
func (s *suite) AgentSendAs(actor, name, text string) (string, error) {
	if actor == "" {
		actor = "jevons"
	}
	return s.MCPToolCall("jevons_agent_send", map[string]any{
		"name": name, "text": text, "actor": actor,
	})
}

// AgentStop stops a registered agent process.
func (s *suite) AgentStop(name string) (string, error) {
	return s.MCPToolCall("jevons_agent_stop", map[string]any{"name": name})
}

// AgentKill kills and deregisters an agent (requires actor).
func (s *suite) AgentKill(name, actor string) (string, error) {
	return s.MCPToolCall("jevons_agent_kill", map[string]any{"name": name, "actor": actor})
}

// ── Assert helpers ────────────────────────────────────────────────────

// MustAgentRunning fails if name is missing or not running in /api/agents.
func (s *suite) MustAgentRunning(name string) error {
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return err
	}
	for _, a := range agents {
		if a.Name == name {
			if a.Status != "running" {
				return fmt.Errorf("agent %q status %q, want running", name, a.Status)
			}
			return nil
		}
	}
	return fmt.Errorf("agent %q not in /api/agents", name)
}

// ── String helpers ────────────────────────────────────────────────────

var sessionFragRE = regexp.MustCompile(`session[:\s]+([0-9a-fA-F….]{6,})`)

func extractSessionFragment(s string) string {
	if i := strings.Index(s, "session:"); i >= 0 {
		rest := strings.TrimSpace(s[i+len("session:"):])
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
