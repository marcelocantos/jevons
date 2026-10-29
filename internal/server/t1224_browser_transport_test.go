// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/ui"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// Browser drives the real mux and HTTP handlers against the tracked embedded bundle.
// Fixture observations enter the real replica, sqldeep subscription and transport.
// The separate controlplane tests cover pollers and replication; no auth is read here.
func TestT1224BrowserTransport(t *testing.T) {
	if os.Getenv("JEVONS_T1224_BROWSER") != "1" {
		t.Skip("opt-in Playwright transport isolate")
	}
	s := New("test", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	replica, err := controlplane.OpenReplica(filepath.Join(dir, "replica.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = replica.Close() }()
	if _, err := planusage.OpenReplicaOverrides(replica, dir); err != nil {
		t.Fatal(err)
	}
	projection, err := planusage.SubscribeProjection(ctx, replica, s.FanPlanUsage)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPlanUsageSource(func() any { return projection.JSON() })
	var phase atomic.Int32
	observed := time.Now().UTC().Add(-10 * time.Minute)
	publish := func(current int32) error {
		remaining := 41
		if current == 2 {
			remaining = 33
		}
		if current == 3 {
			remaining = 24
		}
		status, reason := "available", ""
		if current == 1 {
			status, reason = "unavailable", "meter 429"
		}
		reading, err := json.Marshal(map[string]any{"windows": []any{map[string]any{
			"name": "weekly", "remaining_percent": remaining, "used_percent": 100 - remaining,
			"resets_at": time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339), "limit_window": int64(7 * 24 * time.Hour), "band": "ok",
		}}})
		if err != nil {
			return err
		}
		success := observed
		if current > 1 {
			success = time.Now().UTC()
		}
		err = replica.Write(func(db *sqlpipe.Database) error {
			return db.Exec(`INSERT INTO claudia_plan_usage
                (id, provider, status, reason, attempted_at, success_at, next_attempt_at, refusals, reading_json)
                VALUES (1, 'claude', ?, ?, ?, ?, ?, 0, ?)
                ON CONFLICT(id) DO UPDATE SET status=excluded.status, reason=excluded.reason,
                attempted_at=excluded.attempted_at, success_at=excluded.success_at,
                next_attempt_at=excluded.next_attempt_at, reading_json=excluded.reading_json`,
				status, reason, time.Now().UTC().Format(time.RFC3339), success.Format(time.RFC3339),
				time.Now().UTC().Add(time.Minute).Format(time.RFC3339), string(reading))
		})
		if err == nil {
			phase.Store(current)
		}
		return err
	}
	if err := publish(0); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	files, err := ui.Files()
	if err != nil {
		t.Fatal(err)
	}
	if err := RegisterReactUIRoutes(mux, files); err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/fixture/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := publish(1); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]int32{"phase": phase.Load()})
	})
	for path, value := range map[string]int32{"/fixture/reconnect-value": 3, "/fixture/release": 2} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST required", 405)
				return
			}
			if err := publish(value); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	script, err := filepath.Abs("../../scripts/react-ui-test/t1224-real-transport-test.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Env = append(os.Environ(), "T1224_URL="+srv.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser transport: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
