// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// Exercise independent CLI processes and actual command admission, rather
// than total runtime: a slow host can make overlapping runs look serialised.
func TestT603CLIHeavyRunsWaitBeforeStarting(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	makefile := filepath.Join(dir, "Makefile")
	if err := os.WriteFile(makefile, []byte(`test-first:
	@mkdir active && touch first-started && while [ ! -f release ]; do sleep 0.05; done; rmdir active
test-second:
	@mkdir active && touch second-started && rmdir active
`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := func(target string) (<-chan error, *bytes.Buffer) {
		t.Helper()
		cmd := exec.CommandContext(ctx, gateBinary(t), "--", "make", "-f", makefile, target)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), gate.StoreDirEnv+"="+store)
		cmd.WaitDelay = time.Second
		out := new(bytes.Buffer)
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		return done, out
	}
	// Release the first command even if an assertion fails, so private
	// fixture children never remain waiting after the test exits.
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(dir, "release"), nil, 0600) })
	first, firstOut := start("test-first")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "first-started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first heavy command never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	second, secondOut := start("test-second")
	select {
	case err := <-second:
		t.Fatalf("second gate completed while first held lease: %v\n%s", err, secondOut)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dir, "second-started")); !os.IsNotExist(err) {
		t.Fatalf("second command started before lease release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		done <-chan error
		out  *bytes.Buffer
	}{{first, firstOut}, {second, secondOut}} {
		if err := <-run.done; err != nil {
			t.Fatalf("gate failed: %v\n%s", err, run.out)
		}
		if !strings.Contains(run.out.String(), "exit=0 GREEN") {
			t.Fatalf("gate did not record a passing command:\n%s", run.out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "second-started")); err != nil {
		t.Fatalf("second command never ran after release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "active")); !os.IsNotExist(err) {
		t.Fatalf("fixture commands overlapped or left active marker: %v", err)
	}
}
