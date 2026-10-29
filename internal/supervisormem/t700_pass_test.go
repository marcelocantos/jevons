// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervisormem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestT700HealthyPassIsQuietNotMuzzled(t *testing.T) {
	out := FinishPass(false, "", nil)
	if out.Line != PassLineQuietHealthy {
		t.Fatalf("line=%q", out.Line)
	}
	if out.Muzzled || out.Escalated {
		t.Fatalf("outcome=%+v", out)
	}
}

func TestT700MuzzledPassIsNotQuiet(t *testing.T) {
	out := FinishPass(true, "", errString("blurter requires approval"))
	if !out.Muzzled {
		t.Fatalf("outcome=%+v", out)
	}
	if strings.HasPrefix(out.Line, "quiet:") {
		t.Fatalf("muzzled must not look quiet: %q", out.Line)
	}
	if !strings.HasPrefix(out.Line, PassPrefixMuzzled) {
		t.Fatalf("line=%q", out.Line)
	}
}

func TestT700EscalatedPassNamesSpool(t *testing.T) {
	out := FinishPass(true, "/tmp/spool/abc", nil)
	if !out.Escalated || out.Muzzled {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.Contains(out.Line, PassPrefixEscalated) || !strings.Contains(out.Line, "/tmp/spool/abc") {
		t.Fatalf("line=%q", out.Line)
	}
}

// 🎯T700 acceptance: unattended pass against a wake-worthy fixture reaches the
// owner through daemon blurter (or names the gap loudly).
func TestT700UnattendedPassEscalatesViaDaemonHTTP(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/supervisor/escalate" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Subject == "" || req.Body == "" {
			http.Error(w, "subject and body required", http.StatusBadRequest)
			return
		}
		posted = true
		_ = json.NewEncoder(w).Encode(map[string]string{"spool": "/fixture/spool"})
	}))
	t.Cleanup(srv.Close)

	fix := DefaultWakeFixture(time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC))
	out, err := RunUnattendedPass(srv.URL, fix, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("daemon escalate endpoint was not called")
	}
	if !out.Escalated || out.Muzzled {
		t.Fatalf("outcome=%+v", out)
	}
	if !strings.HasPrefix(out.Line, PassPrefixEscalated) {
		t.Fatalf("line=%q", out.Line)
	}
}

func errString(s string) error { return &staticErr{s} }

type staticErr struct{ s string }

func (e *staticErr) Error() string { return e.s }
