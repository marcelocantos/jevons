// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sandboxcap

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T596 hermetic: a target tagged as needing a listener plus a codex
// destination yields a refusal citing the capability; the same target
// plus claude is admitted.
func TestT596ListenerTargetRefusesCodexAdmitsClaude(t *testing.T) {
	needs := NeedsFromTags([]string{TagNeedsListener})
	if len(needs) != 1 || needs[0] != LocalhostListener {
		t.Fatalf("NeedsFromTags = %v, want [localhost_listener]", needs)
	}
	if why := PlacementRefusal(claudia.ProviderCodex, needs); why == "" {
		t.Fatal("codex destination admitted a needs-listener target")
	} else {
		for _, want := range []string{"localhost_listener", "codex", "claude"} {
			if !strings.Contains(why, want) {
				t.Fatalf("refusal %q missing %q", why, want)
			}
		}
	}
	if why := PlacementRefusal(claudia.ProviderClaude, needs); why != "" {
		t.Fatalf("claude refused a needs-listener target: %s", why)
	}
	// Sidecar spelling of the same plan must refuse too — registry is by
	// plan id, not by the raw provider string.
	if why := PlacementRefusal(claudia.Provider("openai-codex"), needs); why == "" {
		t.Fatal("openai-codex admitted a needs-listener target")
	}
}

// Registry is a table: capabilities are not inferred by switching on the
// backend name at each call site.
func TestT596CapsComeFromTheRegistry(t *testing.T) {
	if Has(claudia.ProviderCodex, LocalhostListener) {
		t.Fatal("codex registry row grants localhost_listener")
	}
	if !Has(claudia.ProviderCodex, GateLogWrite) {
		t.Fatal("codex registry row lacks gate_log_write (🎯T598)")
	}
	for _, p := range []claudia.Provider{
		claudia.ProviderClaude, claudia.ProviderGrok, claudia.ProviderCursor,
	} {
		if !Has(p, LocalhostListener) || !Has(p, GateLogWrite) {
			t.Fatalf("%s missing a capability: %v", p, Caps(p))
		}
	}
}

func TestT596NeedsFromTagsOnlyExactTags(t *testing.T) {
	if got := NeedsFromTags([]string{"ui", "journey", "make-test-ui"}); len(got) != 0 {
		t.Fatalf("inferred needs from non-exact tags: %v", got)
	}
	got := NeedsFromTags([]string{"needs-listener", "Needs-Gate-Log", "other"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestT596EligibleBackendsNamesWhoCan(t *testing.T) {
	elig := EligibleBackends([]Capability{LocalhostListener})
	joined := strings.Join(elig, ",")
	if strings.Contains(joined, "codex") {
		t.Fatalf("codex listed as eligible for listener: %v", elig)
	}
	if !strings.Contains(joined, "claude") {
		t.Fatalf("claude missing from eligible: %v", elig)
	}
}

func TestT596NoNeedsNeverRefuses(t *testing.T) {
	if why := PlacementRefusal(claudia.ProviderCodex, nil); why != "" {
		t.Fatalf("empty needs refused: %s", why)
	}
}

func TestT596CapabilityBlockedReport(t *testing.T) {
	body := FinishReport("T557", claudia.ProviderCodex, []Capability{LocalhostListener})
	for _, want := range []string{
		"jevons: status capability_blocked",
		"localhost_listener",
		"codex",
		"T557",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("report missing %q:\n%s", want, body)
		}
	}
}
