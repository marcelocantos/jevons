// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

// 🎯T700: non-interactive supervisor-pass against a wake-worthy fixture raises
// owner notification through the daemon escalate path.
func TestT700SupervisorPassCLIAgainstFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/supervisor/escalate" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"spool": "/oracle/spool"})
	}))
	t.Cleanup(srv.Close)

	fixPath := filepath.Join(t.TempDir(), "fixture.json")
	fix := supervisormem.DefaultWakeFixture(time.Date(2026, 9, 20, 21, 0, 0, 0, time.UTC))
	b, err := json.Marshal(fix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "supervisor-pass")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	run := exec.Command(bin, "-daemon", srv.URL, "-fixture", fixPath)
	run.Env = os.Environ()
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	line := strings.TrimSpace(string(out))
	if !strings.HasPrefix(line, supervisormem.PassPrefixEscalated) {
		t.Fatalf("want escalated line, got %q", line)
	}
}
