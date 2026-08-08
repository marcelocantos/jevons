// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/writconf"
)

func TestSecurityStatusAndConfinedExec(t *testing.T) {
	dir := t.TempDir()
	s := server.New("test", dir)
	a := secauditor.New()
	var delivered []string
	a.Deliverer = deliverFunc(func(text string) error {
		delivered = append(delivered, text)
		return nil
	})
	s.SetSecurityAuditor(a)
	s.SetWritExecutor(writconf.PureExecutor{}, "", false)

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// Status probe (T194 daily path shape).
	resp, err := http.Get(ts.URL + "/api/security/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st secauditor.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if !st.Ready || st.Confinement != "writ" || st.Auditor != secauditor.DefaultName {
		t.Fatalf("status: %+v", st)
	}

	// Allowed low-risk (pure path).
	allowBody, _ := json.Marshal(map[string]any{
		"argv":      []string{"curl", "https://api.x.ai/v1"},
		"net_hosts": []string{"api.x.ai"},
		"pure":      true,
	})
	ar, err := http.Post(ts.URL+"/api/security/confined-exec", "application/json", bytes.NewReader(allowBody))
	if err != nil {
		t.Fatal(err)
	}
	defer ar.Body.Close()
	var allowOut map[string]any
	_ = json.NewDecoder(ar.Body).Decode(&allowOut)
	if ar.StatusCode != 200 || allowOut["denied"] == true {
		t.Fatalf("allow: status=%d out=%v", ar.StatusCode, allowOut)
	}

	// Blocked high-risk undeclared host → 403 + auditor alert.
	denyBody, _ := json.Marshal(map[string]any{
		"argv":      []string{"curl", "https://blocked.invalid/secret"},
		"agent":     "jv-t335",
		"net_hosts": []string{"example.com"},
		"pure":      true,
	})
	dr, err := http.Post(ts.URL+"/api/security/confined-exec", "application/json", bytes.NewReader(denyBody))
	if err != nil {
		t.Fatal(err)
	}
	defer dr.Body.Close()
	var denyOut map[string]any
	_ = json.NewDecoder(dr.Body).Decode(&denyOut)
	if dr.StatusCode != http.StatusForbidden || denyOut["denied"] != true {
		t.Fatalf("deny: status=%d out=%v", dr.StatusCode, denyOut)
	}
	if len(a.Recent()) == 0 {
		t.Fatal("auditor must observe deny")
	}
	if len(delivered) == 0 {
		t.Fatal("auditor must alert overseer path")
	}
}

type deliverFunc func(string) error

func (f deliverFunc) DeliverAlert(text string) error { return f(text) }
