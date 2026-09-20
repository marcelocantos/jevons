// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Doctrine files that must name every verdict internal/gate can emit (🎯T733).
// fleet_brief.go is the injected standing brief; persona.md is the
// internal/config copy; agents-guide.md's T360 mirror is help_agent.md.
var t733DoctrineFiles = []string{
	"AGENTS.md",
	"internal/config/persona.md",
	"internal/mcpserver/fleet_brief.go",
	"agents-guide.md",
}

// verdictConstRe pulls VerdictX Verdict = "WORD" out of internal/gate/gate.go.
// Source parse, not an AllVerdicts() helper: adding a constant the helper
// forgot to list must still go RED.
var verdictConstRe = regexp.MustCompile(`(?m)^\s+Verdict\w+\s+Verdict\s*=\s*"([A-Z]+)"`)

// attestationWordsRe is the GATE-line verdict alternation in record.go.
var attestationWordsRe = regexp.MustCompile(`\((GREEN\|[A-Z|]+)\)`)

// TestT733DoctrineNamesEveryEmittableVerdict ratchets 🎯T733: every verdict
// constant in internal/gate must appear as a whole token in the standing
// doctrine, and the GATE-line parser must name the same set. Adding
// VerdictFoo = "FOO" with no doctrine mention goes RED.
func TestT733DoctrineNamesEveryEmittableVerdict(t *testing.T) {
	words := emittableVerdicts(t)
	if len(words) == 0 {
		t.Fatal("internal/gate/gate.go has no Verdict constants")
	}
	for _, doc := range t733DoctrineFiles {
		body := readRepo(t, doc)
		for _, w := range words {
			if !hasWholeToken(body, w) {
				t.Errorf("%s does not name emittable verdict %q", doc, w)
			}
		}
		if !strings.Contains(body, "uncommitted") {
			t.Errorf("%s missing DIRTY gloss (uncommitted changes)", doc)
		}
		if !strings.Contains(body, "bin/gate -clean") {
			t.Errorf("%s missing DIRTY gloss (bin/gate -clean)", doc)
		}
	}

	attested := attestationVerdicts(t)
	if !sameStringSet(words, attested) {
		t.Errorf("attestationRe verdicts %v != gate.go constants %v", attested, words)
	}
}

// TestT733NewVerdictConstantWithoutDoctrineGoesRed is the mutation clause:
// a VerdictFoo = "FOO" constant that no doctrine file mentions must fail
// the same whole-token check the live ratchet applies.
func TestT733NewVerdictConstantWithoutDoctrineGoesRed(t *testing.T) {
	src := "\tVerdictFoo Verdict = \"FOO\"\n"
	m := verdictConstRe.FindAllStringSubmatch(src, -1)
	if len(m) != 1 || m[0][1] != "FOO" {
		t.Fatalf("parser missed FOO in %q: %v", src, m)
	}
	for _, doc := range t733DoctrineFiles {
		body := readRepo(t, doc)
		if hasWholeToken(body, "FOO") {
			t.Errorf("%s already names FOO; mutation would not go RED", doc)
		}
		if !hasWholeToken(body, "DIRTY") {
			t.Errorf("%s missing DIRTY after the doctrine update", doc)
		}
	}
}

func emittableVerdicts(t *testing.T) []string {
	t.Helper()
	src := readRepo(t, "internal/gate/gate.go")
	seen := map[string]bool{}
	var out []string
	for _, m := range verdictConstRe.FindAllStringSubmatch(src, -1) {
		w := m[1]
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

func attestationVerdicts(t *testing.T) []string {
	t.Helper()
	src := readRepo(t, "internal/gate/record.go")
	m := attestationWordsRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("internal/gate/record.go has no GATE-line verdict alternation")
	}
	parts := strings.Split(m[1], "|")
	seen := map[string]bool{}
	var out []string
	for _, w := range parts {
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

func hasWholeToken(body, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(body)
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
