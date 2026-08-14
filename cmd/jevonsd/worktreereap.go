// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// worktreeSweepInterval is how often the daemon sweeps the shared clone's
// worktrees. Ten minutes is short enough that a killed ratchet's tree is gone
// before anyone reads `git worktree list` looking for an answer, and long
// enough that the walk is invisible next to what a fleet of workers is doing
// to the same disk.
const worktreeSweepInterval = 10 * time.Minute

// startWorktreeReap runs the 🎯T440 sweep until ctx ends.
//
// It lives in the daemon rather than in the tests that leak because the whole
// finding is that cleanup owned by the dying process does not run. jevonsd is
// the one thing here that is already outside every process a killed
// verification run tears down — the same argument that put the restart
// watchdog outside the restart (🎯T405).
//
// Deliberately not gated by the capacity governor (🎯T359): the sweep spends no
// tokens, and deferring it when budget is tight would let disk fill precisely
// when the fleet is busiest. Same reasoning as the butler's idle-thread GC.
func startWorktreeReap(ctx context.Context, cfg config.Config) {
	root := strings.TrimSpace(cfg.WorkDir)
	if root == "" {
		return
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		slog.Warn("worktree reap: resolve workdir", "workdir", root, "err", err)
		return
	}
	// EvalSymlinks because git reports resolved paths, and a protected path
	// that does not match git's spelling protects nothing.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		// Not a clone. A daemon whose workdir is a plain directory has no
		// worktrees to sweep and must not say anything about it every tick.
		return
	}

	pol := worktreereap.Policy{
		RepoRoot:  abs,
		Protected: []string{filepath.Join(cfg.StateDir, "build-snapshot")},
	}

	go func() {
		sweep := func() {
			res, err := worktreereap.Sweep(worktreereap.Config{
				Policy: pol,
				Log:    func(msg string) { slog.Info("worktree reap", "report", msg) },
			})
			if err != nil {
				slog.Warn("worktree reap", "root", abs, "err", err)
				return
			}
			if res.Removed > 0 || res.Failed > 0 {
				slog.Info("worktree reap swept",
					"root", abs, "entries", len(res.Verdicts),
					"removed", res.Removed, "held", res.Held, "failed", res.Failed)
			}
		}
		// A sweep at startup as well as on the tick: a daemon that was
		// restarted because the machine was in a bad state is exactly when
		// the leaked trees are worst, and waiting ten minutes to say so
		// wastes the one moment somebody is watching the log.
		sweep()
		t := time.NewTicker(worktreeSweepInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweep()
			}
		}
	}()
}
