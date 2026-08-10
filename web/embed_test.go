// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptSrcRE matches <script src="…"> (single or double quotes).
// CDN / absolute URLs are filtered after capture.
var scriptSrcRE = regexp.MustCompile(`(?i)<script[^>]+src=["']([^"']+)["']`)

// commentRE is a second, independent implementation of comment removal —
// product code has its own byte scanner, and an oracle that shares the parser
// it checks proves nothing about the parser. A tag inside <!-- … --> is not a
// load, so it is neither embedded nor expected on disk.
var commentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

func uncommented(raw []byte) []byte {
	return commentRE.ReplaceAll(raw, nil)
}

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
	for _, m := range scriptSrcRE.FindAllSubmatch(uncommented(raw), -1) {
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

// TestWorkingTreeScriptsCoverIndex closes the other half of the 🎯T292 blind
// spot that let scripts/pending_turns.js slip.
//
// The test above reads the EMBEDDED index.html against the EMBEDDED FS, so it
// only ever describes a released binary. The daily daemon runs dev mode and
// serves web/ from disk, which is why a module could be referenced by
// index.html and absent from the path the owner actually loads while that
// test stayed green. Both paths now have to hold: embedded (above) and
// on-disk (here).
//
// This is not the same check as internal/server's disk guard. That one proves
// the SERVING CODE reacts to an incomplete tree, using synthetic trees; this
// proves THIS REPOSITORY's tree is complete, which no synthetic fixture can
// say. index.html is read from disk deliberately — reading the embedded copy
// would make a stale build look healthy.
func TestWorkingTreeScriptsCoverIndex(t *testing.T) {
	raw, err := os.ReadFile("index.html")
	if err != nil {
		t.Fatalf("read web/index.html from disk: %v", err)
	}

	var missing []string
	var local int
	for _, m := range scriptSrcRE.FindAllSubmatch(uncommented(raw), -1) {
		src := string(m[1])
		if !strings.HasPrefix(src, "scripts/") || strings.Contains(src, "..") {
			continue
		}
		local++
		if _, err := os.Stat(filepath.FromSlash(src)); err != nil {
			missing = append(missing, src)
		}
	}

	if local == 0 {
		t.Fatal("on-disk index.html has no local scripts/… tags — parse or tree broke")
	}
	if len(missing) > 0 {
		t.Fatalf("index.html loads modules absent from the serving tree — the daily "+
			"dev-mode daemon would 404 these:\n  %s", strings.Join(missing, "\n  "))
	}
}
