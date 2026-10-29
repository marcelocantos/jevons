// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// jOwnerPauseResume is the 🎯T966 journey oracle: cockpit fleet-pause toggles
// durable blocked_owner intent; a running seat must not begin a new
// provider turn while paused; a held owner turn must be answered after resume.
func (s *suite) jOwnerPauseResume() error {
	const overseer = "jevons"
	session, err := s.waitOverseerSession(overseer, 45*time.Second)
	if err != nil {
		return err
	}

	// J40 runs alone as well as in the full suite. Seed its own live seat
	// rather than relying on an earlier journey having left one behind.
	active := "j40-pause-seat-" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)
	if _, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": active, "workdir": s.stateDir, "actor": overseer,
		"provider": s.provider, "owner_asked": true,
	}); err != nil {
		return fmt.Errorf("start pause witness: %w", err)
	}
	defer func() {
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{
			"name": active, "actor": overseer, "force": true,
		})
	}()
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return err
	}
	if pickRunningSeat(agents, overseer) == "" {
		return fmt.Errorf("pause witness %s not running", active)
	}
	asstBefore, err := assistantTurnCount(s.agentTranscriptHTTP(active))
	if err != nil {
		return fmt.Errorf("active seat %s transcript: %w", active, err)
	}

	if err := s.postFleetPause(true); err != nil {
		return fmt.Errorf("cockpit pause: %w", err)
	}
	body, err := s.getPath("/api/fleet-intent")
	if err != nil {
		return err
	}
	if !strings.Contains(body, "blocked_owner") {
		return fmt.Errorf("fleet-intent body: %s", trim(body, 400))
	}

	logAt, err := s.logOffset()
	if err != nil {
		return err
	}

	probe := "jv-p40-" + strconv.FormatInt(time.Now().UnixNano()%10000, 10)
	startText, startErr := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name":    probe,
		"workdir": s.stateDir,
		"actor":   "owner",
	})
	if startErr == nil {
		return fmt.Errorf("spawn was allowed during pause: %s", trim(startText, 400))
	}
	refusal := startErr.Error() + " " + startText
	if !strings.Contains(refusal, "refusing to start") || !strings.Contains(refusal, "blocked on the owner") {
		return fmt.Errorf("spawn refusal: %s", trim(refusal, 400))
	}
	agents, err = s.ListAgentsHTTP()
	if err != nil {
		return err
	}
	for _, a := range agents {
		if a.Name == probe {
			return fmt.Errorf("probe seat %s registered during pause", probe)
		}
	}
	if err := s.requireOverseerSession(overseer, session); err != nil {
		return fmt.Errorf("during pause: %w", err)
	}

	token := "pause-hold-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	ownerPrompt := "Reply with exactly " + token
	// 🎯T1129: MCP agent_send is *agent* origin even with actor=owner.
	// The pause contract concerns owner words, so use the owner-origin HTTP
	// admission path rather than asserting an agent note is an owner turn.
	send, sendErr := s.ownerSurfaceSend(overseer, ownerPrompt, "owner")
	if sendErr != nil || send.Status != "queued" {
		return fmt.Errorf("paused owner send was not held: err=%v result=%+v", sendErr, send)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		tr, terr := s.agentTranscriptHTTP(overseer)
		if terr != nil {
			return terr
		}
		if assistantContains(tr, token) {
			return fmt.Errorf("overseer answered the held owner turn during pause")
		}
		asstNow, aerr := assistantTurnCount(s.agentTranscriptHTTP(active))
		if aerr != nil {
			return aerr
		}
		if asstNow > asstBefore {
			return fmt.Errorf("seat %s gained assistant turns during fleet pause (%d -> %d)", active, asstBefore, asstNow)
		}
		span, serr := s.logSince(logAt)
		if serr != nil {
			return serr
		}
		if strings.Contains(span, "decision=drain") || strings.Contains(span, "delivered_unconfirmed") {
			return fmt.Errorf("notify queue drained during blocked_owner: %s", trim(span, 500))
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := s.postFleetPause(false); err != nil {
		return fmt.Errorf("cockpit resume: %w", err)
	}
	if err := s.requireOverseerSession(overseer, session); err != nil {
		return fmt.Errorf("after lift: %w", err)
	}
	deliverBy := time.Now().Add(45 * time.Second)
	for {
		tr, terr := s.agentTranscriptHTTP(overseer)
		if terr != nil {
			return terr
		}
		if assistantContains(tr, token) {
			return nil
		}
		if time.Now().After(deliverBy) {
			return fmt.Errorf("overseer did not answer the queued owner turn after resume")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func pickRunningSeat(agents []AgentInfo, skip string) string {
	for _, a := range agents {
		if a.Name == skip || a.Status != "running" {
			continue
		}
		return a.Name
	}
	return ""
}

func assistantTurnCount(tr map[string]any, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	turns, _ := tr["turns"].([]any)
	n := 0
	for _, t := range turns {
		m, _ := t.(map[string]any)
		if m["role"] == "assistant" {
			n++
		}
	}
	return n, nil
}

func (s *suite) postFleetPause(pause bool) error {
	payload, _ := json.Marshal(map[string]any{"pause": pause})
	resp, err := http.Post("http://"+s.host+"/api/fleet-pause", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /api/fleet-pause HTTP %d: %s", resp.StatusCode, trim(string(raw), 300))
	}
	var out struct {
		Paused bool `json:"paused"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	if out.Paused != pause {
		return fmt.Errorf("fleet-pause response paused=%v want %v", out.Paused, pause)
	}
	return nil
}

func (s *suite) waitOverseerSession(name string, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	var last error
	for {
		snap, err := bounceRegistrySnapshot(s.agentsPath())
		if err != nil {
			last = err
		} else if def, ok := snap[name]; ok && def.SessionID != "" {
			return def.SessionID, nil
		} else {
			last = fmt.Errorf("overseer %s has no session id yet", name)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("overseer session not ready: %w", last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *suite) requireOverseerSession(name, session string) error {
	snap, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	def, ok := snap[name]
	if !ok {
		return fmt.Errorf("overseer %s missing from registry", name)
	}
	if def.SessionID != session {
		return fmt.Errorf("session changed: was %s now %s", session, def.SessionID)
	}
	return nil
}

func (s *suite) getPath(path string) (string, error) {
	resp, err := http.Get("http://" + s.host + path)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET %s HTTP %d: %s", path, resp.StatusCode, trim(string(body), 200))
	}
	return string(body), nil
}

func (s *suite) logOffset() (int64, error) {
	info, err := os.Stat(s.logPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (s *suite) logSince(offset int64) (string, error) {
	f, err := os.Open(s.logPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	body, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// assistantContains reads authored assistant rows only. 🎯T806 journals the
// owner's bubble on queue acceptance, before the paused provider sees it;
// searching the whole journal would falsely count that bubble as delivery.
func assistantContains(tr map[string]any, token string) bool {
	turns, _ := tr["turns"].([]any)
	for _, turn := range turns {
		row, _ := turn.(map[string]any)
		if row["role"] != "assistant" {
			continue
		}
		raw, _ := row["raw"].(map[string]any)
		body, _ := json.Marshal(raw)
		if strings.Contains(string(body), token) {
			return true
		}
	}
	return false
}
