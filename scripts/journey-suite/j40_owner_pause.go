// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// jOwnerPauseResume is the owner-pause slice: blocked_owner refuses a new
// spawn, keeps the overseer session, and holds an overseer send. A send that
// comes back without both "NOT delivered" and "fleet intent" is a failure —
// that is the 2026-10-18 drain to delivered_unconfirmed. The lift restores
// working and the same session. A post-lift turn is not this assertion.
func (s *suite) jOwnerPauseResume() error {
	const overseer = "jevons"
	session, err := s.waitOverseerSession(overseer, 45*time.Second)
	if err != nil {
		return err
	}

	pause, err := s.MCPToolCall("jevons_fleet_intent", map[string]any{
		"state":  "blocked_owner",
		"actor":  "owner",
		"reason": "owner pause probe",
	})
	if err != nil {
		return fmt.Errorf("set blocked_owner: %w", err)
	}
	if !strings.Contains(pause, "blocked on the owner") {
		return fmt.Errorf("pause reply: %s", trim(pause, 400))
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
	agents, err := s.ListAgentsHTTP()
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
	sendText, sendErr := s.MCPToolCall("jevons_agent_send", map[string]any{
		"name":  overseer,
		"text":  token,
		"actor": "owner",
	})
	if !strings.Contains(sendText, "NOT delivered") || !strings.Contains(sendText, "fleet intent") {
		return fmt.Errorf("paused send was not held: err=%v text=%s", sendErr, trim(sendText, 400))
	}
	if strings.Contains(sendText, "delivered_unconfirmed") {
		return fmt.Errorf("paused send reported delivered_unconfirmed: %s", trim(sendText, 400))
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		tr, terr := s.agentTranscriptHTTP(overseer)
		if terr != nil {
			return terr
		}
		journal, _ := tr["journal"].(string)
		if strings.Contains(journal, token) {
			return fmt.Errorf("token reached the overseer transcript during pause")
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

	lift, err := s.MCPToolCall("jevons_fleet_intent", map[string]any{
		"state":  "working",
		"actor":  "owner",
		"reason": "owner pause lift",
	})
	if err != nil {
		return fmt.Errorf("lift: %w", err)
	}
	if !strings.Contains(lift, "now working") {
		return fmt.Errorf("lift reply: %s", trim(lift, 400))
	}
	if err := s.requireOverseerSession(overseer, session); err != nil {
		return fmt.Errorf("after lift: %w", err)
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
