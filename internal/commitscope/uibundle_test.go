// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

// TestIsBundleInputCountsOnlyWhatVitePackages pins the set of paths whose
// change alters ui/bundle.zip. Tests and oracle fixtures live under ui/src but
// never reach the bundle; counting them would refuse every test-only commit.
func TestIsBundleInputCountsOnlyWhatVitePackages(t *testing.T) {
	for p, want := range map[string]bool{
		"ui/src/App.tsx":                          true,
		"ui/src/components/AgentTree.tsx":         true,
		"ui/src/styles/app.css":                   true,
		"ui/src/logo.svg":                         true,
		"ui/index.html":                           true,
		"ui/public/favicon.svg":                   true,
		"ui/package.json":                         true,
		"ui/package-lock.json":                    true,
		"ui/vite.config.ts":                       true,
		"ui/tsconfig.app.json":                    true,
		"ui/src/clock.test.ts":                    false,
		"ui/src/components/AgentTree.test.tsx":    false,
		"ui/src/components/t811_delivery.test.ts": false,
		"ui/src/oracle/fixtures.ts":               false,
		"ui/src/oracle/methodology.md":            false,
		"ui/src/test-setup-timeouts.ts":           false,
		"ui/README.md":                            false,
		"ui/bundle.zip":                           false,
		"ui/embed.go":                             false,
		"internal/server/chat.go":                 false,
		"docs/ui/src/x.tsx":                       false,
	} {
		if got := IsBundleInput(p); got != want {
			t.Errorf("IsBundleInput(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestDecideRefusesUISourceWithoutBundle(t *testing.T) {
	scoped := "/r/.git/next-index-9.lock"
	for _, c := range []struct {
		name    string
		req     Request
		refused bool
	}{
		{"source without bundle", Request{IndexFile: scoped, Staged: []string{"ui/src/App.tsx"}}, true},
		{"source with bundle", Request{IndexFile: scoped, Staged: []string{"ui/src/App.tsx", "ui/bundle.zip"}}, false},
		{"bundle alone", Request{IndexFile: scoped, Staged: []string{"ui/bundle.zip"}}, false},
		{"test-only ui change", Request{IndexFile: scoped, Staged: []string{"ui/src/a.test.tsx"}}, false},
		{"no ui path at all", Request{IndexFile: scoped, Staged: []string{"internal/x.go"}}, false},
		{"lockfile without bundle", Request{IndexFile: scoped, Staged: []string{"ui/package-lock.json"}}, true},
		{"bundle opt-out", Request{IndexFile: scoped, Staged: []string{"ui/src/App.tsx"}, BundleDisabled: true}, false},
		{"scope opt-out does not lift the bundle rule", Request{IndexFile: scoped, Staged: []string{"ui/src/App.tsx"}, Disabled: true}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := Decide(&c.req)
			if v.Refused != c.refused {
				t.Fatalf("Refused = %v, want %v\n%s", v.Refused, c.refused, v.Message)
			}
		})
	}
}

func TestBundleRefusalNamesTheStaleAssetsAndTheRemedy(t *testing.T) {
	v := Decide(&Request{IndexFile: "/r/.git/next-index-9.lock", Staged: []string{"ui/src/App.tsx", "ui/src/b.tsx", "go.mod"}})
	for _, want := range []string{"ui/src/App.tsx", "ui/src/b.tsx", "ui/bundle.zip", "make ui-build", "--only", UIBundleEnv + "=off"} {
		if !strings.Contains(v.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, v.Message)
		}
	}
	if strings.Contains(v.Message, "go.mod") {
		t.Errorf("message names a path that is not a bundle input:\n%s", v.Message)
	}
}

func TestBypassIsNamedNotSilent(t *testing.T) {
	v := Decide(&Request{IndexFile: "/r/.git/next-index-9.lock", Staged: []string{"ui/src/App.tsx"}, BundleDisabled: true})
	if v.Refused || !strings.Contains(v.Message, BypassNoticePrefix) || !strings.Contains(v.Message, "ui/src/App.tsx") {
		t.Fatalf("bypass verdict = %+v", v)
	}
}
