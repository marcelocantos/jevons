// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Journey-or-exception (🎯T537.2): no new Universe-B journey this slice.
// Machine check: GET / is the React document when ui/dist exists. Live
// owner-chat soak is the daily :13705 probe after restart (T505 cutover).

func TestT537_2ReactDistBeatsVanillaIndex(t *testing.T) {
	root := t.TempDir()
	webDir := filepath.Join(root, "web")
	dist := filepath.Join(root, "ui", "dist")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"),
		[]byte(`<!doctype html><title>VANILLA_COCKPIT</title>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"),
		[]byte(`<!doctype html><div id="root">REACT_COCKPIT</div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if ds := RegisterUIRoutes(mux, webDir); ds == nil {
		t.Fatal("expected disk SPA server")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "VANILLA_COCKPIT") {
		t.Fatalf("GET / served vanilla index.html:\n%s", body)
	}
	if !strings.Contains(body, `id="root"`) || !strings.Contains(body, "REACT_COCKPIT") {
		t.Fatalf("GET / is not the React cockpit:\n%s", body)
	}
}
