// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/marcelocantos/jevons/internal/buildident"
)

// 🎯T993: every /ws/mux connection opens with a hello that names the serving
// build, so a reconnecting cockpit can compare it with the build it loaded.
func TestT993MuxHelloCarriesBuildID(t *testing.T) {
	s := New("test", t.TempDir())
	s.buildID = "0123456789abcdef"
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/mux", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	var env struct {
		V    int    `json:"v"`
		Ch   string `json:"ch"`
		T    string `json:"t"`
		Body struct {
			ConnID string `json:"conn_id"`
			Build  string `json:"build"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("hello %s: %v", data, err)
	}
	if env.T != "hello" || env.Ch != "" || env.V != 1 {
		t.Fatalf("first frame is not the hello: %s", data)
	}
	if env.Body.ConnID == "" {
		t.Fatalf("hello lost conn_id: %s", data)
	}
	if env.Body.Build != "0123456789abcdef" {
		t.Fatalf("hello build=%q want the server's build id: %s", env.Body.Build, data)
	}
}

// A server that cannot name its binary says so with "" rather than
// inventing an id that would differ every restart.
func TestT993MuxHelloUnknownBuildIsEmptyNotInvented(t *testing.T) {
	s := New("test", t.TempDir())
	s.buildID = ""
	first := s.muxHelloBody()
	second := s.muxHelloBody()
	if first["build"] != "" || second["build"] != "" {
		t.Fatalf("unknown build must be \"\": %v / %v", first, second)
	}
	if first["conn_id"] == second["conn_id"] {
		t.Fatalf("conn_id must still be per-connection: %v", first["conn_id"])
	}
}

// New() names the binary the same way restart-jevonsd's identity does: the
// process's own executable, so a plain restart of the same bytes keeps the id.
func TestT993ServerBuildIDIsTheRunningBinary(t *testing.T) {
	s := New("test", t.TempDir())
	if s.buildID == "" {
		t.Fatal("New() left buildID empty for a readable test executable")
	}
	if s.buildID != buildident.Binary() {
		t.Fatalf("buildID=%q want buildident.Binary()=%q", s.buildID, buildident.Binary())
	}
	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var health map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("health %s: %v", rec.Body.String(), err)
	}
	if health["build"] != s.buildID {
		t.Fatalf("/health build=%v want %q", health["build"], s.buildID)
	}
}

// The served document carries the same id under <meta name="jevons-build">,
// which is the build the cockpit treats itself as loaded with.
func TestT993IndexCarriesBuildMeta(t *testing.T) {
	files := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html>\n<html>\n  <head>\n    <title>t</title>\n  </head>\n  <body><div id=\"root\"></div></body>\n</html>\n")},
	}
	mux := http.NewServeMux()
	if err := registerReactUIRoutes(mux, files, "feedfacecafebeef"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	want := `<head><meta name="jevons-build" content="feedfacecafebeef">`
	if rec.Code != http.StatusOK || !strings.Contains(body, want) {
		t.Fatalf("status=%d body=%s\nwant to contain %s", rec.Code, body, want)
	}
	if !strings.Contains(body, `id="root"`) {
		t.Fatalf("stamping lost the document: %s", body)
	}
	// The SPA fallback document is stamped too: a deep link loads the same build.
	req = httptest.NewRequest(http.MethodGet, "/some/deep/link", nil)
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("fallback status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Unknown build: the document is served unchanged, no meta to mislead.
	mux = http.NewServeMux()
	if err := registerReactUIRoutes(mux, files, ""); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), BuildMetaName) {
		t.Fatalf("unknown build must not stamp a meta: %s", rec.Body.String())
	}
}

// The real embedded bundle has a <head> to stamp, so the product document
// carries the id, not only the fixture.
func TestT993EmbeddedBundleIndexIsStamped(t *testing.T) {
	t.Chdir(t.TempDir())
	mux := http.NewServeMux()
	if err := RegisterProductUIRoutes(mux); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	want := `<meta name="jevons-build" content="` + buildident.Binary() + `">`
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("embedded index lacks %s:\n%s", want, rec.Body.String())
	}
}
