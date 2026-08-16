// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if migrate == "" {
		t.Fatal("migrate seed empty")
	}
	if n := handover.TokenCount(migrate); n > handover.MaxBriefTokens+200 {
		t.Errorf("migrate seed tokens=%d over brief cap", n)
	}
	low := strings.ToLower(migrate)
	for _, bad := range forbiddenReadPhrases() {
		if strings.Contains(low, bad) {
			t.Errorf("migrate seed still assigns a transcript walk (%q):\n%s", bad, migrate)
		}
	}
	if strings.Contains(low, "restarted") {
		t.Errorf("migrate seed still calls the switch a restart:\n%s", migrate)
	}
	if !strings.Contains(low, "provider switch") || !strings.Contains(migrate, "claude") || !strings.Contains(migrate, "grok") {
		t.Errorf("migrate seed must name the provider switch:\n%s", migrate)
	}
	if !strings.Contains(low, "hard stop") && !strings.Contains(low, "opus") {
		t.Errorf("migrate brief lost predecessor content:\n%s", migrate)
	}

	if got := handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	}); got != "" {
		t.Fatalf("same-provider seed must be empty, got:\n%s", got)
	}
}

func TestSameProviderNeverUsesMigrateStory(t *testing.T) {
	path := predecessorFixture(t)
	if seed := handover.SeedMessage("grok", "grok", path); seed != "" {
		t.Fatalf("same-provider SeedMessage must be empty, got:\n%s", seed)
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
		t.Fatalf("same-provider owner paint = %q, want empty", got)
	}
	if seed := compact.Seed(); seed != "" {
		t.Fatalf("same-provider seed must be empty, got:\n%s", seed)
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
	seed := handover.ComposeSeed(handover.Pending{
		From: "claude", To: "grok", Kind: handover.KindMigrate, TranscriptPath: path,
	})
	if seed == "" {
		t.Fatal("migrate seed empty on fat predecessor")
	}
	if handover.TokenCount(seed) >= handover.TokenCount(assigned) {
		t.Fatalf("shipped seed (%d) is not smaller than a transcript walk (%d)",
			handover.TokenCount(seed), handover.TokenCount(assigned))
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
	if err := os.WriteFile(filepath.Join(dir, "seed-migrate.txt"), []byte(migrate), 0o644); err != nil {
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
	if handover.ComposeSeed(handover.Pending{
		From: "grok", To: "grok", Kind: handover.KindCompact, TranscriptPath: path,
	}) != "" {
		t.Fatal("same-provider compose must not look like a migrate seed")
	}
	if handover.LooksLikeSeed("Migrate it.") {
		t.Fatal("ordinary owner line classified as a seed")
	}
}
