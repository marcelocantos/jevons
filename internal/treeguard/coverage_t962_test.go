package treeguard

import (
	"strings"
	"testing"
)

func TestT962ProviderCoverageAnthropicIsSidecarNotHook(t *testing.T) {
	c := ProviderCoverage("anthropic")
	if !c.Refuses || !c.Detects {
		t.Fatalf("anthropic = %+v, want refuses+detects via sidecar", c)
	}
	for _, want := range []string{"OMP sidecar", "T962", "pre-exec"} {
		if !strings.Contains(c.Note, want) {
			t.Fatalf("anthropic note missing %q: %s", want, c.Note)
		}
	}
}

func TestT962ProviderCoverageClaudeIsHookPath(t *testing.T) {
	c := ProviderCoverage("claude")
	if !c.Refuses {
		t.Fatal("claude should refuse via hook")
	}
	if strings.Contains(c.Note, "OMP sidecar") {
		t.Fatalf("claude note should not claim sidecar path: %s", c.Note)
	}
}
