// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// 🎯T1072: plan bands have one source of truth, Claudia's Spend Pressure Map.
// Twice before, a second copy of the model drifted from it — the cockpit's
// ratio classifier (🎯T610) and this package's fixed-15% leftover override,
// which painted a late-month Cursor green while Claudia said under. This
// ratchet fails when a classifier or a vertex copy reappears outside Claudia.

type t1072Rule struct {
	re  *regexp.Regexp
	why string
}

func TestT1072NoSecondBandModelInPlanusage(t *testing.T) {
	rules := []t1072Rule{
		{regexp.MustCompile(`math\.Log\(`), "pressure is claudia.Pressure; a local log is a second model"},
		{regexp.MustCompile(`\b(PanicAmberLn|PanicRedLn|WasteUnderLn|WasteLockedLn|ShrinkPriorK|LowRemainingPercent)\s*[:=]\s*-?[0-9]`), "vertices come from claudia.DefaultPlanThresholds, not a literal"},
		{regexp.MustCompile(`\b(applyWeeklyWaste|dampedBurn|burningFastReachable|continuation\s*:?=)`), "the fixed-15% / ratio model was retired by 🎯T1072"},
	}
	dir := t1072PackageDir(t)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		t1072Scan(t, f, rules)
	}
	if checked == 0 {
		t.Fatal("scanned no planusage sources")
	}
	src, err := os.ReadFile(filepath.Join(dir, "thresholds.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "claudia.DefaultPlanThresholds()") {
		t.Fatal("DefaultThresholds no longer reads claudia.DefaultPlanThresholds()")
	}
}

func TestT1072CockpitClassifiesNothing(t *testing.T) {
	rules := []t1072Rule{
		{regexp.MustCompile(`\b(classifyPace|weeklyWaste|paceColor|fillColorForWindow|dampedBurn|overspendStops|applyThresholds|hsvLerpRgb|burningFastReachable)\b`), "the cockpit paints the served band; it does not classify"},
		{regexp.MustCompile(`/api/plan-usage/thresholds`), "the cockpit has no use for vertices; the band is served"},
		{regexp.MustCompile(`Math\.log\(`), "pressure is computed by Claudia, served beside the band"},
		{regexp.MustCompile(`\b(panic_amber_ln|panic_red_ln|waste_under_ln|waste_locked_ln|shrink_prior_k|under_waste_percent|locked_waste_percent|ahead_ratio|hot_ratio)\b`), "a vertex key in the cockpit is the start of a second model"},
	}
	root := filepath.Join(t1072PackageDir(t), "..", "..", "ui", "src")
	checked := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(p)
		if ext != ".ts" && ext != ".tsx" {
			return nil
		}
		base := filepath.Base(p)
		if strings.Contains(base, ".test.") {
			return nil
		}
		checked++
		t1072Scan(t, p, rules)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("scanned no cockpit sources")
	}
}

func t1072Scan(t *testing.T, path string, rules []t1072Rule) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(b), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "/*") {
			continue
		}
		for _, r := range rules {
			if r.re.MatchString(line) {
				t.Errorf("%s:%d: %s\n  %s", path, i+1, r.why, trim)
			}
		}
	}
}

func t1072PackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}
