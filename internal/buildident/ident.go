// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package buildident computes the identity of the *sources* a daemon build
// was made from, so the restart path can tell "already activated" from
// "stale binary" (🎯T218) even when the change lives in a go.work sibling
// rather than in this repo (🎯T580).
//
// WHY NOT THE BINARY HASH ALONE. The daily restart hashed bin/jevonsd and
// compared it against the hash recorded for the process holding the port.
// On 2026-08-29 a claudia-only fix (f8869fe) was rebuilt into bin/jevonsd
// and the script still logged "🎯T218 already activated: :13705 serves this
// exact build (sha be40be43bc66)", leaving the pre-fix daemon serving; only
// --force activated it. jevons consumes local-master claudia through
// ../go.work (🎯T448), so a sibling commit changes what is built while this
// repo's tree — and, in that incident, the binary hash — says nothing moved.
// The identity therefore covers this repo's HEAD and dirty fingerprint plus
// every go.work sibling's, and the restart compares it alongside the hash.
//
// DEGRADATION IS NAMED, NEVER SILENT. Without a go.work (a pristine clone, a
// buildsnap worktree, a test fixture) there are no siblings to hash and the
// identity is repo-only. Report.Degraded says so in words, so a caller that
// logs the report cannot mistake a narrower identity for a complete one.
package buildident

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Module is one checkout contributing to the build.
type Module struct {
	Name  string // directory base name ("jevons", "claudia")
	Root  string // absolute path
	HEAD  string // full commit SHA, or "" when unknown
	Dirty string // fingerprint of uncommitted state, "" when clean
}

// Report is the computed source identity plus what went into it.
type Report struct {
	Repo     Module   // this repo
	Siblings []Module // go.work siblings, sorted by name
	WorkFile string   // the go.work that named the siblings, "" when none
	Degraded string   // non-empty: why the identity is narrower than intended
	Identity string   // hex sha256 over the canonical lines below
}

// Compute returns the source identity for a build made from repoRoot.
// It never fails on a missing sibling or a missing go.work — those degrade
// the identity and say so. A repoRoot that is not a git work tree is an
// error: an identity that cannot see this repo's own HEAD is not one.
func Compute(repoRoot string) (Report, error) {
	var r Report
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return r, fmt.Errorf("abs %s: %w", repoRoot, err)
	}
	r.Repo, err = describe(abs)
	if err != nil {
		return r, err
	}

	work, uses := workUses(abs)
	r.WorkFile = work
	switch {
	case work == "":
		r.Degraded = "no go.work beside or above " + abs + "; identity covers this repo only (🎯T580)"
	case len(uses) == 0:
		r.Degraded = "go.work " + work + " names no sibling checkouts; identity covers this repo only (🎯T580)"
	}
	for _, use := range uses {
		m, err := describe(use)
		if err != nil {
			// A use directory that is not a git work tree (or has no HEAD)
			// still contributes its path: dropping it entirely would make
			// two different workspaces hash the same.
			m = Module{Name: filepath.Base(use), Root: use, HEAD: "", Dirty: ""}
			if r.Degraded == "" {
				r.Degraded = "sibling " + use + " is not a readable git work tree; its content is not in the identity (🎯T580)"
			}
		}
		r.Siblings = append(r.Siblings, m)
	}
	sort.Slice(r.Siblings, func(i, j int) bool { return r.Siblings[i].Name < r.Siblings[j].Name })

	h := sha256.New()
	for _, line := range Lines(r) {
		fmt.Fprintln(h, line)
	}
	r.Identity = hex.EncodeToString(h.Sum(nil))
	return r, nil
}

// Lines are the canonical inputs hashed into the identity, in order. They
// are also what a human reads when two identities differ.
func Lines(r Report) []string {
	lines := []string{modLine("repo", r.Repo)}
	for _, s := range r.Siblings {
		lines = append(lines, modLine("sibling", s))
	}
	return lines
}

func modLine(kind string, m Module) string {
	return fmt.Sprintf("%s %s head=%s dirty=%s", kind, m.Name, orNone(m.HEAD), orNone(m.Dirty))
}

// describe reads one checkout's HEAD and a fingerprint of its uncommitted
// state. The fingerprint hashes both the porcelain status (so an added or
// removed file counts) and the working diff against HEAD (so an edit in
// place counts) — a status line alone repeats for every further edit to the
// same file, which is exactly the sibling case this exists for.
func describe(root string) (Module, error) {
	m := Module{Name: filepath.Base(root), Root: root}
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return m, fmt.Errorf("%s: %w", root, err)
	}
	m.HEAD = head

	status, err := git(root, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return m, fmt.Errorf("%s status: %w", root, err)
	}
	diff, err := git(root, "diff", "HEAD")
	if err != nil {
		// A repo whose diff cannot be taken is still identifiable by HEAD
		// and status; record what we have rather than losing the module.
		diff = ""
	}
	if strings.TrimSpace(status) != "" || strings.TrimSpace(diff) != "" {
		sum := sha256.Sum256([]byte(status + "\x00" + diff))
		m.Dirty = hex.EncodeToString(sum[:])[:16]
	}
	return m, nil
}

// workUses returns the go.work that governs repoRoot and the absolute use
// directories it names, excluding repoRoot itself. The workspace file sits
// beside the clone in this layout (…/marcelocantos/go.work), so we look in
// repoRoot and then walk up; GOWORK wins when it names a real file.
func workUses(repoRoot string) (string, []string) {
	var work string
	if env := strings.TrimSpace(os.Getenv("GOWORK")); env != "" && env != "off" {
		if _, err := os.Stat(env); err == nil {
			work = env
		}
	}
	if work == "" {
		for dir := repoRoot; ; dir = filepath.Dir(dir) {
			cand := filepath.Join(dir, "go.work")
			if _, err := os.Stat(cand); err == nil {
				work = cand
				break
			}
			if filepath.Dir(dir) == dir {
				return "", nil
			}
		}
	}
	body, err := os.ReadFile(work)
	if err != nil {
		return "", nil
	}
	base := filepath.Dir(work)
	var uses []string
	inBlock := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
			continue
		case line == "use (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			uses = append(uses, resolve(base, line))
		case strings.HasPrefix(line, "use "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "use "))
			if rest == "(" {
				inBlock = true
				continue
			}
			uses = append(uses, resolve(base, rest))
		}
	}
	var out []string
	for _, u := range uses {
		if u == "" || u == repoRoot {
			continue
		}
		out = append(out, u)
	}
	return work, out
}

func resolve(base, p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// FormatHuman renders the report for a restart log.
func FormatHuman(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "build identity: %s\n", r.Identity)
	for _, line := range Lines(r) {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	if r.WorkFile != "" {
		fmt.Fprintf(&b, "  go.work: %s\n", r.WorkFile)
	}
	if r.Degraded != "" {
		fmt.Fprintf(&b, "  DEGRADED: %s\n", r.Degraded)
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "none"
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
