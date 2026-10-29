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

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

// 🎯T700: escalate API is the unattended pass channel (daemon blurter, not seat Bash).
func TestT700SupervisorEscalateAPIUsesDaemonBlurter(t *testing.T) {
	old := supervisormem.DefaultBlurterSend
	supervisormem.DefaultBlurterSend = func(subject, body, severity, key string) (string, error) {
		if subject == "" || body == "" {
			t.Fatal("missing subject/body")
		}
		if severity != "warning" {
			t.Fatalf("severity=%q", severity)
		}
		return "/hermetic/spool", nil
	}
	defer func() { supervisormem.DefaultBlurterSend = old }()

	state := t.TempDir()
	s := New("test", state)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(map[string]string{
		"subject":  "T700",
		"body":     "wake-worthy fixture",
		"severity": "warning",
	})
	resp, err := http.Post(srv.URL+"/api/supervisor/escalate", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Spool string `json:"spool"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Spool, "spool") {
		t.Fatalf("spool=%q", out.Spool)
	}
}
