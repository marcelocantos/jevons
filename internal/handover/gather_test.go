// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/handover"
)

func TestGatherBriefDeadOutgoingDistills(t *testing.T) {
	path := predecessorFixture(t)
	got := handover.GatherBrief(handover.Pending{
		From: "claude", To: "grok", TranscriptPath: path,
	}, handover.GatherHooks{})
	if got.Source != handover.SourceDistill {
		t.Fatalf("source=%s want distill", got.Source)
	}
	if got.Text == "" || handover.DistillTooThin(got.Text) {
		t.Fatalf("dead-outgoing Distill empty/thin:\n%s", got.Text)
	}
	if got.CompactSessionID != "" {
		t.Fatalf("distill path allocated a compact session %q", got.CompactSessionID)
	}
}

func TestGatherBriefLiveOutgoingUsesSelfBrief(t *testing.T) {
	path := predecessorFixture(t)
	got := handover.GatherBrief(handover.Pending{
		From: "claude", To: "grok", TranscriptPath: path,
	}, handover.GatherHooks{
		SelfBrief: func(handover.Pending) (string, error) {
			return "from memory: hold the hard-stop; do not start work", nil
		},
	})
	if got.Source != handover.SourceSelf {
		t.Fatalf("source=%s want self-brief", got.Source)
	}
	if !strings.Contains(got.Text, "from memory") {
		t.Fatalf("self-brief lost: %q", got.Text)
	}
}

func TestGatherBriefTimeoutFallsThroughToDistill(t *testing.T) {
	path := predecessorFixture(t)
	got := handover.GatherBrief(handover.Pending{
		From: "claude", To: "grok", TranscriptPath: path,
	}, handover.GatherHooks{
		SelfBrief: func(handover.Pending) (string, error) {
			return "", errors.New("timeout")
		},
	})
	if got.Source != handover.SourceDistill {
		t.Fatalf("timeout source=%s want distill", got.Source)
	}
	if got.Text == "" {
		t.Fatal("timeout swallowed Distill")
	}
}

func TestGatherBriefThinUsesThrowawayCompact(t *testing.T) {
	dir := t.TempDir()
	thin := filepath.Join(dir, "thin.jsonl")
	// Empty file: Distill returns "" → too thin.
	if err := os.WriteFile(thin, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := handover.GatherBrief(handover.Pending{
		From: "claude", To: "grok", TranscriptPath: thin,
	}, handover.GatherHooks{
		Compact: func(handover.Pending) (string, string, error) {
			return "compact-sess-aaa", "in flight: T999 still open", nil
		},
	})
	if got.Source != handover.SourceCompact {
		t.Fatalf("source=%s want throwaway-compact", got.Source)
	}
	if got.CompactSessionID != "compact-sess-aaa" {
		t.Fatalf("compact session=%q", got.CompactSessionID)
	}
	if !strings.Contains(got.Text, "T999") {
		t.Fatalf("compact brief lost: %q", got.Text)
	}
	if handover.PlanThrowawayCompact(handover.Distill(thin), false) != true {
		t.Fatal("PlanThrowawayCompact false on empty predecessor")
	}
}

func TestGatherBriefSameProviderIsEmpty(t *testing.T) {
	got := handover.GatherBrief(handover.Pending{
		From: "grok", To: "grok", TranscriptPath: predecessorFixture(t),
	}, handover.GatherHooks{
		SelfBrief: func(handover.Pending) (string, error) {
			t.Fatal("self-brief must not run on a same-provider pending")
			return "", nil
		},
	})
	if got.Text != "" || got.Source != "" {
		t.Fatalf("same-provider brief = %+v", got)
	}
}

func TestComposeSeedUsesPersistedBriefNotPath(t *testing.T) {
	seed := handover.ComposeSeed(handover.Pending{
		From: "claude", To: "grok",
		Brief:          "in flight: hold the hard-stop",
		BriefSource:    string(handover.SourceSelf),
		TranscriptPath: "/secret/predecessor.jsonl",
	})
	if seed == "" {
		t.Fatal("empty seed")
	}
	if strings.Contains(seed, "/secret/predecessor.jsonl") {
		t.Fatalf("work seed cited the predecessor path:\n%s", seed)
	}
	if strings.Contains(strings.ToLower(seed), "start at the end") {
		t.Fatalf("work seed assigned a walk:\n%s", seed)
	}
	if !strings.Contains(seed, "in flight: hold the hard-stop") {
		t.Fatalf("work seed lost the brief:\n%s", seed)
	}
	if !strings.Contains(strings.ToLower(seed), "what was in flight") {
		t.Fatalf("seed is not brief-shaped:\n%s", seed)
	}
}

func TestDistillTooThin(t *testing.T) {
	if !handover.DistillTooThin("") {
		t.Fatal("empty not thin")
	}
	if handover.DistillTooThin(handover.Distill(predecessorFixture(t))) {
		t.Fatal("predecessor fixture Distill is thin — two-id path would fire on every migrate")
	}
}


