// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestT5401CensusCoversTaggedPreReactUI ratchets 🎯T540.1: every
// achieved-before-2026-08-22 target tagged as cockpit/chat/web UI is
// named in ui/src/oracle/census.ts. The census is the set; catalog.ts
// is only the runner map.
func TestT5401CensusCoversTaggedPreReactUI(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "bullseye.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Targets map[string]struct {
			Status   string   `yaml:"status"`
			Achieved string   `yaml:"achieved"`
			Tags     []string `yaml:"tags"`
			Name     string   `yaml:"name"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	cutoff, err := time.Parse("2006-01-02", "2026-08-22")
	if err != nil {
		t.Fatal(err)
	}
	uiTag := map[string]bool{
		"web": true, "cockpit": true, "chat": true, "visual": true,
		"composer": true, "virtual-list": true, "virtualization": true,
		"collapse": true, "plan-usage": true, "activity-strip": true,
		"keyboard": true, "markdown": true, "scroll": true, "frontier": true, "ux": true,
	}
	census := readRepo(t, "ui/src/oracle/census.ts")
	have := map[string]bool{}
	for _, m := range regexp.MustCompile(`id: "(T[0-9.]+)"`).FindAllStringSubmatch(census, -1) {
		have[m[1]] = true
	}
	if !strings.Contains(census, "CENSUS_CUTOFF = '2026-08-22'") {
		t.Fatal("census.ts must lock CENSUS_CUTOFF to 2026-08-22")
	}
	var missing []string
	for id, tgt := range doc.Targets {
		if !strings.EqualFold(tgt.Status, "achieved") || tgt.Achieved == "" {
			continue
		}
		ad, err := time.Parse("2006-01-02", tgt.Achieved)
		if err != nil || !ad.Before(cutoff) {
			continue
		}
		tagged := false
		for _, tag := range tgt.Tags {
			if uiTag[tag] {
				tagged = true
				break
			}
		}
		if !tagged {
			continue
		}
		if !have[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tagged pre-React UI targets missing from census.ts: %s", strings.Join(missing, " "))
	}
}
