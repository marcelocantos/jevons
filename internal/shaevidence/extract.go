// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package shaevidence extracts and checks commit SHAs cited as attestation
// evidence (🎯T427). A SHA that is honest at the moment it is written can
// evaporate under bullseye's yaml-only amend — the worker must prove
// reachability before citing, and a standing check reports citations that
// are no longer ancestors of HEAD.
package shaevidence

import (
	"regexp"
	"strings"
)

// Citation is one evidence-shaped SHA found in prose (finish report or ledger).
type Citation struct {
	SHA string
	// Line is the source line (trimmed) the SHA was taken from.
	Line string
}

// Patterns that name the evidence commit itself. Deliberately narrower than
// "any hex on a line that mentions commit": a line like
//
//	git merge-base --is-ancestor 9f93f16 HEAD … against current c97d187
//
// cites 9f93f16 as evidence and c97d187 only as the then-tip — treating the
// tip as a citation flags honest reports the moment HEAD moves (🎯T468 fixture).
var (
	cuedSHARe = regexp.MustCompile(
		`(?i)\b(?:sha|commit(?:ted)?|landed(?:\s+by)?)\b.{0,40}?([0-9a-f]{7,40})\b`)
	ancestorArgRe = regexp.MustCompile(
		`(?i)merge-base\s+--is-ancestor\s+([0-9a-f]{7,40})\b`)
	shaThenAncestorRe = regexp.MustCompile(
		`(?i)\b([0-9a-f]{7,40})\b.{0,48}?\b(?:ancestor|reachable)\b`)
	ancestorThenSHARe = regexp.MustCompile(
		`(?i)\b(?:ancestor|reachable)\b.{0,40}?([0-9a-f]{7,40})\b`)
)

// ExtractEvidenceSHAs returns every evidence-shaped SHA in text, de-duplicated
// in first-seen order. Pure: no git, no filesystem.
func ExtractEvidenceSHAs(text string) []Citation {
	seen := make(map[string]bool)
	var out []Citation
	add := func(sha, line string) {
		sha = strings.ToLower(strings.TrimSpace(sha))
		if sha == "" || seen[sha] {
			return
		}
		seen[sha] = true
		out = append(out, Citation{SHA: sha, Line: line})
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		for _, m := range cuedSHARe.FindAllStringSubmatch(line, -1) {
			add(m[1], line)
		}
		for _, m := range ancestorArgRe.FindAllStringSubmatch(line, -1) {
			add(m[1], line)
		}
		for _, m := range shaThenAncestorRe.FindAllStringSubmatch(line, -1) {
			add(m[1], line)
		}
		for _, m := range ancestorThenSHARe.FindAllStringSubmatch(line, -1) {
			add(m[1], line)
		}
	}
	return out
}

// TouchesOnlyLedger reports whether paths is exactly one bullseye.yaml entry.
// That — together with an unpushed tip — is what makes a commit amend-vulnerable
// under bullseye's auto-commit (🎯T427 / bullseye 🎯T22). File count alone is
// not the predicate: a single-file code commit is safe.
func TouchesOnlyLedger(paths []string) bool {
	if len(paths) != 1 {
		return false
	}
	p := strings.ReplaceAll(paths[0], "\\", "/")
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	return p == "bullseye.yaml"
}
