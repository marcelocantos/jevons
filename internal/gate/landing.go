// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// FlagLandingSHA checks a declared oracle SHA against the shared checkout,
// not the worker's worktree HEAD. A rolled-back integration leaves the object
// in the repository, so cat-file and worktree-local ancestry both pass.
// Without a valid git worktree/shared checkout there is no tip to compare.
func FlagLandingSHA(sha, workDir string) []Flag {
	sha = strings.TrimSpace(sha)
	if sha == "" || strings.TrimSpace(workDir) == "" {
		return nil
	}
	// Never pass a report-controlled revision beginning with '-' to git.
	if len(sha) < 7 || len(sha) > 64 {
		return nil
	}
	for _, c := range sha {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return nil
		}
	}
	// Missing/rewritten citations belong to the existing T427 SHA checker;
	// this flag distinguishes a commit that DOES exist from one that LANDED.
	known, ok := CommitKnownIn(workDir)
	if !ok || !known(sha) {
		return nil
	}
	common, err := gitOut(workDir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil
	}
	common = strings.TrimSpace(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(workDir, common)
	}
	shared := filepath.Dir(common)
	// A bare repository has no shared working tree HEAD to consult.
	if _, err := gitOut(shared, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}
	tip, err := gitOut(shared, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	if exec.Command("git", "-C", shared, "merge-base", "--is-ancestor", sha, strings.TrimSpace(tip)).Run() == nil {
		return nil
	}
	return []Flag{{Kind: FlagLandingUnreachable,
		Detail:   fmt.Sprintf("declared landing commit %s is not an ancestor of shared checkout %s at %s; integration may have rolled back (🎯T1236)", sha, shared, strings.TrimSpace(tip)),
		Evidence: "jevons: oracle sha=" + sha}}
}

// FlagLandingClaims checks explicit landing assertions in PO/auditor prose on
// the report-time path. Unlike the reap veto, this is reader advice only.
var landingClaim = regexp.MustCompile(`(?i)\b(?:integrated at|landed(?: at)?)\s+(?:(?:reachable )?merge\s+)?([0-9a-f]{7,40})\b`)

func FlagLandingClaims(report, workDir string) []Flag {
	var flags []Flag
	seen := map[string]bool{}
	if m, _ := envelope.Parse(report); m != nil && m.SHA != "" {
		seen[strings.ToLower(m.SHA)] = true
	}
	for _, match := range landingClaim.FindAllStringSubmatch(report, -1) {
		sha := match[1]
		if seen[strings.ToLower(sha)] {
			continue
		}
		seen[strings.ToLower(sha)] = true
		flags = append(flags, FlagLandingSHA(sha, workDir)...)
	}
	return flags
}
