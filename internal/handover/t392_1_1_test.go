// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/ctxcap"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func predecessorFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join("testdata", "predecessor.jsonl")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return p
}

func writeFatTranscript(t *testing.T) string {
	t.Helper()
	// Enough runes that TokenCount(file) exceeds the 100k ceiling when
	// the retired assignment causes a full read.
	const ceiling = 100_000
	var b strings.Builder
	pad := strings.Repeat("word ", 200)
	for i := 0; handover.TokenCount(b.String()) < ceiling+10_000; i++ {
		line, _ := json.Marshal(map[string]any{
			"type":    "user",
			"content": []map[string]string{{"type": "text", "text": pad + " open thread T999 in flight"}},
		})
		b.Write(line)
		b.WriteByte('\n')
		aline, _ := json.Marshal(map[string]any{
			"type":    "assistant",
			"content": "I'll finish T999 next. " + pad,
		})
		b.Write(aline)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "fat.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func forbiddenReadPhrases() []string {
	return []string{
		"start at the end",
		"work backwards",
		"read it before doing anything else",
	}
}

func TestComposeSeedUsesBriefNotTranscriptWalk(t *testing.T) {
	path := predecessorFixture(t)
	turns, err := transcript.ReadPath(path)
	if err != nil || len(turns) == 0 {
		t.Fatalf("T213 reader must see the fixture: turns=%d err=%v", len(turns), err)
	}

	migrate := handover.ComposeSeed(handover.Pending{
		From: "claude", To: "grok", Kind: handover.KindMigrate, TranscriptPath: path,
	})
	compact := handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	})

	for name, seed := range map[string]string{"migrate": migrate, "compact": compact} {
		if seed == "" {
			t.Fatalf("%s seed empty", name)
		}
		if n := handover.TokenCount(seed); n > handover.MaxBriefTokens+200 {
			// Wrapper text is small; the brief is the capped part. Whole
			// seed must stay far under the 100k ceiling.
			t.Errorf("%s seed tokens=%d want << ceiling", name, n)
		}
		if n := handover.TokenCount(seed); int64(n) > ctxcap.DefaultCeiling {
			t.Errorf("%s first-turn seed %d exceeds ceiling %d", name, n, ctxcap.DefaultCeiling)
		}
		low := strings.ToLower(seed)
		for _, bad := range forbiddenReadPhrases() {
			if strings.Contains(low, bad) {
				t.Errorf("%s seed still assigns a transcript walk (%q):\n%s", name, bad, seed)
			}
		}
		if !strings.Contains(low, "lookup only") {
			t.Errorf("%s seed must cite the path as lookup-only", name)
		}
		if !strings.Contains(seed, path) {
			t.Errorf("%s seed dropped the citation path", name)
		}
		if !strings.Contains(low, "hard stop") && !strings.Contains(low, "migrate") && !strings.Contains(low, "opus") {
			t.Errorf("%s brief lost predecessor content:\n%s", name, seed)
		}
	}

	if strings.Contains(strings.ToLower(compact), "different agent backend") {
		t.Errorf("compact seed claimed a backend change:\n%s", compact)
	}
	if strings.Contains(compact, "grok → grok") || strings.Contains(compact, "claude → grok") {
		t.Errorf("compact seed named a provider pair:\n%s", compact)
	}
	if !strings.Contains(migrate, "claude") || !strings.Contains(migrate, "grok") {
		t.Errorf("migrate seed must name the provider change:\n%s", migrate)
	}
	if !strings.Contains(strings.ToLower(migrate), "different agent backend") {
		t.Errorf("migrate seed must still name the backend change:\n%s", migrate)
	}
}

func TestSameProviderNeverUsesMigrateStory(t *testing.T) {
	path := predecessorFixture(t)
	// A caller that labels grok→grok as migrate must still get compact copy.
	seed := handover.SeedMessage("grok", "grok", path)
	low := strings.ToLower(seed)
	if strings.Contains(low, "different agent backend") {
		t.Fatalf("same-provider SeedMessage used the migrate story:\n%s", seed)
	}
	if !strings.Contains(low, "context compact") && !strings.Contains(low, "same backend") {
		t.Fatalf("same-provider seed is not the compact story:\n%s", seed)
	}
	if handover.ProviderSwitch("grok", "grok") {
		t.Fatal("ProviderSwitch(grok, grok) is true")
	}
	if !handover.ProviderSwitch("claude", "grok") {
		t.Fatal("ProviderSwitch(claude, grok) is false")
	}
}

func TestCompactOwnerVisibleIsSilent(t *testing.T) {
	path := predecessorFixture(t)
	compact := handover.Pending{From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path}
	if got := compact.OwnerVisible(); got != "" {
		t.Fatalf("compact owner paint = %q, want empty (not reconstruction)", got)
	}
	seed := compact.Seed()
	if strings.Contains(strings.ToLower(seed), "start at the end") {
		t.Fatal("compact seed still asks for reconstruction")
	}
	if !strings.Contains(strings.ToLower(seed), "one short sentence") &&
		!strings.Contains(strings.ToLower(seed), "say nothing") {
		t.Fatalf("compact seed does not constrain the owner-visible reply:\n%s", seed)
	}
	migrate := handover.Pending{From: "claude", To: "grok", Kind: handover.KindMigrate, TranscriptPath: path}
	if got := migrate.OwnerVisible(); !strings.Contains(got, "grok") || !strings.Contains(got, "claude") {
		t.Fatalf("migrate owner paint = %q", got)
	}
}

func TestAssignedReadAgainstFatFixtureExceedsCeiling(t *testing.T) {
	path := writeFatTranscript(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Mutation: restore the retired assignment and charge the full file
	// as the first-turn context. That is what blew 105k today.
	assigned := handover.AssignedReadAssignment + "\n" + string(raw)
	if n := handover.TokenCount(assigned); int64(n) <= ctxcap.DefaultCeiling {
		t.Fatalf("mutation control too thin: assigned-read tokens=%d want > %d", n, ctxcap.DefaultCeiling)
	}
	seed := handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	})
	if n := handover.TokenCount(seed); int64(n) > ctxcap.DefaultCeiling {
		t.Fatalf("shipped seed tokens=%d still over the ceiling", n)
	}
	if n := handover.TokenCount(seed); n > handover.MaxBriefTokens+400 {
		t.Fatalf("shipped seed tokens=%d over brief cap", n)
	}
}

func TestSeedMessageEmptyPathStillSeedsNothing(t *testing.T) {
	if seed := handover.SeedMessage("grok", "claude", ""); seed != "" {
		t.Fatalf("empty path produced a seed:\n%s", seed)
	}
}

func TestCaptureVerificationSeeds(t *testing.T) {
	dir := os.Getenv("T392_SCRATCH")
	if dir == "" {
		t.Skip("T392_SCRATCH unset")
	}
	path := predecessorFixture(t)
	migrate := handover.ComposeSeed(handover.Pending{
		From: "claude", To: "grok", Kind: handover.KindMigrate, TranscriptPath: path,
	})
	compact := handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	})
	if err := os.WriteFile(filepath.Join(dir, "seed-migrate.txt"), []byte(migrate), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed-compact.txt"), []byte(compact), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLooksLikeSeed(t *testing.T) {
	path := predecessorFixture(t)
	if !handover.LooksLikeSeed(handover.ComposeSeed(handover.Pending{
		From: "claude", To: "grok", TranscriptPath: path,
	})) {
		t.Fatal("migrate seed not recognised as a seed")
	}
	if !handover.LooksLikeSeed(handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	})) {
		t.Fatal("compact seed not recognised as a seed")
	}
	if handover.LooksLikeSeed("Migrate it.") {
		t.Fatal("ordinary owner line classified as a seed")
	}
}
