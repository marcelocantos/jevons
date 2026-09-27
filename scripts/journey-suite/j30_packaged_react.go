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

// J30 exercises the actual packaged React composer and mux wire. No direct
// chat injection, Vite substitute, seeded reply, or API/transport interception.
func (s *suite) jPackagedReactOwnerTurn() error {
	return s.jPackagedReactScript("test.cjs", 4*time.Minute)
}

// J31 holds an actual provider tool while the owner submits a follow-up.
func (s *suite) jPackagedReactOwnerBoundary() error {
	return s.jPackagedReactScript("boundary.cjs", 8*time.Minute)
}

// J32 (🎯T789.1) sends and Cuts in on a busy seat over a transcript taller than 12,000 px.
func (s *suite) jPackagedReactSendCutIn() error {
	return s.jPackagedReactScript("t789-live.cjs", 25*time.Minute)
}

// J33 (🎯T811): the daemon refuses the first owner delivery with the broker's
// real not_owner text (isolate-only fault seam); the packaged cockpit must show
// it undelivered with Resend, and Resend must deliver the same message once.
// Nothing reaches the development overseer or its grant.
func (s *suite) jUndeliveredResend() error {
	return s.jPackagedReactScript("t811-undelivered.cjs", 10*time.Minute)
}

func (s *suite) jPackagedReactScript(script string, timeout time.Duration) error {
	provider := string(s.provider)
	var ready error
	// Brokered subscriptions already proved their sidecar during overseer
	// startup; a vendor CLI is not a prerequisite for that product path.
	if s.brokerSocket == "" {
		if provider == "cursor" {
			_, ready = exec.LookPath("cursor-agent")
		} else {
			ready = backendCLIReady(provider)
		}
	}
	if ready != nil {
		return &outageError{step: "J30 provider prerequisite", class: agenterr.ClassBackendUnavailable, msg: ready.Error()}
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	root, err := j19RepoRoot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	aside := "react-aside-" + uuid.NewString()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "react-ui-test", script), "--host", surface.host, "--provider", provider, "--workdir", s.stateDir, "--aside", aside)
	names := []string{"jevons", aside}
	if script == "t789-live.cjs" {
		// Drives the overseer only; no aside is minted. The screenshot must
		// outlive the sandbox so the T493.1 visual verdict can be read.
		names = []string{"jevons"}
		cmd.Args = append(cmd.Args, "--screenshot", filepath.Join(os.TempDir(), "t789-live.png"))
	}
	if script == "t811-undelivered.cjs" {
		names = []string{"jevons"}
		cmd.Args = append(cmd.Args, "--fault", filepath.Join(s.stateDir, "fault-owner-not-owner"),
			"--screenshot", filepath.Join(os.TempDir(), "t811-undelivered.png"))
	}
	if script == "boundary.cjs" {
		// The real sweep is every two minutes. Observe its completion, rather
		// than sleeping and assuming it happened; four minutes bounds outage.
		cmd.Args = append(cmd.Args, "--daemon-log", s.logPath, "--sweep-deadline-ms", fmt.Sprint((4 * time.Minute).Milliseconds()))
		// T627.4: the idle reaper's owner-visible case is the MCP-spawned aside.
		cmd.Args = append(cmd.Args, "--aside-only")
		// 🎯T540.7.1.1: keep the reload screenshots past the sandbox for the T493.1 verdict.
		cmd.Args = append(cmd.Args, "--screenshot", filepath.Join(os.TempDir(), "j31"))
	}
	// The browser needs no checkout as its working directory. Assets come
	// exclusively from the isolated daemon's embedded bundle.
	cmd.Dir = s.stateDir
	out, err := cmd.CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		// A browser assertion timeout is a failed product observation, not
		// proof that the provider was unavailable. Prerequisites are above.
		return fmt.Errorf("packaged React conversation journey: %w\n%s", err, out)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return fmt.Errorf("read runtime provider evidence: %w", err)
	}
	for _, name := range names {
		if err := queueJourneyProvider(logs, name, provider); err != nil {
			return err
		}
	}
	return nil
}
