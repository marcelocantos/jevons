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
)

// j29TmuxAnchorSpawn is the 🎯T579 isolate journey: a fleet worker is
// spawned with NO default tmux server reachable, and again after the
// claudia server has been killed out from under the daemon.
//
// The production failure it stands against: eight fleet-health
// recoveries on 2026-08-29 died with
//
//	tmux new-window: exit status 1: no server running on <claudia sock>
//
// while the fleet's own panes were alive. Two mechanisms, both covered
// here — the census reaping the claudia-anchor placeholder that holds
// the server open, and the server going away between EnsureServer and
// new-window. TMUX_TMPDIR points the daemon at an empty directory, so
// any tmux call that forgets -S finds no server at all and the spawn
// fails loudly rather than silently talking to the owner's own tmux.
func (s *suite) j29TmuxAnchorSpawn() error {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J29 refuses the development port")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is a dependency of this suite, not an optional extra: %w", err)
	}

	// Short paths: an AF_UNIX socket is capped near 104 bytes.
	sockDir, err := os.MkdirTemp("", "jvt579")
	if err != nil {
		return err
	}
	sock := filepath.Join(sockDir, "s.sock")
	emptyTmux := filepath.Join(sockDir, "notmux")
	if err := os.MkdirAll(emptyTmux, 0o700); err != nil {
		return err
	}

	restoreEnv := append([]string(nil), s.daemonEnv...)
	s.daemonEnv = append(s.daemonEnv,
		"CLAUDIA_TMUX_SOCKET="+sock,
		"TMUX_TMPDIR="+emptyTmux,
	)
	defer func() {
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{
			"name": "jv-t579-j29a", "actor": "jevons",
		})
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{
			"name": "jv-t579-j29b", "actor": "jevons",
		})
		_ = exec.Command("tmux", "-S", sock, "kill-server").Run()
		_ = os.RemoveAll(sockDir)
		s.daemonEnv = restoreEnv
		_ = s.bounceDrain()
	}()
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("bounce onto isolated tmux socket: %w", err)
	}

	// No default tmux server exists under TMUX_TMPDIR, and none is
	// created: every tmux call the spawn path makes must carry -S.
	if err := exec.Command("tmux", "list-sessions").Run(); err == nil {
		return fmt.Errorf("a default tmux server is reachable under %s — the journey's premise is broken", emptyTmux)
	}

	work := filepath.Join(s.stateDir, "t579-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}

	out, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t579-j29a", "workdir": work, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "claude",
	})
	if o := asOutage("spawn with no default tmux server", err); o != nil {
		return o
	}
	if err != nil {
		return fmt.Errorf("spawn with no default tmux server: %w (%s)", err, trim(out, 200))
	}
	if err := exec.Command("tmux", "-S", sock, "has-session", "-t", "claudia-anchor").Run(); err != nil {
		return fmt.Errorf("worker did not land on claudia's socket %s: %w", sock, err)
	}

	// Now the production shape: the server goes away under the daemon.
	// The next spawn must bring it back, not report the failure the
	// fleet-health recoveries reported.
	if out, err := exec.Command("tmux", "-S", sock, "kill-server").CombinedOutput(); err != nil {
		return fmt.Errorf("kill claudia tmux server: %w: %s", err, out)
	}
	time.Sleep(200 * time.Millisecond)

	out, err = s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": "jv-t579-j29b", "workdir": work, "actor": "jevons",
		"parent": "jevons", "purpose": "work", "provider": "claude",
	})
	if o := asOutage("spawn after kill-server", err); o != nil {
		return o
	}
	if err != nil {
		return fmt.Errorf("spawn after the tmux server died: %w (%s)", err, trim(out, 200))
	}
	if err := exec.Command("tmux", "-S", sock, "has-session", "-t", "claudia-anchor").Run(); err != nil {
		return fmt.Errorf("anchor session not restored on %s after kill-server: %w", sock, err)
	}

	// The error string the target names must not appear at all.
	raw, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Errorf("read isolate log: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "no server running on") {
			return fmt.Errorf("isolate log still carries the 🎯T579 failure: %s", trim(line, 240))
		}
	}
	return nil
}
