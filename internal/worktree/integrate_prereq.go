// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 🎯T1249: a repo can declare a prerequisite that cmd/integrate runs in the
// pristine post-merge checkout before go build / touched-package tests.
// Generated, gitignored vendor artifacts (CGO headers, static libs) then
// exist for the gate instead of looking like a regression on a clean tree.
//
// 🎯T1255: the same file can declare test_tags so the touched-package go test
// runs with -tags <declared>. A package build-tagged out of the default build
// is then actually tested, instead of "matched no packages". Not a bypass:
// real tests still run under those tags.
//
// The declaration is the target repo's own file, not a per-repo special case
// in jevons. It is not a bypass: the prerequisite has to succeed, and the
// existing gate still runs after it.

// IntegratePrereqFile is the repo-root declaration integrate reads from the
// merge commit. Absence means today's behaviour (gate only).
const IntegratePrereqFile = ".jevons-integrate-prereq.yaml"

// ErrIntegratePrereq is returned when the declared prerequisite cannot be
// loaded or fails in the pristine checkout.
var ErrIntegratePrereq = errors.New("prerequisite failed")

// integratePrereq is the on-disk shape of IntegratePrereqFile.
//
//	submodules: true                 # optional; git submodule update --init --recursive
//	script: c/vendor/build.sh        # repo-relative; must stay inside the checkout
//	args: [libsodium]                # extra argv after the script
//	test_tags: [csdke2e]             # optional; go test -tags for the touched-package gate
//
// script is executed directly (shebang + executable bit), cwd = checkout root.
// test_tags is repo-wide: every touched-package go test gets -tags <joined>.
type integratePrereq struct {
	Submodules bool     `yaml:"submodules"`
	Script     string   `yaml:"script"`
	Args       []string `yaml:"args"`
	TestTags   []string `yaml:"test_tags"`
}

// PrereqPrepare gives callers outside package worktree (🎯T1267: bin/gate
// -clean run directly, not only through cmd/integrate's own post-merge gate)
// the same T1249/T1255 parity: load .jevons-integrate-prereq.yaml from commit
// in repo and return a Prepare func suitable for gate.CleanArgs.Prepare, plus
// any declared test_tags. A repo with no prerequisite file answers
// (nil, nil, nil) — exactly today's no-prerequisite behaviour, so a caller
// that always wires this in changes nothing for a repo that declares none.
func PrereqPrepare(repo, commit string) (func(string) error, []string, error) {
	p, err := loadIntegratePrereq(repo, commit)
	if err != nil {
		return nil, nil, err
	}
	return p.prepare(), p.testTags(), nil
}

func loadIntegratePrereq(repo, commit string) (*integratePrereq, error) {
	if repo == "" || commit == "" {
		return nil, nil
	}
	out, code, err := gitRun(repo, "show", commit+":"+IntegratePrereqFile)
	if err != nil {
		if code == 128 || strings.Contains(out, "does not exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: read %s at %s: %v: %s", ErrIntegratePrereq, IntegratePrereqFile, short(commit), err, clip(out))
	}
	p, err := parseIntegratePrereq(out)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrIntegratePrereq, IntegratePrereqFile, err)
	}
	return p, nil
}

func parseIntegratePrereq(raw string) (*integratePrereq, error) {
	dec := yaml.NewDecoder(strings.NewReader(raw))
	dec.KnownFields(true)
	var p integratePrereq
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *integratePrereq) validate() error {
	if p == nil {
		return nil
	}
	tags, err := normalizeTestTags(p.TestTags)
	if err != nil {
		return err
	}
	p.TestTags = tags
	if strings.TrimSpace(p.Script) == "" {
		if len(p.Args) > 0 {
			return fmt.Errorf("args without script")
		}
		if !p.Submodules && len(p.TestTags) == 0 {
			return fmt.Errorf("names neither script nor submodules nor test_tags")
		}
		return nil
	}
	return confinedRepoPath(p.Script)
}

func (p *integratePrereq) testTags() []string {
	if p == nil {
		return nil
	}
	return p.TestTags
}

func normalizeTestTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			return nil, fmt.Errorf("test_tags contains an empty tag")
		}
		if !validGoBuildTag(t) {
			return nil, fmt.Errorf("test_tags tag %q is not a Go build-tag identifier", t)
		}
		if seen[t] {
			return nil, fmt.Errorf("test_tags repeats %q", t)
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, nil
}

func validGoBuildTag(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func joinGoTestTags(tags []string) string {
	var out []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

func confinedRepoPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("script path is empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("script path %q must be repo-relative", p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("script path %q escapes the repo", p)
	}
	if strings.HasPrefix(clean, string(os.PathSeparator)) {
		return fmt.Errorf("script path %q must be repo-relative", p)
	}
	return nil
}

func confinedJoin(root, rel string) (string, error) {
	if err := confinedRepoPath(rel); err != nil {
		return "", err
	}
	abs := filepath.Join(root, filepath.Clean(rel))
	relOut, err := filepath.Rel(root, abs)
	if err != nil || relOut == ".." || strings.HasPrefix(relOut, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("script path %q escapes the repo", rel)
	}
	return abs, nil
}

func (p *integratePrereq) prepare() func(string) error {
	if p == nil {
		return nil
	}
	return p.run
}

func (p *integratePrereq) run(wt string) error {
	if p == nil {
		return nil
	}
	if p.Submodules {
		cmd := exec.Command("git", "submodule", "update", "--init", "--recursive")
		cmd.Dir = wt
		cmd.Stdin = nil
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w: git submodule update --init --recursive: %v: %s", ErrIntegratePrereq, err, clip(string(out)))
		}
	}
	if strings.TrimSpace(p.Script) == "" {
		return nil
	}
	abs, err := confinedJoin(wt, p.Script)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIntegratePrereq, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("%w: prerequisite script %q: %v", ErrIntegratePrereq, p.Script, err)
	}
	if st.IsDir() {
		return fmt.Errorf("%w: prerequisite script %q is a directory", ErrIntegratePrereq, p.Script)
	}
	cmd := exec.Command(abs, p.Args...)
	cmd.Dir = wt
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: prerequisite script %q: %v: %s", ErrIntegratePrereq, p.Script, err, clip(string(out)))
	}
	return nil
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	const max = 4096
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
