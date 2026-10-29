// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// jT966FleetPauseVisual is the T493.1 visual half of T966 on an isolate.
func (s *suite) jT966FleetPauseVisual() error {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runtime.Caller failed")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	script := filepath.Join(repo, "scripts", "chat-ui-test", "t966-fleet-pause-visual-test.js")
	cmd := exec.Command("node", script)
	cmd.Env = append(os.Environ(), "UI_HOST="+s.host)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("t966 fleet-pause visual: %w\n%s", err, trim(firstNonEmpty(stderr.String(), stdout.String()), 800))
	}
	if out := strings.TrimSpace(stdout.String()); out != "" {
		fmt.Println(out)
	}
	return nil
}
