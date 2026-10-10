// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/server"
)

func TestDaemonConfiguredScopedTargetRoute(t *testing.T) {
	reposRoot := t.TempDir()
	org := filepath.Join(reposRoot, "marcelocantos")
	for slug, title := range map[string]string{"jevons": "Column widths", "claudia": "Purge legacy CLI transport"} {
		dir := filepath.Join(org, slug)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte("targets:\n  T177:\n    name: "+title+"\n    status: identified\n    acceptance: [verified]\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := server.New("test", t.TempDir())
	cfg := config.Default()
	cfg.ReposRoot = reposRoot
	cfg.WorkDir = filepath.Join(org, "jevons")
	// Exercise the same setup call as main, not the test-only index setter.
	if err := configureTargetRepoIndex(s, cfg); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/frontier/target?repo=claudia&id=T177&cwd="+cfg.WorkDir, nil))
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Found        bool   `json:"found"`
		RepoIdentity string `json:"repo_identity"`
		Target       struct {
			Name       string   `json:"name"`
			Acceptance []string `json:"acceptance"`
		} `json:"target"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Target.Name != "Purge legacy CLI transport" || got.RepoIdentity != filepath.Base(reposRoot)+"/marcelocantos/claudia" || len(got.Target.Acceptance) != 1 {
		t.Fatalf("qualified response: %+v", got)
	}
}

func TestDaemonConfiguredWorkspaceFailClosed(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	s := server.New("test", t.TempDir())
	cfg := config.Default()
	cfg.ReposRoot = root
	cfg.WorkDir = elsewhere
	if err := configureTargetRepoIndex(s, cfg); err == nil {
		t.Fatal("workdir outside trusted root admitted")
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/frontier/target?repo=claudia&id=T177", nil))
	if rr.Code != 404 {
		t.Fatalf("unconfigured index: %d %s", rr.Code, rr.Body.String())
	}
}
