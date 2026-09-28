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

// J35 (🎯T562.2): a real WORKER seat holds an actual tool call while the
// owner drives THAT seat's real packaged composer (#agent-inspect-input,
// never a raw mux submit around it). The seat's transcript meta must carry a
// live phase sample; while it reads non-idle, an ordinary Enter must stay
// client-side (visible queue strip, nothing on /ws/mux) and must drain onto
// the wire on its own once the tool releases and phase returns to idle. This
// is the live half of T562.2's acceptance — its hermetic half is
// ui/src/components/AgentInteraction.seatPhase.test.tsx.
func (s *suite) jWorkerBusyVisibleQueue() error {
	provider := string(s.provider)
	var ready error
	if s.brokerSocket == "" {
		ready = backendCLIReady(provider)
	}
	if ready != nil {
		return &outageError{step: "J35 provider prerequisite", class: agenterr.ClassBackendUnavailable, msg: ready.Error()}
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	root, err := j19RepoRoot()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp(s.stateDir, "j35-worker-queue-")
	if err != nil {
		return err
	}
	name := "j35-worker-queue-" + uuid.NewString()
	nonce := uuid.NewString()
	post := "POST-" + nonce
	ready1 := filepath.Join(work, "ready")
	release := filepath.Join(work, "release")
	completed := filepath.Join(work, "completed")
	helper := filepath.Join(work, "wait.cjs")
	// A bounded, independent real tool workload (never the harness faking a
	// busy phase): the seat's own Bash tool call blocks on this file poll.
	script := fmt.Sprintf(`const fs = require('node:fs');
fs.writeFileSync(%q, %q);
const limit = setTimeout(() => { console.error('J35 hold was never released'); process.exit(2); }, 120000);
const poll = setInterval(() => { if (fs.existsSync(%q)) { clearInterval(poll); clearTimeout(limit); fs.writeFileSync(%q, %q); console.log('released'); } }, 25);
`, ready1, nonce, release, completed, nonce)
	if err := os.WriteFile(helper, []byte(script), 0o600); err != nil {
		return err
	}
	prompt := fmt.Sprintf("Run exactly this shell command using your tool and wait for it to return; do not background it, do not create the release file yourself: node %s\nThen reply with exactly: %s", helper, post)
	if _, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": name, "workdir": work, "prompt": prompt, "owner_asked": true,
	}); err != nil {
		return fmt.Errorf("J35 start worker: %w", err)
	}
	defer func() {
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{"name": name, "actor": "jevons", "force": true})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "react-ui-test", "t562-worker-queue-live.cjs"),
		"--host", surface.host, "--agent", name,
		"--ready", ready1, "--release", release, "--completed", completed,
		"--nonce", nonce, "--post", post,
		"--screenshot", filepath.Join(os.TempDir(), "j35-worker-queue.png"))
	cmd.Dir = s.stateDir
	out, err := cmd.CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		return fmt.Errorf("J35 worker busy visible queue: %w\n%s", err, out)
	}
	return nil
}
