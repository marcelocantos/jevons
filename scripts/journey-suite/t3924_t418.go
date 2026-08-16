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

func (s *suite) eventsPath() string {
	return filepath.Join(s.stateDir, "logs", "events.jsonl")
}

func printMatching(label, blob string, needles ...string) {
	fmt.Println("---", label, "---")
	for _, line := range strings.Split(blob, "\n") {
		for _, n := range needles {
			if strings.Contains(line, n) {
				fmt.Println(line)
				break
			}
		}
	}
}

func newTail(prev, now []byte) string {
	if len(now) <= len(prev) {
		return ""
	}
	return string(now[len(prev):])
}

// jT3924CheckpointResume is the 🎯T392.4 isolate journey: a worker that
// crosses the depth ceiling is asked to checkpoint and then resumed in a
// new turn; a below-ceiling control still completes.
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
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": name})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": name, "workdir": work, "description": "T392.4 checkpoint journey worker",
	})
	if out := asOutage("spawn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("start: %w (%s)", err, trim(spawnOut, 80))
	}

	ctrl, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": name, "text": "Reply with exactly PONG and do not call any tools.",
	})
	if out := asOutage("control", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("control send: %w", err)
	}
	if strings.TrimSpace(ctrl) == "" {
		return fmt.Errorf("control turn produced no reply")
	}
	fmt.Println("T392.4 control reply:", trim(ctrl, 240))

	deep := "You MUST use run_terminal_command four times in this turn: echo T3924-A, then echo T3924-B, then echo T3924-C, then echo T3924-D. Do not stop after the first call."
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": name, "text": deep,
	}); err != nil {
		if out := asOutage("deep", err); out != nil {
			return out
		}
		return fmt.Errorf("deep send: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	var sawAsk, sawResume bool
	var blob string
	for time.Now().Before(deadline) {
		body, _ := os.ReadFile(s.eventsPath())
		blob = string(body)
		if strings.Contains(blob, "checkpoint_asked") {
			sawAsk = true
		}
		if strings.Contains(blob, "checkpoint_resume") {
			sawResume = true
		}
		if sawAsk && sawResume {
			break
		}
		time.Sleep(2 * time.Second)
	}
	printMatching("T392.4 eventlog", blob, "checkpoint_asked", "checkpoint_resume")
	if !sawAsk {
		return fmt.Errorf("deep turn produced no checkpoint_asked in %s", s.eventsPath())
	}
	if !sawResume {
		return fmt.Errorf("checkpoint asked but no checkpoint_resume")
	}
	fmt.Println("T392.4 observed checkpoint_asked and checkpoint_resume")
	return nil
}

// jT418QueueBounce requires the second send to be queued, snapshots
// logs, bounces, and asserts a NEW recover/undelivered/re-offer line.
func (s *suite) jT418QueueBounce() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	name := fmt.Sprintf("jv-t418q-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t418-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": name})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": name, "workdir": work, "description": "T418 queue-bounce worker",
	})
	if out := asOutage("spawn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("start: %w (%s)", err, trim(spawnOut, 80))
	}

	go func() {
		_, _ = s.mcpText("jevons_thread_direct", map[string]any{
			"id": name, "text": "Count slowly from 1 to 40 in your reply, then say DONE. Do not finish early.",
		})
	}()

	token := "QUEUED-TOKEN-T418-SURVIVE"
	var queued string
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(800 * time.Millisecond)
		out, err := s.mcpText("jevons_agent_send", map[string]any{
			"name": name, "text": token + " after the bounce.", "actor": "jevons",
		})
		if asOutage("queue send", err) != nil {
			return asOutage("queue send", err)
		}
		if err != nil {
			// A busy in-flight send often returns queued as text, not err.
			if strings.Contains(strings.ToLower(out+" "+err.Error()), "queued") {
				queued = out + " " + err.Error()
				break
			}
			continue
		}
		if strings.Contains(strings.ToLower(out), "queued") {
			queued = out
			break
		}
	}
	if queued == "" || !strings.Contains(strings.ToLower(queued), "queued") {
		return fmt.Errorf("second send never queued (need queued, not sent/delivered): last=%s", trim(queued, 240))
	}
	fmt.Println("T418 queued reply:", trim(queued, 240))

	preEvents, _ := os.ReadFile(s.eventsPath())
	preLogs, _ := os.ReadFile(s.logPath)

	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce: %w", err)
	}

	needles := []string{
		"recovered a queued message",
		"UNDELIVERED",
		"backlog sweep: re-offering",
		"🎯T418 recovered",
	}
	wait := time.Now().Add(45 * time.Second)
	for time.Now().Before(wait) {
		ev, _ := os.ReadFile(s.eventsPath())
		lg, _ := os.ReadFile(s.logPath)
		delta := newTail(preEvents, ev) + "\n" + newTail(preLogs, lg)
		for _, n := range needles {
			if strings.Contains(delta, n) {
				printMatching("T418 post-bounce NEW lines", delta, needles...)
				fmt.Println("T418 bounce-survive-or-tell: saw", n)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("no NEW post-bounce recover/UNDELIVERED/re-offer line (pre-bounce token does not count)")
}

// jT418HandoverMute plants a stale pending handover and a queued send,
// then stops every registered agent so nobody can press Enter, and
// asserts the daemon reports MUTE.
func (s *suite) jT418HandoverMute() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	name := fmt.Sprintf("jv-t418h-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t418-handover")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": name})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": name, "workdir": work, "description": "T418 mute fixture worker",
	})
	if out := asOutage("spawn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("start: %w (%s)", err, trim(spawnOut, 80))
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
	if err := os.WriteFile(filepath.Join(work, "pred.jsonl"), []byte("{}\n"), 0o644); err != nil {
		return err
	}

	go func() {
		_, _ = s.mcpText("jevons_thread_direct", map[string]any{
			"id": name, "text": "Count slowly from 1 to 40 in your reply, then say DONE.",
		})
	}()
	var queued string
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(800 * time.Millisecond)
		out, err := s.mcpText("jevons_agent_send", map[string]any{
			"name": name, "text": "MUTE-TOKEN-T418 queued while everyone is about to be stuck.", "actor": "jevons",
		})
		blob := strings.ToLower(out)
		if err != nil {
			blob += " " + strings.ToLower(err.Error())
		}
		if strings.Contains(blob, "queued") {
			queued = out
			if err != nil {
				queued += " " + err.Error()
			}
			break
		}
	}
	if queued == "" {
		return fmt.Errorf("mute fixture never accepted a queued send")
	}
	fmt.Println("T418 mute queued reply:", trim(queued, 240))

	preLogs, _ := os.ReadFile(s.logPath)
	preEvents, _ := os.ReadFile(s.eventsPath())

	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	if len(agents) == 0 {
		return fmt.Errorf("mute fixture: no registered agents")
	}
	var overseer string
	var others []string
	for _, a := range agents {
		if a.Name == "jevons" {
			overseer = a.Name
			continue
		}
		others = append(others, a.Name)
	}
	for _, n := range others {
		_, _ = s.AgentStop(n)
	}
	if overseer != "" {
		_, _ = s.AgentStop(overseer)
	}

	wait := time.Now().Add(30 * time.Second)
	var sawMute bool
	for time.Now().Before(wait) {
		lg, _ := os.ReadFile(s.logPath)
		ev, _ := os.ReadFile(s.eventsPath())
		delta := newTail(preLogs, lg) + "\n" + newTail(preEvents, ev)
		if strings.Contains(delta, "MUTE:") || strings.Contains(delta, "fleet mute") {
			printMatching("T418 MUTE NEW lines", delta, "MUTE:", "fleet mute")
			fmt.Println("T418 no-rescuer mute observed")
			sawMute = true
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !sawMute {
		return fmt.Errorf("no MUTE report after stopping every registered agent with queued work")
	}

	// Drive SweepHandovers: bounce so NotifyDaemonRestarted retries or
	// surfaces the planted stale pending. The Put above is unused unless
	// we assert a NEW handover line after this bounce.
	preHandoverLogs, _ := os.ReadFile(s.logPath)
	preHandoverEvents, _ := os.ReadFile(s.eventsPath())
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("handover bounce: %w", err)
	}
	needles := []string{
		"UNDELIVERED HANDOVER",
		"pending handover surfaced",
		"handover retry",
		"🎯T418 handover retry",
		"🎯T418 pending handover surfaced",
	}
	wait = time.Now().Add(45 * time.Second)
	for time.Now().Before(wait) {
		lg, _ := os.ReadFile(s.logPath)
		ev, _ := os.ReadFile(s.eventsPath())
		delta := newTail(preHandoverLogs, lg) + "\n" + newTail(preHandoverEvents, ev)
		for _, n := range needles {
			if strings.Contains(delta, n) {
				printMatching("T418 handover NEW lines", delta, needles...)
				fmt.Println("T418 handover retried or surfaced:", n)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("planted pending handover was not retried or surfaced after bounce (SweepHandovers never fired)")
}
