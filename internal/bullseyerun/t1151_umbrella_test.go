// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package bullseyerun_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/jevons/internal/bullseyerun"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// 🎯T1151 umbrella hermetic: four isolated workers land code through the
// integrator while filing ledger rows through the atomic writer; the shared
// clone stays clean and the ledger only grows.
func TestT1151UmbrellaConcurrentWorkersIntegrateAndLedger(t *testing.T) {
	requireCommitModeBullseye(t)
	clone := t.TempDir()
	initGitRepo(t, clone)
	before := ledgerTargetKeys(t, clone)

	const workers = 4
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agent := fmt.Sprintf("jv-t1151-u%d", i)
			wt, err := worktree.Ensure(clone, agent)
			if err != nil {
				errs[i] = err
				return
			}
			if samePath(wt, clone) {
				errs[i] = fmt.Errorf("%s: worktree path equals shared clone", agent)
				return
			}
			rel := fmt.Sprintf("t1151_%d.txt", i)
			path := filepath.Join(wt, rel)
			if err := os.WriteFile(path, []byte(fmt.Sprintf("w%d\n", i)), 0o644); err != nil {
				errs[i] = err
				return
			}
			git(t, wt, "add", rel)
			git(t, wt, "commit", "-q", "-m", agent+" code")
			_, err = worktree.Integrate(&worktree.IntegrateArgs{
				BaseWorkdir: clone,
				AgentName:   agent,
			})
			if err != nil {
				errs[i] = err
				return
			}
			trackName := fmt.Sprintf("T1151 umbrella worker %d", i)
			if _, err := bullseyerun.RunMutation(
				"commit", "--op", "track", "--cwd", wt,
				"--name", trackName, "--acceptance", "present after concurrent fleet landing",
			); err != nil {
				errs[i] = err
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	after := ledgerTargetKeys(t, clone)
	for k := range before {
		if !after[k] {
			t.Fatalf("ledger lost pre-existing target key %q", k)
		}
	}
	if len(after) < len(before)+workers {
		t.Fatalf("ledger grew by %d keys, want at least %d new tracks",
			len(after)-len(before), workers)
	}
	for i := range workers {
		rel := fmt.Sprintf("t1151_%d.txt", i)
		got, err := os.ReadFile(filepath.Join(clone, rel))
		if err != nil || string(got) != fmt.Sprintf("w%d\n", i) {
			t.Fatalf("shared master missing %s after landings", rel)
		}
	}
	ledgerClean(t, clone)
	if st := gitOut(t, clone, "status", "--porcelain"); st != "" {
		t.Fatalf("shared clone dirty after concurrent landings:\n%s", st)
	}
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	for i := range workers {
		want := fmt.Sprintf("T1151 umbrella worker %d", i)
		if !strings.Contains(blob, want) {
			t.Fatalf("HEAD ledger missing track %q", want)
		}
	}
}

func ledgerTargetKeys(t *testing.T, clone string) map[string]bool {
	t.Helper()
	blob := gitOut(t, clone, "show", "HEAD:bullseye.yaml")
	out := map[string]bool{}
	for _, line := range strings.Split(blob, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "T") || !strings.HasSuffix(line, ":") {
			continue
		}
		out[strings.TrimSuffix(line, ":")] = true
	}
	return out
}

