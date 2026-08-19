// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	_ = s.cmd.Process.Signal(os.Interrupt)
	waitCh := s.cmdWait
	if waitCh == nil {
		waitCh = make(chan error, 1)
		cmd := s.cmd
		go func() { waitCh <- cmd.Wait() }()
	}
	select {
	case <-waitCh:
		s.cmd = nil
		s.cmdWait = nil
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

// jBounceResume is the 🎯T40.2 isolate oracle: after a SIGTERM drain+start,
// every unchanged-provider row keeps its session_id and receives no T285 seed.
func (s *suite) jBounceResume() error {
	before, err := upgrade.SessionSnapshotFromFile(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot before bounce: %w", err)
	}
	if len(before) == 0 {
		return fmt.Errorf("agents.json empty before bounce")
	}
	hadHandover := map[string]bool{}
	for name := range before {
		if _, err := os.Stat(s.handoverPath(name)); err == nil {
			hadHandover[name] = true
		}
	}

	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce: %w", err)
	}

	after, err := upgrade.SessionSnapshotFromFile(s.agentsPath())
	if err != nil {
		return fmt.Errorf("snapshot after bounce: %w", err)
	}
	if drift := upgrade.SessionDrift(before, after); len(drift) != 0 {
		return fmt.Errorf("bounce minted or dropped sessions: %v", drift)
	}
	for name := range before {
		if hadHandover[name] {
			continue
		}
		if _, err := os.Stat(s.handoverPath(name)); err == nil {
			return fmt.Errorf("bounce wrote a T285 handover for %s", name)
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
