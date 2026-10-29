// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestTrustReplacePersonaNotes(t *testing.T) {
	cfg := "workdir: /tmp/x\npersona_notes: |\n  Do not spawn workers.\n  Short answers only.\n"
	out := trustReplacePersonaNotes(cfg)
	if strings.Contains(out, "Do not spawn workers") {
		t.Fatal("default journey persona must be replaced for T1183")
	}
	if !strings.Contains(out, "jevons-po") {
		t.Fatal("T1183 persona must route to jevons-po")
	}
}

func TestAssertTrustOwnerChat(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	prompt := "Add Double to pkg/calc"
	ok := map[string]any{
		"turns": []any{
			map[string]any{"role": "user", "raw": map[string]any{"message": map[string]any{
				"role": "user", "content": prompt,
			}}},
			map[string]any{"role": "assistant", "raw": map[string]any{"message": map[string]any{
				"role": "assistant", "content": "Landed at " + sha + ".",
			}}},
		},
	}
	if err := assertTrustOwnerChat(ok, prompt, sha, false); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{
		"turns": []any{
			map[string]any{"role": "user", "raw": map[string]any{"message": map[string]any{
				"role": "user", "content": prompt,
			}}},
			map[string]any{"role": "assistant", "raw": map[string]any{"message": map[string]any{
				"role": "assistant", "content": "No action needed.",
			}}},
		},
	}
	if err := assertTrustOwnerChat(bad, prompt, sha, false); err == nil {
		t.Fatal("expected routine reply to fail")
	}
	interim := map[string]any{
		"turns": []any{
			map[string]any{"role": "user", "raw": map[string]any{"message": map[string]any{
				"role": "user", "content": prompt,
			}}},
			map[string]any{"role": "assistant", "raw": map[string]any{"message": map[string]any{
				"role": "assistant", "content": "Routing this to jevons-po now.",
			}}},
			map[string]any{"role": "assistant", "raw": map[string]any{"message": map[string]any{
				"role": "assistant", "content": "Landed at " + sha + ".",
			}}},
		},
	}
	if err := assertTrustOwnerChat(interim, prompt, sha, false); err == nil {
		t.Fatal("expected interim owner chat before sealed SHA reply to fail")
	}
	if err := assertTrustOwnerChat(interim, prompt, sha, true); err != nil {
		t.Fatal("mux-sealed path should not fail on transcript interim")
	}
	journalOnly := map[string]any{
		"turns": []any{
			map[string]any{"role": "user", "raw": map[string]any{"message": map[string]any{
				"role": "user", "content": prompt,
			}}},
		},
		"journal": "tool traffic\nLanded at " + sha + " on master.\n",
	}
	if err := assertTrustOwnerChat(journalOnly, prompt, sha, false); err != nil {
		t.Fatal(err)
	}
}
