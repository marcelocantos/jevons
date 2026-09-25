// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeFromSpoolIncludesGrokAndCursor(t *testing.T) {
	for _, id := range []string{"cursor", "anthropic", "openai-codex", "xai-oauth"} {
		if !ResumeFromSpool(id, false) {
			t.Fatalf("%s must resume from spool", id)
		}
		if !SidecarProvider(id) {
			t.Fatalf("SidecarProvider(%s)", id)
		}
	}
	if ResumeFromSpool("grok", false) || ResumeFromSpool("claude", false) || ResumeFromSpool("codex", false) {
		t.Fatal("grok/claude/codex CLI ids resume from vendor JSONL until reminted")
	}
	if !ResumeFromSpool("grok", true) {
		t.Fatal("a reminted grok seat reads the spool")
	}
}

func TestAsJSONLAndEnsureView(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"po","type":"text","text":"hi","model":"grok"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := EnsureView(dir, "po")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"role":"assistant"`) || !strings.Contains(string(body), "hi") {
		t.Fatalf("view = %s", body)
	}
}

func TestLatestPathNamesDatedSpoolNotVendor(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-24.log"), []byte(
		`{"ts":"2026-09-24T00:00:00.000Z","seat":"po","type":"text","text":"old"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "events-2026-09-25.log")
	if err := os.WriteFile(live, []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"po","type":"text","text":"new"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LatestPath(dir, "po"); got != live {
		t.Fatalf("LatestPath = %q, want %q", got, live)
	}
	if got := LatestPath(dir, "missing"); got != "" {
		t.Fatalf("missing seat path = %q", got)
	}
}

func TestAsJSONLIsChatWireInput(t *testing.T) {
	recs, err := ReadSeat(filepath.Dir(mustWriteSpool(t,
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"po","type":"text","text":"hello from sidecar"}`+"\n",
	)), "po")
	if err != nil {
		t.Fatal(err)
	}
	body := string(AsJSONL(recs))
	if !strings.Contains(body, "hello from sidecar") || !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("AsJSONL is not chat-wire input: %s", body)
	}
}

func mustWriteSpool(t *testing.T, line string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events-2026-09-25.log")
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
