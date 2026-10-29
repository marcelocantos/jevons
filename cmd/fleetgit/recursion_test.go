// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package main_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Exercise the actual executable with no project-dir hint, as in the incident.
// Every invocation gets its own process group: a deadline kills descendants too.
func TestT107412FleetgitCannotResolveItself(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "wrapper", "fleetgit")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realGit, filepath.Join(realDir, "git")); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "PATH", "CLAUDE_PROJECT_DIR", "JEVONS_AGENT", "JEVONS_FLEETGIT_REPOSITORY_PROBE":
			continue
		}
		env = append(env, item)
	}
	for _, kind := range []string{"symlink", "hardlink", "missing-real-git", "nested-probe"} {
		t.Run(kind, func(t *testing.T) {
			alias := filepath.Join(root, kind)
			if err := os.Mkdir(alias, 0o755); err != nil {
				t.Fatal(err)
			}
			link := os.Symlink
			if kind == "hardlink" {
				link = os.Link
			}
			if err := link(bin, filepath.Join(alias, "git")); err != nil {
				t.Fatal(err)
			}
			path := alias
			if kind != "missing-real-git" {
				path += string(os.PathListSeparator) + realDir
			}
			// A recursing wrapper never finishes, so any bound catches it; 2s
			// killed a healthy `git --version` under full-suite load.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(alias, "git"), "--version")
			cmd.Dir = root
			cmd.Env = append(append([]string{}, env...), "PATH="+path)
			if kind == "nested-probe" {
				cmd.Env = append(cmd.Env, "JEVONS_FLEETGIT_REPOSITORY_PROBE=1")
			}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
			cmd.WaitDelay = time.Second
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("wrapper exceeded deadline; process group killed: %v\n%s", err, out)
			}
			switch kind {
			case "missing-real-git":
				if err == nil || !strings.Contains(string(out), "real git not found") {
					t.Fatalf("expected prompt missing-Git error: %v\n%s", err, out)
				}
			case "nested-probe":
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "recursive repository probe refused") {
					t.Fatalf("expected recursive-probe refusal: %v\n%s", err, out)
				}
			default:
				if err != nil || !strings.HasPrefix(string(out), "git version ") {
					t.Fatalf("expected real Git version output: %v\n%s", err, out)
				}
			}
		})
	}
}
