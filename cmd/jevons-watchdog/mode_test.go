// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No real launchd job or daemon is touched. A missing repo makes a mistaken
// legacy action visible without needing a restart script or a listening port.
func TestKeepAliveNeverCompetesWithLaunchd(t *testing.T) {
	repo := t.TempDir()
	script := filepath.Join(repo, "scripts", "restart-jevonsd.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho touched >"+filepath.Join(repo, "touched")+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	detail := restartForMode(repo, 12345, true, nil)
	if !strings.Contains(detail, "KeepAlive is loaded") || !strings.Contains(detail, "no competing kickstart") {
		t.Fatalf("missing passive recovery explanation: %q", detail)
	}
	if _, err := os.Stat(filepath.Join(repo, "touched")); !os.IsNotExist(err) {
		t.Fatalf("restart script ran under KeepAlive: %v", err)
	}
}

func TestUnknownModeFailsClosedAndUnloadedModeFallsBack(t *testing.T) {
	repo := t.TempDir() // no script: the legacy route reports this exact absence
	if got := restartForMode(repo, 12345, false, errors.New("launchctl unavailable")); !strings.Contains(got, "refusing a competing restart") {
		t.Fatalf("unknown mode: %q", got)
	}
	if got := restartForMode(repo, 12345, false, nil); !strings.Contains(got, "no restart script") {
		t.Fatalf("legacy fallback: %q", got)
	}
}
