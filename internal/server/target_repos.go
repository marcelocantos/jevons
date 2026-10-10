// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// targetRepo is an indexed, canonical ledger identity, never a browser path.
type targetRepo struct{ cwd, ledger, key, identity string }

var repoSlugRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var repoSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
var scopedIDRe = regexp.MustCompile(`^T[0-9]+(?:\.[0-9]+)*$`)

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// SetTargetRepoRoots builds a bounded, explicit index of direct git checkouts
// beneath trusted workspace roots. This is called during daemon setup, not on
// every hover. An oversized/unreadable root fails closed rather than giving a
// partial index. Duplicate slugs are marked ambiguous even with different
// ledgers; aliases to the same canonical ledger share one identity.
func (s *Server) SetTargetRepoRoots(roots ...string) error {
	const maxEntries = 256
	index := make(map[string]targetRepo)
	ambiguous := make(map[string]bool)
	for _, root := range roots {
		realRoot, err := canonicalPath(root)
		if err != nil {
			return err
		}
		if realRoot == string(filepath.Separator) {
			return fmt.Errorf("refuse filesystem root as target repo root")
		}
		entries, err := os.ReadDir(realRoot)
		if err != nil {
			return err
		}
		if len(entries) > maxEntries {
			return fmt.Errorf("target repo root exceeds %d entries", maxEntries)
		}
		for _, entry := range entries {
			slug := entry.Name()
			if !repoSlugRe.MatchString(slug) {
				continue
			}
			cwd, err := canonicalPath(filepath.Join(realRoot, slug))
			if err != nil {
				continue
			}
			st, err := os.Stat(cwd)
			if err != nil || !st.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
				continue
			}
			ledger, notInit, err := discoverLedgerPath(cwd)
			if err != nil || notInit || ledger == "" {
				continue
			}
			key, err := canonicalPath(ledger)
			if err != nil {
				continue
			}
			identity := filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(realRoot)), filepath.Base(realRoot), slug))
			repo := targetRepo{cwd: cwd, ledger: ledger, key: key, identity: identity}
			if prev, ok := index[slug]; ok && prev.key != key {
				ambiguous[slug] = true
			}
			if prev, ok := index[identity]; ok && prev.key != key {
				ambiguous[identity] = true
			}
			index[slug] = repo
			index[identity] = repo
		}
	}
	s.mu.Lock()
	s.targetRepos = index
	s.ambiguousTargetRepos = ambiguous
	s.mu.Unlock()
	return nil
}

func (s *Server) scopedTargetRepo(slug string) (targetRepo, int, string) {
	parts := strings.Split(slug, "/")
	if len(parts) != 1 && len(parts) != 3 {
		return targetRepo{}, 400, "invalid repo slug"
	}
	if len(parts) == 1 && !repoSlugRe.MatchString(parts[0]) {
		return targetRepo{}, 400, "invalid repo slug"
	}
	for _, part := range parts {
		if !repoSegmentRe.MatchString(part) {
			return targetRepo{}, 400, "invalid repo slug"
		}
	}
	s.mu.RLock()
	repo, ok := s.targetRepos[slug]
	ambiguous := s.ambiguousTargetRepos[slug]
	s.mu.RUnlock()
	if ambiguous {
		return targetRepo{}, 409, "ambiguous repo slug"
	}
	if !ok {
		return targetRepo{}, 404, "unknown repo"
	}
	// A deleted or replaced ledger is not allowed to slide onto a different one.
	key, err := canonicalPath(repo.ledger)
	if err != nil || key != repo.key {
		return targetRepo{}, 404, "repo ledger unavailable"
	}
	return repo, 200, ""
}

func parseScopedTarget(repo, rawID string) (string, string, bool) {
	id := normalizeLedgerTargetID(rawID)
	if strings.Contains(id, "/") {
		if repo != "" {
			return repo, id, false
		}
		parts := strings.Split(id, "/")
		if len(parts) != 2 {
			return repo, id, false
		}
		repo, id = parts[0], normalizeLedgerTargetID(parts[1])
	}
	return repo, id, scopedIDRe.MatchString(id)
}
