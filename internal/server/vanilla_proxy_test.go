// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVanillaReferenceMuxServesWebAndProxiesAPI(t *testing.T) {
	dir := t.TempDir()
	index := "<!doctype html><!-- DEPRECATED REFERENCE --><title>vanilla</title>"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/frontier" {
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	h := VanillaReferenceMux(dir, u)
	srv := httptest.NewServer(h)
	defer srv.Close()

	doc, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Body.Close()
	body, _ := io.ReadAll(doc.Body)
	if doc.StatusCode != 200 || !strings.Contains(string(body), "DEPRECATED REFERENCE") {
		t.Fatalf("GET / = %d %s", doc.StatusCode, body)
	}

	api, err := http.Get(srv.URL + "/api/frontier")
	if err != nil {
		t.Fatal(err)
	}
	defer api.Body.Close()
	got, _ := io.ReadAll(api.Body)
	if api.StatusCode != 200 || string(got) != `{"ok":true}` {
		t.Fatalf("GET /api/frontier = %d %s", api.StatusCode, got)
	}
}
