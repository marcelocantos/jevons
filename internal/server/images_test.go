// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageUploadAndGet(t *testing.T) {
	dir := t.TempDir()
	s := New("test", dir)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	// Minimal PNG header-ish bytes (not a full PNG; storage is opaque).
	pngish := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03}
	if _, err := part.Write(pngish); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/images", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	// Same-origin style so rejectCrossSite does not block.
	req.Header.Set("Origin", "http://localhost")
	req.Host = "localhost"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("upload status %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	id, _ := resp["id"].(string)
	marker, _ := resp["marker"].(string)
	if id == "" || !strings.Contains(marker, id) {
		t.Fatalf("bad response: %+v", resp)
	}
	// File on disk under stateDir/images
	entries, err := os.ReadDir(filepath.Join(dir, "images"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("images dir: %v entries=%v", err, entries)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/images/"+id, nil)
	gr := httptest.NewRecorder()
	mux.ServeHTTP(gr, get)
	if gr.Code != http.StatusOK {
		t.Fatalf("get status %d", gr.Code)
	}
	got, _ := io.ReadAll(gr.Body)
	if !bytes.Equal(got, pngish) {
		t.Fatalf("round-trip bytes mismatch: %v vs %v", got, pngish)
	}
	if ct := gr.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type %q", ct)
	}
}
