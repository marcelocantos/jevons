// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Doctrine files that must name every string-matching false-green rule
// (🎯T742). Same surfaces as 🎯T733: the next author who adds a hazard
// matcher has to see the constraint, not discover it by banner-flagging
// a careful report. agents-guide.md's T360 mirror is help_agent.md.
var t742DoctrineFiles = []string{
	"AGENTS.md",
	"internal/config/persona.md",
	"internal/mcpserver/fleet_brief.go",
	"agents-guide.md",
}

// flagKindConstRe pulls FlagX FlagKind = "word" out of internal/gate/*.go.
// Source parse, not a helper: adding a constant the helper forgot must still go RED.
var flagKindConstRe = regexp.MustCompile(`(?m)^\s+(Flag\w+)\s+FlagKind\s*=\s*"([a-z_]+)"`)

// hazardRuleRe pulls {kind: FlagX, region: RegionY from the closed table.
var hazardRuleRe = regexp.MustCompile(`\{kind:\s*(Flag\w+),\s*region:\s*(Region\w+)`)

// TestT742DoctrineNamesEveryStringMatchingRule ratchets 🎯T742: every
// hazardRules entry in internal/gate is a named token in standing doctrine,
// and its region is quoted or shaped — never unbounded. Adding
// FlagFoo = "new_hazard_trap" to the table with no doctrine mention goes RED.
func TestT742DoctrineNamesEveryStringMatchingRule(t *testing.T) {
	rules := stringMatchingRules(t)
	if len(rules) < 4 {
		t.Fatalf("found only %d string-matching rules (%v) — the enumerator has rotted, not the rules", len(rules), rules)
	}
	for _, r := range rules {
		if r.region != "RegionQuoted" && r.region != "RegionShaped" {
			t.Errorf("%s region %s is not quoted or shaped — a rule added later cannot ship scanning unbounded text (🎯T742)", r.kind, r.region)
		}
	}

	// T733 bound gate.go Verdict constants to attestationRe. Same shape:
	// every FlagKind constant must sit in structuredFlagKinds or hazardRules.
	// A fifth string-matching rule therefore cannot ship as an unclassified
	// constant — it has to join the table, which then forces doctrine to move.
	consts := flagKindConstNames(t)
	if len(consts) < 10 {
		t.Fatalf("found only %d FlagKind constants (%v) — the enumerator has rotted", len(consts), consts)
	}
	seen := map[string]bool{}
	var classified []string
	for _, n := range structuredFlagNames(t) {
		if seen[n] {
			continue
		}
		seen[n] = true
		classified = append(classified, n)
	}
	for _, r := range rules {
		if seen[r.kind] {
			t.Errorf("%s is in both structuredFlagKinds and hazardRules", r.kind)
			continue
		}
		seen[r.kind] = true
		classified = append(classified, r.kind)
	}
	sort.Strings(classified)
	if !sameStringSet(consts, classified) {
		t.Errorf("FlagKind constants %v != structured∪hazard %v — a new constant must join structuredFlagKinds or hazardRules (🎯T742 / 🎯T733)", consts, classified)
	}

	for _, doc := range t742DoctrineFiles {
		body := readRepo(t, doc)
		if !stringsContainsT742(body) {
			t.Errorf("%s does not name 🎯T742", doc)
		}
		if !hasWholeToken(body, "scannable region") && !hasWholeToken(body, "ScanRegion") {
			t.Errorf("%s missing scannable-region gloss (🎯T742)", doc)
		}
		if !hasWholeToken(body, "quoted") {
			t.Errorf("%s missing quoted-region gloss (🎯T742)", doc)
		}
		if !hasWholeToken(body, "dirty_tree_gate") {
			t.Errorf("%s missing dirty_tree_gate keep-strength gloss (🎯T742)", doc)
		}
		for _, r := range rules {
			if !hasWholeToken(body, r.word) {
				t.Errorf("%s does not name string-matching rule %q", doc, r.word)
			}
		}
	}
}

// TestT742NewHazardRuleWithoutDoctrineGoesRed is the mutation clause:
// a FlagFoo = "new_hazard_trap" table entry that no doctrine file mentions
// must fail the same whole-token check the live ratchet applies.
func TestT742NewHazardRuleWithoutDoctrineGoesRed(t *testing.T) {
	src := "\tFlagFoo FlagKind = \"new_hazard_trap\"\n"
	m := flagKindConstRe.FindAllStringSubmatch(src, -1)
	if len(m) != 1 || m[0][1] != "FlagFoo" || m[0][2] != "new_hazard_trap" {
		t.Fatalf("parser missed FlagFoo in %q: %v", src, m)
	}
	table := "{kind: FlagFoo, region: RegionQuoted, scan: scanFoo},"
	tm := hazardRuleRe.FindAllStringSubmatch(table, -1)
	if len(tm) != 1 || tm[0][1] != "FlagFoo" || tm[0][2] != "RegionQuoted" {
		t.Fatalf("parser missed table entry in %q: %v", table, tm)
	}
	for _, doc := range t742DoctrineFiles {
		body := readRepo(t, doc)
		if hasWholeToken(body, "new_hazard_trap") {
			t.Errorf("%s already names new_hazard_trap; mutation would not go RED", doc)
		}
		if !hasWholeToken(body, "shell_array_trap") {
			t.Errorf("%s missing shell_array_trap after the doctrine update", doc)
		}
	}
}

type t742Rule struct {
	kind, region, word string
}

func stringMatchingRules(t *testing.T) []t742Rule {
	t.Helper()
	values := map[string]string{}
	for _, m := range flagKindConstRe.FindAllStringSubmatch(readRepo(t, "internal/gate/claim.go"), -1) {
		values[m[1]] = m[2]
	}
	if len(values) == 0 {
		t.Fatal("internal/gate/claim.go has no FlagKind constants")
	}
	var out []t742Rule
	seen := map[string]bool{}
	for _, m := range hazardRuleRe.FindAllStringSubmatch(readRepo(t, "internal/gate/scan_region.go"), -1) {
		kind, region := m[1], m[2]
		word, ok := values[kind]
		if !ok {
			t.Errorf("hazardRules names %s but claim.go has no FlagKind constant for it", kind)
			continue
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, t742Rule{kind: kind, region: region, word: word})
	}
	return out
}

func flagKindConstNames(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var names []string
	// 🎯T800: a FlagKind constant declared in ANY non-test file of
	// internal/gate is seen — 🎯T765 declared three in achieve.go while this
	// scan read only claim.go, so the ratchet went red for the wrong reason.
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "internal", "gate"))
	if err != nil {
		t.Fatalf("read internal/gate: %v", err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		for _, m := range flagKindConstRe.FindAllStringSubmatch(readRepo(t, "internal/gate/"+n), -1) {
			if seen[m[1]] {
				continue
			}
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	return names
}

func structuredFlagNames(t *testing.T) []string {
	t.Helper()
	src := readRepo(t, "internal/gate/scan_region.go")
	const marker = "var structuredFlagKinds"
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatal("internal/gate/scan_region.go has no structuredFlagKinds")
	}
	rest := src[start:]
	open := strings.Index(rest, "{")
	close := strings.Index(rest, "}")
	if open < 0 || close <= open {
		t.Fatal("structuredFlagKinds block malformed")
	}
	block := rest[open:close]
	seen := map[string]bool{}
	var names []string
	for _, m := range regexp.MustCompile(`\b(Flag\w+)\b`).FindAllStringSubmatch(block, -1) {
		if m[1] == "FlagKind" || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		names = append(names, m[1])
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("structuredFlagKinds enumerator found nothing")
	}
	return names
}

func stringsContainsT742(body string) bool {
	return hasWholeToken(body, "T742")
}
