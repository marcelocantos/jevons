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

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// J30 exercises the actual packaged React composer and mux wire. No direct
// chat injection, Vite substitute, seeded reply, or API/transport interception.
func (s *suite) jPackagedReactOwnerTurn() error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "scripts", "react-ui-test", "test.cjs"), "--host", surface.host, "--provider", provider)
	// The browser needs no checkout as its working directory. Assets come
	// exclusively from the isolated daemon's embedded bundle.
	cmd.Dir = s.stateDir
	out, err := cmd.CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		failure := fmt.Errorf("packaged React owner journey: %w\n%s", err, out)
		if outage := asOutage("J30 real provider", failure); outage != nil {
			return outage
		}
		return failure
	}
	return nil
}
