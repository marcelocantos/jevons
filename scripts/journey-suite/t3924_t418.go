// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// jT3924CheckpointResume is the 🎯T392.4 isolate journey: a worker that
// crosses the depth ceiling is asked to checkpoint and then resumed in a
// new turn; a below-ceiling control still completes. Interacts with a
// live fleet agent (not a hermetic stub).
func (s *suite) jT3924CheckpointResume() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	s.daemonEnv = []string{"JEVONS_TURNDEPTH_CEILING=4", "JEVONS_TURNDEPTH_INTERRUPT=off"}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce with ceiling: %w", err)
	}
	defer func() { s.daemonEnv = nil }()

	name := fmt.Sprintf("jv-t3924-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t3924-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.AgentKill(name, "jevons") }()

	if _, err := s.AgentStart(name, work, "jevons", "jevons"); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if err := s.MustAgentRunning(name); err != nil {
		return err
	}

	// Control: a below-ceiling turn still completes.
	out, err := s.AgentSend(name, "Reply with exactly PONG and do not call any tools.")
	if err != nil {
		return fmt.Errorf("control send: %w", err)
	}
	if strings.Contains(strings.ToLower(out), "not delivered") && strings.Contains(strings.ToLower(out), "not queued") {
		return fmt.Errorf("control send refused: %s", trim(out, 200))
	}

	// Deep turn: ask for several shell calls so the daemon sees tool_use.
	deep := "Call the shell four times in this turn: echo T3924-A, then echo T3924-B, then echo T3924-C, then echo T3924-D. Do not stop after the first call."
	if _, err := s.AgentSend(name, deep); err != nil {
		return fmt.Errorf("deep send: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	var sawAsk, sawResume bool
	for time.Now().Before(deadline) {
		body, _ := os.ReadFile(s.eventsPath())
		text := string(body)
		if strings.Contains(text, "checkpoint_asked") || strings.Contains(text, "checkpoint asked") {
			sawAsk = true
		}
		if strings.Contains(text, "checkpoint_resume") {
			sawResume = true
		}
		if sawAsk && sawResume {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !sawAsk {
		// The agent may have finished in fewer than 4 tool calls. Drive
		// the shipped observe path is hermetic; the journey still interacted
		// with a live agent (start + two sends). Fail only if the agent
		// never ran.
		if err := s.MustAgentRunning(name); err != nil {
			return fmt.Errorf("deep turn: no checkpoint_asked and agent is gone: %w", err)
		}
		return fmt.Errorf("deep turn produced no checkpoint_asked in %s (agent ran; ceiling may not have been reached)", s.eventsPath())
	}
	if !sawResume {
		return fmt.Errorf("checkpoint asked but no checkpoint_resume — successor turn was not scheduled")
	}
	return nil
}

func (s *suite) eventsPath() string {
	return filepath.Join(s.stateDir, "logs", "events.jsonl")
}

// jT418QueueBounce is the 🎯T418 isolate journey: an accepted queued send
// survives a daemon bounce and is delivered or the sender is told.
func (s *suite) jT418QueueBounce() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	name := fmt.Sprintf("jv-t418q-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t418-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.AgentKill(name, "jevons") }()

	if _, err := s.AgentStart(name, work, "jevons", "jevons"); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if err := s.MustAgentRunning(name); err != nil {
		return err
	}

	// Occupy the turn so the second send queues.
	if _, err := s.AgentSend(name, "Think for a while: count slowly from 1 to 20 in your reply, then say DONE."); err != nil {
		return fmt.Errorf("occupy: %w", err)
	}
	queued, err := s.AgentSend(name, "QUEUED-TOKEN-T418-SURVIVE after the bounce.")
	if err != nil {
		return fmt.Errorf("queue send: %w", err)
	}
	low := strings.ToLower(queued)
	if !strings.Contains(low, "queued") && !strings.Contains(low, "sent") && !strings.Contains(low, "delivered") {
		return fmt.Errorf("second send was not accepted: %s", trim(queued, 200))
	}

	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce: %w", err)
	}

	// After bounce the durable queue must still exist or the payload
	// must have been delivered / surfaced.
	sendq := filepath.Join(s.stateDir, "sendq")
	events, _ := os.ReadFile(s.eventsPath())
	logs, _ := os.ReadFile(s.logPath)
	blob := string(events) + "\n" + string(logs)
	if strings.Contains(blob, "QUEUED-TOKEN-T418-SURVIVE") ||
		strings.Contains(blob, "recovered a queued message") ||
		strings.Contains(blob, "UNDELIVERED") {
		return nil
	}
	if _, err := os.Stat(sendq); err == nil {
		entries, _ := os.ReadDir(sendq)
		if len(entries) > 0 {
			return nil
		}
	}
	// Agent may have drained it after restart — look at the transcript.
	tr, _ := s.agentTranscriptHTTP(name)
	if dump, _ := tr["text"].(string); strings.Contains(dump, "QUEUED-TOKEN-T418-SURVIVE") {
		return nil
	}
	if raw, _ := tr["raw"].(string); strings.Contains(raw, "QUEUED-TOKEN-T418-SURVIVE") {
		return nil
	}
	return fmt.Errorf("queued send vanished across bounce; no recover/undelivered/transcript evidence")
}

// jT418HandoverMute covers clause 5 (pending handover retried or surfaced)
// and clause 6 (mute reported when nobody can press Enter). Interacts with
// a live agent so the mute control has a real seat.
func (s *suite) jT418HandoverMute() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	name := fmt.Sprintf("jv-t418h-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t418-handover")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.AgentKill(name, "jevons") }()

	if _, err := s.AgentStart(name, work, "jevons", "jevons"); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	store := handover.NewStore(filepath.Join(s.stateDir, "handover"))
	if err := store.Put(handover.Pending{
		Agent:          name,
		From:           "claude",
		To:             "grok",
		TranscriptPath: filepath.Join(work, "pred.jsonl"),
		CreatedAt:      time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("plant pending: %w", err)
	}
	// Give the predecessor file a body so Usable is true.
	if err := os.WriteFile(filepath.Join(work, "pred.jsonl"), []byte("{}\n"), 0o644); err != nil {
		return err
	}

	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce: %w", err)
	}

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		logs, _ := os.ReadFile(s.logPath)
		events, _ := os.ReadFile(s.eventsPath())
		blob := string(logs) + string(events)
		if strings.Contains(blob, "handover retry") ||
			strings.Contains(blob, "handover retry") ||
			strings.Contains(blob, "UNDELIVERED HANDOVER") ||
			strings.Contains(blob, "pending handover surfaced") ||
			strings.Contains(blob, "handover reaped") ||
			strings.Contains(blob, "MUTE:") {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("planted pending handover was not retried, surfaced, or reaped after bounce")
}
