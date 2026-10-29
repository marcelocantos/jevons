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

	"gopkg.in/yaml.v3"
)

const t1151JourneyBudget = 90 * time.Minute

// jT1151ConcurrentFleet (🎯T1151): on a Cursor trust isolate, two work seats
// under jevons-po each land a code-only change through cmd/integrate while the
// shared clone stays the integration branch and workdirs are isolated trees.
func (s *suite) jT1151ConcurrentFleet() error {
	return s.withTrustJourneyIsolate(t1151ConcurrentFleetRun)
}

func t1151ConcurrentFleetRun(iso *suite, repo string) error {
	if err := trustEnsureRepoPO(iso, repo); err != nil {
		return err
	}
	baseline, err := t1151LedgerTargetKeys(repo)
	if err != nil {
		return err
	}
	// Two concurrent landings prove the shared clone stays the integration
	// branch with isolated workdirs; four cost twice the turns and gate runs
	// to show the same thing (🎯T1299).
	const workers = 2
	names := make([]string, workers)
	trackNames := make([]string, workers)
	for i := range workers {
		names[i] = fmt.Sprintf("jv-t1151-j51-%d", i)
		trackNames[i] = fmt.Sprintf("T1151 J51 worker %d", i)
	}
	defer func() {
		for _, n := range names {
			_, _ = iso.mcpText("jevons_agent_kill", map[string]any{"name": n, "actor": overseerName, "force": true})
		}
		_, _ = iso.mcpText("jevons_agent_kill", map[string]any{"name": "jevons-po", "actor": overseerName, "force": true})
	}()

	poBrief := "[silent] T1151 J51 journey: workers " + strings.Join(names, ", ") +
		" will each land t1151_j51_N.txt in an isolated worktree, file a bullseye track named " +
		"\"T1151 J51 worker N\", and report the allocated target id plus landed SHA. " +
		"You must achieve each filed target from the PO seat (workers cannot self-achieve, T1160) " +
		"with a full-package gate on go test ./pkg/calc/... -count=1 and attestation citing the worker SHA."
	if _, err := iso.mcpText("jevons_agent_send", map[string]any{
		"name":  "jevons-po",
		"actor": overseerName,
		"text":  poBrief,
	}); err != nil {
		return fmt.Errorf("brief jevons-po: %w", err)
	}

	for i, name := range names {
		if _, err := iso.mcpText("jevons_agent_start", map[string]any{
			"name":     name,
			"workdir":  repo,
			"actor":    "jevons-po",
			"parent":   "jevons-po",
			"role":     "worker",
			"purpose":  "work",
			"provider": string(iso.provider),
		}); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		brief := fmt.Sprintf(
			"🎯T1151 J51 worker %d: only in your isolated worktree, add t1151_j51_%d.txt with exactly w%d plus newline, commit, "+
				"run bin/gate -clean with GREEN on tip for `go test ./pkg/calc/... -count=1`, land via `go run ./cmd/integrate` naming seat %s, "+
				"file bullseye track %q via jevons_target_file, message jevons-po the allocated target id and landed SHA for achieve (do not achieve yourself). "+
				"Finish with jevons finish-report.",
			i, i, i, name, trackNames[i],
		)
		if _, err := iso.mcpText("jevons_agent_send", map[string]any{
			"name":  name,
			"actor": "jevons-po",
			"text":  brief,
		}); err != nil {
			return fmt.Errorf("brief %s: %w", name, err)
		}
	}

	deadline := time.Now().Add(t1151JourneyBudget)
	var nudgedPO, triedLedgerCommit bool
	for time.Now().Before(deadline) {
		if t1151AllLanded(repo, workers) && !triedLedgerCommit {
			if dirty, _, _ := t1151SharedCloneDirty(repo); dirty {
				_ = t1151CommitDirtyLedger(repo)
				triedLedgerCommit = true
			}
		}
		if t1151AllLanded(repo, workers) && !nudgedPO {
			if dirty, _, _ := t1151SharedCloneDirty(repo); dirty {
				_, _ = iso.mcpText("jevons_agent_send", map[string]any{
					"name":  "jevons-po",
					"actor": overseerName,
					"text":  "[silent] J51: shared trust-repo bullseye.yaml is dirty with uncommitted achieves. Run bullseye commit / ledgercommit for each pending row so HEAD matches achieved state.",
				})
				nudgedPO = true
			}
		}
		if err := t1151JourneyComplete(repo, baseline, workers, trackNames); err == nil {
			agents, err := iso.listAgentsHTTP()
			if err != nil {
				return err
			}
			if err := t1151WorkdirsIsolated(repo, agents, names); err != nil {
				return err
			}
			head, _ := trustRepoHead(repo)
			fmt.Printf("J51: T1151 concurrent fleet landed %d slices achieved %d tracks HEAD=%s provider=%s\n",
				workers, workers, head, iso.provider)
			_ = os.WriteFile(filepath.Join(iso.stateDir, "j51-t1151-result.txt"),
				[]byte(fmt.Sprintf("head=%s workers=%d repo=%s\n", head, workers, repo)), 0o600)
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("T1151 journey: not all %d worker slices filed+achieved before deadline", workers)
}

func t1151JourneyComplete(repo string, baseline map[string]bool, workers int, trackNames []string) error {
	if !t1151AllLanded(repo, workers) {
		return fmt.Errorf("files")
	}
	if err := t1151SharedCloneClean(repo); err != nil {
		return err
	}
	after, err := t1151LedgerTargetKeys(repo)
	if err != nil {
		return err
	}
	if len(after) < len(baseline)+workers {
		return fmt.Errorf("ledger grew by %d, want %d new tracks", len(after)-len(baseline), workers)
	}
	blob, err := t1151LedgerBlob(repo)
	if err != nil {
		return err
	}
	for _, name := range trackNames {
		if !t1151TrackAchieved(blob, name) {
			return fmt.Errorf("track %q not achieved on HEAD", name)
		}
	}
	return nil
}

func t1151AllLanded(repo string, workers int) bool {
	for i := range workers {
		path := filepath.Join(repo, fmt.Sprintf("t1151_j51_%d.txt", i))
		body, err := os.ReadFile(path)
		if err != nil || string(body) != fmt.Sprintf("w%d\n", i) {
			return false
		}
	}
	return true
}

func t1151SharedCloneClean(repo string) error {
	dirty, porcelain, err := t1151SharedCloneDirty(repo)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("shared clone dirty:\n%s", porcelain)
	}
	return nil
}

func t1151SharedCloneDirty(repo string) (bool, string, error) {
	out, err := trustGitOutput(repo, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	return strings.TrimSpace(out) != "", out, nil
}

func t1151CommitDirtyLedger(repo string) error {
	root, err := jevonsRepoRoot()
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "run", "./cmd/ledgercommit",
		"-repo", repo, "-m", "J51 journey: commit pending ledger rows")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ledgercommit: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func t1151LedgerBlob(repo string) (string, error) {
	return trustGitOutput(repo, "show", "HEAD:bullseye.yaml")
}

func t1151LedgerTargetKeys(repo string) (map[string]bool, error) {
	blob, err := t1151LedgerBlob(repo)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, line := range strings.Split(blob, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "T") || !strings.HasSuffix(line, ":") {
			continue
		}
		out[strings.TrimSuffix(line, ":")] = true
	}
	return out, nil
}

// t1151TrackAchieved reports whether some ledger row named trackName is
// achieved. Bullseye writes names unquoted, and a worker may file the same
// track more than once (the PO sets the spares aside), so it parses the
// ledger rather than matching a quoted line and judging the first hit.
func t1151TrackAchieved(blob, trackName string) bool {
	var ledger struct {
		Targets map[string]struct {
			Name   string `yaml:"name"`
			Status string `yaml:"status"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal([]byte(blob), &ledger); err != nil {
		return false
	}
	for _, t := range ledger.Targets {
		if t.Name == trackName && t.Status == "achieved" {
			return true
		}
	}
	return false
}

func trustGitOutput(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = filepath.Clean(repo)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), repo, err)
	}
	return string(out), nil
}

func t1151WorkdirsIsolated(repo string, agents []AgentInfo, names []string) error {
	repo = filepath.Clean(repo)
	by := map[string]AgentInfo{}
	for _, a := range agents {
		by[a.Name] = a
	}
	for _, name := range names {
		a, ok := by[name]
		if !ok {
			return fmt.Errorf("agent %q missing from registry", name)
		}
		wd := filepath.Clean(a.WorkDir)
		if wd == repo {
			return fmt.Errorf("%q workdir is the shared clone %s", name, repo)
		}
		if !strings.Contains(wd, ".jevons-worktrees") {
			return fmt.Errorf("%q workdir %q is not an isolated tree", name, wd)
		}
	}
	return nil
}
