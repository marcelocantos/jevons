// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// openStatuses are non-terminal target statuses. An achieved target that
// still depends on one of these is skipped by bullseye graph reads
// ("achieved while depending on open target(s)"), so it drops out of
// frontier computation without a hard fail — the 🎯T699 failure mode.
var openStatuses = map[string]bool{
	"identified":  true,
	"converging":  true,
}

// TestT699NoAchievedDependsOnOpen is the standing regression check for
// 🎯T699: the ledger must not carry an achieved target whose depends_on
// still names an open (identified/converging) target. set_aside deps are
// terminal dispositions and are allowed (they unblock the same way
// achieved does).
func TestT699NoAchievedDependsOnOpen(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Targets map[string]struct {
			Status    string   `yaml:"status"`
			DependsOn []string `yaml:"depends_on"`
			Name      string   `yaml:"name"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Targets) == 0 {
		t.Fatal("bullseye.yaml parsed zero targets")
	}

	var bad []string
	for id, tgt := range doc.Targets {
		if !strings.EqualFold(tgt.Status, "achieved") {
			continue
		}
		for _, dep := range tgt.DependsOn {
			depT, ok := doc.Targets[dep]
			if !ok {
				bad = append(bad, fmt.Sprintf("%s depends on missing %s", id, dep))
				continue
			}
			st := strings.ToLower(strings.TrimSpace(depT.Status))
			if openStatuses[st] {
				bad = append(bad, fmt.Sprintf(
					"%s (achieved) depends on open %s (%s)", id, dep, st))
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatalf("🎯T699: achieved targets still depend on open ones — "+
			"bullseye skips these nodes and frontier reads go quiet:\n  %s",
			strings.Join(bad, "\n  "))
	}
}

// TestT699T553PairReconciled locks the concrete pair that filed 🎯T699:
// T553.2 was achieved while T553.1 was still open. Both must now be
// terminal-achieved (the chosen reconciliation, 2026-09-22).
func TestT699T553PairReconciled(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Targets map[string]struct {
			Status string `yaml:"status"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T553.1", "T553.2"} {
		tgt, ok := doc.Targets[id]
		if !ok {
			t.Errorf("%s missing from ledger — 🎯T699 pair must stay present", id)
			continue
		}
		if !strings.EqualFold(tgt.Status, "achieved") {
			t.Errorf("%s status=%q, want achieved (🎯T699 reconciliation)", id, tgt.Status)
		}
	}
}
