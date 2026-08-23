// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package claudiapin proves that jevons/go.mod's claudia pin contains the
// fleet-needed commits, and is loud when a sibling claudia master has
// landed fixes the pin does not yet include (🎯T448).
package claudiapin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ModulePath is the module path pinned in go.mod.
const ModulePath = "github.com/marcelocantos/claudia"

// RequiredInPin are claudia commits the published pin must contain.
//
// T28's original tip SHAs (2017074, aa71680) were squash-merged into
// a27d3fd (PR #51 / tag v0.22.0). Ancestry checks use the squash
// descendant — the pre-squash SHAs are not ancestors of any release tag.
var RequiredInPin = []RequiredCommit{
	{
		SHA:     "a27d3fdfe952e702dcaa10167395eed9675d6ee8",
		Short:   "a27d3fd",
		Summary: "T28 send-submit (squash of 2017074 + aa71680)",
	},
}

// RequiredCommit is one fleet-needed claudia commit the pin must contain.
type RequiredCommit struct {
	SHA     string
	Short   string
	Summary string
}

// Report is the daily-path pin check result.
type Report struct {
	PinVersion      string   // go.mod require version (e.g. v0.24.0)
	PinSHA          string   // resolved commit (sibling tag and/or module-cache Origin.Hash)
	SiblingRoot     string   // absolute path to sibling checkout, if found
	SiblingHEAD     string   // sibling HEAD full SHA
	Missing         []string // "short subject" lines on sibling not in pin
	MissingRequired []string
	Decision        string
	Loud            string // non-empty when the gap should be audible
}

// DecisionSeam is the recorded T448 policy under local-master (T104) + no Ship.
const DecisionSeam = "go.mod pins last published claudia release; daily path consumes local-master via ../go.work + buildsnap sibling inject — not a committed replace"

// Check reads go.mod at repoRoot and names the pin SHA. When a sibling
// claudia checkout is reachable (adjacent dir, or adjacent to the primary
// git worktree — so bin/gate -clean still finds it), it also names sibling
// commits the pin is missing and hard-fails on required ancestry gaps.
// Without a sibling, PinSHA falls back to the module-cache Origin.Hash.
func Check(repoRoot string) (Report, error) {
	var r Report
	r.Decision = DecisionSeam

	modPath := filepath.Join(repoRoot, "go.mod")
	body, err := os.ReadFile(modPath)
	if err != nil {
		return r, fmt.Errorf("read go.mod: %w", err)
	}
	r.PinVersion = RequireVersion(string(body))
	if r.PinVersion == "" {
		return r, fmt.Errorf("go.mod has no require %s", ModulePath)
	}

	sib := findSibling(repoRoot)
	if sib == "" {
		if sha := pinSHAFromModCache(r.PinVersion); sha != "" {
			r.PinSHA = sha
			r.Loud = fmt.Sprintf(
				"claudia pin=%s sha=%s — no sibling checkout beside %s (or primary worktree); missing-commit delta not computed (T448)",
				r.PinVersion, short(sha), repoRoot,
			)
			return r, nil
		}
		r.Loud = fmt.Sprintf(
			"claudia pin=%s — no sibling checkout and no module-cache Origin.Hash; cannot name pin SHA or missing commits",
			r.PinVersion,
		)
		return r, nil
	}
	r.SiblingRoot = sib

	pinSHA, err := git(sib, "rev-parse", r.PinVersion)
	if err != nil {
		if sha := pinSHAFromModCache(r.PinVersion); sha != "" {
			r.PinSHA = sha
		}
		r.Loud = fmt.Sprintf("claudia pin=%s — sibling at %s cannot rev-parse pin ref (%v)", r.PinVersion, sib, err)
		return r, nil
	}
	r.PinSHA = pinSHA

	head, err := git(sib, "rev-parse", "HEAD")
	if err != nil {
		return r, fmt.Errorf("sibling HEAD: %w", err)
	}
	r.SiblingHEAD = head

	for _, req := range RequiredInPin {
		if err := gitOK(sib, "merge-base", "--is-ancestor", req.SHA, r.PinVersion); err != nil {
			r.MissingRequired = append(r.MissingRequired,
				fmt.Sprintf("%s %s", req.Short, req.Summary))
		}
	}

	missingOut, err := git(sib, "log", "--oneline", "--no-decorate", r.PinVersion+"..HEAD")
	if err != nil {
		return r, fmt.Errorf("git log pin..HEAD: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(missingOut), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r.Missing = append(r.Missing, line)
	}

	switch {
	case len(r.MissingRequired) > 0:
		r.Loud = fmt.Sprintf(
			"CLAUDIA PIN STALE (hard): pin=%s sha=%s missing required: %s",
			r.PinVersion, short(r.PinSHA), strings.Join(r.MissingRequired, "; "),
		)
	case len(r.Missing) > 0:
		r.Loud = fmt.Sprintf(
			"CLAUDIA PIN BEHIND SIBLING: pin=%s sha=%s sibling_HEAD=%s missing %d commit(s) — daily builds via go.work/buildsnap sibling; publish+bump pin to close (T448). First missing: %s",
			r.PinVersion, short(r.PinSHA), short(r.SiblingHEAD), len(r.Missing), r.Missing[0],
		)
	}
	return r, nil
}

// findSibling returns an absolute path to a claudia checkout usable for
// pin/ref ancestry. bin/gate -clean runs in an ephemeral worktree whose
// parent dir is not the monorepo sibling layout — so we also look beside
// the primary worktree of this git clone.
func findSibling(repoRoot string) string {
	seen := map[string]bool{}
	var cands []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		cands = append(cands, p)
	}
	add(filepath.Join(filepath.Dir(repoRoot), "claudia"))
	if primary := primaryWorktree(repoRoot); primary != "" {
		add(filepath.Join(filepath.Dir(primary), "claudia"))
	}
	for _, sib := range cands {
		if _, err := os.Stat(filepath.Join(sib, "go.mod")); err == nil {
			return sib
		}
	}
	return ""
}

// primaryWorktree returns the first worktree path from git worktree list
// (the main checkout). Empty when repoRoot is not a git work tree.
func primaryWorktree(repoRoot string) string {
	out, err := git(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			return rest
		}
	}
	return ""
}

// pinSHAFromModCache reads Origin.Hash from the module download .info for
// the pinned version (GOWORK-independent). Empty when the cache entry is
// missing or has no Origin.
func pinSHAFromModCache(version string) string {
	gopath, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	infoPath := filepath.Join(strings.TrimSpace(string(gopath)), "cache", "download",
		"github.com", "marcelocantos", "claudia", "@v", version+".info")
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		return ""
	}
	var info struct {
		Origin *struct {
			Hash string `json:"Hash"`
		} `json:"Origin"`
	}
	if err := json.Unmarshal(raw, &info); err != nil || info.Origin == nil {
		return ""
	}
	return strings.TrimSpace(info.Origin.Hash)
}

// RequireVersion returns the claudia version from a go.mod body.
// Accepts both block form (`path ver` inside require ()) and single-line
// `require path ver`.
func RequireVersion(mod string) string {
	for _, line := range strings.Split(mod, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 2 && fields[0] == ModulePath:
			return fields[1]
		case len(fields) >= 3 && fields[0] == "require" && fields[1] == ModulePath:
			return fields[2]
		}
	}
	return ""
}

// FormatHuman renders a one-block report for restart logs / CLI stdout.
func FormatHuman(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "claudia pin: version=%s sha=%s\n", r.PinVersion, orDash(short(r.PinSHA)))
	if r.SiblingRoot != "" {
		fmt.Fprintf(&b, "claudia sibling: %s HEAD=%s\n", r.SiblingRoot, orDash(short(r.SiblingHEAD)))
	} else {
		b.WriteString("claudia sibling: (none)\n")
	}
	if len(r.MissingRequired) > 0 {
		b.WriteString("missing required:\n")
		for _, m := range r.MissingRequired {
			fmt.Fprintf(&b, "  - %s\n", m)
		}
	}
	if len(r.Missing) > 0 {
		fmt.Fprintf(&b, "commits on sibling not in pin (%d):\n", len(r.Missing))
		limit := len(r.Missing)
		if limit > 20 {
			limit = 20
		}
		for _, m := range r.Missing[:limit] {
			fmt.Fprintf(&b, "  %s\n", m)
		}
		if len(r.Missing) > limit {
			fmt.Fprintf(&b, "  … +%d more\n", len(r.Missing)-limit)
		}
	} else if r.SiblingRoot != "" && r.PinSHA != "" {
		b.WriteString("commits on sibling not in pin: (none)\n")
	}
	fmt.Fprintf(&b, "decision: %s\n", r.Decision)
	if r.Loud != "" {
		fmt.Fprintf(&b, "LOUD: %s\n", r.Loud)
	}
	return b.String()
}

// HardFail reports whether the pin is missing a required fleet commit.
func HardFail(r Report) bool {
	return len(r.MissingRequired) > 0
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitOK(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
