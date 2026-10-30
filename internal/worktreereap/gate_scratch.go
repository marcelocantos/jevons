// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktreereap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const gateScratchMarker = "jevons-gate-owner.json"

// MarkGateScratch records the creator outside the checkout, before git adds it.
// The record survives both a killed gate and git pruning its admin entry.
func MarkGateScratch(scratch, repo string) error {
	m := struct {
		Marker
		Repo string `json:"repo"`
	}{Marker: Marker{PID: os.Getpid(), Cmd: commandLine(os.Getpid()), Created: time.Now()}, Repo: repo}
	blob, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(scratch, gateScratchMarker), blob)
}

// SweepGateScratch removes only orphaned gate-clean directories in tempRoot.
// A missing/invalid marker is legacy: wait for an idle grace and refuse to
// touch it while any gate process may still be active. A marker for a different
// repository is never this clone's property.
func SweepGateScratch(repo, tempRoot string, dryRun bool) (int, error) {
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return 0, err
	}
	live, err := Snapshot()
	if err != nil {
		return 0, err
	}
	registered, err := listWorktrees(repo)
	if err != nil {
		return 0, err
	}
	known := map[string]bool{}
	for _, e := range registered {
		known[filepath.Clean(e.Path)] = true
	}
	// The conservative legacy fallback cannot prove which gate owns a directory.
	// Do not reclaim ANY legacy scratch while a gate may hold the heavy lease.
	gateLive := false
	for _, cmd := range live.Cmds {
		if strings.Contains(cmd, "bin/gate") || strings.Contains(cmd, "cmd/gate") || strings.Contains(cmd, "jevons-gate") || strings.Contains(cmd, "gate -clean") {
			gateLive = true
			break
		}
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "gate-clean-") {
			continue
		}
		scratch := filepath.Join(tempRoot, e.Name())
		tree := filepath.Join(scratch, "tree")
		blob, readErr := os.ReadFile(filepath.Join(scratch, gateScratchMarker))
		var mark struct {
			Marker
			Repo string `json:"repo"`
		}
		valid := readErr == nil && json.Unmarshal(blob, &mark) == nil && mark.PID > 0 && mark.Repo != ""
		if valid {
			if filepath.Clean(mark.Repo) != filepath.Clean(repo) || live.PIDAlive(mark.PID, mark.Cmd) {
				continue
			}
		} else {
			fi, err := e.Info()
			if err != nil || time.Since(fi.ModTime()) < 24*time.Hour || gateLive {
				continue
			}
			// Legacy scratch has no owner marker. A remaining .git pointer
			// proves this checkout belongs to this clone even when git has
			// already pruned its registration. Otherwise do not claim another
			// project's scratch merely because it shares the prefix.
			if !known[filepath.Clean(tree)] {
				pointer, readErr := os.ReadFile(filepath.Join(tree, ".git"))
				common, gitErr := git(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
				if readErr != nil || gitErr != nil || !strings.Contains(string(pointer), filepath.Join(common, "worktrees")+string(os.PathSeparator)) {
					continue
				}
			}
		}
		// A gate's child may still be finishing after its creator dies. A process
		// explicitly naming this scratch is a veto independent of the owner pid.
		busy := false
		for _, cmd := range live.Cmds {
			if strings.Contains(cmd, scratch) {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		if dryRun {
			removed++
			continue
		}
		if known[filepath.Clean(tree)] {
			if err := remove(repo, tree); err != nil {
				return removed, fmt.Errorf("gate scratch %s: %w", scratch, err)
			}
		}
		if err := os.RemoveAll(scratch); err != nil {
			return removed, fmt.Errorf("gate scratch %s: %w", scratch, err)
		}
		removed++
	}
	if !dryRun && removed > 0 {
		if _, err := git(repo, "worktree", "prune"); err != nil {
			return removed, err
		}
	}
	return removed, nil
}
