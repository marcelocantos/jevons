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

func TestReactDistReady(t *testing.T) {
	if ReactDistReady(t.TempDir()) {
		t.Fatal("empty dir is not a React dist")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<div id="root"></div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !ReactDistReady(dir) {
		t.Fatal("index.html must make dist ready")
	}
}

func TestRegisterReactUIRoutesServesRootAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(`<!doctype html><div id="root"></div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("window.__react=1"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	RegisterReactUIRoutes(mux, dir)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="root"`) {
		t.Fatalf("GET / must be the React document, got %q", body)
	}
	if strings.Contains(body, "DEPRECATED REFERENCE") || strings.Contains(body, "boot_sentinel.js") {
		t.Fatal("GET / must not be vanilla web/")
	}

	recA := httptest.NewRecorder()
	mux.ServeHTTP(recA, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if recA.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.js = %d", recA.Code)
	}
	if !strings.Contains(recA.Body.String(), "window.__react=1") {
		t.Fatalf("asset body: %q", recA.Body.String())
	}

	if err := os.WriteFile(filepath.Join(dir, "favicon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	recF := httptest.NewRecorder()
	mux.ServeHTTP(recF, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if recF.Code != http.StatusOK {
		t.Fatalf("GET /favicon.svg = %d", recF.Code)
	}
}

func TestRegisterProductUIRoutesDailyRequiresDist(t *testing.T) {
	mux := http.NewServeMux()
	_, err := RegisterProductUIRoutes(mux, t.TempDir(), t.TempDir(), true)
	if err == nil {
		t.Fatal("daily without ui/dist must fail closed")
	}
	if !strings.Contains(err.Error(), "T540.2") {
		t.Fatalf("error must name T540.2: %v", err)
	}
}

func TestRegisterProductUIRoutesIsolateFallsBackToVanilla(t *testing.T) {
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte(`<html>DEPRECATED REFERENCE</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ds, err := RegisterProductUIRoutes(mux, t.TempDir(), web, false)
	if err != nil {
		t.Fatal(err)
	}
	if ds == nil {
		t.Fatal("isolate without dist must disk-serve vanilla")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "DEPRECATED REFERENCE") {
		t.Fatalf("isolate fallback must be vanilla, got %q", rec.Body.String())
	}
}

func TestRegisterProductUIRoutesPrefersReactWhenDistExists(t *testing.T) {
	react := t.TempDir()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(react, "index.html"), []byte(`<div id="root"></div>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte(`<html>DEPRECATED REFERENCE</html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ds, err := RegisterProductUIRoutes(mux, react, web, true)
	if err != nil {
		t.Fatal(err)
	}
	if ds != nil {
		t.Fatal("React path must not start a vanilla DevServer")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("must serve React, got %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "DEPRECATED REFERENCE") {
		t.Fatal("must not serve vanilla when dist exists")
	}
}
