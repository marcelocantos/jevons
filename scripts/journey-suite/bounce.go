// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/upgrade"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

func (s *suite) startDaemon() error {
	if s == nil {
		return fmt.Errorf("start daemon: no suite")
	}
	// 🎯T526: refuse to start when the isolate port is already held.
	// Otherwise a bind failure exits the child while waitReady adopts the
	// foreign daemon and journey MCP mints into daily ~/.jevons.
	if err := portguard.ErrIfPortHeld(s.port); err != nil {
		return err
	}
	cmd := exec.Command(s.daemonBin,
		"-config", s.cfgPath,
		"-port", fmt.Sprint(s.port),
		"-bind", "127.0.0.1",
		"-workdir", s.workdir,
	)
	cmd.Stdout = s.logFile
	cmd.Stderr = s.logFile
	cmd.Dir = s.workdir
	if len(s.daemonEnv) > 0 {
		cmd.Env = append(os.Environ(), s.daemonEnv...)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	s.cmd = cmd
	s.cmdWait = waitCh
	if err := s.waitReadyOwned(readyTimeout); err != nil {
		_ = s.signalStop(2 * time.Second)
		return err
	}
	return nil
}

func (s *suite) signalStop(timeout time.Duration) error {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	signalErr := s.cmd.Process.Signal(os.Interrupt)
	waitCh := s.cmdWait
	if waitCh == nil {
		waitCh = make(chan error, 1)
		cmd := s.cmd
		go func() { waitCh <- cmd.Wait() }()
	}
	select {
	case err := <-waitCh:
		s.cmd = nil
		s.cmdWait = nil
		if signalErr != nil {
			return fmt.Errorf("signal daemon: %w", signalErr)
		}
		if err != nil {
			return fmt.Errorf("daemon exited while draining: %w", err)
		}
		return nil
	case <-time.After(timeout):
		_ = s.cmd.Process.Kill()
		<-waitCh
		s.cmd = nil
		s.cmdWait = nil
		return fmt.Errorf("daemon did not exit after interrupt")
	}
}

// waitReadyOwned is waitReady plus 🎯T526: ready only when OUR child is the
// TCP listener. Foreign /health must not count — that is how J20 minted into
// a leftover :13715 on daily ~/.jevons after "address already in use".
func (s *suite) waitReadyOwned(d time.Duration) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return fmt.Errorf("wait ready: no daemon process")
	}
	ourPID := s.cmd.Process.Pid
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		if s.cmdWait != nil {
			select {
			case err := <-s.cmdWait:
				s.cmd = nil
				s.cmdWait = nil
				if err == nil {
					err = fmt.Errorf("exit 0")
				}
				return fmt.Errorf("daemon exited before ready (likely bind failure); refusing foreign listener on %s: %w", s.host, err)
			default:
			}
		}
		if err := portguard.ErrIfForeignListener(s.port, ourPID); err != nil {
			return err
		}
		if pid, err := portguard.ListenPID(s.port); err == nil && pid == ourPID {
			if err := probeReady(s.host); err == nil {
				return nil
			} else {
				last = err
			}
		} else if err != nil {
			last = err
		}
		time.Sleep(400 * time.Millisecond)
	}
	if last == nil {
		last = fmt.Errorf("timeout")
	}
	return last
}

func (s *suite) bounceDrain() error {
	if err := s.signalStop(8 * time.Second); err != nil {
		// Restart anyway — a stuck child still occupies the port.
		_ = err
	}
	return s.startDaemon()
}

func (s *suite) agentsPath() string {
	return filepath.Join(s.stateDir, "agents.json")
}

func (s *suite) handoverPath(name string) string {
	return filepath.Join(s.stateDir, "handover", name+".json")
}

// The normal registry loader treats read failures as an empty registry. An
// oracle must distinguish failed observation from an actual missing seat.
func bounceRegistrySnapshot(path string) (map[string]claudia.AgentDef, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var defs []claudia.AgentDef
	if err := json.Unmarshal(body, &defs); err != nil {
		return nil, err
	}
	if defs == nil {
		return nil, fmt.Errorf("registry is null, not an agent list")
	}
	out := make(map[string]claudia.AgentDef, len(defs))
	for _, def := range defs {
		if _, duplicate := out[def.Name]; duplicate || def.Name == "" {
			return nil, fmt.Errorf("duplicate or empty registry name %q", def.Name)
		}
		out[def.Name] = def
	}
	return out, nil
}

// J14 exercises a normal SIGINT drain and restart, not crash or SIGHUP adoption.
// Stable registry IDs alone cannot prove an agent resumed: require a fresh
// post-restart answer that uses a fact supplied only before the restart.
func (s *suite) jBounceResume() error {
	// J13 intentionally migrates the overseer. Use a fresh explicitly selected
	// aside so full-suite ordering cannot certify that other backend as ours.
	id := "bounce-aside-" + uuid.NewString()
	work, err := os.MkdirTemp(s.stateDir, "bounce-work-")
	if err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "provider": string(s.provider),
		"description": "disposable normal-drain continuity probe",
	}); err != nil {
		if outage := asOutage("bounce fixture spawn", err); outage != nil {
			return outage
		}
		return fmt.Errorf("bounce fixture spawn: %w", err)
	}
	direct := func(prompt, expected string) error {
		out, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": prompt})
		if outage := asOutage("bounce direct", err); outage != nil {
			return outage
		}
		if err != nil {
			return err
		}
		if outage := replyOutage("bounce direct reply", out); outage != nil {
			return outage
		}
		if strings.TrimSpace(out) != expected {
			return fmt.Errorf("direct reply %q differs from requested %q", trim(out, 200), expected)
		}
		return nil
	}
	secret := "bounce-memory-" + uuid.NewString()
	ack := "bounce-stored-" + uuid.NewString()
	seed := "Remember this journey continuity secret for my next question: " + secret + ". Reply with exactly: " + ack
	if err := direct(seed, ack); err != nil {
		return fmt.Errorf("pre-bounce seed turn: %w", err)
	}

	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot before bounce: %w", err)
	}
	if before[id].SessionID == "" || before[id].Provider != s.provider {
		return fmt.Errorf("bounce fixture lacks its selected provider/session identity")
	}
	preLogs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if err := queueJourneyProvider(preLogs, id, string(s.provider)); err != nil {
		return fmt.Errorf("original aside: %w", err)
	}
	handovers := map[string]string{}
	for name := range before {
		body, err := os.ReadFile(s.handoverPath(name))
		if err == nil {
			if name == id {
				return fmt.Errorf("fresh bounce fixture already has a handover")
			}
			handovers[name] = string(body)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("pre-bounce handover %s: %w", name, err)
		}
	}

	if err := s.signalStop(8 * time.Second); err != nil {
		return fmt.Errorf("normal drain failed: %w", err)
	}
	// Only the replacement process may supply post-bounce launch evidence.
	drainedLogs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(drainedLogs, preLogs) {
		return fmt.Errorf("daemon log changed during drain")
	}
	normalDrain := false
	for _, line := range strings.Split(string(drainedLogs[len(preLogs):]), "\n") {
		fields, err := queueJourneyLogFields(line)
		if err == nil && fields["msg"] == "shutting down" && fields["exit_mode"] == "normal" && fields["stop_agents"] == "true" {
			normalDrain = true
		}
	}
	if !normalDrain {
		return fmt.Errorf("drain lacks normal stop-agents evidence; upgrade exit is not this journey")
	}
	preLogs = drainedLogs
	if err := s.startDaemon(); err != nil {
		return fmt.Errorf("restart: %w", err)
	}
	challenge := "bounce-now-" + uuid.NewString()
	prompt := "What journey continuity secret did I give you before the restart? Reply with exactly two words separated by one space: the saved secret, then " + challenge + ". No labels or punctuation."
	expected := secret + " " + challenge
	directErr := direct(prompt, expected)
	// Check identity even when the provider call failed. A timeout must not
	// disguise an observed session replacement as an external outage.
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot after bounce: %w", err)
	}
	for name, agent := range before {
		if next, ok := after[name]; !ok || next.SessionID != agent.SessionID || next.Provider != agent.Provider {
			return fmt.Errorf("bounce changed existing session/provider for %s", name)
		}
	}
	if directErr != nil {
		return fmt.Errorf("post-bounce aside turn: %w", directErr)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(logs, preLogs) {
		return fmt.Errorf("daemon launch log was replaced during bounce")
	}
	if err := queueJourneyProvider(logs[len(preLogs):], id, string(s.provider)); err != nil {
		return fmt.Errorf("replacement aside: %w", err)
	}

	for name := range before {
		body, err := os.ReadFile(s.handoverPath(name))
		previous, existed := handovers[name]
		if existed {
			// An older pending migration may legitimately advance or be reaped.
			// Report that residue; this fixture requires no handover record for
			// its fresh aside and no new records for the other seats. File
			// absence alone does not prove that no seed was ever delivered.
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("post-bounce handover %s: %w", name, err)
			}
			if err != nil || string(body) != previous {
				fmt.Printf("J14 pre-existing handover changed for %s; prior migration is outside this drain-only probe\n", name)
			}
			continue
		}
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("post-bounce handover %s: %w", name, err)
		}
		return fmt.Errorf("bounce wrote a new T285 handover for %s", name)
	}
	if _, err := s.mcpText("jevons_thread_remove", map[string]any{"id": id}); err != nil {
		return fmt.Errorf("remove bounce fixture: %w", err)
	}
	list, err := s.mcpText("jevons_thread_list", nil)
	if err != nil {
		return err
	}
	if strings.Contains(list, id) {
		return fmt.Errorf("bounce fixture remains in thread list")
	}
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent.Name == id {
			return fmt.Errorf("bounce fixture remains in agent registry")
		}
	}
	return nil
}

// jSwitchSeedShape is the 🎯T285.1 isolate oracle: a provider switch
// delivers a brief-shaped seed, not a walk-the-predecessor-file assignment.
// When Distill is too thin the work session id differs from the compact id.
func (s *suite) jSwitchSeedShape() error {
	id := fmt.Sprintf("orch-seed-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "seed-shape-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id})
	}()

	to := "claude"
	if string(s.provider) == "claude" {
		to = "grok"
	}
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey seed-shape worker",
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id":   id,
		"text": "Remember this for later — the mission token is SEEDSHAPE9. Reply with exactly: STORED",
	}); err != nil {
		return fmt.Errorf("plant: %w", err)
	}

	out, err := s.mcpText("jevons_agent_migrate", map[string]any{
		"name": id, "provider": to,
	})
	if err != nil {
		return fmt.Errorf("migrate: %w (%s)", err, trim(out, 200))
	}

	raw, err := os.ReadFile(s.handoverPath(id))
	if err != nil {
		return fmt.Errorf("handover record: %w (migrate said %s)", err, trim(out, 160))
	}
	store := handover.NewStore(filepath.Join(s.stateDir, "handover"))
	pending, ok, err := store.Get(id)
	if err != nil || !ok {
		return fmt.Errorf("handover get: ok=%v err=%v raw=%s", ok, err, trim(string(raw), 160))
	}
	seed := pending.Seed()
	if seed == "" {
		return fmt.Errorf("switch produced no seed")
	}
	low := strings.ToLower(seed)
	if !strings.Contains(low, "provider switch") || !strings.Contains(low, "what was in flight") {
		return fmt.Errorf("seed is not brief-shaped: %s", trim(seed, 240))
	}
	for _, bad := range []string{"start at the end", "work backwards", "read it before doing anything else"} {
		if strings.Contains(low, bad) {
			return fmt.Errorf("seed assigns a walk (%q): %s", bad, trim(seed, 240))
		}
	}
	if pending.TranscriptPath != "" && strings.Contains(seed, pending.TranscriptPath) {
		return fmt.Errorf("work seed cites the predecessor path: %s", trim(seed, 240))
	}

	after, err := upgrade.SessionSnapshotFromFile(s.agentsPath())
	if err != nil {
		return fmt.Errorf("work session snapshot: %w", err)
	}
	workID := after[id]
	if pending.CompactSessionID != "" {
		if workID == "" || workID == pending.CompactSessionID {
			return fmt.Errorf("thin Distill reused the compact session (work=%q compact=%q)",
				workID, pending.CompactSessionID)
		}
	}
	return nil
}
