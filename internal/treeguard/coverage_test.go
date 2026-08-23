package treeguard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/treeguard"
)

func TestProviderCoverageRefusesOnlyClaudeFamily(t *testing.T) {
	claude := treeguard.ProviderCoverage("claude")
	if !claude.Refuses || !claude.Detects {
		t.Fatalf("claude coverage = %+v; want refuses+detects", claude)
	}
	if !strings.Contains(claude.Note, "REFUSED") {
		t.Errorf("claude note does not say refused: %s", claude.Note)
	}

	for _, p := range []string{"grok", "codex", "claudia", "", "mystery"} {
		c := treeguard.ProviderCoverage(p)
		if c.Refuses {
			t.Errorf("provider %q claims refusal coverage it cannot deliver: %+v", p, c)
		}
		if !c.Detects {
			t.Errorf("provider %q must still detect via pre-commit sweep: %+v", p, c)
		}
		if !strings.Contains(c.BriefLine(), "DETECT-ONLY") {
			t.Errorf("provider %q brief must surface DETECT-ONLY, got %q", p, c.BriefLine())
		}
		if !strings.Contains(c.Note, "NOT refused") {
			t.Errorf("provider %q note must say it is NOT refused: %s", p, c.Note)
		}
	}
}

func TestDetectProviderPrefersExplicitEnv(t *testing.T) {
	lookup := func(k string) string {
		switch k {
		case treeguard.ProviderEnv:
			return "grok"
		case treeguard.ClaudeCodeEnv:
			return "1"
		default:
			return ""
		}
	}
	if got := treeguard.DetectProvider(lookup); got != "grok" {
		t.Fatalf("DetectProvider = %q, want grok (explicit env wins over CLAUDECODE)", got)
	}
	lookup = func(k string) string {
		if k == treeguard.ClaudeCodeEnv {
			return "1"
		}
		return ""
	}
	if got := treeguard.DetectProvider(lookup); got != "claude" {
		t.Fatalf("DetectProvider = %q, want claude from CLAUDECODE", got)
	}
	if got := treeguard.DetectProvider(func(string) string { return "" }); got != "" {
		t.Fatalf("DetectProvider = %q, want empty for unknown harness", got)
	}
}

func TestCoverageNoticeSurfacesOncePerIntervalForUnprotected(t *testing.T) {
	storeRoot := t.TempDir()
	env := &treeguard.Env{Store: &treeguard.Store{Root: storeRoot}}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	first := env.CoverageNotice("grok", now)
	if first == "" || !strings.Contains(first, "DETECT-ONLY") {
		t.Fatalf("first notice missing DETECT-ONLY: %q", first)
	}
	second := env.CoverageNotice("grok", now.Add(time.Hour))
	if second != "" {
		t.Fatalf("second notice inside interval should be suppressed, got %q", second)
	}
	third := env.CoverageNotice("grok", now.Add(treeguard.CoverageNoticeInterval+time.Second))
	if third == "" {
		t.Fatal("notice should resurface after the interval")
	}
	if env.CoverageNotice("claude", now) != "" {
		t.Fatal("protected provider must not be nagged on every commit")
	}
	// Stamp file is under the store root so a missing directory cannot silence it.
	if _, err := os.Stat(filepath.Join(storeRoot, "coverage-notice")); err != nil {
		t.Fatalf("coverage stamp not written: %v", err)
	}
}
