// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T551: marked send probes must never re-fire as owner-intent-resume after bounce.

func TestExtractOpenOwnerIntentIgnoresSendProbeOnly(t *testing.T) {
	t.Parallel()
	cases := []string{
		"http-send-probe-do-not-treat-as-owner-intent",
		"ping-send-check",
		"ws-send-check",
		"send-probe-mux-echo",
	}
	for _, probe := range cases {
		got := ExtractOpenOwnerIntent([]OwnerIntentTurn{{Text: probe}})
		if got.Recoverable() {
			t.Fatalf("probe %q must not recover, got text=%q", probe, got.Text)
		}
		if got.Residual != ResidualNotSubstantive {
			t.Fatalf("probe %q: residual=%q want %q", probe, got.Residual, ResidualNotSubstantive)
		}
	}
}

func TestExtractOpenOwnerIntentSendProbeAnsweredStillNotSubstantive(t *testing.T) {
	t.Parallel()
	// Live incident: probe answered with "Probe only — no action" still must not
	// resume — T344 closes substantive turns; probes never enter the set.
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "http-send-probe-do-not-treat-as-owner-intent"},
		{Role: "assistant", Text: "Received. Probe only — no action."},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("answered probe must not recover, text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualNotSubstantive {
		t.Fatalf("residual=%q want %q", got.Residual, ResidualNotSubstantive)
	}
}

func TestExtractOpenOwnerIntentStillRecoversAfterSendProbe(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{Role: "user", Text: "ping-send-check", TS: time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)},
		{Role: "assistant", Text: "Received. Probe only — no action."},
		{
			Role: "user",
			Text: "Please implement open intent send-probe filter for restart resume",
			TS:   time.Date(2026, 8, 26, 10, 5, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("real implement after probe must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "send-probe filter") {
		t.Fatalf("got %q", got.Text)
	}
}

func TestLoadOpenOwnerIntentSendProbeChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-26T12:00:00Z","message":{"role":"user","content":"http-send-probe-do-not-treat-as-owner-intent"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Received. Probe only — no action."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "jevons.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("probe-only chatlog must not resume: %+v", got)
	}
	if got.Residual != ResidualNotSubstantive {
		t.Fatalf("residual=%q want %q", got.Residual, ResidualNotSubstantive)
	}
}

func TestIsOpenIntentSendProbe(t *testing.T) {
	t.Parallel()
	probes := []string{
		"http-send-probe-do-not-treat-as-owner-intent",
		"ping-send-check",
		" mux-send-check ",
		"send-probe-root-echo",
	}
	for _, p := range probes {
		if !isOpenIntentSendProbe(p) {
			t.Errorf("want probe: %q", p)
		}
	}
	notProbes := []string{
		"",
		"Please implement the send path fix",
		"Why does ping fail on send?",
		"Probe the API and file a target if broken",
		"Please implement open intent send-probe filter for restart resume",
	}
	for _, p := range notProbes {
		if isOpenIntentSendProbe(p) {
			t.Errorf("want NOT probe: %q", p)
		}
	}
}
