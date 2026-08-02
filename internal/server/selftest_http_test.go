// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/selftest"
)

func TestSelfTestRunHTTPAll(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "1", Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "2", Provider: "grok", Parent: "jevons"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}

	s := New("test-v", t.TempDir())
	s.SetOverseerName("jevons")
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body := []byte(`{"pack":"all","site":"ci"}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/self_test/run", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Same-origin Host so rejectCrossSite allows POST.
	req.Host = req.URL.Host

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var result selftest.RunResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Reports) != 3 {
		t.Fatalf("reports=%d", len(result.Reports))
	}
	for _, r := range result.Reports {
		if r.Grade != selftest.GradePass {
			t.Fatalf("pack %s grade=%s narrative=%s ms=%+v", r.Pack, r.Grade, r.Narrative, r.Measurements)
		}
		if r.SchemaVersion != selftest.SchemaVersion {
			t.Fatalf("schema")
		}
	}
}

func TestSelfTestPacksList(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/self_test/packs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var packs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&packs); err != nil {
		t.Fatal(err)
	}
	if len(packs) < 3 {
		t.Fatalf("packs=%+v", packs)
	}
}

func TestSelfTestSinglePack(t *testing.T) {
	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body := []byte(`{"pack":"composer-growth-L2","site":"ci"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/self_test/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Host = req.URL.Host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result selftest.RunResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Single == nil || result.Single.Grade != selftest.GradePass {
		t.Fatalf("%+v", result)
	}
}
