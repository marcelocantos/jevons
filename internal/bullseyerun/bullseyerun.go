// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package bullseyerun is the fleet ledger writer routing layer (🎯T1151.2).
// Every mutating bullseye CLI invocation from Jevons runs in commit mode on
// the repository's main working tree, then mirrors bullseye.yaml into linked
// worktrees so no checkout holds uncommitted ledger rows.
package bullseyerun

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/commitscope"
	"github.com/marcelocantos/jevons/internal/ledgercommit"
	"github.com/marcelocantos/jevons/internal/targetfile"
	"github.com/marcelocantos/jevons/internal/treeguard"
)

const (
	commitEnv        = "BULLSEYE_COMMIT"
	integrationEnv   = "BULLSEYE_INTEGRATION_REF"
	binEnv           = "BULLSEYE_BIN"
	defaultLedgerRel = "bullseye.yaml"
	mutationAttempts = 12
)

// mutationMu serializes ledger mutations per process (🎯T1151 single-writer).
var mutationMu sync.Mutex

// Command resolves the bullseye binary: BULLSEYE_BIN, then ~/.cargo/bin/bullseye,
// then PATH.
func Command() string {
	if v := strings.TrimSpace(os.Getenv(binEnv)); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		cargo := filepath.Join(home, ".cargo", "bin", "bullseye")
		if st, err := os.Stat(cargo); err == nil && !st.IsDir() {
			return cargo
		}
	}
	return "bullseye"
}

// RunMutationHook is replaced in tests. Production code calls RunMutation.
var RunMutationHook = runMutation

// RunMutation runs a bullseye CLI mutation with commit mode on the integration branch.
func RunMutation(args ...string) (string, error) {
	return RunMutationHook(args...)
}

func runMutation(args ...string) (string, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	callerCWD, rest, err := extractCWDFlag(args)
	if err != nil {
		return "", err
	}
	commitTop, mirrorTop := targetfile.WriteRoots(callerCWD)
	if commitTop == "" {
		commitTop = callerCWD
	}
	ledgerPath, err := discoverLedger(commitTop)
	if err != nil {
		return "", err
	}
	rewritten := rewriteCWDFlag(rest, commitTop)
	var out string
	var runErr error
	for attempt := 0; attempt < mutationAttempts; attempt++ {
		ref, err := integrationRef(commitTop)
		if err != nil {
			return "", err
		}
		if refExists(commitTop, ref) {
			if err := syncLedgerBeforeMutation(commitTop, mirrorTop, ledgerPath, ref); err != nil {
				return "", err
			}
		}
		out, runErr = execBullseye(rewritten, ref)
		if runErr == nil {
			break
		}
		if !isRetryableConflict(out) {
			break
		}
		if refExists(commitTop, ref) {
			if err := syncLedgerAfterMutation(commitTop, mirrorTop, ledgerPath, ref); err != nil {
				return out, fmt.Errorf("bullseyerun: refresh ledger after conflict: %w", err)
			}
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	if runErr != nil {
		return out, runErr
	}
	ref, err := integrationRef(commitTop)
	if err != nil {
		return out, err
	}
	if refExists(commitTop, ref) {
		if err := syncLedgerAfterMutation(commitTop, mirrorTop, ledgerPath, ref); err != nil {
			return out, fmt.Errorf("bullseyerun: mirror ledger after write: %w", err)
		}
	}
	recordBullseyeLedger(commitTop, mirrorTop, ledgerPath)
	return out, nil
}

func refExists(repoTop, ref string) bool {
	err := exec.Command("git", "-C", repoTop, "rev-parse", "--verify", ref).Run()
	return err == nil
}

func isRetryableConflict(out string) bool {
	return strings.Contains(out, "code=conflict") ||
		strings.Contains(out, "not achieved on disk")
}

func execBullseye(args []string, integrationRef string) (string, error) {
	cmd := exec.Command(Command(), args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.Env = append(os.Environ(),
		commitEnv+"=1",
		integrationEnv+"="+integrationRef,
	)
	err := cmd.Run()
	return buf.String(), err
}

func integrationRef(repoTop string) (string, error) {
	if out, err := exec.Command("git", "-C", repoTop, "symbolic-ref", "-q", "HEAD").Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			return ref, nil
		}
	}
	if out, err := exec.Command("git", "-C", repoTop, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" && branch != "HEAD" {
			return "refs/heads/" + branch, nil
		}
	}
	return "refs/heads/master", nil
}

func extractCWDFlag(args []string) (cwd string, rest []string, err error) {
	rest = append([]string{}, args...)
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--cwd" && i+1 < len(rest) {
			cwd = rest[i+1]
			rest = append(rest[:i], rest[i+2:]...)
			return cwd, rest, nil
		}
		if strings.HasPrefix(rest[i], "--cwd=") {
			cwd = strings.TrimPrefix(rest[i], "--cwd=")
			rest = append(rest[:i], rest[i+1:]...)
			return cwd, rest, nil
		}
	}
	return "", nil, fmt.Errorf("bullseyerun: --cwd is required for ledger mutations")
}

func rewriteCWDFlag(args []string, commitTop string) []string {
	out := append([]string{}, args...)
	for i := 0; i < len(out); i++ {
		if out[i] == "--cwd" && i+1 < len(out) {
			out[i+1] = commitTop
			return out
		}
		if strings.HasPrefix(out[i], "--cwd=") {
			out[i] = "--cwd=" + commitTop
			return out
		}
	}
	return append(out, "--cwd", commitTop)
}

func discoverLedger(repoTop string) (string, error) {
	p, err := targetfile.DiscoverLedgerPath(repoTop)
	if err != nil {
		return filepath.Join(repoTop, defaultLedgerRel), nil
	}
	return p, nil
}

func ledgerRel(commitTop, ledgerAbs string) string {
	rel, err := filepath.Rel(commitTop, ledgerAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return defaultLedgerRel
	}
	return rel
}

func mirrorLedgerRel(mirrorTop string) string {
	mRel := defaultLedgerRel
	if lp, err := targetfile.DiscoverLedgerPath(mirrorTop); err == nil {
		if r, err := filepath.Rel(mirrorTop, lp); err == nil && !strings.HasPrefix(r, "..") {
			mRel = r
		}
	}
	return mRel
}

func syncLedgerBeforeMutation(commitTop, mirrorTop, ledgerAbs, ref string) error {
	rel := ledgerRel(commitTop, ledgerAbs)
	if err := preserveUncommittedLedger(commitTop, rel); err != nil {
		return err
	}
	return mirrorLedgerToRef(commitTop, mirrorTop, rel, ref)
}

func syncLedgerAfterMutation(commitTop, mirrorTop, ledgerAbs, ref string) error {
	rel := ledgerRel(commitTop, ledgerAbs)
	if err := alignLedgerToRef(commitTop, ref, rel); err != nil {
		return err
	}
	return mirrorLedgerToRef(commitTop, mirrorTop, rel, ref)
}

func mirrorLedgerToRef(commitTop, mirrorTop, rel, ref string) error {
	tops := []string{commitTop}
	if mirrorTop != "" && mirrorTop != commitTop {
		tops = append(tops, mirrorTop)
	}
	for _, top := range tops {
		mRel := rel
		if top != commitTop {
			mRel = mirrorLedgerRel(top)
		}
		if top == commitTop {
			continue // commit top handled by preserve/align
		}
		if err := alignLedgerToRef(top, ref, mRel); err != nil {
			return err
		}
	}
	return nil
}

// preserveUncommittedLedger unions working-tree rows onto HEAD and commits when
// they differ from the ref (🎯T1171 / 🎯T1145). It never runs a blind checkout.
func preserveUncommittedLedger(repoTop, rel string) error {
	if err := exec.Command("git", "-C", repoTop, "diff", "--quiet", "HEAD", "--", rel).Run(); err == nil {
		return nil
	}
	head, err := gitShowBlob(repoTop, "HEAD:"+rel)
	if err != nil {
		return fmt.Errorf("bullseyerun: read HEAD %s: %w", rel, err)
	}
	path := filepath.Join(repoTop, rel)
	working, _ := os.ReadFile(path)
	drop, ids, err := commitscope.WouldDropWorkingDivergence(head, working, head)
	if err != nil {
		return fmt.Errorf("bullseyerun: ledger preserve scan: %w", err)
	}
	if !drop {
		return alignLedgerToRef(repoTop, "HEAD", rel)
	}
	merged, err := commitscope.UnionPreservingWorkingDivergence(head, working, head)
	if err != nil {
		return fmt.Errorf("bullseyerun: ledger union: %w", err)
	}
	stillDrop, stillIDs, err := commitscope.WouldDropWorkingDivergence(head, working, merged)
	if err != nil {
		return err
	}
	if stillDrop {
		return fmt.Errorf("%s", strings.TrimSpace(commitscope.FormatWorkingDivergenceRefusal("ledger mirror sync", stillIDs)))
	}
	// Commit the working-tree blob bullseye wrote (file mode); do not re-encode via Union output.
	if err := exec.Command("git", "-C", repoTop, "add", rel).Run(); err != nil {
		return fmt.Errorf("bullseyerun: stage preserved ledger: %w", err)
	}
	if err := ledgercommit.CommitDirtyLedger(repoTop, "bullseyerun: preserve uncommitted ledger rows (🎯T1171)"); err != nil {
		return fmt.Errorf("bullseyerun: commit preserved ledger: %w", err)
	}
	_ = ids
	return nil
}

// alignLedgerToRef sets the working tree and index to match ref for rel (🎯T1171).
func alignLedgerToRef(repoTop, ref, rel string) error {
	checkout := exec.Command("git", "-C", repoTop, "checkout", ref, "--", rel)
	if out, err := checkout.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s -- %s: %w: %s", ref, rel, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// recordBullseyeLedger journals the post-mutation ledger so treeguard's
// next sweep does not report a sanctioned replacement as UNGUARDED CHANGE
// (🎯T1247). Best-effort: a journal failure must not fail the mutation.
func recordBullseyeLedger(commitTop, mirrorTop, ledgerAbs string) {
	_ = treeguard.RecordLedgerWrite(ledgerAbs)
	if mirrorTop == "" || mirrorTop == commitTop {
		return
	}
	_ = treeguard.RecordLedgerWrite(filepath.Join(mirrorTop, mirrorLedgerRel(mirrorTop)))
}

func gitShowBlob(repoTop, spec string) ([]byte, error) {
	out, err := exec.Command("git", "-C", repoTop, "show", spec).Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}
