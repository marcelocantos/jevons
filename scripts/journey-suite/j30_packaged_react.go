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

func (s *suite) jPackagedReactScript(script string, timeout time.Duration) error {
	provider := string(s.provider)
	var ready error
	if provider == "cursor" {
		_, ready = exec.LookPath("cursor-agent")
	} else {
		ready = backendCLIReady(provider)
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
	for _, name := range []string{"jevons", aside} {
		if err := queueJourneyProvider(logs, name, provider); err != nil {
			return err
		}
	}
	return nil
}
