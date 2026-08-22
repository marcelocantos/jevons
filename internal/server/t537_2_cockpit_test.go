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

// 🎯T537.2: when ui/dist exists it is GET /. Vanilla is not the product document.

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
	rootRec := httptest.NewRecorder()
	mux.ServeHTTP(rootRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rootRec.Body.String(), "VANILLA_COCKPIT") {
		t.Fatalf("GET / must be vanilla for screenshots, got:\n%s", rootRec.Body.String())
	}
	appRec := httptest.NewRecorder()
	mux.ServeHTTP(appRec, httptest.NewRequest(http.MethodGet, "/app/", nil))
	if appRec.Code != http.StatusOK {
		t.Fatalf("/app/ status=%d", appRec.Code)
	}
	body := appRec.Body.String()
	if strings.Contains(body, "VANILLA_COCKPIT") {
		t.Fatalf("GET /app/ served vanilla:\n%s", body)
	}
	if !strings.Contains(body, `id="root"`) || !strings.Contains(body, "REACT_COCKPIT") {
		t.Fatalf("GET /app/ is not the React cockpit:\n%s", body)
	}
}
