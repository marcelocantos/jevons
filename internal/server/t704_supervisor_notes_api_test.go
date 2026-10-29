// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 🎯T704: supervisor memory is readable and appendable over the served path
// without touching ~/.jevons from the caller process.
func TestT704SupervisorNotesAPIRoundTrip(t *testing.T) {
	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/supervisor/notes")
	if err != nil {
		t.Fatalf("GET notes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET status=%d body=%s", resp.StatusCode, body)
	}
	var empty supervisorNotesResponse
	if err := json.NewDecoder(resp.Body).Decode(&empty); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if empty.Content != "" {
		t.Fatalf("missing file should read empty, got %q", empty.Content)
	}

	block := "### 2026-10-22 07:30\nT704 hermetic API append."
	postBody, _ := json.Marshal(supervisorRollingRequest{Block: block})
	post, err := http.Post(srv.URL+"/api/supervisor/notes/rolling", "application/json", bytes.NewReader(postBody))
	if err != nil {
		t.Fatalf("POST rolling: %v", err)
	}
	defer post.Body.Close()
	if post.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(post.Body)
		t.Fatalf("POST status=%d body=%s", post.StatusCode, body)
	}

	resp2, err := http.Get(srv.URL + "/api/supervisor/notes")
	if err != nil {
		t.Fatalf("GET after append: %v", err)
	}
	defer resp2.Body.Close()
	var got supervisorNotesResponse
	if err := json.NewDecoder(resp2.Body).Decode(&got); err != nil {
		t.Fatalf("decode GET2: %v", err)
	}
	if !strings.Contains(got.Content, block) {
		t.Fatalf("content missing block:\n%s", got.Content)
	}
	if got.RollingLines <= 0 {
		t.Fatalf("rolling_lines=%d", got.RollingLines)
	}
}
