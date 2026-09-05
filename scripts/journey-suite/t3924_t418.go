// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/sendq"
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

// jT418QueueBounce proves that one request, accepted while a real agent is
// busy, produces its own shell effect after restart on the selected provider.
// Recovery logs and a "queued" reply alone cannot establish this property.
func (s *suite) jT418QueueBounce() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	work, err := os.MkdirTemp(s.stateDir, "t418-work-")
	if err != nil {
		return err
	}
	name := "jv-" + filepath.Base(work)
	token := "completed-" + filepath.Base(work)
	ready := filepath.Join(work, "busy-ready")
	release := filepath.Join(work, "release-busy")
	result := filepath.Join(work, "queued-result")
	defer func() {
		// Release any surviving shell even when a precondition fails. The
		// agent also has a bounded wait, independent of this cleanup.
		_ = os.WriteFile(release, nil, 0o600)
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": name})
	}()

	spawnOut, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": name, "workdir": work, "description": "T418 queue-bounce worker",
		"provider": string(s.provider),
	})
	if out := asOutage("spawn", err); out != nil {
		return out
	}
	if err != nil {
		return fmt.Errorf("start: %w (%s)", err, trim(spawnOut, 80))
	}

	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	const busyLimitSeconds = 180
	busyCommand := fmt.Sprintf("printf 'ready\\n' > %s; n=0; while [ ! -f %s ] && [ \"$n\" -lt %d ]; do sleep 1; n=$((n+1)); done; test -f %s",
		quote(ready), quote(release), busyLimitSeconds, quote(release))
	busyDone := make(chan error, 1)
	go func() {
		_, err := s.mcpText("jevons_agent_send", map[string]any{
			"name": name, "actor": "jevons", "text": "Use your shell tool to run exactly this command and wait for it to return. Do not create the release file, background the command, or finish early. This bounded wait is part of a restart test:\n" + busyCommand,
		})
		busyDone <- err
	}()
	deadline := time.Now().Add(turnTimeout)
	for {
		if body, err := os.ReadFile(ready); err == nil && string(body) == "ready\n" {
			break
		} else if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("busy marker: %w", err)
		}
		select {
		case err := <-busyDone:
			if outage := asOutage("busy work", err); outage != nil {
				return outage
			}
			if err != nil {
				return fmt.Errorf("busy work submission: %w", err)
			}
			// Agent send acknowledges a started turn before its shell runs.
			// A nil reply is neither the busy marker nor proof of completion.
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent did not establish the busy-work marker")
		}
		time.Sleep(200 * time.Millisecond)
	}
	preLogs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if err := queueJourneyProvider(preLogs, name, string(s.provider)); err != nil {
		return err
	}
	payload := fmt.Sprintf("Use your shell tool to run exactly: printf '%%s\\n' %s >> %s\nRun it once. Do not delegate. Then reply with only the value you wrote.", quote(token), quote(result))
	// One acceptance only. Retrying until some reply says "queued" can
	// accidentally prove a different send, or create duplicate obligations.
	out, sendErr := s.mcpText("jevons_agent_send", map[string]any{
		"name": name, "text": payload, "actor": "jevons",
	})
	if outage := asOutage("queue send", sendErr); outage != nil {
		return outage
	}
	store := sendq.NewStore(filepath.Join(s.stateDir, "sendq"))
	entries, err := store.Snapshot(name)
	if err != nil {
		return err
	}
	entry, err := queueJourneyAcceptance(entries, payload)
	if err != nil {
		return fmt.Errorf("%w (send reply: %s; error: %v)", err, trim(out, 240), sendErr)
	}
	if _, err := os.Stat(release); !os.IsNotExist(err) {
		return fmt.Errorf("busy release appeared before the harness released it: %v", err)
	}
	if err := queueJourneyNoResult(result); err != nil {
		return err
	}
	fmt.Printf("T418 accepted entry=%s provider=%s while shell work was held\n", entry.ID, s.provider)
	if err := s.signalStop(8 * time.Second); err != nil {
		return fmt.Errorf("stop before bounce: %w", err)
	}
	if err := queueJourneyNoResult(result); err != nil {
		return fmt.Errorf("work completed before restart: %w", err)
	}
	// The replacement process must supply its own launch evidence.
	preLogs, err = os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if err := s.startDaemon(); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		return err
	}
	deadline = time.Now().Add(turnTimeout)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(result)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if string(body) != token+"\n" {
				return fmt.Errorf("queued request produced a stale, unrelated, or duplicate result: %q", body)
			}
			logs, err := os.ReadFile(s.logPath)
			if err != nil {
				return err
			}
			if err := queueJourneyProvider([]byte(newTail(preLogs, logs)), name, string(s.provider)); err != nil {
				return fmt.Errorf("replacement agent: %w", err)
			}
			entries, err := store.Snapshot(name)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Printf("T418 entry=%s completed its fresh shell effect after restart on %s; queue settled\n", entry.ID, s.provider)
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("entry=%s did not complete its fresh shell effect and settle after restart", entry.ID)
}

// The named runtime launch, not the requested default or another agent's
// launch, establishes which provider actually executed this journey.
func queueJourneyProvider(logs []byte, name, expected string) error {
	found := false
	for _, line := range strings.Split(string(logs), "\n") {
		event, err := queueJourneyLogFields(line)
		if err != nil || event["msg"] != "agent started" || event["name"] != name {
			continue
		}
		if event["provider"] != expected {
			return fmt.Errorf("agent %s launched on %q, requested %q", name, event["provider"], expected)
		}
		found = true
	}
	if !found {
		return fmt.Errorf("no named runtime launch evidence for %s on %s", name, expected)
	}
	return nil
}

// jevonsd uses slog.TextHandler. Consume whole quoted values so a field-like
// string inside a message cannot masquerade as the launch name or provider.
func queueJourneyLogFields(line string) (map[string]string, error) {
	fields := make(map[string]string)
	for line = strings.TrimSpace(line); line != ""; line = strings.TrimSpace(line) {
		key, rest, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t\"\\") || rest == "" {
			return nil, fmt.Errorf("malformed log field")
		}
		var value string
		if rest[0] == '"' {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return nil, err
			}
			value, err = strconv.Unquote(quoted)
			if err != nil {
				return nil, err
			}
			line = rest[len(quoted):]
			if line != "" && line[0] != ' ' && line[0] != '\t' {
				return nil, fmt.Errorf("malformed quoted field separator")
			}
		} else {
			value, line, _ = strings.Cut(rest, " ")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("duplicate log field")
		}
		fields[key] = value
	}
	return fields, nil
}

func queueJourneyAcceptance(entries []sendq.Entry, payload string) (sendq.Entry, error) {
	if len(entries) != 1 || entries[0].ID == "" || entries[0].Text != payload || entries[0].State != sendq.Pending {
		return sendq.Entry{}, fmt.Errorf("expected exactly one matching durable pending acceptance; got %d entries", len(entries))
	}
	return entries[0], nil
}

func queueJourneyNoResult(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("queued result already exists before restart")
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
	if st, err := os.Stat(s.handoverPath(name)); err != nil {
		return fmt.Errorf("handover record missing after Put: %w", err)
	} else {
		fmt.Println("T418 planted handover", s.handoverPath(name), "bytes", st.Size())
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
	if ents, err := os.ReadDir(filepath.Join(s.stateDir, "handover")); err != nil {
		fmt.Println("T418 handover dir after bounce:", err)
	} else {
		fmt.Println("T418 handover dir after bounce:", len(ents), "entries")
		for _, e := range ents {
			fmt.Println("  ", e.Name())
		}
	}
	needles := []string{
		"UNDELIVERED HANDOVER",
		"pending handover surfaced",
		"handover retry",
		"handover classify",
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
