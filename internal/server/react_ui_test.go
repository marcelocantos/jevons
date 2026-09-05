// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/marcelocantos/jevons/ui"
)

func TestReactBundleServesStandaloneWithNavigationAndAPIPrecedence(t *testing.T) {
	// No disk dist or developer checkout is an input to the serving path.
	t.Chdir(t.TempDir())
	mux := http.NewServeMux()
	if err := RegisterProductUIRoutes(mux); err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	// Query parameters are the product deep-link contract. The arbitrary
	// path checks only HTTP SPA fallback, not that React implements that route.
	for _, route := range []string{"/", "/?agent=jevons&tab=transcript", "/navigation-fallback"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="root"`) {
			t.Fatalf("React navigation %s: status=%d body=%s", route, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "boot_sentinel") || strings.Contains(rec.Body.String(), "DEPRECATED REFERENCE") {
			t.Fatal("packaged surface contains vanilla runtime")
		}
	}
	for route, status := range map[string]int{
		"/mcp":               http.StatusNoContent,
		"/api/agents":        http.StatusOK,
		"/api/missing":       http.StatusNotFound,
		"/api":               http.StatusNotFound,
		"/ws":                http.StatusNotFound,
		"/ws/missing":        http.StatusNotFound,
		"/mcp/missing":       http.StatusNotFound,
		"/assets/missing.js": http.StatusNotFound,
		"/assets/missing":    http.StatusNotFound,
		"/missing.css":       http.StatusNotFound,
	} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Errorf("GET %s = %d, want %d", route, rec.Code, status)
		}
	}
}

func TestReactBundleEveryAssetIsServed(t *testing.T) {
	files, err := ui.Files()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := RegisterProductUIRoutes(mux); err != nil {
		t.Fatal(err)
	}
	var scripts, styles, images int
	err = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		want, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) {
			t.Errorf("packaged %s: status=%d or bytes differ", name, rec.Code)
		}
		switch {
		case strings.HasSuffix(name, ".js"):
			scripts++
		case strings.HasSuffix(name, ".css"):
			styles++
		case strings.HasSuffix(name, ".svg"):
			images++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scripts == 0 || styles == 0 || images == 0 {
		t.Fatalf("incomplete real bundle: scripts=%d styles=%d images=%d", scripts, styles, images)
	}
}

func TestReactBundleMissingReferencedAssetFailsClosed(t *testing.T) {
	for _, missing := range []string{"app.js", "app.css", "favicon.svg"} {
		t.Run(missing, func(t *testing.T) {
			files := fstest.MapFS{
				"index.html":  {Data: []byte(`<link rel="icon" href="/favicon.svg"><link rel="stylesheet" href="/app.css"><div id="root"></div><script src="/app.js"></script>`)},
				"app.js":      {Data: []byte("window.app = true")},
				"app.css":     {Data: []byte("body { color: black }")},
				"favicon.svg": {Data: []byte("<svg/>")},
			}
			delete(files, missing)
			if err := RegisterReactUIRoutes(http.NewServeMux(), files); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("missing %s must prevent serving, got %v", missing, err)
			}
		})
	}
}

// Preserve the old asset guard's comment/traversal guarantees on the new
// immutable product path. Browser-inactive tags cannot invent missing loads.
func TestReactBundleAssetReferencesFollowHTMLSemantics(t *testing.T) {
	for _, suffix := range []string{
		`<!-- <script src="/missing.js"></script> -->`,
		`<!-- unterminated <script src="/missing.js">`,
		`<script src="https://example.com/external.js"></script>`,
	} {
		files := fstest.MapFS{"index.html": {Data: []byte(`<div id="root"></div><script src='/real.js?v=2'></script>` + suffix)}, "real.js": {Data: []byte("app()")}}
		if err := RegisterReactUIRoutes(http.NewServeMux(), files); err != nil {
			t.Fatal(err)
		}
	}
	for _, src := range []string{"../private.js", "/assets/../../private.js", "/assets/%2e%2e/private.js"} {
		files := fstest.MapFS{"index.html": {Data: []byte(`<div id="root"></div><script src="` + src + `"></script>`)}}
		if err := RegisterReactUIRoutes(http.NewServeMux(), files); err == nil {
			t.Fatalf("invalid local asset path %q accepted", src)
		}
	}
}
