// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// J36 (🎯T562.5 / 🎯T895): the real packaged composer against a real mux
// wire, proving the per-send correlated outcome landed in fd8f384c — not
// just the hermetic Go/vitest suites cited by that commit's oracle. The
// send frame the composer actually puts on /ws/mux carries a client id;
// the daemon's status reply (ack) echoes that same id and the draft clears
// only then. A send to a name the daemon never registered (definite
// failure) gets an error reply echoing the same id, and the composer must
// leave the draft untouched — never silently swallow the text.
func (s *suite) jSendAckCorrelation() error {
	provider := string(s.provider)
	var ready error
	if s.brokerSocket == "" {
		ready = backendCLIReady(provider)
	}
	if ready != nil {
		return &outageError{step: "J36 provider prerequisite", class: agenterr.ClassBackendUnavailable, msg: ready.Error()}
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	root, err := j19RepoRoot()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp(s.stateDir, "j36-send-ack-")
	if err != nil {
		return err
	}
	name := "j36-send-ack-" + uuid.NewString()
	if _, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": name, "workdir": work, "owner_asked": true,
	}); err != nil {
		return fmt.Errorf("J36 start worker: %w", err)
	}
	defer func() {
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{"name": name, "actor": "jevons", "force": true})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "react-ui-test", "t895-send-ack-live.cjs"),
		"--host", surface.host, "--agent", name,
		"--screenshot", filepath.Join(os.TempDir(), "j36-send-ack.png"))
	cmd.Dir = s.stateDir
	out, err := cmd.CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		return fmt.Errorf("J36 send ack correlation: %w\n%s", err, out)
	}
	return nil
}
