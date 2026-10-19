// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

func TestIsGoCompilePath(t *testing.T) {
	for p, want := range map[string]bool{
		"scripts/journey-suite/chrome_journeys.go": true,
		"scripts/docratchet/t818_node_check_test.go": true,
		"cmd/commitscope/main.go":                  true,
		"cmd/gate/main.go":                         true,
		"internal/commitscope/scope.go":            false,
		"ui/src/App.tsx":                           false,
		"scripts/hooks/pre-commit":                 false,
		"scripts/journey-suite/README.md":          false,
		"go.mod":                                   false,
	} {
		if got := IsGoCompilePath(p); got != want {
			t.Errorf("IsGoCompilePath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestTouchedGoPackages(t *testing.T) {
	got := TouchedGoPackages([]string{
		"ui/src/App.tsx",
		"scripts/journey-suite/chrome_journeys.go",
		"scripts/journey-suite/bounce.go",
		"cmd/commitscope/main.go",
		"internal/x.go",
		"scripts/hooks/pre-commit",
	})
	want := []string{"./cmd/commitscope", "./scripts/journey-suite"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if n := TouchedGoPackages([]string{"docs/x.md", "internal/a.go"}); len(n) != 0 {
		t.Fatalf("non-watched paths produced packages: %v", n)
	}
}

func TestGoCompileRefusalNamesPackageAndCompilerLine(t *testing.T) {
	msg := GoCompileRefusal(
		[]string{"./scripts/journey-suite"},
		"scripts/journey-suite/chrome_journeys.go:271:15: undefined: exec",
	)
	for _, want := range []string{
		"🎯T822",
		"./scripts/journey-suite",
		"chrome_journeys.go",
		"undefined: exec",
		"go build",
		GoCompileEnv + "=off",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal lacks %q:\n%s", want, msg)
		}
	}
}
