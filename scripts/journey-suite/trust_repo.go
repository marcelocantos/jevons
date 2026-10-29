// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// prepareTrustJourneyRepo clones the current Jevons tree into scratch and
// seeds a minimal pkg/calc package plus an in-repo bullseye ledger (🎯T1183).
func prepareTrustJourneyRepo(srcRoot, scratch string) (string, error) {
	srcRoot = filepath.Clean(srcRoot)
	dest := filepath.Join(scratch, "trust-repo")
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	clone := exec.Command("git", "clone", "-q", "--depth", "1", srcRoot, dest)
	clone.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := clone.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone trust repo: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// A clone of a detached checkout (bin/gate -clean runs in one) is itself
	// detached, and bullseye refuses to write a ledger on a detached HEAD
	// (code=unsafe_repo). Put the clone on a named branch either way.
	if out, err := exec.Command("git", "-C", dest, "switch", "-q", "-C", "trust-journey").CombinedOutput(); err != nil {
		return "", fmt.Errorf("trust repo branch: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// Local dev go.work points at sibling repos; the throwaway clone must not
	// inherit that or isolated broker builds resolve ../../claudia and fail.
	_ = os.Remove(filepath.Join(dest, "go.work"))
	_ = os.Remove(filepath.Join(dest, "go.work.sum"))
	if err := trustGitConfig(dest); err != nil {
		return "", err
	}
	calcDir := filepath.Join(dest, "pkg", "calc")
	if err := os.MkdirAll(calcDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(calcDir, "calc.go"), []byte("package calc\n\n// Placeholder for the journey: workers add Double here.\n"), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(calcDir, "calc_test.go"), []byte("package calc\n\nimport \"testing\"\n\nfunc TestPlaceholder(t *testing.T) {}\n"), 0o644); err != nil {
		return "", err
	}
	// This package is a real test binary in the cloned Jevons module. Without
	// the same testhome linkage as every production test package, the fixture's
	// own full gate fails before either J51 worker can land (🎯T1325).
	if err := os.WriteFile(filepath.Join(calcDir, "testhome_test.go"),
		[]byte("package calc\n\nimport _ \"github.com/marcelocantos/jevons/internal/testhome\"\n"), 0o644); err != nil {
		return "", err
	}
	// Mark only the throwaway clone: historical ledger ratchets still validate
	// generic integrity here, but cannot require production's achieved IDs in
	// the fresh journey ledger (git history reserves those IDs).
	if err := os.WriteFile(filepath.Join(dest, ".jevons-trust-journey"), []byte("throwaway fixture\n"), 0o644); err != nil {
		return "", err
	}
	if err := trustGit(dest, "add", ".jevons-trust-journey", "pkg/calc/calc.go", "pkg/calc/calc_test.go", "pkg/calc/testhome_test.go"); err != nil {
		return "", err
	}
	if err := trustGit(dest, "commit", "-q", "-m", "journey: seed pkg/calc"); err != nil {
		return "", err
	}
	if err := trustBullseye(dest); err != nil {
		return "", err
	}
	return dest, nil
}

func trustBullseye(repo string) error {
	// Shallow clone carries the full production ledger; that poisons the isolate
	// (sentinel po_stall, frontier auto-spawn for meta 🎯T1183). Start fresh.
	_ = os.Remove(filepath.Join(repo, "bullseye.yaml"))
	run := func(args ...string) error {
		out, err := exec.Command("bullseye", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("bullseye %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run("open", "--cwd", repo, "--location", "in_repo"); err != nil {
		return err
	}
	return run("commit", "--op", "track", "--cwd", repo,
		"--name", "Trust journey pkg/calc Double",
		"--acceptance", "pkg/calc exports Double(x int) int with a test; lands through cmd/integrate with a GREEN gate on pkg/calc.")
}

func trustGitConfig(repo string) error {
	for _, args := range [][]string{
		{"config", "user.email", "journey-trust@jevons.test"},
		{"config", "user.name", "journey-trust"},
	} {
		if err := trustGit(repo, args[0], args[1], args[2]); err != nil {
			return err
		}
	}
	return nil
}

// trustEnsureRepoPO starts the repo product-owner seat when absent. Production
// fleets keep this agent; the journey still proves owner chat → overseer → PO
// → worker → integrate, but without a PO registry row routing never begins.
func trustEnsureRepoPO(iso *suite, repo string) error {
	agents, err := iso.listAgentsHTTP()
	if err != nil {
		return err
	}
	if trustPOSpawned(agents) {
		return nil
	}
	if _, err := iso.mcpText("jevons_agent_start", map[string]any{
		"name":     "jevons-po",
		"workdir":  repo,
		"actor":    overseerName,
		"parent":   overseerName,
		"role":     "product-owner",
		"provider": string(iso.provider),
	}); err != nil {
		return fmt.Errorf("start jevons-po on trust repo: %w", err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		agents, err := iso.listAgentsHTTP()
		if err != nil {
			return err
		}
		if trustPOSpawned(agents) {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("jevons-po did not register after start")
}

func trustGit(repo string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s in %s: %v: %s", strings.Join(args, " "), repo, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func jevonsRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(wd, "cmd", "integrate")); err == nil {
				return wd, nil
			}
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "", fmt.Errorf("jevons repo root not found from cwd")
}

func trustRepoHead(repo string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func trustRepoHasDouble(repo string) bool {
	body, err := os.ReadFile(filepath.Join(repo, "pkg", "calc", "calc.go"))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), "func Double")
}
