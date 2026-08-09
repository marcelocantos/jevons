// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 🎯T374 — the daily cockpit is served from disk (dev mode), so the
// compile-time embed ratchet (🎯T292 TestEmbeddedScriptsCoverIndex) never
// covers the path the owner actually loads. A <script src="scripts/x.js">
// whose file is absent from the serving tree used to 404 quietly; the
// resulting undefined global threw at inline-script top level and aborted
// the rest of index.html, leaving every later `let` in TDZ and producing
// recurring window.onerror on each fleet poll. These tests assert the
// serving path makes that condition LOUD instead.
//
// The banner marker is inlined rather than imported so the test states the
// contract independently of the implementation.
const assetErrorMarker = "data-jevons-asset-error"

// testScriptSrcRE is deliberately a second, independent implementation of
// the script-tag scan (product code has its own) — an oracle that shares
// the parser it checks proves nothing about the parser.
var testScriptSrcRE = regexp.MustCompile(`(?i)<script[^>]+src=["'](scripts/[^"']+)["']`)

// serveIndexFromDir registers the disk-mode UI routes over dir and returns
// the body of GET /.
func serveIndexFromDir(t *testing.T, dir string) (int, string) {
	t.Helper()
	mux := http.NewServeMux()
	if ds := RegisterUIRoutes(mux, dir); ds == nil {
		t.Fatalf("RegisterUIRoutes(%q) chose embedded mode; disk mode required", dir)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code, rec.Body.String()
}

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDiskIndexFlagsMissingScript is the core 🎯T374 guard: a script tag with
// no file behind it on the serving path must produce an owner-visible marker
// naming the module, not a quiet 404.
func TestDiskIndexFlagsMissingScript(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html": `<!doctype html><html><head>` +
			`<script src="scripts/present.js"></script>` +
			`<script src="scripts/gone.js"></script>` +
			`</head><body><div id="app"></div></body></html>`,
		"scripts/present.js": "// present\n",
	})

	code, body := serveIndexFromDir(t, dir)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200 (must still serve, with recovery)", code)
	}
	if !strings.Contains(body, assetErrorMarker) {
		t.Errorf("missing scripts/gone.js served silently: no %q in body", assetErrorMarker)
	}
	if !strings.Contains(body, "scripts/gone.js") {
		t.Errorf("body does not name the missing module scripts/gone.js")
	}
	if strings.Contains(body, "scripts/present.js\"") && strings.Count(body, "scripts/present.js") > 1 {
		t.Errorf("present module reported as missing; body mentions it %d times",
			strings.Count(body, "scripts/present.js"))
	}
	if !strings.Contains(body, `id="app"`) {
		t.Errorf("original document body was dropped")
	}
}

// TestDiskIndexCleanTreeHasNoBanner keeps the guard from crying wolf: a
// self-consistent tree must be served untouched.
func TestDiskIndexCleanTreeHasNoBanner(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"index.html": `<!doctype html><html><head>` +
			`<script src="scripts/a.js"></script>` +
			`<script src="https://cdn.example/x.js"></script>` +
			`</head><body>ok</body></html>`,
		"scripts/a.js": "// a\n",
	})

	code, body := serveIndexFromDir(t, dir)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if strings.Contains(body, assetErrorMarker) {
		t.Errorf("clean tree flagged as broken:\n%s", body)
	}
	if !strings.Contains(body, "<body>ok</body>") {
		t.Errorf("clean tree body was rewritten: %q", body)
	}
}

// TestDiskIndexReproducesPendingTurnsFault replays the actual production
// fault against the real web/index.html: every module it loads is present
// except scripts/pending_turns.js — the exact tree the daily daemon served
// while the cockpit error cascade was live.
func TestDiskIndexReproducesPendingTurnsFault(t *testing.T) {
	const dropped = "scripts/pending_turns.js"

	realIndex, err := os.ReadFile(filepath.Join("..", "..", "web", "index.html"))
	if err != nil {
		t.Fatalf("read repo web/index.html: %v", err)
	}
	refs := testScriptSrcRE.FindAllSubmatch(realIndex, -1)
	if len(refs) == 0 {
		t.Fatal("no local script tags found in web/index.html — scan broke")
	}

	dir := t.TempDir()
	files := map[string]string{"index.html": string(realIndex)}
	found := false
	for _, m := range refs {
		src := string(m[1])
		if src == dropped {
			found = true
			continue // the historically missing module
		}
		files[src] = "// stub\n"
	}
	if !found {
		t.Skipf("web/index.html no longer loads %s; fault no longer reproducible", dropped)
	}
	writeTree(t, dir, files)

	code, body := serveIndexFromDir(t, dir)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(body, assetErrorMarker) {
		t.Fatalf("the production fault (missing %s on the serving path) is still silent", dropped)
	}
	if !strings.Contains(body, dropped) {
		t.Errorf("banner does not name %s", dropped)
	}
}
