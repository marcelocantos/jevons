// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeFromSpoolExcludesGrokWithoutOMP(t *testing.T) {
	if ResumeFromSpool("grok", false) {
		t.Fatal("grok without OMP still uses the Grok Build store")
	}
	if !ResumeFromSpool("grok", true) {
		t.Fatal("OMP grok must resume from spool")
	}
	if !ResumeFromSpool("cursor", false) {
		t.Fatal("cursor resumes from spool")
	}
	if !SidecarProvider("grok") || !SidecarProvider("anthropic") {
		t.Fatal("SidecarProvider")
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
