// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestT574BounceRequiredSetIsDocumented ratchets 🎯T574: every config.yaml
// field the code treats as bounce-required (config.RestartOnlyDiff) is named
// in the design doc's bounce-required table, and so are the two non-yaml
// elements. The set shrinks by deliberate commit only — a field dropped
// from the code must be dropped from the doc in the same change, and a
// field added to the code without a documented reason fails here.
func TestT574BounceRequiredSetIsDocumented(t *testing.T) {
	code := readRepo(t, "internal/config/watch.go")
	doc := readRepo(t, "docs/design/hot-config.md")
	idx := strings.Index(doc, "### Bounce-required set")
	if idx < 0 {
		t.Fatal("docs/design/hot-config.md has no '### Bounce-required set' table")
	}
	table := doc[idx:]
	re := regexp.MustCompile(`\{"([a-z_]+)", (?:old|fmt)`)
	fields := re.FindAllStringSubmatch(code, -1)
	if len(fields) == 0 {
		t.Fatal("RestartOnlyDiff field table not found in internal/config/watch.go")
	}
	for _, m := range fields {
		if !strings.Contains(table, "`"+m[1]+"`") {
			t.Errorf("bounce-required field %q in RestartOnlyDiff is not in docs/design/hot-config.md", m[1])
		}
	}
	for _, want := range []string{"`disabled`", "`mcp owner map`", "🎯T392.5", "JEVONS_CONFIG_BOUNCE"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/design/hot-config.md missing %q", want)
		}
	}
}

// TestT574NoReadOnceLoaderInDaemon ratchets the seam: a policy loader is
// only ever called as a config.Watch Load (or from the boot read the
// watch re-registers). A new `x, err := LoadPolicy(path)` at start is the
// bug this target kills, and it fails here before it reaches a daemon.
func TestT574NoReadOnceLoaderInDaemon(t *testing.T) {
	loaders := []string{"capacity.LoadPolicy(", "cost.LoadBudgetConfig(", "cost.LoadPortfolioOverride(", "config.Load(", "claudia.LoadMCP("}
	allowed := map[string]bool{
		// The boot read: the same path is registered with the watcher a
		// few lines later, so it is the first poll, not a read-once.
		"cmd/jevonsd/main.go|cfg, err := config.Load(cfgPath)": true,
		// The mcpscope boot read feeds watchMCPOwnerMap's Sources.
		"cmd/jevonsd/mcpscope.go|inv, err := claudia.LoadMCP(load)": true,
	}
	for _, file := range []string{"cmd/jevonsd/main.go", "cmd/jevonsd/capacity.go", "cmd/jevonsd/cost.go", "cmd/jevonsd/mcpscope.go", "cmd/jevonsd/config_bounce.go"} {
		lines := strings.Split(readRepo(t, file), "\n")
		for i, line := range lines {
			trim := strings.TrimSpace(line)
			for _, l := range loaders {
				if !strings.Contains(trim, l) || strings.HasPrefix(trim, "//") {
					continue
				}
				if strings.Contains(trim, "Load:") || allowed[file+"|"+trim] || insideWatchLoader(lines, i) {
					continue
				}
				t.Errorf("%s:%d: %q is a read-once policy load outside the config.Watch seam (🎯T574)", file, i+1, trim)
			}
		}
	}
}

// insideWatchLoader reports whether line i sits in a `Load: func(` or
// `func budgetLoader(` body — the two shapes the seam calls loaders from.
func insideWatchLoader(lines []string, i int) bool {
	for j := i; j >= 0 && j > i-12; j-- {
		s := strings.TrimSpace(lines[j])
		if strings.HasPrefix(s, "Load: func(") || strings.Contains(s, "return func(path string) (*cost.BudgetConfig, error) {") {
			return true
		}
	}
	return false
}
