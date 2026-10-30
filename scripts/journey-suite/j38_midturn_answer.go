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

const (
	j38SleepCalls   = 6
	j38SleepSeconds = 8
)

// relayLine is what the daemon logs when it hands a mid-turn answer on.
const relayLine = "🎯T902 relayed a mid-turn answer"

func (s *suite) midTurnAnswerWithBroker() (result error) {
	id := fmt.Sprintf("orch-midturn-%d", time.Now().Unix()%100000)
	defer func() {
		if result != nil {
			s.diagnoseMidTurnFailure(id)
		}
	}()
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
		"prompt": fmt.Sprintf("Run `sleep %d` in %d SEPARATE Bash calls in sequence; do not finish your turn before all calls. ", j38SleepSeconds, j38SleepCalls) +
			"After the sixth call reply SLEPTTWICE. If a question arrives, answer it in a NON-TERMINAL assistant " +
			"message, THEN continue making the remaining Bash calls in the SAME turn. Answering and ending the turn " +
			"without another Bash call does not count as answering mid-turn.",
	}); err != nil {
		return fmt.Errorf("spawn busy worker: %w", err)
	}
	if err := s.waitAgentPhase(id, func(p string) bool { return p != "" && p != "idle" }, 90*time.Second); err != nil {
		return fmt.Errorf("worker never became busy: %w", err)
	}
	// Six 30-second calls used to exceed the 150-second post-relay deadline:
	// both full gates 9a74e031 and ccd84972 recorded Paris relayed and then
	// an empty end_turn 150 seconds later, with no SLEPTTWICE. Six 8-second
	// calls retain a real tool boundary and >5 seconds of post-relay work
	// without making the model finish at the oracle deadline.
	// Sidecar Bash calls can return much faster than their requested sleep
	// (for instance on a sandbox refusal). Waiting ten wall-clock seconds let
	// this worker finish and notify the overseer before the question was sent.
	// Steer as soon as the first tool call is observed instead.
	if err := s.waitWorkerToolUse(id, 45*time.Second); err != nil {
		return err
	}

	// The receipt assertion is for an idle-overseer handoff. A worker that
	// finishes its work before the steer can notify the overseer and occupy
	// its turn; a subsequent relay is correctly queued, not status=sent.
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" || p == "" }, time.Second); err != nil {
		return fmt.Errorf("overseer was already busy before the question; idle-handoff receipt cannot be tested: %w", err)
	}
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
		// A terminal answer is not a mid-turn answer: the capture is
		// deliberately dropped at end_turn. Waiting the full 150 seconds
		// cannot turn that same answer into a relay.
		if payload, err := s.agentTranscriptHTTP(id); err == nil {
			rows, _ := payload["turns"].([]any)
			if terminalJ38Answer(rows) {
				return fmt.Errorf("worker answered only at terminal end_turn, not mid-turn for %s", id)
			}
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

// terminalJ38Answer recognizes an answer only when it follows the steered
// question and the very answer row ends the turn. A non-terminal answer must
// still earn a relay; this does not relax the relay assertion.
func terminalJ38Answer(rows []any) bool {
	asked := false
	for _, row := range rows {
		turn, _ := row.(map[string]any)
		raw, _ := json.Marshal(turn["raw"])
		if turn["role"] == "user" && strings.Contains(strings.ToUpper(string(raw)), strings.ToUpper(j38Question)) {
			asked = true
		}
		if asked && turn["role"] == "assistant" && strings.Contains(strings.ToUpper(string(raw)), "PARIS") &&
			strings.Contains(string(raw), `"stop_reason":"end_turn"`) {
			return true
		}
	}
	return false
}

// waitWorkerToolUse proves the first turn has begun actual work, not merely
// that the registry reports a non-idle launch phase.
func (s *suite) waitWorkerToolUse(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			return err
		}
		if strings.Contains(payload["journal"].(string), `"type":"tool_use"`) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("worker did not start its first tool call within %s", timeout)
}

// diagnoseMidTurnFailure prints the actual worker rows while the isolate is still
// up (before withIsolatedBroker tears it down). A missing relay is only a
// model-behaviour failure if the worker never answered after the question.
func (s *suite) diagnoseMidTurnFailure(id string) {
	line := relayLogLine(s.logPath, id)
	if log, err := os.ReadFile(s.logPath); err == nil {
		for _, item := range strings.Split(string(log), "\n") {
			if strings.Contains(item, "msg=notify_queue") || strings.Contains(item, "send receipt lookup") || strings.Contains(item, relayLine) {
				fmt.Println("J38 notify evidence:", item)
			}
		}
	}
	payload, err := s.agentTranscriptHTTP(id)
	if err != nil {
		fmt.Printf("J38 diagnosis: transcript unavailable: %v; relay=%q\n", err, line)
		return
	}
	rows, _ := payload["turns"].([]any)
	asked := -1
	for i, row := range rows {
		turn, _ := row.(map[string]any)
		if turn["role"] == "user" {
			raw, _ := json.Marshal(turn["raw"])
			if strings.Contains(strings.ToUpper(string(raw)), strings.ToUpper(j38Question)) {
				asked = i
			}
		}
	}
	answered := false
	terminalAnswer := false
	fmt.Printf("J38 diagnosis: worker=%s question-row=%d rows-through-deadline=%d relay=%q\n", id, asked+1, len(rows), line)
	for i := asked + 1; i < len(rows); i++ {
		turn, _ := rows[i].(map[string]any)
		raw, _ := json.Marshal(turn["raw"])
		fmt.Printf("J38 worker row %d role=%v raw=%s\n", i+1, turn["role"], raw)
		if (turn["role"] == "assistant" || turn["role"] == "agent_note") && strings.Contains(strings.ToUpper(string(raw)), "PARIS") {
			if strings.Contains(string(raw), `"stop_reason":"end_turn"`) {
				terminalAnswer = true
			} else {
				answered = true
			}
		}
	}
	fmt.Println("J38 diagnosis:", classifyMidTurnFailure(asked >= 0, answered, terminalAnswer, line))
}

// classifyMidTurnFailure refuses to call absent evidence a model failure.
func classifyMidTurnFailure(questionFound, answered, terminalAnswer bool, relay string) string {
	switch {
	case relay != "":
		return "worker answered; daemon logged relay (inspect receipt/status, not a missing relay)"
	case !questionFound:
		return "question missing from worker transcript; cannot distinguish model from delivery"
	case answered:
		return "(b) worker answered non-terminally after steer; daemon did not log a mid-turn relay — relay-path bug"
	case terminalAnswer:
		return "(a) worker answered only at terminal end_turn, not mid-turn — model-behaviour flake"
	default:
		return "(a) worker never answered after steer before deadline — model-behaviour flake"
	}
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
