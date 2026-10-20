// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// jMidTurnAnswer (🎯T902): the overseer asks a busy worker a question; the
// worker takes it between two tool calls, answers, and carries on. The answer
// must reach the overseer when it is given — before the worker's turn ends —
// not only inside the turn-end report.
func (s *suite) jMidTurnAnswer() error {
	return s.withIsolatedBroker((*suite).midTurnAnswerWithBroker)
}

// relayLine is what the daemon logs when it hands a mid-turn answer on.
const relayLine = "🎯T902 relayed a mid-turn answer"

func (s *suite) midTurnAnswerWithBroker() error {
	id := fmt.Sprintf("orch-midturn-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "midturn-answer-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": s.provider, "owner_asked": true,
		"prompt": "Run exactly `sleep 30` with your Bash tool. Then run exactly `sleep 30` again, as a second, separate Bash call. " +
			"Then reply with exactly SLEPTTWICE. If a message arrives while you are doing this, answer it in one short sentence " +
			"first and then carry on with the remaining sleep.",
	}); err != nil {
		return fmt.Errorf("spawn busy worker: %w", err)
	}
	if err := s.waitAgentPhase(id, func(p string) bool { return p != "" && p != "idle" }, 90*time.Second); err != nil {
		return fmt.Errorf("worker never became busy: %w", err)
	}
	time.Sleep(10 * time.Second) // inside the first sleep

	// The overseer's question, on the overseer's own path.
	sentAt := time.Now()
	out, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": id, "actor": "jevons",
		"text": "Overseer here: what is the " + j38Question + "? Answer in one word, then carry on.",
	})
	if err != nil {
		return fmt.Errorf("overseer send: %w", err)
	}
	if !strings.Contains(out, "steered") {
		return fmt.Errorf("the overseer's question to a busy worker was not steered: %s", out)
	}

	// The relay must happen while the worker is still working.
	deadline := time.Now().Add(150 * time.Second)
	var relayedAt time.Time
	for time.Now().Before(deadline) {
		if logHasRelay(s.logPath, id) {
			relayedAt = time.Now()
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if relayedAt.IsZero() {
		return fmt.Errorf("no mid-turn answer was relayed to the overseer for %s", id)
	}
	// The relay is an agent note to an idle overseer, handed off by its own
	// drain, so its batch receipt settles it: status=sent. Before that read
	// existed, every such relay logged delivered_unconfirmed and nothing
	// later settled it (gate 252cf6ac).
	if line := relayLogLine(s.logPath, id); !strings.Contains(line, "status=sent") {
		return fmt.Errorf("the mid-turn answer relay was not settled by its receipt: %s", line)
	}
	if err := s.waitRepliesAfterQuestion(id, 150*time.Second, "PARIS", "SLEPTTWICE"); err != nil {
		return fmt.Errorf("the worker did not finish its turn: %w", err)
	}
	ahead := time.Since(relayedAt)
	if ahead < 5*time.Second {
		return fmt.Errorf("the answer reached the overseer only %s before the turn ended; it was not relayed mid-turn", ahead.Round(time.Second))
	}
	fmt.Printf("J38: the worker's mid-turn answer reached the overseer %s before its turn ended (asked at %s)\n",
		ahead.Round(time.Second), sentAt.Format("15:04:05"))
	if raw, err := os.ReadFile(s.logPath); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "🎯T902") && strings.Contains(line, id) {
				fmt.Println("J38:", line)
			}
		}
	}
	return nil
}

// logHasRelay reports whether the daemon log records a relay from agent.
func logHasRelay(path, agent string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, relayLine) && strings.Contains(line, "agent="+agent) {
			return true
		}
	}
	return false
}

// relayLogLine is the daemon's relay record for agent, or "".
func relayLogLine(path, agent string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, relayLine) && strings.Contains(line, "agent="+agent) {
			return line
		}
	}
	return ""
}

// j38Question is the overseer's steered question; its row is the boundary
// the worker's later text must be stored after.
const j38Question = "capital of France"

// repliesAfterQuestion reports whether every one of wants appears in a reply
// row stored after the steered question's row, and in none at or before it
// (🎯T1089). The canonical store used to fold a turn's assistant text into
// the row its stream opened: in the second full suite on 024bbdf1 the
// worker's "I'll run the first `sleep 30` now." opened row 2, and "Paris"
// and "SLEPTTWICE" were later folded into that same row, above the tool
// call and the question they follow. The oracle now refuses that shape.
func repliesAfterQuestion(now []any, wants ...string) error {
	text := func(item any) (role, upper string) {
		turn, _ := item.(map[string]any)
		role, _ = turn["role"].(string)
		raw, _ := json.Marshal(turn["raw"])
		return role, strings.ToUpper(string(raw))
	}
	asked := -1
	for i, item := range now {
		if role, got := text(item); role == "user" && strings.Contains(got, strings.ToUpper(j38Question)) {
			asked = i
		}
	}
	if asked < 0 {
		return fmt.Errorf("the steered question is not in the transcript")
	}
	for _, want := range wants {
		want = strings.ToUpper(want)
		after := false
		for i, item := range now {
			role, got := text(item)
			if role != "assistant" && role != "agent_note" || !strings.Contains(got, want) {
				continue
			}
			if i <= asked {
				return fmt.Errorf("%q was stored in row %d, at or above the question in row %d", want, i+1, asked+1)
			}
			after = true
		}
		if !after {
			return fmt.Errorf("no reply row after the question contains %q", want)
		}
	}
	return nil
}

// waitRepliesAfterQuestion polls name's transcript until
// repliesAfterQuestion holds. A reply folded above the question fails at
// once: waiting cannot move it.
func (s *suite) waitRepliesAfterQuestion(name string, timeout time.Duration, wants ...string) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			return err
		}
		now, _ := payload["turns"].([]any)
		last = repliesAfterQuestion(now, wants...)
		if last == nil {
			return nil
		}
		if strings.Contains(last.Error(), "at or above the question") {
			return last
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("within %s: %w", timeout, last)
}
