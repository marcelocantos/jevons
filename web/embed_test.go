// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// scriptSrcRE matches <script src="…"> (single or double quotes).
// CDN / absolute URLs are filtered after capture.
var scriptSrcRE = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)

// TestEmbeddedScriptsCoverIndex is the 🎯T292 drift ratchet: every local
// scripts/… module referenced by index.html must exist in the embedded FS.
// Adding a <script src="scripts/…"> without a matching //go:embed entry
// fails this test (the file is absent from FS after compile).
func TestEmbeddedScriptsCoverIndex(t *testing.T) {
	raw, err := FS.ReadFile("index.html")
	if err != nil {
		t.Fatalf("embedded index.html missing: %v", err)
	}

	var missing []string
	var local []string
	for _, m := range scriptSrcRE.FindAllSubmatch(raw, -1) {
		src := string(m[1])
		if !strings.HasPrefix(src, "scripts/") {
			continue // CDN / external
		}
		if strings.Contains(src, "..") {
			t.Errorf("refusing path traversal script src %q", src)
			continue
		}
		local = append(local, src)
		if _, err := FS.Open(src); err != nil {
			missing = append(missing, src)
		}
	}

	if len(local) == 0 {
		t.Fatal("index.html has no local scripts/… tags — parse or embed broke")
	}
	if len(missing) > 0 {
		t.Fatalf("index.html loads scripts not in //go:embed (add to web/embed.go):\n  %s",
			strings.Join(missing, "\n  "))
	}

	// Sanity: scripts/ subtree exists and is non-empty.
	entries, err := fs.ReadDir(FS, "scripts")
	if err != nil {
		t.Fatalf("embedded scripts/ missing: %v", err)
	}
	if len(entries) < len(local) {
		t.Errorf("embedded scripts/ has %d files but index loads %d modules",
			len(entries), len(local))
	}
}
